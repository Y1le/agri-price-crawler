package craw

import (
	"context"
	"time"

	craw "github.com/Y1le/agri-price-crawler/internal/craw/crawler"
	task "github.com/Y1le/agri-price-crawler/internal/craw/cron"
	"github.com/Y1le/agri-price-crawler/internal/craw/store/mysql"
	"github.com/Y1le/agri-price-crawler/pkg/log"
	"github.com/Y1le/agri-price-crawler/pkg/log/cronlog"
	shutdown "github.com/Y1le/agri-price-crawler/pkg/shutdown"
	"github.com/robfig/cron/v3"
	"go.uber.org/zap"
)

func (s *crawServer) initCronTask() {
	ctx, cancel := context.WithCancel(context.Background())

	s.gs.AddShutdownCallback(shutdown.ShutdownFunc(func(string) error {
		cancel()
		return nil
	}))
	if s.crawlerOptions.DeviceID == "" || s.crawlerOptions.Secret == "" {
		log.Fatal("DeviceID or Secret is empty")
	}
	crawlerConfig := craw.CrawlerConfig{

		DeviceID: s.crawlerOptions.DeviceID,
		Secret:   s.crawlerOptions.Secret,
	}

	crawler := craw.NewPriceCrawler(crawlerConfig, nil)
	mysqlStore, _ := mysql.GetMySQLFactoryOr(nil)

	dailySend := task.NewPriceSendTask(mysqlStore)
	// execCtx, execCancel := context.WithTimeout(ctx, 10*time.Minute)
	// defer execCancel()

	// targetDateStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
	// if err := dailySend.Run(execCtx, targetDateStr); err != nil {
	// 	log.Errorf("Daily send failed: %v", err)
	// } else {
	// 	log.Infof("Daily send succeeded")
	// }
	// if crawler == nil {
	// 	log.Fatal("Failed to create PriceCrawler")
	// }
	// targerTime := time.Now().AddDate(0, 0, -1) // 爬昨天
	// if err := crawler.Run(ctx, targerTime); err != nil {
	// 	log.Errorf("Daily crawl failed for %s: %v", targerTime.Format("2006-01-02"), err)
	// } else {
	// 	log.Infof("Daily crawl succeeded for %s", targerTime.Format("2006-01-02"))
	// }
	log.Debugf("Daily crawl time: %s", s.cronOptions.DailyCrawTime)
	log.Debugf("Daily email time: %s", s.cronOptions.DailyEmailTime)

	if !s.cronOptions.EnableDailyCrawSender && !s.cronOptions.EnableDailyEmailSender {
		log.Info("All cron tasks disabled")
		return
	}
	go func() {
		logger, _ := zap.NewProduction()
		defer logger.Sync()

		cronLogger := cronlog.NewLogger(logger.Sugar())
		c := cron.New(
			cron.WithLogger(cronLogger),
		)

		// 爬虫任务
		if s.cronOptions.EnableDailyCrawSender {
			crawFunc := func() {
				execCtx, execCancel := context.WithTimeout(ctx, 10*time.Minute)
				defer execCancel()

				targetDate := time.Now().AddDate(0, 0, -1)
				if err := crawler.Run(execCtx, targetDate); err != nil {
					log.Errorf("Daily crawl failed for %s: %v", targetDate.Format("2006-01-02"), err)
				} else {
					log.Infof("Daily crawl succeeded for %s", targetDate.Format("2006-01-02"))
				}
			}

			if _, err := c.AddFunc(s.cronOptions.DailyCrawTime, crawFunc); err != nil {
				log.Fatalf("Failed to add crawl cron task: %v", err)
			}

		}
		// 发送任务
		if s.cronOptions.EnableDailyEmailSender {
			crawSendFunc := func() {
				execCtx, execCancel := context.WithTimeout(ctx, 10*time.Minute)
				defer execCancel()

				targetDateStr := time.Now().AddDate(0, 0, -1).Format("2006-01-02")
				if err := dailySend.Run(execCtx, targetDateStr); err != nil {
					log.Errorf("Daily send failed: %v", err)
				} else {
					log.Infof("Daily send succeeded")
				}
			}

			if _, err := c.AddFunc(s.cronOptions.DailyEmailTime, crawSendFunc); err != nil {
				log.Fatalf("Failed to add email cron task: %v", err)
			}

		}
		c.Start()
		log.Infof("Daily cron tasks started: crawl=%s, email=%s",
			s.cronOptions.DailyCrawTime, s.cronOptions.DailyEmailTime)

		<-ctx.Done()
		c.Stop()
		log.Info("Daily cron tasks stopped")
	}()
}
