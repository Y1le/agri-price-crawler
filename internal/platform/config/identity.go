package config

import (
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	maxAccessTTL       = time.Hour
	maxRefreshTTL      = 90 * 24 * time.Hour
	maxReuseGrace      = time.Minute
	maxOTPTTL          = 15 * time.Minute
	maxOTPAttempts     = 10
	maxOTPCooldown     = time.Hour
	maxHourlyLimit     = 100_000
	maxProviderTimeout = 30 * time.Second
)

type Identity struct {
	JWT            IdentityJWT
	OTP            IdentityOTP
	RefreshTTL     time.Duration
	ReuseGrace     time.Duration
	WeChat         WeChat
	SMTP           SMTP
	EmailDriver    string
	EnabledClients []string
	Web            WebSecurity
}

type IdentityJWT struct {
	PrivateKey ed25519.PrivateKey
	KeyID      string
	Issuer     string
	Audience   string
	AccessTTL  time.Duration
}

type IdentityOTP struct {
	Pepper       []byte
	TTL          time.Duration
	Attempts     int
	Cooldown     time.Duration
	EmailPerHour int
	IPPerHour    int
}

type WeChat struct {
	AppID     string
	AppSecret string
	BaseURL   string
	Timeout   time.Duration
	IPPerHour int
}

type SMTP struct {
	Host     string
	Port     int
	Username string
	Password string
	From     string
	TLSMode  string
	Timeout  time.Duration
}

type WebSecurity struct {
	CookieName     string
	CookieSecure   bool
	AllowedOrigins []string
	TrustedProxies []string
}

func loadIdentity(environment string) (Identity, error) {
	privateKey, err := identityPrivateKeyEnv()
	if err != nil {
		return Identity{}, err
	}
	pepper, err := base64SecretEnv("IDENTITY_OTP_PEPPER_BASE64")
	if err != nil {
		return Identity{}, err
	}

	accessTTL, err := durationEnv("IDENTITY_JWT_ACCESS_TTL", 15*time.Minute)
	if err != nil {
		return Identity{}, err
	}
	refreshTTL, err := durationEnv("IDENTITY_REFRESH_TTL", 720*time.Hour)
	if err != nil {
		return Identity{}, err
	}
	reuseGrace, err := durationEnv("IDENTITY_REFRESH_REUSE_GRACE", 10*time.Second)
	if err != nil {
		return Identity{}, err
	}
	otpTTL, err := durationEnv("IDENTITY_OTP_TTL", 10*time.Minute)
	if err != nil {
		return Identity{}, err
	}
	otpAttempts, err := intEnv("IDENTITY_OTP_ATTEMPTS", 5)
	if err != nil {
		return Identity{}, err
	}
	otpCooldown, err := durationEnv("IDENTITY_OTP_COOLDOWN", time.Minute)
	if err != nil {
		return Identity{}, err
	}
	otpEmailPerHour, err := intEnv("IDENTITY_OTP_EMAIL_PER_HOUR", 5)
	if err != nil {
		return Identity{}, err
	}
	otpIPPerHour, err := intEnv("IDENTITY_OTP_IP_PER_HOUR", 30)
	if err != nil {
		return Identity{}, err
	}
	wechatTimeout, err := durationEnv("IDENTITY_WECHAT_TIMEOUT", 5*time.Second)
	if err != nil {
		return Identity{}, err
	}
	wechatIPPerHour, err := intEnv("IDENTITY_WECHAT_IP_PER_HOUR", 60)
	if err != nil {
		return Identity{}, err
	}
	smtpPort, err := intEnv("IDENTITY_SMTP_PORT", 0)
	if err != nil {
		return Identity{}, err
	}
	smtpTimeout, err := durationEnv("IDENTITY_SMTP_TIMEOUT", 5*time.Second)
	if err != nil {
		return Identity{}, err
	}
	cookieSecure, err := boolEnv("IDENTITY_COOKIE_SECURE", environment == "production")
	if err != nil {
		return Identity{}, err
	}

	cookieName := "agri_refresh"
	if environment == "production" {
		cookieName = "__Secure-agri_refresh"
	}
	enabledClientsValue, enabledClientsSet := os.LookupEnv("IDENTITY_ENABLED_CLIENTS")
	enabledClients := stringList(enabledClientsValue)
	if !enabledClientsSet {
		enabledClients = []string{"web", "wechat_mini"}
	}

	return Identity{
		JWT: IdentityJWT{
			PrivateKey: privateKey,
			KeyID:      stringEnv("IDENTITY_JWT_KEY_ID", "identity-v1"),
			Issuer:     stringEnv("IDENTITY_JWT_ISSUER", "agri-price-crawler"),
			Audience:   stringEnv("IDENTITY_JWT_AUDIENCE", "agri-clients"),
			AccessTTL:  accessTTL,
		},
		OTP: IdentityOTP{
			Pepper:       pepper,
			TTL:          otpTTL,
			Attempts:     otpAttempts,
			Cooldown:     otpCooldown,
			EmailPerHour: otpEmailPerHour,
			IPPerHour:    otpIPPerHour,
		},
		RefreshTTL: refreshTTL,
		ReuseGrace: reuseGrace,
		WeChat: WeChat{
			AppID:     os.Getenv("IDENTITY_WECHAT_APP_ID"),
			AppSecret: os.Getenv("IDENTITY_WECHAT_APP_SECRET"),
			BaseURL:   stringEnv("IDENTITY_WECHAT_BASE_URL", "https://api.weixin.qq.com"),
			Timeout:   wechatTimeout,
			IPPerHour: wechatIPPerHour,
		},
		SMTP: SMTP{
			Host:     os.Getenv("IDENTITY_SMTP_HOST"),
			Port:     smtpPort,
			Username: os.Getenv("IDENTITY_SMTP_USERNAME"),
			Password: os.Getenv("IDENTITY_SMTP_PASSWORD"),
			From:     os.Getenv("IDENTITY_SMTP_FROM"),
			TLSMode:  stringEnv("IDENTITY_SMTP_TLS_MODE", "starttls"),
			Timeout:  smtpTimeout,
		},
		EmailDriver:    os.Getenv("IDENTITY_EMAIL_DRIVER"),
		EnabledClients: enabledClients,
		Web: WebSecurity{
			CookieName:     stringEnv("IDENTITY_COOKIE_NAME", cookieName),
			CookieSecure:   cookieSecure,
			AllowedOrigins: stringListEnv("IDENTITY_WEB_ALLOWED_ORIGINS"),
			TrustedProxies: stringListEnv("IDENTITY_TRUSTED_PROXIES"),
		},
	}, nil
}

