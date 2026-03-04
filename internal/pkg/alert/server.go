package alert

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/config"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/metrics"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/notifications"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

// Server 告警系统服务器
type Server struct {
	cfg       *config.AlertConfig
	engine    *AlertEngine
	metrics   *metrics.Collector
	registry  *metrics.Collector
	historian Historian
}

// NewServer 创建告警系统服务器
func NewServer(cfg *config.AlertConfig, registry *metrics.Collector) (*Server, error) {
	// 创建历史存储
	historian := NewInMemoryHistorian()

	// 创建指标收集器
	metricsCollector := registry

	// 创建邮件通知器
	var notify *notifications.EmailNotification
	if cfg.Enabled && cfg.Email.Host != "" {
		notify = notifications.NewEmailNotification(&cfg.Email)
	}

	// 创建告警引擎
	engine := NewAlertEngine(cfg, historian, metricsCollector, notify)

	return &Server{
		cfg:       cfg,
		engine:    engine,
		metrics:   metricsCollector,
		registry:  registry,
		historian: historian,
	}, nil
}

// Run 启动告警服务器
func (s *Server) Run(ctx context.Context) error {
	if !s.cfg.Enabled {
		log.Info("Alert server disabled")
		return nil
	}

	log.Info("Starting alert server...")

	// 启动告警引擎
	go s.engine.Run(ctx)

	// 启动健康检查端点
	go s.startHealthEndpoint(ctx)

	log.Infof("Alert server started, evaluation interval: %v", s.cfg.EvaluationInterval)
	return nil
}

// startHealthEndpoint 启动健康检查端点
func (s *Server) startHealthEndpoint(ctx context.Context) {
	http.HandleFunc("/health/alerts", func(w http.ResponseWriter, r *http.Request) {
		status := map[string]interface{}{
			"status":     "ok",
			"enabled":    s.cfg.Enabled,
			"timestamp":  time.Now().Unix(),
		}

		if !s.cfg.Enabled {
			status["message"] = "Alert system is disabled"
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(fmt.Sprintf(`%v`, status)))
	})

	log.Info("Alert health endpoint started at /health/alerts")
}

// MetricsMiddleware 返回 Prometheus 指标中间件
func (s *Server) MetricsMiddleware() func(http.Handler) http.Handler {
	return metrics.Middleware()
}

// UpdatePrice 更新价格并触发告警评估
func (s *Server) UpdatePrice(price *v1.Price) {
	if !s.cfg.Enabled {
		return
	}

	s.engine.UpdatePrice(price)
}

// GetStatus 获取告警系统状态
func (s *Server) GetStatus() map[string]interface{} {
	status := s.engine.GetStatus()
	status["config"] = map[string]interface{}{
		"enabled":           s.cfg.Enabled,
		"evaluation_interval": s.cfg.EvaluationInterval.String(),
	}

	// 添加历史存储状态
	if s.historian != nil {
		if hist, ok := s.historian.(*InMemoryHistorian); ok {
			status["history"] = hist.GetStats()
		}
	}

	return status
}

// Shutdown 关闭告警服务器
func (s *Server) Shutdown() error {
	log.Info("Shutting down alert server...")
	return nil
}
