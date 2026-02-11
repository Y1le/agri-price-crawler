package craw

import (
	"context"
	"time"

	craw "github.com/Y1le/agri-price-crawler/internal/craw/crawler"
	task "github.com/Y1le/agri-price-crawler/internal/craw/cron"
	"github.com/Y1le/agri-price-crawler/internal/craw/store/mysql"
	"github.com/Y1le/agri-price-crawler/pkg/log"
	shutdown "github.com/Y1le/agri-price-crawler/pkg/shutdown"
	"github.com/robfig/cron/v3"
)

func (s *crawServer) initCronTask() {
	ctx, cancel := context.WithCancel(context.Background())

	s.gs.AddShutdownCallback(shutdown.ShutdownFunc(func(string) error {
		cancel()
		return nil
	}))

	if !s.cronOptions.EnableDailyEmailSender {
		log.Info("Daily crawl task is disabled")
		return
	}

	if s.crawlerOptions.DeviceID == "" || s.crawlerOptions.Secret == "" {
		log.Fatal("DeviceID or Secret is empty")
	}
	crawlerConfig := craw.CrawlerConfig{

		DeviceID: s.crawlerOptions.DeviceID,
		Secret:   s.crawlerOptions.Secret,
	}

	crawler := craw.NewPriceCrawler(crawlerConfig, nil)
	mysqlStore, _ := mysql.GetMySQLFactoryOr(nil)
	if mysqlStore != nil {
		_ = mysqlStore.Close()
	}

	dailySend := task.NewPriceSendTask(mysqlStore)
	// if crawler == nil {
	// 	log.Fatal("Failed to create PriceCrawler")
	// }
	// targerTime := time.Now().AddDate(0, 0, -1) // 爬昨天
	// if err := crawler.Run(ctx, targerTime); err != nil {
	// 	log.Errorf("Daily crawl failed for %s: %v", targerTime.Format("2006-01-02"), err)
	// } else {
	// 	log.Infof("Daily crawl succeeded for %s", targerTime.Format("2006-01-02"))
	// }
	go func() {
		c := cron.New()
		targetDate := time.Now().AddDate(0, 0, -1) // 爬昨天
		crawFunc := func() {
			execCtx, execCancel := context.WithTimeout(ctx, 10*time.Minute)
			defer execCancel()

			if err := crawler.Run(execCtx, targetDate); err != nil {
				log.Errorf("Daily crawl failed for %s: %v", targetDate.Format("2006-01-02"), err)
			} else {
				log.Infof("Daily crawl succeeded for %s", targetDate.Format("2006-01-02"))
			}
		}

		if _, err := c.AddFunc(s.cronOptions.DailyCrawTime, crawFunc); err != nil {
			log.Fatalf("Failed to add cron task: %v", err)
		}

		crawSendFunc := func() {
			execCtx, execCancel := context.WithTimeout(ctx, 10*time.Minute)
			defer execCancel()

			if err := dailySend.Run(execCtx, targetDate.Format("2006-01-02")); err != nil {
				log.Errorf("Daily send failed: %v", err)
			} else {
				log.Infof("Daily send succeeded")
			}
		}

		if _, err := c.AddFunc(s.cronOptions.DailyEmailTime, crawSendFunc); err != nil {
			log.Fatalf("Failed to add cron task: %v", err)
		}

		c.Start()
		log.Infof("Daily cron task started at %s", s.cronOptions.DailyEmailTime)

		<-ctx.Done()
		c.Stop()
		log.Info("Daily cron task stopped")
	}()
}