// ValidateGateway validates settings required only by the public Gateway process.
// Worker and migration commands intentionally do not call this method.
func (c Config) ValidateGateway() error {
	identity := c.Identity

	if c.Environment != "development" && c.Environment != "production" {
		return fmt.Errorf("APP_ENV must be development or production")
	}
	if len(identity.JWT.PrivateKey) != ed25519.PrivateKeySize {
		return fmt.Errorf("IDENTITY_JWT_PRIVATE_KEY_BASE64 must contain exactly %d seed bytes", ed25519.SeedSize)
	}
	if identity.JWT.KeyID == "" {
		return fmt.Errorf("IDENTITY_JWT_KEY_ID is required")
	}
	if identity.JWT.Issuer == "" {
		return fmt.Errorf("IDENTITY_JWT_ISSUER is required")
	}
	if identity.JWT.Audience == "" {
		return fmt.Errorf("IDENTITY_JWT_AUDIENCE is required")
	}
	if identity.JWT.AccessTTL < time.Second ||
		identity.JWT.AccessTTL%time.Second != 0 ||
		identity.JWT.AccessTTL > maxAccessTTL {
		return fmt.Errorf("IDENTITY_JWT_ACCESS_TTL must be whole seconds between 1s and 1h")
	}
	if identity.RefreshTTL <= 0 || identity.RefreshTTL > maxRefreshTTL {
		return fmt.Errorf("IDENTITY_REFRESH_TTL must be positive and at most 2160h")
	}
	if identity.ReuseGrace < 0 {
		return fmt.Errorf("IDENTITY_REFRESH_REUSE_GRACE must not be negative")
	}
	if identity.ReuseGrace > maxReuseGrace {
		return fmt.Errorf("IDENTITY_REFRESH_REUSE_GRACE must be at most 1m")
	}
	if identity.ReuseGrace >= identity.RefreshTTL {
		return fmt.Errorf("IDENTITY_REFRESH_REUSE_GRACE must be shorter than IDENTITY_REFRESH_TTL")
	}

	if len(identity.OTP.Pepper) < 32 {
		return fmt.Errorf("IDENTITY_OTP_PEPPER_BASE64 must contain at least 32 bytes")
	}
	if identity.OTP.TTL <= 0 || identity.OTP.TTL > maxOTPTTL {
		return fmt.Errorf("IDENTITY_OTP_TTL must be positive and at most 15m")
	}
	if identity.OTP.Attempts <= 0 || identity.OTP.Attempts > maxOTPAttempts {
		return fmt.Errorf("IDENTITY_OTP_ATTEMPTS must be between 1 and 10")
	}
	if identity.OTP.Cooldown <= 0 || identity.OTP.Cooldown > maxOTPCooldown {
		return fmt.Errorf("IDENTITY_OTP_COOLDOWN must be positive and at most 1h")
	}
	if identity.OTP.EmailPerHour <= 0 || identity.OTP.EmailPerHour > maxHourlyLimit {
		return fmt.Errorf("IDENTITY_OTP_EMAIL_PER_HOUR must be between 1 and 100000")
	}
	if identity.OTP.IPPerHour <= 0 || identity.OTP.IPPerHour > maxHourlyLimit {
		return fmt.Errorf("IDENTITY_OTP_IP_PER_HOUR must be between 1 and 100000")
	}

	if identity.WeChat.AppID == "" {
		return fmt.Errorf("IDENTITY_WECHAT_APP_ID is required")
	}
	if identity.WeChat.AppSecret == "" {
		return fmt.Errorf("IDENTITY_WECHAT_APP_SECRET is required")
	}
	if err := validateHTTPBaseURL("IDENTITY_WECHAT_BASE_URL", identity.WeChat.BaseURL, c.Environment == "production"); err != nil {
		return err
	}
	if identity.WeChat.Timeout <= 0 || identity.WeChat.Timeout > maxProviderTimeout {
		return fmt.Errorf("IDENTITY_WECHAT_TIMEOUT must be positive and at most 30s")
	}
	if identity.WeChat.IPPerHour <= 0 || identity.WeChat.IPPerHour > maxHourlyLimit {
		return fmt.Errorf("IDENTITY_WECHAT_IP_PER_HOUR must be between 1 and 100000")
	}
	if identity.SMTP.Timeout <= 0 || identity.SMTP.Timeout > maxProviderTimeout {
		return fmt.Errorf("IDENTITY_SMTP_TIMEOUT must be positive and at most 30s")
	}

	switch identity.EmailDriver {
	case "memory":
		if c.Environment == "production" {
			return fmt.Errorf("IDENTITY_EMAIL_DRIVER memory is not allowed in production")
		}
	case "smtp":
		if err := validateSMTP(identity.SMTP, c.Environment); err != nil {
			return err
		}
	default:
		return fmt.Errorf("IDENTITY_EMAIL_DRIVER must be memory or smtp")
	}

	if len(identity.EnabledClients) == 0 {
		return fmt.Errorf("IDENTITY_ENABLED_CLIENTS must enable web or wechat_mini")
	}
	enabledClients := make(map[string]struct{}, len(identity.EnabledClients))
	for _, client := range identity.EnabledClients {
		if client != "web" && client != "wechat_mini" {
			return fmt.Errorf("IDENTITY_ENABLED_CLIENTS contains unsupported client %q", client)
		}
		if _, exists := enabledClients[client]; exists {
			return fmt.Errorf("IDENTITY_ENABLED_CLIENTS contains duplicate client %q", client)
		}
		enabledClients[client] = struct{}{}
	}

	if (&http.Cookie{Name: identity.Web.CookieName, Value: "token"}).String() == "" {
		return fmt.Errorf("IDENTITY_COOKIE_NAME is invalid")
	}
	if c.Environment == "production" {
		if !identity.Web.CookieSecure {
			return fmt.Errorf("IDENTITY_COOKIE_SECURE must be true in production")
		}
		if !strings.HasPrefix(identity.Web.CookieName, "__Secure-") {
			return fmt.Errorf("IDENTITY_COOKIE_NAME must use the __Secure- prefix in production")
		}
		if len(identity.Web.AllowedOrigins) == 0 {
			return fmt.Errorf("IDENTITY_WEB_ALLOWED_ORIGINS is required in production")
		}
	}
	for _, origin := range identity.Web.AllowedOrigins {
		if err := validateOrigin(origin, c.Environment == "production"); err != nil {
			return fmt.Errorf("IDENTITY_WEB_ALLOWED_ORIGINS: %w", err)
		}
	}

	return nil
}

