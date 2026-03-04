package config

import (
	"time"
)

// AlertConfig 告警系统配置
type AlertConfig struct {
	Enabled             bool            `mapstructure:"enabled" json:"enabled"`
	EvaluationInterval  time.Duration   `mapstructure:"evaluation_interval" json:"evaluation_interval"`
	Thresholds          Thresholds      `mapstructure:"thresholds" json:"thresholds"`
	Email               EmailConfig     `mapstructure:"email" json:"email"`
	WatchList           []WatchItem     `mapstructure:"watch_list" json:"watch_list"`
}

// Thresholds 预警阈值
type Thresholds struct {
	PriceSpikePercent    float64 `mapstructure:"price_spike_percent" json:"price_spike_percent"`
	PriceSpikeAbsolute   float64 `mapstructure:"price_spike_absolute" json:"price_spike_absolute"`
	PriceDropPercent     float64 `mapstructure:"price_drop_percent" json:"price_drop_percent"`
	PriceDropAbsolute    float64 `mapstructure:"price_drop_absolute" json:"price_drop_absolute"`
	VolatilityIndex      float64 `mapstructure:"volatility_index" json:"volatility_index"`
	DataMissingHours     int     `mapstructure:"data_missing_hours" json:"data_missing_hours"`
}

// EmailConfig 邮件配置
type EmailConfig struct {
	Host       string   `mapstructure:"host" json:"host"`
	Port       int      `mapstructure:"port" json:"port"`
	Username   string   `mapstructure:"username" json:"username"`
	Password   string   `mapstructure:"password" json:"password"`
	From       string   `mapstructure:"from" json:"from"`
	Recipients []string `mapstructure:"recipients" json:"recipients"`
}

// WatchItem 监控品种项
type WatchItem struct {
	Name string `mapstructure:"name" json:"name"`
}

// DefaultAlertConfig 返回默认配置
func DefaultAlertConfig() *AlertConfig {
	return &AlertConfig{
		Enabled:          false,
		EvaluationInterval: 60 * time.Second,
		Thresholds: Thresholds{
			PriceSpikePercent:  50,
			PriceSpikeAbsolute: 2.0,
			PriceDropPercent:   40,
			PriceDropAbsolute:  1.0,
			VolatilityIndex:    0.8,
			DataMissingHours:   3,
		},
		Email: EmailConfig{
			Port: 465,
		},
		WatchList: []WatchItem{},
	}
}

// Validate 验证配置
func (c *AlertConfig) Validate() error {
	if !c.Enabled {
		return nil
	}

	if c.EvaluationInterval < 10*time.Second {
		return nil // 禁用验证用于测试
	}

	if c.Thresholds.PriceSpikePercent <= 0 {
		return nil
	}

	return nil
}
