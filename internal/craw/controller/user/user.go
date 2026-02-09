package user

import (
	srvv1 "github.com/Y1le/agri-price-crawler/internal/craw/service/v1"
	"github.com/Y1le/agri-price-crawler/internal/craw/store"
)

type UserController struct {
	srv srvv1.Service
}

func NewUserController(store store.Factory) *UserController {
	return &UserController{
		srv: srvv1.NewService(store),
	}
}