func identityPrivateKeyEnv() (ed25519.PrivateKey, error) {
	seed, err := base64SecretEnv("IDENTITY_JWT_PRIVATE_KEY_BASE64")
	if err != nil || len(seed) == 0 {
		return nil, err
	}
	if len(seed) != ed25519.SeedSize {
		clear(seed)
		return nil, fmt.Errorf("IDENTITY_JWT_PRIVATE_KEY_BASE64 must contain exactly %d seed bytes", ed25519.SeedSize)
	}
	return identityPrivateKeyFromSeed(seed), nil
}

func identityPrivateKeyFromSeed(seed []byte) ed25519.PrivateKey {
	defer clear(seed)
	return ed25519.NewKeyFromSeed(seed)
}

func base64SecretEnv(name string) ([]byte, error) {
	value := os.Getenv(name)
	if value == "" {
		return nil, nil
	}
	decoded, err := base64.StdEncoding.DecodeString(value)
	if err != nil {
		return nil, fmt.Errorf("%s is not valid base64", name)
	}
	return decoded, nil
}

func stringListEnv(name string) []string {
	return stringList(os.Getenv(name))
}

func stringList(raw string) []string {
	values := strings.Split(raw, ",")
	result := make([]string, 0, len(values))
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			result = append(result, trimmed)
		}
	}
	return result
}

