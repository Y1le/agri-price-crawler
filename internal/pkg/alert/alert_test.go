package alert

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/config"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/notifications"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/metrics"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/store"
	"github.com/prometheus/client_golang/prometheus"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
)

// TestAlertEngine 告警引擎单元测试
func TestAlertEngine(t *testing.T) {
	// 创建测试配置
	cfg := config.DefaultAlertConfig()
	cfg.Enabled = true

	// 创建 Mock 历史存储
	historian := NewMockHistorian()

	// 创建指标收集器
	metricsCollector := metrics.NewCollector(cfg)

	// 创建通知器
	notify := notifications.NewEmailNotification(&cfg.Email)

	// 创建告警引擎
	engine := NewAlertEngine(cfg, historian, metricsCollector, notify)

	if engine == nil {
		t.Fatal("Failed to create alert engine")
	}

	t.Log("Alert engine created successfully")
}

// TestAlertContent 告警内容测试
func TestAlertContent(t *testing.T) {
	alert := &notifications.AlertContent{
		Type:      notifications.AlertTypePriceSpike,
		Severity:  notifications.SeverityWarning,
		Product:   "番茄",
		Price:     8.5,
		ChangePct: 65,
		ChangeAbs: 3.5,
		Region:    "北京市",
		Category:  "蔬菜",
		Timestamp: time.Now(),
		Extra: map[string]string{
			"test": "value",
		},
	}

	if alert.Type != notifications.AlertTypePriceSpike {
		t.Errorf("Expected AlertTypePriceSpike, got %v", alert.Type)
	}

	if alert.Severity != notifications.SeverityWarning {
		t.Errorf("Expected SeverityWarning, got %v", alert.Severity)
	}

	if alert.Product != "番茄" {
		t.Errorf("Expected Product '番茄', got '%s'", alert.Product)
	}

	if alert.Price != 8.5 {
		t.Errorf("Expected Price 8.5, got %f", alert.Price)
	}
}

// TestPriceChangeEvaluator 价格变化评估器测试
func TestPriceChangeEvaluator(t *testing.T) {
	cfg := config.DefaultAlertConfig()

	evaluator := NewPriceChangeEvaluator(cfg)
	if evaluator == nil {
		t.Fatal("Failed to create price change evaluator")
	}

	if evaluator.Name() != "price_change" {
		t.Errorf("Expected name 'price_change', got '%s'", evaluator.Name())
	}
}

// TestVolatilityEvaluator 波动评估器测试
func TestVolatilityEvaluator(t *testing.T) {
	cfg := config.DefaultAlertConfig()

	evaluator := NewVolatilityEvaluator(cfg)
	if evaluator == nil {
		t.Fatal("Failed to create volatility evaluator")
	}

	if evaluator.Name() != "volatility" {
		t.Errorf("Expected name 'volatility', got '%s'", evaluator.Name())
	}
}

// TestStatCalculator 统计计算器测试
func TestStatCalculator(t *testing.T) {
	calc := &StatCalculator{}

	// 测试波动指数计算
	prices := []float64{10, 12, 11, 13, 12, 14, 13}
	index := calc.CalculateVolatilityIndex(prices)

	if index < 0 || index > 1 {
		t.Errorf("Volatility index should be 0-1, got %f", index)
	}

	// 测试百分比变化计算
	change := calc.CalculateChangePercent(100, 150)
	if change != 50 {
		t.Errorf("Expected 50%% change, got %f%%", change)
	}

	// 测试绝对值变化计算
	absChange := calc.CalculateChangeAbsolute(100, 150)
	if absChange != 50 {
		t.Errorf("Expected 50 absolute change, got %f", absChange)
	}
}

