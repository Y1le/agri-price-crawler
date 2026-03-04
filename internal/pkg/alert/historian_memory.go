package alert

import (
	"fmt"
	"time"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

// InMemoryHistorian 内存历史存储（用于测试和开发）
type InMemoryHistorian struct {
	prices []priceRecord
}

// priceRecord 带时间戳的价格记录
type priceRecord struct {
	Price     *v1.Price
	Timestamp time.Time
}

// NewInMemoryHistorian 创建内存历史存储
func NewInMemoryHistorian() *InMemoryHistorian {
	return &InMemoryHistorian{
		prices: make([]priceRecord, 0),
	}
}

// GetPriceHistory 获取价格历史
func (h *InMemoryHistorian) GetPriceHistory(category, breed string, maxDays int) ([]*v1.Price, error) {
	result := make([]*v1.Price, 0)
	cutoff := time.Now().AddDate(0, 0, -maxDays)

	for _, record := range h.prices {
		if record.Timestamp.After(cutoff) &&
			record.Price.CateName == category &&
			record.Price.BreedName == breed {
			result = append(result, record.Price)
		}
	}

	// 按时间排序（按 CreatedAt 字段）
	// utils_sort.go 中定义了 sortPricesByTime 函数
	sortPricesByTime(result)

	log.Debugf("Historian: retrieved %d price records for %s/%s", len(result), category, breed)
	return result, nil
}

// SavePriceHistory 保存价格历史
func (h *InMemoryHistorian) SavePriceHistory(prices []*v1.Price) error {
	now := time.Now()
	for _, price := range prices {
		h.prices = append(h.prices, priceRecord{
			Price:     price,
			Timestamp: now,
		})
	}

	log.Debugf("Historian: saved %d price records", len(prices))
	return nil
}

// ClearOldRecords 清除旧记录
func (h *InMemoryHistorian) ClearOldRecords(days int) error {
	cutoff := time.Now().AddDate(0, 0, -days)
	newPrices := make([]priceRecord, 0)

	for _, record := range h.prices {
		if record.Timestamp.After(cutoff) {
			newPrices = append(newPrices, record)
		}
	}

	h.prices = newPrices
	log.Debugf("Historian: cleared old records, remaining: %d", len(h.prices))
	return nil
}

// GetStats 获取统计信息
func (h *InMemoryHistorian) GetStats() map[string]interface{} {
	return map[string]interface{}{
		"total_records": len(h.prices),
	}
}

// GetLatest 获取最新的价格记录
func (h *InMemoryHistorian) GetLatest(breed string, limit int) ([]*v1.Price, error) {
	var result []*v1.Price
	var latestTime time.Time

	for _, record := range h.prices {
		if record.Price.BreedName == breed {
			if record.Timestamp.After(latestTime) {
				latestTime = record.Timestamp
				result = append([]*v1.Price{record.Price}, result...)
			}
		}
	}

	if len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// PriceSnapshot 价格快照
type PriceSnapshot struct {
	Category     string    `json:"category"`
	Breed        string    `json:"breed"`
	CurrentPrice float64   `json:"current_price"`
	LastUpdate   time.Time `json:"last_update"`
	Price7DMin   float64   `json:"price_7d_min"`
	Price7DMax   float64   `json:"price_7d_max"`
	Price7DAvg   float64   `json:"price_7d_avg"`
}

// GetPriceSnapshot 获取价格快照
func (h *InMemoryHistorian) GetPriceSnapshot(category, breed string) (*PriceSnapshot, error) {
	history, err := h.GetPriceHistory(category, breed, 7)
	if err != nil {
		return nil, err
	}

	if len(history) == 0 {
		return nil, fmt.Errorf("no price history found for %s/%s", category, breed)
	}

	// 找到最新价格
	var latest *v1.Price
	var latestTime time.Time
	for _, p := range history {
		if p.CreatedAt.After(latestTime) {
			latestTime = p.CreatedAt
			latest = p
		}
	}

	if latest == nil {
		return nil, fmt.Errorf("no latest price found")
	}

	// 计算7日统计
	var minPrice, maxPrice, sumPrice float64
	count := 0
	for _, p := range history {
		if count == 0 || p.AvgPrice < minPrice {
			minPrice = p.AvgPrice
		}
		if count == 0 || p.AvgPrice > maxPrice {
			maxPrice = p.AvgPrice
		}
		sumPrice += p.AvgPrice
		count++
	}

	return &PriceSnapshot{
		Category:     category,
		Breed:        breed,
		CurrentPrice: latest.AvgPrice,
		LastUpdate:   latestTime,
		Price7DMin:   minPrice,
		Price7DMax:   maxPrice,
		Price7DAvg:   sumPrice / float64(count),
	}, nil
}
