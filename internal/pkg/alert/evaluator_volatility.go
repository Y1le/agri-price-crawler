package alert

import (
	"context"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/config"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/notifications"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

// VolatilityEvaluator 波动指数评估器
type VolatilityEvaluator struct {
	cfg *config.AlertConfig
}

// NewVolatilityEvaluator 创建波动指数评估器
func NewVolatilityEvaluator(cfg *config.AlertConfig) *VolatilityEvaluator {
	return &VolatilityEvaluator{cfg: cfg}
}

// Name 返回评估器名称
func (e *VolatilityEvaluator) Name() string {
	return "volatility"
}

// Evaluate 评估价格波动指数
func (e *VolatilityEvaluator) Evaluate(ctx context.Context, prices []*v1.Price) ([]*notifications.AlertContent, error) {
	alerts := []*notifications.AlertContent{}

	// 获取历史价格数据进行波动分析
	history, err := e.getHistoricalPrices(ctx)
	if err != nil {
		log.Errorf("Failed to get historical prices for volatility: %v", err)
		return nil, err
	}

	// 按类别和品种分组价格
	priceGroups := e.groupPricesByCategory(history)

	// 计算每个组的波动指数
	for groupKey, groupPrices := range priceGroups {
		if len(groupPrices) < 7 {
			// 需要至少7天的数据
			continue
		}

		// 提取平均价格
		var prices []float64
		for _, p := range groupPrices {
			prices = append(prices, p.AvgPrice)
		}

		// 计算波动指数
		calc := &StatCalculator{}
		index := calc.CalculateVolatilityIndex(prices)

		// 检查是否触发告警
		if index >= e.cfg.Thresholds.VolatilityIndex {
			alert := e.createVolatilityAlert(groupKey, index, prices[len(prices)-1])
			if alert != nil {
				alerts = append(alerts, alert)
			}
		}

		// 记录指标
		e.updateVolatilityMetrics(groupKey, index)
	}

	log.Debugf("VolatilityEvaluator: found %d alerts", len(alerts))
	return alerts, nil
}

// createVolatilityAlert 创建波动告警
func (e *VolatilityEvaluator) createVolatilityAlert(groupKey string, index float64, currentPrice float64) *notifications.AlertContent {
	// 解析 groupKey
	// format: category:breed:city
	return &notifications.AlertContent{
		Type:      notifications.AlertTypeHighVolatility,
		Severity:  notifications.SeverityCritical,
		Product:   "", // 从 groupKey 解析
		Price:     currentPrice,
		ChangePct: 0,
		ChangeAbs: 0,
		Region:    "", // 从 groupKey 解析
		Category:  "", // 从 groupKey 解析
		Timestamp: time.Now(),
		Extra: map[string]string{
			"volatility_index": formatVolatility(index),
			"threshold":        formatThreshold(e.cfg.Thresholds.VolatilityIndex),
		},
	}
}

// updateVolatilityMetrics 更新波动指标
func (e *VolatilityEvaluator) updateVolatilityMetrics(groupKey string, index float64) {
	log.Debugf("Volatility index: %s = %.4f", groupKey, index)
}

// groupPricesByCategory 按类别和品种分组价格
func (e *VolatilityEvaluator) groupPricesByCategory(prices []*v1.Price) map[string][]*v1.Price {
	groups := make(map[string][]*v1.Price)

	for _, p := range prices {
		key := e.getPriceKey(p)
		groups[key] = append(groups[key], p)
	}

	return groups
}

// getPriceKey 生成价格键
func (e *VolatilityEvaluator) getPriceKey(price *v1.Price) string {
	return price.CateName + ":" + price.BreedName
}

// getHistoricalPrices 获取历史价格
func (e *VolatilityEvaluator) getHistoricalPrices(ctx context.Context) ([]*v1.Price, error) {
	// 这里应该从历史存储中获取7天的价格
	return []*v1.Price{}, nil
}
