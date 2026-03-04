package alert

import (
	"fmt"
	"time"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
)

// PriceRecord 价格记录（用于时间排序）
type PriceRecord struct {
	Price     *v1.Price
	Timestamp time.Time
}

// PriceRange 价格范围
type PriceRange struct {
	Min float64
	Max float64
	Avg float64
}

// CalculatePriceRange 计算价格范围
func CalculatePriceRange(prices []float64) *PriceRange {
	if len(prices) == 0 {
		return &PriceRange{}
	}

	min, max := prices[0], prices[0]
	sum := 0.0

	for _, p := range prices {
		sum += p
		if p < min {
			min = p
		}
		if p > max {
			max = p
		}
	}

	avg := sum / float64(len(prices))

	return &PriceRange{
		Min: min,
		Max: max,
		Avg: avg,
	}
}

// PriceChange 价格变化
type PriceChange struct {
	Absolute  float64
	Percent   float64
	IsIncrease bool
}

// CalculatePriceChange 计算价格变化
func CalculatePriceChange(old, new float64) *PriceChange {
	if old == 0 {
		return &PriceChange{}
	}

	abs := new - old
	pct := (abs / old) * 100

	return &PriceChange{
		Absolute:   abs,
		Percent:    pct,
		IsIncrease: abs > 0,
	}
}

// FormatPrice 格式化价格
func FormatPrice(price float64) string {
	return fmt.Sprintf("%.2f", price)
}

// FormatPercentage 格式化百分比
func FormatPercentage(pct float64) string {
	if pct >= 0 {
		return fmt.Sprintf("+%.2f%%", pct)
	}
	return fmt.Sprintf("%.2f%%", pct)
}

// formatPercentage 格式化百分比（用于 map）
func formatPercentage(pct float64) string {
	if pct >= 0 {
		return fmt.Sprintf("%.2f%%", pct)
	}
	return fmt.Sprintf("%.2f%%", pct)
}

// formatAbsolute 格式化绝对值（用于 map）
func formatAbsolute(abs float64) string {
	if abs >= 0 {
		return fmt.Sprintf("%.2f元", abs)
	}
	return fmt.Sprintf("%.2f元", abs)
}

// formatVolatility 格式化波动指数（用于 map）
func formatVolatility(index float64) string {
	return fmt.Sprintf("%.4f", index)
}

// formatThreshold 格式化阈值（用于 map）
func formatThreshold(value float64) string {
	return fmt.Sprintf("%.2f", value)
}
