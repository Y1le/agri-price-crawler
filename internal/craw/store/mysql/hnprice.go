package mysql

import (
	"context"
	"fmt"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/pkg/util/gormutil"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/Y1le/agri-price-crawler/pkg/log"
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
	db := p.db.Model(&v1.Price{})
	if cateName, ok := selector.RequiresExactMatch("cateName"); ok {
		db = db.Where("cateName = ?", cateName)
	}
	if breeName, ok := selector.RequiresExactMatch("breeName"); ok {
		db = db.Where("breeName = ?", breeName)
	}
	if createdAtStr, ok := selector.RequiresExactMatch("createdAt"); ok {
		targetDate, err := time.Parse("2006-01-02", createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("invalid date: %v", err)
		}

		start := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, time.UTC)
		nextDay := start.Add(24 * time.Hour)

		db = db.Where("createdAt >= ?", start).Where("createdAt < ?", nextDay)
	}
	if addressDetail, ok := selector.RequiresExactMatch("addressDetail"); ok {
		db = db.Where("addressDetail LIKE ?", addressDetail+"%")
	}
	addressDetail, _ := selector.RequiresExactMatch("addressDetail")
	log.Debugf("opts: %v", addressDetail)
	d := db.Offset(ol.Offset).
		Limit(ol.Limit).
		Order("createdAt desc").
		Find(&priceList.Items).
		Offset(-1).
		Limit(-1).
		Count(&priceList.TotalCount)

	return priceList, d.Error

}