func validateSMTP(smtp SMTP, environment string) error {
	if smtp.Host == "" {
		return fmt.Errorf("IDENTITY_SMTP_HOST is required for smtp")
	}
	if smtp.Port < 1 || smtp.Port > 65535 {
		return fmt.Errorf("IDENTITY_SMTP_PORT must be between 1 and 65535")
	}
	if smtp.Username == "" {
		return fmt.Errorf("IDENTITY_SMTP_USERNAME is required for smtp")
	}
	if smtp.Password == "" {
		return fmt.Errorf("IDENTITY_SMTP_PASSWORD is required for smtp")
	}
	if smtp.From == "" {
		return fmt.Errorf("IDENTITY_SMTP_FROM is required for smtp")
	}
	switch smtp.TLSMode {
	case "implicit", "starttls":
	case "none":
		if environment == "production" {
			return fmt.Errorf("IDENTITY_SMTP_TLS_MODE none is not allowed in production")
		}
	default:
		return fmt.Errorf("IDENTITY_SMTP_TLS_MODE must be implicit, starttls, or none")
	}
	return nil
}

func validateHTTPBaseURL(name, value string, requireHTTPS bool) error {
	parsed, err := url.Parse(value)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("%s must be an absolute HTTP(S) URL", name)
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("%s must use https in production", name)
	}
	if parsed.User != nil || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("%s must not contain credentials, query, or fragment", name)
	}
	return nil
}

func validateOrigin(origin string, requireHTTPS bool) error {
	if origin == "*" {
		return fmt.Errorf("wildcard origin is not allowed")
	}
	parsed, err := url.Parse(origin)
	if err != nil || (parsed.Scheme != "http" && parsed.Scheme != "https") || parsed.Host == "" {
		return fmt.Errorf("value must be an absolute HTTP(S) origin")
	}
	if requireHTTPS && parsed.Scheme != "https" {
		return fmt.Errorf("origin must use https in production")
	}
	if parsed.User != nil || parsed.Path != "" || parsed.RawQuery != "" || parsed.Fragment != "" {
		return fmt.Errorf("value must be an exact origin")
	}
	return nil
}
