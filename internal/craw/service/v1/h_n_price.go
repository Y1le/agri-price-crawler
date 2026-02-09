package v1

import (
	"context"

	"github.com/Y1le/agri-price-crawler/internal/craw/store"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
)

type HNPriceSrv interface {
	List(ctx context.Context, opts metav1.ListOptions) (*v1.PriceList, error)
}

type hNPriceService struct {
	store store.Factory
}

var _ HNPriceSrv = (*hNPriceService)(nil)

func newHNPrice(s *service) *hNPriceService {
	return &hNPriceService{store: s.store}
}

func (h *hNPriceService) List(c context.Context, opts metav1.ListOptions) (*v1.PriceList, error) {
	return h.store.HNPrices().List(c, opts)
}
