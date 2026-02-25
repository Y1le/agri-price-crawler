package craw

import (
	"context"
	"strconv"

	"github.com/Y1le/agri-price-crawler/internal/ai"
	"github.com/Y1le/agri-price-crawler/internal/ai/doubao"
	"github.com/Y1le/agri-price-crawler/internal/craw/config"
	"github.com/Y1le/agri-price-crawler/internal/craw/gapi"
	mailer "github.com/Y1le/agri-price-crawler/internal/craw/mailer"
	"github.com/Y1le/agri-price-crawler/internal/craw/store"
	"github.com/Y1le/agri-price-crawler/internal/craw/store/mysql"
	genericoptions "github.com/Y1le/agri-price-crawler/internal/pkg/options"
	genericcrawserver "github.com/Y1le/agri-price-crawler/internal/pkg/server"
	"github.com/Y1le/agri-price-crawler/pb"
	"github.com/Y1le/agri-price-crawler/pkg/log"
	shutdown "github.com/Y1le/agri-price-crawler/pkg/shutdown"
	"github.com/Y1le/agri-price-crawler/pkg/shutdown/shutdownmanagers/posixsignal"
	"github.com/Y1le/agri-price-crawler/pkg/storage"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials"
	"google.golang.org/grpc/reflection"
)

type crawServer struct {
	gs                *shutdown.GracefulShutdown
	mysqlOptions      *genericoptions.MySQLOptions
	redisOptions      *genericoptions.RedisOptions
	cronOptions       *genericoptions.CronOptions
	crawlerOptions    *genericoptions.CrawlerOptions
	gRPCServer        *grpcServer
	genericCrawServer *genericcrawserver.GenericCrawServer

	emailOptions  *genericoptions.EmailOptions
	doubaoOptions *genericoptions.DoubaoOptions
}

type preparedCrawServer struct {
	*crawServer
}

// ExtraConfig defines extra configuration for the craw-server.
type ExtraConfig struct {
	Addr         string
	MaxMsgSize   int
	ServerCert   genericoptions.GeneratableKeyCert
	mysqlOptions *genericoptions.MySQLOptions
	// etcdOptions      *genericoptions.EtcdOptions
}

func createCRAWServer(cfg *config.Config) (*crawServer, error) {
	gs := shutdown.New()
	gs.AddShutdownManager(posixsignal.NewPosixSignalManager())

	genericConfig, err := buildGenericConfig(cfg)
	if err != nil {
		return nil, err
	}

	extraConfig, err := buildExtraConfig(cfg)
	if err != nil {
		return nil, err
	}

	genericServer, err := genericConfig.Complete().New()
	if err != nil {
		return nil, err
	}
	extraServer, err := extraConfig.complete().New()
	if err != nil {
		return nil, err
	}
	server := &crawServer{
		gs:                gs,
		mysqlOptions:      cfg.MySQLOptions,
		redisOptions:      cfg.RedisOptions,
		cronOptions:       cfg.CronOptions,
		crawlerOptions:    cfg.CrawlerOptions,
		gRPCServer:        extraServer,
		genericCrawServer: genericServer,
		emailOptions:      cfg.EmailOptions,
		doubaoOptions:     cfg.DoubaoOptions,
	}

	return server, nil
}

func (s *crawServer) PrepareRun() preparedCrawServer {
	initRouter(s.genericCrawServer.Engine)

	storeIns, err := mysql.GetMySQLFactoryOr(s.mysqlOptions)
	if err != nil {
		log.Fatalf("failed to initialize MySQL store: %v", err)
	}
	store.SetClient(storeIns)

	s.initRedisStore()
	s.initmailer()
	// init doubao recipe client
	log.Debugf("doubao options: %v", s.doubaoOptions)
	doubaoIns, err := doubao.GetDoubaoFactoryOr(s.doubaoOptions)
	ai.SetClient(doubaoIns)
	if err != nil {
		log.Fatalf("failed to initialize Doubao factory: %v", err)
	}
	s.initCronTask()

	s.gRPCServer.Run()

	s.gs.AddShutdownCallback(shutdown.ShutdownFunc(func(string) error {

		s.gRPCServer.Close()

		mysqlStore, _ := mysql.GetMySQLFactoryOr(nil)
		if mysqlStore != nil {
			_ = mysqlStore.Close()
		}

		s.genericCrawServer.Close()

		return nil
	}))

	return preparedCrawServer{s}
}

