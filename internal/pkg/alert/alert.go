package alert

import (
	"context"
	"fmt"
	"math"
	"sync"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/config"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/notifications"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/metrics"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

// Evaluator 预警评估器接口
type Evaluator interface {
	Evaluate(ctx context.Context, prices []*v1.Price) ([]*notifications.AlertContent, error)
	Name() string
}

// Historian 历史数据存储接口
type Historian interface {
	GetPriceHistory(category, breed string, maxDays int) ([]*v1.Price, error)
	SavePriceHistory(prices []*v1.Price) error
}

// AlertEngine 告警引擎
type AlertEngine struct {
	cfg        *config.AlertConfig
	evaluators []Evaluator
	historian  Historian
	metrics    *metrics.Collector
	notify     *notifications.EmailNotification

	// 防抖：记录最近告警时间，避免邮件风暴
	lastAlerts sync.Map // map[string]time.Time
}

// NewAlertEngine 创建新的告警引擎
func NewAlertEngine(cfg *config.AlertConfig, historian Historian, metrics *metrics.Collector, notify *notifications.EmailNotification) *AlertEngine {
	engine := &AlertEngine{
		cfg:        cfg,
		evaluators: []Evaluator{},
		historian:  historian,
		metrics:    metrics,
		notify:     notify,
		lastAlerts: sync.Map{},
	}

	// 根据配置注册评估器
	if cfg.Enabled {
		engine.registerEvaluators()
	}

	return engine
}

// registerEvaluators 注册所有评估器
func (e *AlertEngine) registerEvaluators() {
	e.evaluators = append(e.evaluators,
		NewPriceChangeEvaluator(e.cfg),
		NewVolatilityEvaluator(e.cfg),
		NewMissingDataEvaluator(e.cfg, e.historian),
	)
}

// Run 启动评估循环
func (e *AlertEngine) Run(ctx context.Context) {
	if !e.cfg.Enabled {
		log.Info("Alert engine disabled, skipping run")
		return
	}

	log.Infof("Starting alert engine with evaluation interval: %v", e.cfg.EvaluationInterval)

	ticker := time.NewTicker(e.cfg.EvaluationInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			log.Info("Alert engine stopped")
			return
		case <-ticker.C:
			e.evaluateCycle(ctx)
		}
	}
}

// evaluateCycle 执行一轮评估
func (e *AlertEngine) evaluateCycle(ctx context.Context) {
	e.metrics.RecordEvaluationStart()

	log.Debug("Starting alert evaluation cycle")

	// 运行所有评估器
	for _, eval := range e.evaluators {
		e.metrics.RecordEvaluationStart()

		alerts, err := eval.Evaluate(ctx, nil)
		if err != nil {
			log.Errorf("Evaluator %s failed: %v", eval.Name(), err)
			continue
		}

		for _, alert := range alerts {
			e.processAlert(ctx, alert)
		}

		e.metrics.RecordEvaluation(eval.Name(), time.Since(time.Time{}).Seconds())
	}

	// 释放资源
	for _, eval := range e.evaluators {
		e.metrics.RecordEvaluation(fmt.Sprintf("%s_cleanup", eval.Name()), time.Since(time.Time{}).Seconds())
	}
}

// processAlert 处理单个告警
func (e *AlertEngine) processAlert(ctx context.Context, alert *notifications.AlertContent) {
	alertKey := e.generateAlertKey(alert)

	// 检查是否触发了告警
	if !e.shouldAlert(alertKey, alert.Timestamp) {
		log.Debugf("Alert suppressed (flood control): %s", alertKey)
		return
	}

	// 记录告警指标
	e.metrics.RecordAlert(
		string(alert.Type),
		alert.Category,
		string(alert.Severity),
	)

	// 发送邮件通知
	if e.notify != nil {
		if err := e.notify.SendAlert(alert); err != nil {
			log.Errorf("Failed to send alert notification: %v", err)
		}
	}

	// 记录告警时间
	e.lastAlerts.Store(alertKey, alert.Timestamp)
	log.Infof("Alert processed: type=%s, product=%s, severity=%s",
		alert.Type, alert.Product, alert.Severity)
}

