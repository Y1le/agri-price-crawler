package store

import (
	"context"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
)

type HNPriceStore interface {
	Save(ctx context.Context, price []*v1.Price) error
	List(ctx context.Context, opts metav1.ListOptions) (*v1.PriceList, error)
}