// TestInMemoryHistorian 内存历史存储测试
func TestInMemoryHistorian(t *testing.T) {
	historian := NewInMemoryHistorian()

	// 创建测试价格
	now := time.Now()
	prices := []*v1.Price{
		{
			CateName:      "蔬菜",
			BreedName:     "番茄",
			AvgPrice:      5.0,
			AddressDetail: "北京市",
			CreatedAt:     now,
		},
		{
			CateName:      "蔬菜",
			BreedName:     "番茄",
			AvgPrice:      6.0,
			AddressDetail: "北京市",
			CreatedAt:     now.Add(-24 * time.Hour),
		},
	}

	// 保存价格
	if err := historian.SavePriceHistory(prices); err != nil {
		t.Fatalf("Failed to save prices: %v", err)
	}

	// 获取价格历史
	history, err := historian.GetPriceHistory("蔬菜", "番茄", 7)
	if err != nil {
		t.Fatalf("Failed to get history: %v", err)
	}

	if len(history) != 2 {
		t.Errorf("Expected 2 price records, got %d", len(history))
	}

	// 创建新的 InMemoryHistorian 实例来测试 GetLatest 方法
	historian2 := NewInMemoryHistorian()
	historian2.SavePriceHistory(prices)
	latest, err := historian2.GetLatest("番茄", 1)
	if err != nil {
		t.Fatalf("Failed to get latest: %v", err)
	}

	if len(latest) != 1 {
		t.Errorf("Expected 1 latest price, got %d", len(latest))
	}
}

// TestPriceSnapshot 价格快照测试
func TestPriceSnapshot(t *testing.T) {
	historian := NewInMemoryHistorian()

	// 创建测试价格
	now := time.Now()

	prices := []*v1.Price{
		{
			CateName:      "水果",
			BreedName:     "苹果",
			AvgPrice:      8.0,
			AddressDetail: "山东省",
			CreatedAt:     now,
		},
		{
			CateName:      "水果",
			BreedName:     "苹果",
			AvgPrice:      7.5,
			AddressDetail: "山东省",
			CreatedAt:     now.Add(-24 * time.Hour),
		},
		{
			CateName:      "水果",
			BreedName:     "苹果",
			AvgPrice:      8.5,
			AddressDetail: "山东省",
			CreatedAt:     now.Add(-48 * time.Hour),
		},
	}

	if err := historian.SavePriceHistory(prices); err != nil {
		t.Fatalf("Failed to save prices: %v", err)
	}

	// 创建新的 InMemoryHistorian 实例来测试 GetPriceSnapshot 方法
	historian2 := NewInMemoryHistorian()
	historian2.SavePriceHistory(prices)
	// 获取快照
	snapshot, err := historian2.GetPriceSnapshot("水果", "苹果")
	if err != nil {
		t.Fatalf("Failed to get snapshot: %v", err)
	}

	if snapshot == nil {
		t.Fatal("Snapshot should not be nil")
	}

	if snapshot.Breed != "苹果" {
		t.Errorf("Expected breed '苹果', got '%s'", snapshot.Breed)
	}

	if snapshot.CurrentPrice != 8.0 {
		t.Errorf("Expected current price 8.0, got %f", snapshot.CurrentPrice)
	}
}

// TestGetLatestPrice 获取最新价格测试
func TestGetLatestPrice(t *testing.T) {
	now := time.Now()

	prices := []*v1.Price{
		{CreatedAt: now.Add(-1000 * time.Millisecond), AvgPrice: 10},
		{CreatedAt: now, AvgPrice: 20},
		{CreatedAt: now.Add(-500 * time.Millisecond), AvgPrice: 15},
	}

	latest := getLatestPrice(prices)
	if latest.AvgPrice != 20 {
		t.Errorf("Expected latest price 20, got %f", latest.AvgPrice)
	}
}

// TestGetAveragePrice 获取平均价格测试
func TestGetAveragePrice(t *testing.T) {
	prices := []*v1.Price{
		{AvgPrice: 10},
		{AvgPrice: 20},
		{AvgPrice: 30},
	}

	avg := getAveragePrice(prices)
	if avg != 20 {
		t.Errorf("Expected average 20, got %f", avg)
	}
}

// TestSortPricesByTime 时间排序测试
func TestSortPricesByTime(t *testing.T) {
	now := time.Now()

	prices := []*v1.Price{
		{CreatedAt: now.Add(-2 * time.Second)},
		{CreatedAt: now},
		{CreatedAt: now.Add(-1 * time.Second)},
	}

	sortPricesByTime(prices)

	// 比较时间（使用 UnixNano 进行比较）
	if prices[0].CreatedAt.UnixNano() != now.Add(-2*time.Second).UnixNano() {
		t.Errorf("Expected first price to be oldest")
	}

	if prices[len(prices)-1].CreatedAt.UnixNano() != now.UnixNano() {
		t.Errorf("Expected last price to be newest")
	}
}