// shouldAlert 检查是否应该发送告警（防抖）
func (e *AlertEngine) shouldAlert(key string, now time.Time) bool {
	// 检查是否在防抖窗口内（默认5分钟）
	if lastTime, ok := e.lastAlerts.Load(key); ok {
		if lastTimeTime, ok := lastTime.(time.Time); ok {
			if now.Sub(lastTimeTime) < 5*time.Minute {
				return false
			}
		}
	}
	return true
}

// generateAlertKey 生成告警唯一键
func (e *AlertEngine) generateAlertKey(alert *notifications.AlertContent) string {
	return fmt.Sprintf("%s:%s:%s", alert.Type, alert.Product, alert.Region)
}

// UpdatePrice 更新价格并触发评估
func (e *AlertEngine) UpdatePrice(price *v1.Price) {
	if !e.cfg.Enabled {
		return
	}

	// 记录当前价格指标
	e.metrics.RecordPrice(
		price.CateName,
		price.BreedName,
		price.Unit,
		getProvinceName(price.ProvinceID),
		price.AddressDetail,
		price.AvgPrice,
	)

	// 如果启用了历史记录，保存价格
	if e.historian != nil {
		// 异步保存，不影响主流程
		go func() {
			// 简单转换
			prices := []*v1.Price{price}
			_ = e.historian.SavePriceHistory(prices)
		}()
	}
}

// GetStatus 获取告警引擎状态
func (e *AlertEngine) GetStatus() map[string]interface{} {
	return map[string]interface{}{
		"enabled":    e.cfg.Enabled,
		"evaluators": len(e.evaluators),
	}
}

// helper functions

func getProvinceName(provinceID uint32) string {
	// 简单映射，实际应该从数据库或配置中获取
	provinces := map[uint32]string{
		11: "北京",
		12: "天津",
		13: "河北",
		14: "山西",
		15: "内蒙古",
		21: "辽宁",
		22: "吉林",
		23: "黑龙江",
		31: "上海",
		32: "江苏",
		33: "浙江",
		34: "安徽",
		35: "福建",
		36: "江西",
		37: "山东",
		41: "河南",
		42: "湖北",
		43: "湖南",
		44: "广东",
		45: "广西",
		46: "海南",
		50: "重庆",
		51: "四川",
		52: "贵州",
		53: "云南",
		54: "西藏",
		61: "陕西",
		62: "甘肃",
		63: "青海",
		64: "宁夏",
		65: "新疆",
	}
	if name, ok := provinces[provinceID]; ok {
		return name
	}
	return "未知"
}

// StatCalculator 统计计算器
type StatCalculator struct{}

// CalculateVolatilityIndex 计算波动指数
func (c *StatCalculator) CalculateVolatilityIndex(prices []float64) float64 {
	if len(prices) < 2 {
		return 0
	}

	// 计算均值
	var sum float64
	for _, p := range prices {
		sum += p
	}
	mean := sum / float64(len(prices))

	if mean == 0 {
		return 0
	}

	// 计算标准差
	var variance float64
	for _, p := range prices {
		diff := p - mean
		variance += diff * diff
	}
	variance /= float64(len(prices))
	stdDev := math.Sqrt(variance)

	// 计算变异系数（标准化到0-1）
	cv := stdDev / mean

	// 使用 sigmoid 函数标准化
	// cv=0.5 时返回约 0.62；cv=1.0 时返回约 0.73
	normalized := 1 / (1 + math.Exp(-2*(cv-0.5)))

	return math.Min(1.0, math.Max(0.0, normalized))
}

// CalculateChangePercent 计算百分比变化
func (c *StatCalculator) CalculateChangePercent(old, new float64) float64 {
	if old == 0 {
		return 0
	}
	return ((new - old) / old) * 100
}

// CalculateChangeAbsolute 计算绝对值变化
func (c *StatCalculator) CalculateChangeAbsolute(old, new float64) float64 {
	return new - old
}
