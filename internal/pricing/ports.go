package pricing

import "context"

// Catalog is the public application interface for active products and regions.
type Catalog interface {
	ListProducts(context.Context, ProductQuery) (ProductPage, error)
	ListRegions(context.Context, RegionQuery) (RegionPage, error)
}

// CatalogRepository only receives normalized keyset requests from Catalog.
type CatalogRepository interface {
	ListProducts(context.Context, ProductLookup) ([]Product, error)
	ListRegions(context.Context, RegionLookup) ([]Region, error)
}