// TestAlertSuppress 告警抑制测试
func TestAlertSuppress(t *testing.T) {
	now := time.Now()

	// 测试相同告警在5分钟内被抑制
	key := "test_key"

	// 第一次告警不应被抑制
	if !shouldAlertTest(key, now) {
		t.Error("First alert should not be suppressed")
	}

	// 第二次立即触发应该被抑制
	if shouldAlertTest(key, now) {
		t.Error("Second alert within 5 minutes should be suppressed")
	}
}

// shouldAlertTest shouldAlert 的测试版本（使用 sync.Map 模拟）
var alertTimes sync.Map

func shouldAlertTest(key string, now time.Time) bool {
	if lastTime, ok := alertTimes.Load(key); ok {
		if lastTimeTime, ok := lastTime.(time.Time); ok {
			if now.Sub(lastTimeTime) < 5*time.Minute {
				return false // 被抑制
			}
		}
	}
	alertTimes.Store(key, now)
	return true // 不被抑制
}

// TestFormatPrice 格式化价格测试
func TestFormatPrice(t *testing.T) {
	result := FormatPrice(12.345)
	if result != "12.35" {
		t.Errorf("Expected '12.35', got '%s'", result)
	}
}

// TestFormatPercentage 格式化百分比测试
func TestFormatPercentage(t *testing.T) {
	result := FormatPercentage(50.5)
	if result != "+50.50%" {
		t.Errorf("Expected '+50.50%%', got '%s'", result)
	}

	result = FormatPercentage(-25.0)
	if result != "-25.00%" {
		t.Errorf("Expected '-25.00%%', got '%s'", result)
	}
}

// TestCalculatePriceRange 计算价格范围测试
func TestCalculatePriceRange(t *testing.T) {
	prices := []float64{10, 20, 15, 25, 30}
	rng := CalculatePriceRange(prices)

	if rng.Min != 10 {
		t.Errorf("Expected min 10, got %f", rng.Min)
	}

	if rng.Max != 30 {
		t.Errorf("Expected max 30, got %f", rng.Max)
	}

	if rng.Avg != 20 {
		t.Errorf("Expected avg 20, got %f", rng.Avg)
	}
}

// TestCalculatePriceChange 计算价格变化测试
func TestCalculatePriceChange(t *testing.T) {
	change := CalculatePriceChange(100, 150)

	if change.Absolute != 50 {
		t.Errorf("Expected absolute 50, got %f", change.Absolute)
	}

	if change.Percent != 50 {
		t.Errorf("Expected percent 50, got %f", change.Percent)
	}

	if !change.IsIncrease {
		t.Error("Expected IsIncrease true")
	}
}

// TestHistorianClear 清空历史测试
func TestHistorianClear(t *testing.T) {
	historian := NewMockHistorian()

	now := time.Now()
	prices := []*v1.Price{
		{
			CateName:      "蔬菜",
			BreedName:     "番茄",
			AvgPrice:      5.0,
			AddressDetail: "北京市",
			CreatedAt:     now,
		},
	}

	historian.SavePriceHistory(prices)

	// 清空
	historian.Clear()

	// 验证清空
	history, err := historian.GetPriceHistory("蔬菜", "番茄", 7)
	if err != nil {
		t.Fatalf("Failed to get history: %v", err)
	}

	if len(history) != 0 {
		t.Errorf("Expected 0 prices after clear, got %d", len(history))
	}
}

// Test alert content processing
func TestAlertContentProcessing(t *testing.T) {
	alert := &notifications.AlertContent{
		Type:      notifications.AlertTypePriceSpike,
		Severity:  notifications.SeverityCritical,
		Product:   "黄瓜",
		Price:     6.5,
		ChangePct: 80,
		ChangeAbs: 3.0,
		Region:    "上海市",
		Category:  "蔬菜",
		Timestamp: time.Now(),
		Extra: map[string]string{
			"source": "cnhnb.com",
		},
	}

	// 验证告警内容
	if alert.Type != notifications.AlertTypePriceSpike {
		t.Errorf("Expected price spike alert, got %v", alert.Type)
	}

	if alert.Severity != notifications.SeverityCritical {
		t.Errorf("Expected critical severity, got %v", alert.Severity)
	}
}

