package craw

import (
	"context"
	"time"

	craw "github.com/Y1le/agri-price-crawler/internal/craw/crawler"
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

	// 3. 检查是否启用（使用你的现有字段）
	if !s.cronOptions.EnableDailyEmailSender {
		log.Info("Daily crawl task is disabled")
		return
	}

	// 4. 解析时间（复用你的 DailyEmailTime 字段）
	cronExpr, err := cron.ParseStandard(s.cronOptions.DailyEmailTime)
	if err != nil {
		log.Fatalf("Invalid cron time '%s': %v", s.cronOptions.DailyEmailTime, err)
	}
	if s.crawlerOptions.DeviceID == "" || s.crawlerOptions.Secret == "" {
		log.Fatal("DeviceID or Secret is empty")
	}
	// 6. 创建爬虫配置（只取 DeviceID/Secret）
	crawlerConfig := craw.CrawlerConfig{

		DeviceID: s.crawlerOptions.DeviceID,
		Secret:   s.crawlerOptions.Secret,
	}

	crawler := craw.NewPriceCrawler(crawlerConfig, nil)
	if crawler == nil {
		log.Fatal("Failed to create PriceCrawler")
	}
	targerTime := time.Now().AddDate(0, 0, -1) // 爬昨天
	if err := crawler.Run(ctx, targerTime); err != nil {
		log.Errorf("Daily crawl failed for %s: %v", targerTime.Format("2006-01-02"), err)
	} else {
		log.Infof("Daily crawl succeeded for %s", targerTime.Format("2006-01-02"))
	}
	go func() {
		c := cron.New()

		taskFunc := func() {
			execCtx, execCancel := context.WithTimeout(ctx, 10*time.Minute)
			defer execCancel()

			targetDate := time.Now().AddDate(0, 0, -1) // 爬昨天
			if err := crawler.Run(execCtx, targetDate); err != nil {
				log.Errorf("Daily crawl failed for %s: %v", targetDate.Format("2006-01-02"), err)
			} else {
				log.Infof("Daily crawl succeeded for %s", targetDate.Format("2006-01-02"))
			}
		}

		if _, err := c.AddFunc(s.cronOptions.DailyEmailTime, taskFunc); err != nil {
			log.Fatalf("Failed to add cron task: %v", err)
		}

		c.Start()
		log.Infof("Daily cron task started at %s (expr: %s)", s.cronOptions.DailyEmailTime, cronExpr)

		<-ctx.Done()
		c.Stop()
		log.Info("Daily cron task stopped")
	}()
}
