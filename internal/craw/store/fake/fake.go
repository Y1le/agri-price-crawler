package fake

import (
	"fmt"
	"sync"
	"time"

	"github.com/Y1le/agri-price-crawler/internal/craw/store"
	v1 "github.com/Y1le/agri-price-crawler/pkg/api/v1"
	metav1 "github.com/marmotedu/component-base/pkg/meta/v1"
)

// ResourceCount is the count of fake resources.
const ResourceCount = 1000

type datastore struct {
	sync.RWMutex
	users      []*v1.User
	prices     []*v1.Price
	subscribes []*v1.Subscribe
}

func (ds *datastore) Users() store.UserStore {
	return newUsers(ds)
}
func (ds *datastore) HNPrices() store.HNPriceStore {
	return newHNPrices(ds)
}

func (ds *datastore) Subscribes() store.SubscribeStore {
	return newSubscribes(ds)
}

func (ds *datastore) Close() error {
	return nil
}

var (
	fakeFactory store.Factory
	once        sync.Once
)

func GetFakeFactoryOr() (store.Factory, error) {
	once.Do(func() {
		fakeFactory = &datastore{
			users:      FakeUsers(ResourceCount),
			prices:     FakePrices(ResourceCount),
			subscribes: FakeSubscribes(ResourceCount),
		}
	})

	if fakeFactory == nil {
		return nil, fmt.Errorf("failed to get fake store fatory, fakeFactory: %+v", fakeFactory)
	}

	return fakeFactory, nil
}

func FakeUsers(count int) []*v1.User {
	users := make([]*v1.User, 0)
	for i := 0; i < count; i++ {
		users = append(users, &v1.User{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("user-%d", i),
				ID:   uint64(i),
			},
			Nickname: fmt.Sprintf("nickname-%d", i),
			Password: fmt.Sprintf("password-%d@2026", i),
			Email:    fmt.Sprintf("user-%d@example.com", i),
		})
	}
	return users
}

func FakePrices(count int) []*v1.Price {
	prices := make([]*v1.Price, 0)
	for i := 0; i < count; i++ {
		prices = append(prices, &v1.Price{
			ID:                uint64(i),
			CreatedAt:         time.Now(),
			UpdatedAt:         time.Now(),
			FirstCateID:       uint64(i),
			SecondCateID:      uint64(i),
			CateID:            uint64(i),
			CateName:          fmt.Sprintf("cateName-%d", i),
			BreedName:         fmt.Sprintf("breedName-%d", i),
			MinPrice:          float64(i),
			MaxPrice:          float64(i),
			AvgPrice:          float64(i),
			WeightingAvgPrice: float64(i),
			UpDownPrice:       float64(i),
			Increase:          float64(i),
			Unit:              fmt.Sprintf("unit-%d", i),
			AddressDetail:     fmt.Sprintf("addressDetail-%d", i),
			ProvinceID:        uint32(i),
			CityID:            uint32(i),
			AreaID:            uint32(i),
			StatisNum:         uint32(i),
			SourceType:        fmt.Sprintf("sourceType-%d", i),
			Trend:             int8(i),
			TraceID:           fmt.Sprintf("traceId-%d", i),
		})
	}
	return prices
}

func FakeSubscribes(count int) []*v1.Subscribe {
	subscribes := make([]*v1.Subscribe, 0)
	for i := 0; i < count; i++ {
		subscribes = append(subscribes, &v1.Subscribe{
			ObjectMeta: metav1.ObjectMeta{
				Name: fmt.Sprintf("subscribe-%d", i),
				ID:   uint64(i),
			},
			Email:         fmt.Sprintf("subscribe-%d@example.com", i),
			City:          fmt.Sprintf("city-%d", i),
			FavoriteFoods: fmt.Sprintf("favoriteFoods-%d", i),
			DislikeFoods:  fmt.Sprintf("dislikeFoods-%d", i),
		})
	}
	return subscribes
}
