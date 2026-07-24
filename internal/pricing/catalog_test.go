package pricing_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Y1le/agri-price-crawler/internal/pricing"
	"github.com/google/uuid"
)

func TestCatalogUsesStablePaginationAndOpaqueCursor(t *testing.T) {
	categoryID := uuid.MustParse("10000000-0000-0000-0000-000000000001")
	firstID := uuid.MustParse("20000000-0000-0000-0000-000000000001")
	secondID := uuid.MustParse("20000000-0000-0000-0000-000000000002")
	repository := &catalogRepositoryStub{products: []pricing.Product{
		{ID: firstID, CategoryID: categoryID, Name: "西红柿", Slug: "tomato", Active: true},
		{ID: secondID, CategoryID: categoryID, Name: "黄瓜", Slug: "cucumber", Active: true},
	}}
	catalog := pricing.NewCatalog(repository)

	page, err := catalog.ListProducts(context.Background(), pricing.ProductQuery{CategoryID: &categoryID, Limit: 1})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != firstID || page.NextCursor == "" {
		t.Fatalf("first page = %+v", page)
	}
	if repository.productQuery.Limit != 2 {
		t.Fatalf("repository limit = %d, want limit plus one", repository.productQuery.Limit)
	}

	_, err = catalog.ListProducts(context.Background(), pricing.ProductQuery{Cursor: page.NextCursor})
	if !errors.Is(err, pricing.ErrInvalidCursor) {
		t.Fatalf("cursor without matching category error = %v, want ErrInvalidCursor", err)
	}

	page, err = catalog.ListProducts(context.Background(), pricing.ProductQuery{CategoryID: &categoryID, Limit: 1, Cursor: page.NextCursor})
	if err != nil {
		t.Fatal(err)
	}
	if len(page.Items) != 1 || page.Items[0].ID != secondID || page.NextCursor != "" {
		t.Fatalf("second page = %+v", page)
	}
	if repository.productQuery.AfterID != firstID || repository.productQuery.AfterName != "西红柿" {
		t.Fatalf("repository cursor = %+v", repository.productQuery)
	}
}

func TestCatalogRejectsInvalidPublicQueries(t *testing.T) {
	catalog := pricing.NewCatalog(&catalogRepositoryStub{})
	for _, query := range []pricing.ProductQuery{
		{Limit: 51},
		{Q: " "},
		{Q: string(make([]rune, 65))},
		{Cursor: "not-a-cursor"},
	} {
		if _, err := catalog.ListProducts(context.Background(), query); err == nil {
			t.Fatalf("ListProducts(%+v) succeeded", query)
		}
	}
}

func TestCatalogFiltersInactiveRowsAndReturnsRegionsInStableOrder(t *testing.T) {
	parentID := uuid.MustParse("30000000-0000-0000-0000-000000000001")
	cityID := uuid.MustParse("30000000-0000-0000-0000-000000000002")
	repository := &catalogRepositoryStub{
		products: []pricing.Product{{ID: uuid.New(), Active: false}},
		regions: []pricing.Region{
			{ID: cityID, ParentID: &parentID, Level: pricing.RegionLevelCity, Name: "潍坊", FullName: "山东省潍坊市", NationalCode: "370700", Active: true},
		},
	}
	catalog := pricing.NewCatalog(repository)

	products, err := catalog.ListProducts(context.Background(), pricing.ProductQuery{})
	if err != nil {
		t.Fatal(err)
	}
	if len(products.Items) != 0 {
		t.Fatalf("inactive products = %+v", products.Items)
	}
	regions, err := catalog.ListRegions(context.Background(), pricing.RegionQuery{ParentID: &parentID})
	if err != nil {
		t.Fatal(err)
	}
	if len(regions.Items) != 1 || regions.Items[0].ID != cityID || repository.regionQuery.ParentID == nil {
		t.Fatalf("regions = %+v query = %+v", regions, repository.regionQuery)
	}
}

type catalogRepositoryStub struct {
	products     []pricing.Product
	regions      []pricing.Region
	productQuery pricing.ProductLookup
	regionQuery  pricing.RegionLookup
}

func (r *catalogRepositoryStub) ListProducts(_ context.Context, query pricing.ProductLookup) ([]pricing.Product, error) {
	r.productQuery = query
	var rows []pricing.Product
	for _, product := range r.products {
		if !product.Active || (query.CategoryID != nil && product.CategoryID != *query.CategoryID) {
			continue
		}
		if query.AfterID != uuid.Nil && (product.Name < query.AfterName || (product.Name == query.AfterName && product.ID.String() <= query.AfterID.String())) {
			continue
		}
		rows = append(rows, product)
		if len(rows) == query.Limit {
			break
		}
	}
	return rows, nil
}

func (r *catalogRepositoryStub) ListRegions(_ context.Context, query pricing.RegionLookup) ([]pricing.Region, error) {
	r.regionQuery = query
	var rows []pricing.Region
	for _, region := range r.regions {
		if !region.Active || (query.ParentID != nil && (region.ParentID == nil || *region.ParentID != *query.ParentID)) {
			continue
		}
		rows = append(rows, region)
	}
	return rows, nil
}
