package alert

import (
	"context"

	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/config"
	"github.com/Y1le/agri-price-crawler/internal/pkg/alert/notifications"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
)

// MissingDataEvaluator 数据缺失评估器
type MissingDataEvaluator struct {
	cfg       *config.AlertConfig
	historian Historian
}

// NewMissingDataEvaluator 创建数据缺失评估器
func NewMissingDataEvaluator(cfg *config.AlertConfig, historian Historian) *MissingDataEvaluator {
	return &MissingDataEvaluator{
		cfg:       cfg,
		historian: historian,
	}
}

// Name 返回评估器名称
func (e *MissingDataEvaluator) Name() string {
	return "missing_data"
}

// Evaluate 评估数据缺失情况
func (e *MissingDataEvaluator) Evaluate(ctx context.Context, prices []*v1.Price) ([]*notifications.AlertContent, error) {
	alerts := []*notifications.AlertContent{}

	// 这里应该检查历史数据中是否有长时间缺失的情况
	// 由于实际的 historian 接口还未集成，这里返回空

	// TODO: 实现数据缺失检测逻辑
	log.Debug("MissingDataEvaluator: checking for missing data")

	return alerts, nil
}

// GetHistorian 获取历史存储
func (e *MissingDataEvaluator) GetHistorian() Historian {
	return e.historian
}
