package subscribe

import (
	"github.com/Y1le/agri-price-crawler/pkg/log"
	"github.com/gin-gonic/gin"
	"github.com/marmotedu/component-base/pkg/core"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
)

func (s *SubscribeController) Delete(c *gin.Context) {
	log.L(c).Info("SubscribeController.Delete")

	if err := s.srv.Subscribes().Delete(c, c.Param("email"), metav1.DeleteOptions{Unscoped: true}); err != nil {
		core.WriteResponse(c, err, nil)

		return
	}

	core.WriteResponse(c, nil, nil)
}