// TestPriceRange 结构体字段测试
func TestPriceRangeFields(t *testing.T) {
	rng := &PriceRange{
		Min: 5.0,
		Max: 15.0,
		Avg: 10.0,
	}

	if rng.Min != 5.0 {
		t.Errorf("Expected Min 5.0, got %f", rng.Min)
	}

	if rng.Max != 15.0 {
		t.Errorf("Expected Max 15.0, got %f", rng.Max)
	}

	if rng.Avg != 10.0 {
		t.Errorf("Expected Avg 10.0, got %f", rng.Avg)
	}
}

// TestPriceChange 结构体字段测试
func TestPriceChangeFields(t *testing.T) {
	change := &PriceChange{
		Absolute:   10.0,
		Percent:    20.0,
		IsIncrease: true,
	}

	if change.Absolute != 10.0 {
		t.Errorf("Expected Absolute 10.0, got %f", change.Absolute)
	}

	if change.Percent != 20.0 {
		t.Errorf("Expected Percent 20.0, got %f", change.Percent)
	}

	if !change.IsIncrease {
		t.Error("Expected IsIncrease true")
	}
}

// TestgetAllMethods GetAll 方法测试
func TestGetAllMethods(t *testing.T) {
	// 使用测试专用的 Prometheus 注册器避免冲突
	registry := prometheus.NewRegistry()
	cfg := config.DefaultAlertConfig()
	collector := metrics.NewTestCollector(cfg, registry)

	// 验证指标收集器不为 nil
	if collector == nil {
		t.Fatal("Metrics collector should not be nil")
	}
}

// TestInMemoryPriceHistoryStore 内存价格历史存储测试
func TestInMemoryPriceHistoryStore(t *testing.T) {
	store := store.NewInMemoryPriceHistoryStore()

	if store == nil {
		t.Fatal("Store should not be nil")
	}
}

// TestStoreSaveBatch 保存批次测试
func TestStoreSaveBatch(t *testing.T) {
	store := store.NewInMemoryPriceHistoryStore()

	now := time.Now()
	prices := []*v1.Price{
		{
			CateName:      "水果",
			BreedName:     "香蕉",
			AvgPrice:      7.0,
			AddressDetail: "广东省",
			CreatedAt:     now,
		},
	}

	if err := store.SaveBatch(context.Background(), prices); err != nil {
		t.Fatalf("Failed to save batch: %v", err)
	}
}

// TestPriceSnapshotEmpty 当没有价格时的快照测试
func TestPriceSnapshotEmpty(t *testing.T) {
	// 获取空历史的快照
	_, err := NewInMemoryHistorian().GetPriceSnapshot("未知", "品种")
	if err == nil {
		t.Error("Expected error for missing price history")
	}
}

// TestGetLatestPriceEmpty 空价格列表测试
func TestGetLatestPriceEmpty(t *testing.T) {
	prices := []*v1.Price{}
	latest := getLatestPrice(prices)

	if latest != nil {
		t.Error("Expected nil for empty prices")
	}
}

// TestGetAveragePriceEmpty 空价格列表平均值测试
func TestGetAveragePriceEmpty(t *testing.T) {
	prices := []*v1.Price{}
	avg := getAveragePrice(prices)

	if avg != 0 {
		t.Errorf("Expected 0 for empty prices, got %f", avg)
	}
}

// TestSortPricesByTimeEmpty 空列表排序测试
func TestSortPricesByTimeEmpty(t *testing.T) {
	prices := []*v1.Price{}
	sortPricesByTime(prices)

	if len(prices) != 0 {
		t.Error("Expected empty slice after sort")
	}
}

// TestCalculatePriceRangeEmpty 空价格范围测试
func TestCalculatePriceRangeEmpty(t *testing.T) {
	rng := CalculatePriceRange([]float64{})

	// 验证返回的范围结构
	if rng == nil {
		t.Error("Expected non-nil range for empty input")
	}
}

// TestStatCalculatorEdgeCases 边界情况测试
func TestStatCalculatorEdgeCases(t *testing.T) {
	calc := &StatCalculator{}

	// 单价格波动指数
	index := calc.CalculateVolatilityIndex([]float64{10})
	if index != 0 {
		t.Errorf("Expected 0 for single price, got %f", index)
	}

	// 零旧价格变化
	change := calc.CalculateChangePercent(0, 100)
	if change != 0 {
		t.Errorf("Expected 0 for zero old price, got %f", change)
	}
}
