package alert

import (
	"fmt"
	"sync"
	"time"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
)

// MockHistorian 用于测试的模拟历史存储
type MockHistorian struct {
	mu     sync.RWMutex
	prices map[string][]*v1.Price
}

// NewMockHistorian 创建 Mock 历史存储
func NewMockHistorian() *MockHistorian {
	return &MockHistorian{
		prices: make(map[string][]*v1.Price),
	}
}

// GetPriceHistory 获取价格历史
func (h *MockHistorian) GetPriceHistory(category, breed string, maxDays int) ([]*v1.Price, error) {
	h.mu.RLock()
	defer h.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", category, breed)
	prices, exists := h.prices[key]
	if !exists {
		return []*v1.Price{}, nil
	}

	// 返回最近 maxDays 天的价格
	cutoff := time.Now().AddDate(0, 0, -maxDays)
	result := make([]*v1.Price, 0)
	for _, p := range prices {
		// CreatedAt 是 time.Time 类型
		if !p.CreatedAt.IsZero() && p.CreatedAt.After(cutoff) {
			result = append(result, p)
		}
	}

	return result, nil
}

// SavePriceHistory 保存价格历史
func (h *MockHistorian) SavePriceHistory(prices []*v1.Price) error {
	h.mu.Lock()
	defer h.mu.Unlock()

	for _, price := range prices {
		key := fmt.Sprintf("%s:%s", price.CateName, price.BreedName)
		h.prices[key] = append(h.prices[key], price)
	}

	return nil
}

// GetPrices 获取所有价格
func (h *MockHistorian) GetPrices(category, breed string) []*v1.Price {
	h.mu.RLock()
	defer h.mu.RUnlock()

	key := fmt.Sprintf("%s:%s", category, breed)
	return h.prices[key]
}

// Clear 清空数据
func (h *MockHistorian) Clear() {
	h.mu.Lock()
	defer h.mu.Unlock()

	h.prices = make(map[string][]*v1.Price)
}
