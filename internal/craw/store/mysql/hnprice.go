package mysql

import (
	"context"

	"github.com/Y1le/agri-price-crawler/internal/pkg/util/gormutil"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/marmotedu/component-base/pkg/fields"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
	"gorm.io/gorm"
)

type prices struct {
	db *gorm.DB
}

func newHNPrices(db *gorm.DB) *prices {
	return &prices{db: db}
}

func (p *prices) Save(ctx context.Context, price []*v1.Price) error {
	return p.db.Save(&price).Error
}

func (p *prices) List(ctx context.Context, opts metav1.ListOptions) (*v1.PriceList, error) {
	priceList := &v1.PriceList{}
	ol := gormutil.Unpointer(opts.Offset, opts.Limit)
	// CateName
	// BreedName
	selector, _ := fields.ParseSelector(opts.FieldSelector)
	db := p.db.Model(&v1.User{})
	if cateName, ok := selector.RequiresExactMatch("cate_name"); ok {
		db = db.Where("cate_name = ?", cateName)
	}
	if breeName, ok := selector.RequiresExactMatch("bree_name"); ok {
		db = db.Where("bree_name = ?", breeName)
	}
	d := db.Offset(ol.Offset).
		Limit(ol.Limit).
		Order("createdAt desc").
		Find(&priceList.Items).
		Offset(-1).
		Limit(-1).
		Count(&priceList.TotalCount)

	return priceList, d.Error

}
