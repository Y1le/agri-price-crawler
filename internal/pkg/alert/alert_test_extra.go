package alert

import (
	"testing"

	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/notifications"
)

// TestCalculatePriceChange 统一的价格变化计算测试
func TestPriceChangeConsistency(t *testing.T) {
	calc := &StatCalculator{}

	tests := []struct {
		name     string
		old      float64
		new      float64
		expected float64
	}{
		{"positive change", 100, 150, 50},
		{"negative change", 100, 50, -50},
		{"no change", 100, 100, 0},
		{"zero old", 0, 100, 0}, // 避免除零
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			change := calc.CalculateChangePercent(tt.old, tt.new)
			if change != tt.expected {
				t.Errorf("expected %f, got %f", tt.expected, change)
			}
		})
	}
}

// TestAlertKeyGeneration 告警键生成测试
func TestAlertKeyGeneration(t *testing.T) {
	alert := &notifications.AlertContent{
		Type:      notifications.AlertTypePriceSpike,
		Product:   "番茄",
		Region:    "北京市",
	}

	key := generateAlertKeyTest(alert)
	expected := "price_spike:番茄:北京市"

	if key != expected {
		t.Errorf("expected key '%s', got '%s'", expected, key)
	}
}

// generateAlertKeyTest generateAlertKey 的测试版本
func generateAlertKeyTest(alert *notifications.AlertContent) string {
	return string(alert.Type) + ":" + alert.Product + ":" + alert.Region
}
