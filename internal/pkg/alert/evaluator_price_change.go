package alert

import (
	"context"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/config"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/notifications"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

// PriceChangeEvaluator 价格变化评估器
type PriceChangeEvaluator struct {
	cfg *config.AlertConfig
}

// NewPriceChangeEvaluator 创建价格变化评估器
func NewPriceChangeEvaluator(cfg *config.AlertConfig) *PriceChangeEvaluator {
	return &PriceChangeEvaluator{cfg: cfg}
}

// Name 返回评估器名称
func (e *PriceChangeEvaluator) Name() string {
	return "price_change"
}

// Evaluate 评估价格变化
func (e *PriceChangeEvaluator) Evaluate(ctx context.Context, prices []*v1.Price) ([]*notifications.AlertContent, error) {
	alerts := []*notifications.AlertContent{}

	// 从历史数据中获取24小时前的价格
	history, err := e.getHistoricalPrices(ctx)
	if err != nil {
		log.Errorf("Failed to get historical prices: %v", err)
		return nil, err
	}

	// 用于存储当前价格映射
	priceMap := make(map[string]*v1.Price)
	for _, p := range prices {
		key := e.getPriceKey(p)
		priceMap[key] = p
	}

	// 用于存储历史价格映射
	histMap := make(map[string]*v1.Price)
	for _, h := range history {
		key := e.getPriceKey(h)
		histMap[key] = h
	}

	// 比较价格变化
	for key, current := range priceMap {
		historyPrice, exists := histMap[key]
		if !exists {
			continue
		}

		calc := &StatCalculator{}
		changePct := calc.CalculateChangePercent(historyPrice.AvgPrice, current.AvgPrice)
		changeAbs := calc.CalculateChangeAbsolute(historyPrice.AvgPrice, current.AvgPrice)

		// 检查是否触发告警
		alert := e.checkPriceChangeAlert(current, changePct, changeAbs)
		if alert != nil {
			alerts = append(alerts, alert)
		}

		// 记录指标
		updateKey := key
		e.updateMetrics(updateKey, changePct)
	}

	log.Debugf("PriceChangeEvaluator: found %d alerts", len(alerts))
	return alerts, nil
}

// checkPriceChangeAlert 检查价格变化是否触发告警
func (e *PriceChangeEvaluator) checkPriceChangeAlert(current *v1.Price, changePct, changeAbs float64) *notifications.AlertContent {
	alertType := notifications.AlertType("")
	severity := notifications.SeverityInfo

	// 检查暴涨
	if changePct >= e.cfg.Thresholds.PriceSpikePercent && changeAbs >= e.cfg.Thresholds.PriceSpikeAbsolute {
		alertType = notifications.AlertTypePriceSpike
		severity = notifications.SeverityWarning
	}

	// 检查暴跌
	if changePct <= -e.cfg.Thresholds.PriceDropPercent && changeAbs <= -e.cfg.Thresholds.PriceDropAbsolute {
		alertType = notifications.AlertTypePriceDrop
		severity = notifications.SeverityInfo
	}

	if alertType == "" {
		return nil
	}

	return &notifications.AlertContent{
		Type:      alertType,
		Severity:  severity,
		Product:   current.BreedName,
		Price:     current.AvgPrice,
		ChangePct: changePct,
		ChangeAbs: changeAbs,
		Region:    current.AddressDetail,
		Category:  current.CateName,
		Timestamp: time.Now(),
		Extra: map[string]string{
			"24h_change_pct": formatPercentage(changePct),
			"24h_change_abs": formatAbsolute(changeAbs),
		},
	}
}

// getPriceKey 生成价格键
func (e *PriceChangeEvaluator) getPriceKey(price *v1.Price) string {
	return price.CateName + ":" + price.BreedName
}

// updateMetrics 更新指标
func (e *PriceChangeEvaluator) updateMetrics(key string, changePct float64) {
	// TODO: 从 key 解析分类和品种
	log.Debugf("Price change recorded: %s = %.2f%%", key, changePct)
}

// getHistoricalPrices 获取历史价格
func (e *PriceChangeEvaluator) getHistoricalPrices(ctx context.Context) ([]*v1.Price, error) {
	// 这里应该从历史存储中获取24小时前的价格
	// 由于实际的 historian 接口还未集成，这里返回空
	return []*v1.Price{}, nil
}
