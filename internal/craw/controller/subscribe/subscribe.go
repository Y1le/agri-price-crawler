package subscribe

import (
	srvv1 "github.com/Y1le/agri-price-crawler/internal/craw/service/v1"
	"github.com/Y1le/agri-price-crawler/internal/craw/store"
)

type SubscribeController struct {
	srv srvv1.Service
}

func NewSubscribeController(store store.Factory) *SubscribeController {
	return &SubscribeController{srv: srvv1.NewService(store)}
}
