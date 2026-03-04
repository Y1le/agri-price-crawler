package metrics

import (
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/config"
	"github.com/Y1le/agri-price-crawler/pkg/log"
	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

// Collector Prometheus 指标收集器
type Collector struct {
	// 价格当前值
	PriceCurrentValue *prometheus.GaugeVec

	// 24小时价格变化百分比
	PriceChange24H *prometheus.GaugeVec

	// 价格波动指数
	PriceVolatilityIndex *prometheus.GaugeVec

	// 爬虫状态
	CrawlerStatus *prometheus.GaugeVec

	// 告警计数
	AlertCount *prometheus.CounterVec

	// 最后告警时间
	AlertLastTrigger *prometheus.GaugeVec

	// 评估次数
	EvaluationCount prometheus.Counter

	// 评估耗时
	EvaluationDuration *prometheus.HistogramVec
}

// NewCollector 创建新的指标收集器
func NewCollector(cfg *config.AlertConfig) *Collector {
	prefix := "agri_"

	return &Collector{
		PriceCurrentValue: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "price_current_value",
				Help: "Current agricultural product price",
			},
			[]string{"category", "breed", "unit", "province", "city"},
		),

		PriceChange24H: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "price_change_24h",
				Help: "24h price change percentage",
			},
			[]string{"category", "breed"},
		),

		PriceVolatilityIndex: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "price_volatility_index",
				Help: "Price volatility index (0-1)",
			},
			[]string{"category", "breed", "city"},
		),

		CrawlerStatus: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "crawler_status",
				Help: "Crawler status (1=running, 0=stopped/error)",
			},
			[]string{"component", "status"},
		),

		AlertCount: promauto.NewCounterVec(
			prometheus.CounterOpts{
				Name: prefix + "alert_count",
				Help: "Total number of alerts by type and category",
			},
			[]string{"type", "category", "severity"},
		),

		AlertLastTrigger: promauto.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "alert_last_trigger",
				Help: "Unix timestamp of last alert trigger",
			},
			[]string{"type", "category"},
		),

		EvaluationCount: promauto.NewCounter(
			prometheus.CounterOpts{
				Name: prefix + "evaluation_count",
				Help: "Total number of evaluation rounds",
			},
		),

		EvaluationDuration: promauto.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    prefix + "evaluation_duration_seconds",
				Help:    "Duration of evaluation round",
				Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60},
			},
			[]string{"stage"},
		),
	}
}

// RecordPrice 记录价格指标
func (c *Collector) RecordPrice(category, breed, unit, province, city string, price float64) {
	c.PriceCurrentValue.WithLabelValues(category, breed, unit, province, city).Set(price)
}

// RecordPriceChange 记录价格变化
func (c *Collector) RecordPriceChange(category, breed string, changePercent, changeAbs float64) {
	c.PriceChange24H.WithLabelValues(category, breed).Set(changePercent)
}

// RecordVolatility 记录波动指数
func (c *Collector) RecordVolatility(category, breed, city string, index float64) {
	c.PriceVolatilityIndex.WithLabelValues(category, breed, city).Set(index)
}

// RecordCrawlerStatus 记录爬虫状态
func (c *Collector) RecordCrawlerStatus(component, status string) {
	val := 0.0
	if status == "running" {
		val = 1.0
	}
	c.CrawlerStatus.WithLabelValues(component, status).Set(val)
	log.Debugf("Crawler status updated: %s = %s", component, status)
}

// RecordAlert 记录告警指标
func (c *Collector) RecordAlert(alertType, category, severity string) {
	c.AlertCount.WithLabelValues(alertType, category, severity).Inc()
	c.AlertLastTrigger.WithLabelValues(alertType, category).SetToCurrentTime()
	log.Debugf("Alert recorded: type=%s, category=%s, severity=%s", alertType, category, severity)
}

// RecordEvaluation 记录评估指标
func (c *Collector) RecordEvaluation(stage string, duration float64) {
	c.EvaluationDuration.WithLabelValues(stage).Observe(duration)
}

// RecordEvaluationStart 记录评估开始
func (c *Collector) RecordEvaluationStart() {
	c.EvaluationCount.Inc()
}

// ClearPriceMetric 清除价格指标（用于下架的品种）
func (c *Collector) ClearPriceMetric(category, breed, unit, province, city string) {
	c.PriceCurrentValue.DeleteLabelValues(category, breed, unit, province, city)
}

// ClearAllMetrics 清除所有指标
func (c *Collector) ClearAllMetrics() {
	log.Info("Clearing all metrics")
	c.PriceCurrentValue.Reset()
	c.PriceChange24H.Reset()
	c.PriceVolatilityIndex.Reset()
	c.CrawlerStatus.Reset()
}

// NewTestCollector 创建测试用的指标收集器（使用自定义注册器）
func NewTestCollector(cfg *config.AlertConfig, registry *prometheus.Registry) *Collector {
	prefix := "agri_"

	collector := &Collector{
		PriceCurrentValue: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "price_current_value",
				Help: "Current agricultural product price",
			},
			[]string{"category", "breed", "unit", "province", "city"},
		),
		PriceChange24H: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "price_change_24h",
				Help: "24h price change percentage",
			},
			[]string{"category", "breed"},
		),
		PriceVolatilityIndex: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "price_volatility_index",
				Help: "Price volatility index (0-1)",
			},
			[]string{"category", "breed", "city"},
		),
		CrawlerStatus: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "crawler_status",
				Help: "Crawler status (1=running, 0=stopped/error)",
			},
			[]string{"component", "status"},
		),
		AlertCount: prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: prefix + "alert_count",
				Help: "Total number of alerts by type and category",
			},
			[]string{"type", "category", "severity"},
		),
		AlertLastTrigger: prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: prefix + "alert_last_trigger",
				Help: "Unix timestamp of last alert trigger",
			},
			[]string{"type", "category"},
		),
		EvaluationCount: prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: prefix + "evaluation_count",
				Help: "Total number of evaluation rounds",
			},
		),
		EvaluationDuration: prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    prefix + "evaluation_duration_seconds",
				Help:    "Duration of evaluation round",
				Buckets: []float64{0.1, 0.5, 1, 5, 10, 30, 60},
			},
			[]string{"stage"},
		),
	}

	// 注册到自定义注册器
	registry.MustRegister(
		collector.PriceCurrentValue,
		collector.PriceChange24H,
		collector.PriceVolatilityIndex,
		collector.CrawlerStatus,
		collector.AlertCount,
		collector.AlertLastTrigger,
		collector.EvaluationCount,
		collector.EvaluationDuration,
	)

	return collector
}
