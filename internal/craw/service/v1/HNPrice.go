package v1

import "github.com/Y1le/agri-price-crawler/internal/craw/store"

type HNPriceSrv interface {
	List(page, pageSize int) error
}

type hNPriceService struct {
	store store.Factory
}

var _ HNPriceSrv = (*hNPriceService)(nil)

func newHNPrice(s *service) *hNPriceService {
	return &hNPriceService{store: s.store}
}

func (h *hNPriceService) List(page, pageSize int) error {
	// Implement the logic to list HN prices with pagination
	return nil
}
