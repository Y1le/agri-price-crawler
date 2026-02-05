package store

import (
	"context"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
)

type PriceStorage interface {
	Save(ctx context.Context, price []*v1.Price) error
}
