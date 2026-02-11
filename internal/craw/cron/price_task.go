package cron

// import (
// 	"context"
// 	"fmt"
// 	"time"

// 	crawler "github.com/Y1le/agri-price-crawler/internal/craw/crawler"
// 	"github.com/Y1le/agri-price-crawler/pkg/log"
// )

// type PriceCrawlTask struct {
// 	crawler *crawler.PriceCrawler
// }

// func NewPriceCrawlTask(crawler *crawler.PriceCrawler) *PriceCrawlTask {
// 	return &PriceCrawlTask{crawler: crawler}
// }

// func (t *PriceCrawlTask) Name() string { return "daily-price-crawler" }

// // func (t *PriceCrawlTask) Spec() string { return "0 0 4 * * *" }

// func (t *PriceCrawlTask) Run(ctx context.Context) error {
// 	// 爬取 **今天** 的数据（因为凌晨4点时，当天数据已生成）
// 	today := time.Now().In(time.Local) // 使用本地时区（或指定 Asia/Shanghai）

// 	// 可选：加 retry 机制
// 	for attempt := 0; attempt < 3; attempt++ {
// 		err := t.crawler.Run(ctx, today)
// 		if err == nil {
// 			return nil // 成功
// 		}
// 		log.Warnf("Price crawl attempt %d failed: %v", attempt+1, err)
// 		if attempt < 2 {
// 			time.Sleep(5 * time.Second) // 重试前等待
// 		}
// 	}
// 	return fmt.Errorf("all attempts failed")
// }
