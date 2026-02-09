package subscribe

import (
	"github.com/Y1le/agri-price-crawler/internal/pkg/code"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
	"github.com/gin-gonic/gin"
	"github.com/marmotedu/component-base/pkg/core"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"github.com/marmotedu/errors"
)

// Create 处理创建订阅的HTTP请求
// 接收JSON格式的订阅数据，验证后保存到数据库
func (s *SubscribeController) Create(c *gin.Context) {
	log.L(c).Info("SubscribeController.Create")
	var r v1.Subscribe
	if err := c.ShouldBindJSON(&r); err != nil {
		core.WriteResponse(c, errors.Errorf("%d: %v", code.ErrBind, err), nil)

		return
	}

	if err := r.Validate(); len(err) != 0 {
		core.WriteResponse(c, errors.Errorf("%d: %v", code.ErrValidation, err.ToAggregate().Error()), nil)

		return
	}

	if err := s.srv.Subscribes().Create(c, &r, metav1.CreateOptions{}); err != nil {
		core.WriteResponse(c, err, nil)

		return
	}

	core.WriteResponse(c, nil, r)
}
