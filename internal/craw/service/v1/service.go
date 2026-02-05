package v1

import "github.com/Y1le/agri-price-crawler/internal/craw/store"

type Service interface {
	HNPrices() HNPriceSrv
}

type service struct {
	store store.Factory
}

// NewService returns Service interface.
func NewService(store store.Factory) Service {
	return &service{
		store: store,
	}
}
func (s *service) HNPrices() HNPriceSrv {
	return newHNPrice(s)
}
