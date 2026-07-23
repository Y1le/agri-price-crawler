// Package bootstrap assembles the dependencies for the new platform processes.
package bootstrap

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"reflect"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/gateway"
	"github.com/Y1le/agri-price-crawler/internal/identity"
	"github.com/Y1le/agri-price-crawler/internal/identity/httpapi"
	identitypostgres "github.com/Y1le/agri-price-crawler/internal/identity/postgres"
	"github.com/Y1le/agri-price-crawler/internal/identity/redisotp"
	identitysmtp "github.com/Y1le/agri-price-crawler/internal/identity/smtp"
	"github.com/Y1le/agri-price-crawler/internal/identity/wechat"
	"github.com/Y1le/agri-price-crawler/internal/platform/config"
	"github.com/Y1le/agri-price-crawler/internal/platform/httpx"
	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
	platformpg "github.com/Y1le/agri-price-crawler/internal/platform/postgres"
	"github.com/Y1le/agri-price-crawler/internal/platform/rediscache"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
)

type gatewayOverrides struct {
	Email  identity.EmailSender
	WeChat identity.WeChatExchanger
	Clock  identity.Clock
	Random io.Reader
	// OTPPrefix is test-only dependency isolation. Production assembly leaves
	// it empty and uses the stable agri:v2:identity namespace.
	OTPPrefix string
}

// RunGateway assembles and runs the HTTP gateway until ctx is cancelled.
func RunGateway(ctx context.Context, cfg config.Config, logger *slog.Logger) error {
	if err := cfg.ValidateGateway(); err != nil {
		return fmt.Errorf("validate Gateway configuration: %w", err)
	}

	pool, err := openPostgres(ctx, cfg.Postgres.URL)
	if err != nil {
		return err
	}
	defer pool.Close()

	if err := requireSchemaVersion(ctx, pool); err != nil {
		return err
	}

	redisClient := rediscache.Open(cfg.Redis)
	defer redisClient.Close()
	if err := redisClient.Ping(ctx).Err(); err != nil && logger != nil {
		logger.Warn("Redis ping failed; continuing without Redis readiness", "error", err)
	}

	handler, err := buildGatewayHandler(pool, redisClient, cfg, logger, gatewayOverrides{})
	if err != nil {
		return err
	}
	if err := serveGatewayHandler(ctx, cfg.Gateway.Addr, handler); err != nil {
		return fmt.Errorf("run Gateway: %w", err)
	}
	return nil
}