func (s preparedCrawServer) Run() error {
	// start shutdown managers
	if err := s.gs.Start(); err != nil {
		log.Fatalf("start shutdown manager failed: %s", err.Error())
	}

	return s.genericCrawServer.Run()
}

func buildGenericConfig(cfg *config.Config) (genericConfig *genericcrawserver.Config, lastErr error) {
	genericConfig = genericcrawserver.NewConfig()
	if lastErr = cfg.GenericServerRunOptions.ApplyTo(genericConfig); lastErr != nil {
		return
	}

	if lastErr = cfg.FeatureOptions.ApplyTo(genericConfig); lastErr != nil {
		return
	}

	if lastErr = cfg.SecureServing.ApplyTo(genericConfig); lastErr != nil {
		return
	}

	if lastErr = cfg.InsecureServing.ApplyTo(genericConfig); lastErr != nil {
		return
	}

	return
}

func buildExtraConfig(cfg *config.Config) (*ExtraConfig, error) {
	return &ExtraConfig{
		Addr:         cfg.GRPCOptions.BindAddress + ":" + strconv.Itoa(cfg.GRPCOptions.BindPort), // 从配置中读取 gRPC 地址
		MaxMsgSize:   cfg.GRPCOptions.MaxMsgSize,                                                 // 从配置中读取最大消息大小
		ServerCert:   cfg.SecureServing.ServerCert,
		mysqlOptions: cfg.MySQLOptions,
		// etcdOptions:      cfg.EtcdOptions,
	}, nil
}

func (s *crawServer) initRedisStore() {
	ctx, cancel := context.WithCancel(context.Background())
	s.gs.AddShutdownCallback(shutdown.ShutdownFunc(func(string) error {
		cancel()

		return nil
	}))

	config := &storage.Config{
		Host:                  s.redisOptions.Host,
		Port:                  s.redisOptions.Port,
		Addrs:                 s.redisOptions.Addrs,
		MasterName:            s.redisOptions.MasterName,
		Username:              s.redisOptions.Username,
		Password:              s.redisOptions.Password,
		Database:              s.redisOptions.Database,
		MaxIdle:               s.redisOptions.MaxIdle,
		MaxActive:             s.redisOptions.MaxActive,
		Timeout:               s.redisOptions.Timeout,
		EnableCluster:         s.redisOptions.EnableCluster,
		UseSSL:                s.redisOptions.UseSSL,
		SSLInsecureSkipVerify: s.redisOptions.SSLInsecureSkipVerify,
	}

	// try to connect to redis
	go storage.ConnectToRedis(ctx, config)
}

func (s *crawServer) initmailer() {
	mailer.Instance = &mailer.SMTPMailer{
		Host:     s.emailOptions.Host,
		Port:     s.emailOptions.Port,
		Username: s.emailOptions.Username,
		Password: s.emailOptions.Password,
		From:     s.emailOptions.From,
	}

}

type completedExtraConfig struct {
	*ExtraConfig
}

// Complete fills in any fields not set that are required to have valid data and can be derived from other fields.
func (c *ExtraConfig) complete() *completedExtraConfig {
	if c.Addr == "" {
		c.Addr = "127.0.0.1:8081"
	}
	if c.MaxMsgSize == 0 {
		c.MaxMsgSize = 1024 * 1024 * 4 // 默认 4MB
	}
	return &completedExtraConfig{c}
}

// New create a grpcAPIServer instance.
func (c *completedExtraConfig) New() (*grpcServer, error) {
	creds, err := credentials.NewServerTLSFromFile(c.ServerCert.CertKey.CertFile, c.ServerCert.CertKey.KeyFile)
	if err != nil {
		log.Fatalf("Failed to generate credentials %s", err.Error())
	}
	opts := []grpc.ServerOption{grpc.MaxRecvMsgSize(c.MaxMsgSize), grpc.Creds(creds)}
	newGrpcServer := grpc.NewServer(opts...)

	storeIns, _ := mysql.GetMySQLFactoryOr(c.mysqlOptions)
	store.SetClient(storeIns)
	crawService := gapi.NewCrawService(storeIns)
	if err != nil {
		log.Fatalf("Failed to get cache instance: %s", err.Error())
	}

	pb.RegisterCrawServiceServer(newGrpcServer, crawService)

	reflection.Register(newGrpcServer)

	return &grpcServer{newGrpcServer, c.Addr}, nil
}
