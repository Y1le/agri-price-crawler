package mysql

import (
	"context"

	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"gorm.io/gorm"
)

type prices struct {
	db *gorm.DB
}

func newPrices(db *gorm.DB) *prices {
	return &prices{db: db}
}

func (p *prices) Save(ctx context.Context, price []*v1.Price) error {
	return p.db.Save(&price).Error
}