func buildGatewayHandler(
	pool *pgxpool.Pool,
	redisClient redis.UniversalClient,
	cfg config.Config,
	logger *slog.Logger,
	overrides gatewayOverrides,
) (http.Handler, error) {
	if (&http.Cookie{Name: cfg.Identity.Web.CookieName, Value: "token"}).String() == "" {
		return nil, fmt.Errorf("construct Identity cookie policy: cookie name is invalid")
	}
	trustedProxies, err := httpx.ParseTrustedProxies(cfg.Identity.Web.TrustedProxies)
	if err != nil {
		return nil, fmt.Errorf("construct Identity trusted-proxy policy: %w", err)
	}
	corsMiddleware, err := exactOriginMiddleware(cfg.Identity.Web.AllowedOrigins)
	if err != nil {
		return nil, fmt.Errorf("construct Gateway CORS policy: %w", err)
	}

	emailSender := overrides.Email
	if emailSender == nil {
		switch cfg.Identity.EmailDriver {
		case "memory":
			emailSender = identitysmtp.NewMemory()
		case "smtp":
			sender, err := identitysmtp.New(cfg.Identity.SMTP)
			if err != nil {
				return nil, fmt.Errorf("construct Identity SMTP sender: %w", err)
			}
			emailSender = sender
		default:
			return nil, fmt.Errorf("construct Identity email sender: unsupported driver")
		}
	}

	weChatExchanger := overrides.WeChat
	if weChatExchanger == nil {
		client, err := wechat.New(cfg.Identity.WeChat)
		if err != nil {
			return nil, fmt.Errorf("construct Identity WeChat client: %w", err)
		}
		weChatExchanger = client
	}

	tokenManager, err := identity.NewTokenManager(identity.TokenConfig{
		PrivateKey: cfg.Identity.JWT.PrivateKey,
		KeyID:      cfg.Identity.JWT.KeyID,
		Issuer:     cfg.Identity.JWT.Issuer,
		Audience:   cfg.Identity.JWT.Audience,
		AccessTTL:  cfg.Identity.JWT.AccessTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("construct Identity token manager: %w", err)
	}

	if pool == nil {
		return nil, fmt.Errorf("construct Identity PostgreSQL repository: pool is required")
	}
	if nilRedisClient(redisClient) {
		return nil, fmt.Errorf("construct Identity Redis OTP store: client is required")
	}
	repository := identitypostgres.NewRepository(pool)
	otpPrefix := overrides.OTPPrefix
	if otpPrefix == "" {
		otpPrefix = "agri:v2:identity"
	}
	otpStore := redisotp.New(redisClient, otpPrefix)
	enabledClients := make([]identity.ClientKind, len(cfg.Identity.EnabledClients))
	for index, client := range cfg.Identity.EnabledClients {
		enabledClients[index] = identity.ClientKind(client)
	}
	service, err := identity.NewService(identity.Dependencies{
		Repository:      repository,
		OTPStore:        otpStore,
		EmailSender:     emailSender,
		WeChatExchanger: weChatExchanger,
		TokenManager:    tokenManager,
		Clock:           overrides.Clock,
		Random:          overrides.Random,
	}, identity.Policy{
		OTPPepper:       cfg.Identity.OTP.Pepper,
		OTPTTL:          cfg.Identity.OTP.TTL,
		OTPAttempts:     cfg.Identity.OTP.Attempts,
		OTPCooldown:     cfg.Identity.OTP.Cooldown,
		OTPEmailPerHour: cfg.Identity.OTP.EmailPerHour,
		OTPIPPerHour:    cfg.Identity.OTP.IPPerHour,
		WeChatIPPerHour: cfg.Identity.WeChat.IPPerHour,
		RefreshTTL:      cfg.Identity.RefreshTTL,
		ReuseGrace:      cfg.Identity.ReuseGrace,
		EnabledClients:  enabledClients,
	})
	if err != nil {
		return nil, fmt.Errorf("construct Identity service: %w", err)
	}

	api := httpapi.New(httpapi.Config{
		Service:     service,
		TokenParser: tokenManager,
		Cookie: httpapi.CookiePolicy{
			Name:           cfg.Identity.Web.CookieName,
			Secure:         cfg.Identity.Web.CookieSecure,
			AllowedOrigins: cfg.Identity.Web.AllowedOrigins,
		},
		TrustedProxies: trustedProxies,
		Clock:          overrides.Clock,
		Logger:         logger,
	})

	return gateway.New(gateway.Config{
		Ready:  pool.Ping,
		Logger: logger,
		API:    api,
		Middleware: func(next http.Handler) http.Handler {
			return httpx.WithRequestID(corsMiddleware(next))
		},
	}).Handler(), nil
}

func nilRedisClient(client redis.UniversalClient) bool {
	if client == nil {
		return true
	}
	value := reflect.ValueOf(client)
	switch value.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return value.IsNil()
	default:
		return false
	}
}

func exactOriginMiddleware(allowedOrigins []string) (func(http.Handler) http.Handler, error) {
	if _, err := httpx.CORS(allowedOrigins, http.NotFoundHandler()); err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		handler, err := httpx.CORS(allowedOrigins, next)
		if err != nil {
			panic("validated Gateway CORS policy became invalid")
		}
		return handler
	}, nil
}

func serveGatewayHandler(ctx context.Context, addr string, handler http.Handler) error {
	server := &http.Server{
		Addr:              addr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}
	listener, err := net.Listen("tcp", addr)
	if err != nil {
		return err
	}
	serveErr := make(chan error, 1)
	go func() {
		serveErr <- server.Serve(listener)
	}()

	select {
	case err := <-serveErr:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		return err
	}
	if err := <-serveErr; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}

func openPostgres(ctx context.Context, url string) (*pgxpool.Pool, error) {
	pool, err := platformpg.Open(ctx, url)
	if err != nil {
		return nil, fmt.Errorf("open PostgreSQL: %w", err)
	}
	return pool, nil
}

func requireSchemaVersion(ctx context.Context, pool *pgxpool.Pool) error {
	version, err := migrate.CurrentVersion(ctx, pool)
	if err != nil {
		return fmt.Errorf("check PostgreSQL schema version: %w", err)
	}
	requiredVersion, err := requiredSchemaVersion()
	if err != nil {
		return fmt.Errorf("determine required PostgreSQL schema version: %w", err)
	}
	if version != requiredVersion {
		return fmt.Errorf("PostgreSQL schema version is %d, want %d", version, requiredVersion)
	}
	return nil
}
