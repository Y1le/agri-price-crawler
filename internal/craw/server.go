package craw

import (
	"context"

	"github.com/Y1le/agri-price-crawler/internal/craw/config"
	emailer "github.com/Y1le/agri-price-crawler/internal/craw/emailer"
	"github.com/Y1le/agri-price-crawler/internal/craw/store"
	"github.com/Y1le/agri-price-crawler/internal/craw/store/mysql"
	genericoptions "github.com/Y1le/agri-price-crawler/internal/pkg/options"
	genericcrawserver "github.com/Y1le/agri-price-crawler/internal/pkg/server"
	"github.com/Y1le/agri-price-crawler/pkg/log"
	shutdown "github.com/Y1le/agri-price-crawler/pkg/shutdown"
	"github.com/Y1le/agri-price-crawler/pkg/shutdown/shutdownmanagers/posixsignal"
	"github.com/Y1le/agri-price-crawler/pkg/storage"
)

type crawServer struct {
	gs                *shutdown.GracefulShutdown
	mysqlOptions      *genericoptions.MySQLOptions
	redisOptions      *genericoptions.RedisOptions
	cronOptions       *genericoptions.CronOptions
	crawlerOptions    *genericoptions.CrawlerOptions
	genericCrawServer *genericcrawserver.GenericCrawServer

	emailOptions *genericoptions.EmailOptions
}

type preparedCrawServer struct {
	*crawServer
}

// ExtraConfig defines extra configuration for the craw-server.
type ExtraConfig struct {
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
	err = extraConfig.complete().New()
	if err != nil {
		return nil, err
	}
	server := &crawServer{
		gs:                gs,
		mysqlOptions:      cfg.MySQLOptions,
		redisOptions:      cfg.RedisOptions,
		cronOptions:       cfg.CronOptions,
		crawlerOptions:    cfg.CrawlerOptions,
		genericCrawServer: genericServer,
		emailOptions:      cfg.EmailOptions,
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
	s.initCronTask()
	s.initEmailer()
	s.gs.AddShutdownCallback(shutdown.ShutdownFunc(func(string) error {
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

func (s *crawServer) initEmailer() {
	emailer.Instance = &emailer.SMTPMailer{
		Host:     s.emailOptions.Username,
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
	return &completedExtraConfig{c}
}

// New create a grpcAPIServer instance.
func (c *completedExtraConfig) New() error {

	storeIns, _ := mysql.GetMySQLFactoryOr(c.mysqlOptions)
	// storeIns, _ := etcd.GetEtcdFactoryOr(c.etcdOptions, nil)
	store.SetClient(storeIns)

	return nil
}
