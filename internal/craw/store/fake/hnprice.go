package fake

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/pkg/util/gormutil"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	"github.com/marmotedu/component-base/pkg/fields"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
)

type hnprices struct {
	ds *datastore
}

func newHNPrices(ds *datastore) *hnprices {
	return &hnprices{ds}
}

// Save saves implements store.PriceStore.
func (p *hnprices) Save(ctx context.Context, price []*v1.Price) error {
	p.ds.Lock()
	defer p.ds.Unlock()

	if len(p.ds.users) > 0 {
		for i := 0; i < len(price); i++ {
			price[i].ID = p.ds.prices[len(p.ds.prices)-1].ID + 1
		}
	}
	p.ds.prices = append(p.ds.prices, price...)
	return nil
}

// List return all prices.
func (p *hnprices) List(ctx context.Context, opts metav1.ListOptions) (*v1.PriceList, error) {
	p.ds.Lock()
	defer p.ds.Unlock()

	ol := gormutil.Unpointer(opts.Offset, opts.Limit)

	selector, _ := fields.ParseSelector(opts.FieldSelector)
	cateName, haveCateName := selector.RequiresExactMatch("cateName")
	breedName, haveBreeName := selector.RequiresExactMatch("breedName")
	var nextDay time.Time
	if createdAtStr, ok := selector.RequiresExactMatch("createdAt"); ok {
		targetDate, err := time.Parse("2006-01-02", createdAtStr)
		if err != nil {
			return nil, fmt.Errorf("invalid date: %v", err)
		}

		start := time.Date(targetDate.Year(), targetDate.Month(), targetDate.Day(), 0, 0, 0, 0, time.UTC)
		nextDay = start.Add(24 * time.Hour)
	}
	addressDetail, haveAddressDetail := selector.RequiresExactMatch("addressDetail")

	prices := make([]*v1.Price, 0)
	i := 0
	for _, price := range p.ds.prices {
		if i == ol.Limit {
			break
		}
		if haveCateName && !strings.Contains(price.CateName, cateName) {
			continue
		}
		if haveBreeName && !strings.Contains(price.BreedName, breedName) {
			continue
		}
		if !nextDay.IsZero() && (price.CreatedAt.Before(nextDay) || price.CreatedAt.Equal(nextDay)) {
			continue
		}
		if haveAddressDetail && !strings.HasPrefix(price.AddressDetail, addressDetail) {
			continue
		}
		prices = append(prices, price)
		i++
	}

	return &v1.PriceList{
		TotalCount: int64(len(prices)),

		Items: prices,
	}, nil
}
