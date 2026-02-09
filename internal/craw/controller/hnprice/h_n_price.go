package hnprice

import (
	srvv1 "github.com/Y1le/agri-price-crawler/internal/craw/service/v1"
	"github.com/Y1le/agri-price-crawler/internal/craw/store"
)

type HNPriceController struct {
	srv srvv1.Service
}

func NewHNPriceController(store store.Factory) *HNPriceController {
	return &HNPriceController{
		srv: srvv1.NewService(store),
	}
}
