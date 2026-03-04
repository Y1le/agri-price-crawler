package alert

import (
	"sort"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
)

// sortPricesByTime 按 CreatedAt 时间排序价格
func sortPricesByTime(prices []*v1.Price) {
	sort.Slice(prices, func(i, j int) bool {
		return prices[i].CreatedAt.Before(prices[j].CreatedAt)
	})
}

// sortPriceRecordsByTime 按时间排序价格记录
func sortPriceRecordsByTime(records []*PriceRecord) {
	sort.Slice(records, func(i, j int) bool {
		return records[i].Timestamp.Before(records[j].Timestamp)
	})
}

// getLatestPrice 获取最新价格
func getLatestPrice(prices []*v1.Price) *v1.Price {
	if len(prices) == 0 {
		return nil
	}

	latest := prices[0]
	for _, p := range prices {
		if p.CreatedAt.After(latest.CreatedAt) {
			latest = p
		}
	}
	return latest
}

// getAveragePrice 获取平均价格
func getAveragePrice(prices []*v1.Price) float64 {
	if len(prices) == 0 {
		return 0
	}

	var sum float64
	for _, p := range prices {
		sum += p.AvgPrice
	}
	return sum / float64(len(prices))
}
