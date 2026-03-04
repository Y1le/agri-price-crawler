package store

import (
	"context"
	"time"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

// PriceHistoryStore 价格历史存储接口
type PriceHistoryStore interface {
	// SaveBatch 批量保存价格历史
	SaveBatch(ctx context.Context, prices []*v1.Price) error

	// GetByTimeRange 获取时间范围内的价格
	GetByTimeRange(ctx context.Context, breed string, startTime, endTime time.Time) ([]*v1.Price, error)

	// GetLatest 获取最新的 N 条价格记录
	GetLatest(ctx context.Context, breed string, limit int) ([]*v1.Price, error)

	// ClearBefore 清除指定时间之前的数据
	ClearBefore(ctx context.Context, before time.Time) error
}

// InMemoryPriceHistoryStore 内存实现（用于测试）
type InMemoryPriceHistoryStore struct {
	prices []*v1.Price
}

// NewInMemoryPriceHistoryStore 创建内存存储
func NewInMemoryPriceHistoryStore() *InMemoryPriceHistoryStore {
	return &InMemoryPriceHistoryStore{
		prices: make([]*v1.Price, 0),
	}
}

// SaveBatch 批量保存价格历史
func (s *InMemoryPriceHistoryStore) SaveBatch(ctx context.Context, prices []*v1.Price) error {
	s.prices = append(s.prices, prices...)
	log.Debugf("Saved %d price records to memory store", len(prices))
	return nil
}

// GetByTimeRange 获取时间范围内的价格
func (s *InMemoryPriceHistoryStore) GetByTimeRange(ctx context.Context, breed string, startTime, endTime time.Time) ([]*v1.Price, error) {
	var result []*v1.Price
	for _, p := range s.prices {
		// 使用 CreatedAt 字段
		createTime := p.CreatedAt
		if p.BreedName == breed && createTime.After(startTime) && createTime.Before(endTime) {
			result = append(result, p)
		}
	}
	return result, nil
}

// GetLatest 获取最新的 N 条价格记录
func (s *InMemoryPriceHistoryStore) GetLatest(ctx context.Context, breed string, limit int) ([]*v1.Price, error) {
	var result []*v1.Price
	var latestTime time.Time

	for _, p := range s.prices {
		if p.BreedName == breed {
			if p.CreatedAt.After(latestTime) {
				latestTime = p.CreatedAt
				result = append([]*v1.Price{p}, result...)
			}
		}
	}

	if len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

// ClearBefore 清除指定时间之前的数据
func (s *InMemoryPriceHistoryStore) ClearBefore(ctx context.Context, before time.Time) error {
	newPrices := make([]*v1.Price, 0)
	for _, p := range s.prices {
		if !p.CreatedAt.Before(before) {
			newPrices = append(newPrices, p)
		}
	}
	s.prices = newPrices
	return nil
}
