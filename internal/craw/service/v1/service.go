package v1

import "github.com/Y1le/agri-price-crawler/internal/craw/store"

type Service interface {
	HNPrices() HNPriceSrv
	Users() UserSrv
	Subscribes() SubscribeSrv
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

func (s *service) Users() UserSrv {
	return newUser(s)
}

func (s *service) Subscribes() SubscribeSrv {
	return newSubscribe(s)
}
