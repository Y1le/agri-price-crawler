package alert

import (
	"context"
	"fmt"
	"time"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/store"
)

// MySQLHistorian MySQL历史存储实现
type MySQLHistorian struct {
	store store.PriceHistoryStore
}

// NewMySQLHistorian 创建 MySQL 历史存储
func NewMySQLHistorian(store store.PriceHistoryStore) *MySQLHistorian {
	return &MySQLHistorian{store: store}
}

// GetPriceHistory 获取价格历史
func (h *MySQLHistorian) GetPriceHistory(category, breed string, maxDays int) ([]*v1.Price, error) {
	startTime := time.Now().AddDate(0, 0, -maxDays)
	endTime := time.Now()

	// TODO: 转换 breed 格式以匹配存储格式
	return h.store.GetByTimeRange(context.Background(), breed, startTime, endTime)
}

// SavePriceHistory 保存价格历史
func (h *MySQLHistorian) SavePriceHistory(prices []*v1.Price) error {
	return h.store.SaveBatch(context.Background(), prices)
}

// CleanOldData 清理旧数据
func (h *MySQLHistorian) CleanOldData(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	return h.store.ClearBefore(context.Background(), cutoff)
}

// GetRecentPrices 获取最近的价格
func (h *MySQLHistorian) GetRecentPrices(breed string, limit int) ([]*v1.Price, error) {
	return h.store.GetLatest(context.Background(), breed, limit)
}

// ValidatePrice 验证价格数据
func (h *MySQLHistorian) ValidatePrice(price *v1.Price) error {
	if price == nil {
		return fmt.Errorf("price cannot be nil")
	}
	if price.BreedName == "" {
		return fmt.Errorf("breed name is required")
	}
	if price.AvgPrice <= 0 {
		return fmt.Errorf("avg price must be positive")
	}
	return nil
}
