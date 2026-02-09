package hnprice

import (
	"github.com/Y1le/agri-price-crawler/internal/pkg/code"
	"github.com/Y1le/agri-price-crawler/pkg/log"
	"github.com/gin-gonic/gin"
	"github.com/marmotedu/component-base/pkg/core"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"github.com/marmotedu/errors"
)

func (h *HNPriceController) List(c *gin.Context) {
	log.L(c).Info("list price function called.")

	var r metav1.ListOptions
	if err := c.ShouldBindQuery(&r); err != nil {
		core.WriteResponse(c, errors.Errorf("%d, %s", code.ErrBind, err.Error()), nil)

		return
	}

	prices, err := h.srv.HNPrices().List(c, r)
	if err != nil {
		core.WriteResponse(c, err, nil)

		return
	}

	core.WriteResponse(c, nil, prices)
}
