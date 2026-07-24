package pricing

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/google/uuid"
)

var (
	ErrInvalidCursor = errors.New("invalid cursor")
	ErrInvalidQuery  = errors.New("invalid catalog query")
)

type catalog struct {
	repository CatalogRepository
}

func NewCatalog(repository CatalogRepository) Catalog {
	return &catalog{repository: repository}
}

func (c *catalog) ListProducts(ctx context.Context, query ProductQuery) (ProductPage, error) {
	lookup, filter, err := productLookup(query)
	if err != nil {
		return ProductPage{}, err
	}
	if c == nil || c.repository == nil {
		return ProductPage{}, fmt.Errorf("list products: repository is unavailable")
	}
	items, err := c.repository.ListProducts(ctx, lookup)
	if err != nil {
		return ProductPage{}, err
	}
	items = activeProducts(items)
	page := ProductPage{Items: items}
	if len(items) <= queryLimit(query.Limit) {
		return page, nil
	}
	page.Items = items[:queryLimit(query.Limit)]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeCursor(cursor{
		Version: 1, Kind: "products", Filter: filter, Name: last.Name, ID: last.ID,
	})
	if err != nil {
		return ProductPage{}, err
	}
	return page, nil
}

func (c *catalog) ListRegions(ctx context.Context, query RegionQuery) (RegionPage, error) {
	lookup, filter, err := regionLookup(query)
	if err != nil {
		return RegionPage{}, err
	}
	if c == nil || c.repository == nil {
		return RegionPage{}, fmt.Errorf("list regions: repository is unavailable")
	}
	items, err := c.repository.ListRegions(ctx, lookup)
	if err != nil {
		return RegionPage{}, err
	}
	items = activeRegions(items)
	page := RegionPage{Items: items}
	if len(items) <= queryLimit(query.Limit) {
		return page, nil
	}
	page.Items = items[:queryLimit(query.Limit)]
	last := page.Items[len(page.Items)-1]
	page.NextCursor, err = encodeCursor(cursor{
		Version: 1, Kind: "regions", Filter: filter, Level: last.Level, FullName: last.FullName, ID: last.ID,
	})
	if err != nil {
		return RegionPage{}, err
	}
	return page, nil
}

func productLookup(query ProductQuery) (ProductLookup, string, error) {
	q, limit, err := normalizeQuery(query.Q, query.Limit)
	if err != nil {
		return ProductLookup{}, "", err
	}
	filter := productFilter(q, query.CategoryID)
	lookup := ProductLookup{Q: q, CategoryID: query.CategoryID, Limit: limit + 1}
	if query.Cursor == "" {
		return lookup, filter, nil
	}
	cursor, err := decodeCursor(query.Cursor)
	if err != nil || cursor.Version != 1 || cursor.Kind != "products" || cursor.Filter != filter || cursor.ID == uuid.Nil || cursor.Name == "" {
		return ProductLookup{}, "", fmt.Errorf("%w: products cursor does not match query", ErrInvalidCursor)
	}
	lookup.AfterName, lookup.AfterID = cursor.Name, cursor.ID
	return lookup, filter, nil
}

func regionLookup(query RegionQuery) (RegionLookup, string, error) {
	q, limit, err := normalizeQuery(query.Q, query.Limit)
	if err != nil {
		return RegionLookup{}, "", err
	}
	filter := regionFilter(q, query.ParentID)
	lookup := RegionLookup{Q: q, ParentID: query.ParentID, Limit: limit + 1}
	if query.Cursor == "" {
		return lookup, filter, nil
	}
	cursor, err := decodeCursor(query.Cursor)
	if err != nil || cursor.Version != 1 || cursor.Kind != "regions" || cursor.Filter != filter || cursor.ID == uuid.Nil || cursor.FullName == "" || !validRegionLevel(cursor.Level) {
		return RegionLookup{}, "", fmt.Errorf("%w: regions cursor does not match query", ErrInvalidCursor)
	}
	lookup.AfterLevel, lookup.AfterFullName, lookup.AfterID = cursor.Level, cursor.FullName, cursor.ID
	return lookup, filter, nil
}

func normalizeQuery(raw string, requestedLimit int) (string, int, error) {
	q := strings.TrimSpace(raw)
	if raw != "" && q == "" {
		return "", 0, fmt.Errorf("%w: q must contain 1 to 64 characters", ErrInvalidQuery)
	}
	if utf8.RuneCountInString(q) > 64 {
		return "", 0, fmt.Errorf("%w: q must contain at most 64 characters", ErrInvalidQuery)
	}
	if requestedLimit < 0 || requestedLimit > MaxPageLimit {
		return "", 0, fmt.Errorf("%w: limit must be between 1 and %d", ErrInvalidQuery, MaxPageLimit)
	}
	if requestedLimit == 0 {
		requestedLimit = DefaultPageLimit
	}
	return q, requestedLimit, nil
}

func queryLimit(requested int) int {
	if requested == 0 {
		return DefaultPageLimit
	}
	return requested
}

func activeProducts(items []Product) []Product {
	result := make([]Product, 0, len(items))
	for _, item := range items {
		if item.Active {
			result = append(result, item)
		}
	}
	return result
}

func activeRegions(items []Region) []Region {
	result := make([]Region, 0, len(items))
	for _, item := range items {
		if item.Active {
			result = append(result, item)
		}
	}
	return result
}

func productFilter(q string, categoryID *uuid.UUID) string {
	category := ""
	if categoryID != nil {
		category = categoryID.String()
	}
	return filterFingerprint("products", q, category)
}

func regionFilter(q string, parentID *uuid.UUID) string {
	parent := ""
	if parentID != nil {
		parent = parentID.String()
	}
	return filterFingerprint("regions", q, parent)
}

func filterFingerprint(kind, q, scope string) string {
	sum := sha256.Sum256([]byte(kind + "\x00" + q + "\x00" + scope))
	return base64.RawURLEncoding.EncodeToString(sum[:])
}

type cursor struct {
	Version  int         `json:"v"`
	Kind     string      `json:"k"`
	Filter   string      `json:"f"`
	Name     string      `json:"n,omitempty"`
	Level    RegionLevel `json:"l,omitempty"`
	FullName string      `json:"fn,omitempty"`
	ID       uuid.UUID   `json:"id"`
}

func encodeCursor(value cursor) (string, error) {
	encoded, err := json.Marshal(value)
	if err != nil {
		return "", fmt.Errorf("encode cursor: %w", err)
	}
	return base64.RawURLEncoding.EncodeToString(encoded), nil
}

func decodeCursor(value string) (cursor, error) {
	encoded, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return cursor{}, err
	}
	decoder := json.NewDecoder(strings.NewReader(string(encoded)))
	decoder.DisallowUnknownFields()
	var decoded cursor
	if err := decoder.Decode(&decoded); err != nil {
		return cursor{}, err
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return cursor{}, errors.New("cursor contains trailing data")
	}
	return decoded, nil
}

func validRegionLevel(level RegionLevel) bool {
	return level == RegionLevelProvince || level == RegionLevelCity || level == RegionLevelDistrict
}
