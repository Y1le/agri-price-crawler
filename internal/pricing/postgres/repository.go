package postgres

import (
	"context"
	"fmt"
	"strings"

	"github.com/Y1le/agri-price-crawler/internal/pricing"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
)

type repository struct {
	pool *pgxpool.Pool
}

var _ pricing.CatalogRepository = (*repository)(nil)

// NewCatalogRepository creates the Pricing read repository backed by PostgreSQL.
func NewCatalogRepository(pool *pgxpool.Pool) pricing.CatalogRepository {
	return &repository{pool: pool}
}

func (r *repository) ListProducts(ctx context.Context, lookup pricing.ProductLookup) ([]pricing.Product, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("list pricing products: repository is unavailable")
	}
	query := `
		SELECT id, category_id, name, slug, pinyin, aliases, canonical_unit, active
		FROM pricing_products
		WHERE active`
	args := make([]any, 0, 4)
	if lookup.Q != "" {
		args = append(args, likePrefix(lookup.Q))
		placeholder := fmt.Sprintf("$%d", len(args))
		query += " AND (name ILIKE " + placeholder + " || '%' ESCAPE '\\' OR pinyin ILIKE " + placeholder + " || '%' ESCAPE '\\' OR EXISTS (SELECT 1 FROM unnest(aliases) AS alias WHERE alias ILIKE " + placeholder + " || '%' ESCAPE '\\'))"
	}
	if lookup.CategoryID != nil {
		args = append(args, *lookup.CategoryID)
		query += fmt.Sprintf(" AND category_id = $%d", len(args))
	}
	if lookup.AfterID != uuid.Nil {
		args = append(args, lookup.AfterName, lookup.AfterID)
		query += fmt.Sprintf(" AND (name, id) > ($%d, $%d)", len(args)-1, len(args))
	}
	args = append(args, lookup.Limit)
	query += fmt.Sprintf(" ORDER BY name ASC, id ASC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query pricing products: %w", err)
	}
	defer rows.Close()

	items := make([]pricing.Product, 0, lookup.Limit)
	for rows.Next() {
		var product pricing.Product
		if err := rows.Scan(
			&product.ID,
			&product.CategoryID,
			&product.Name,
			&product.Slug,
			&product.Pinyin,
			&product.Aliases,
			&product.CanonicalUnit,
			&product.Active,
		); err != nil {
			return nil, fmt.Errorf("scan pricing product: %w", err)
		}
		items = append(items, product)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing products: %w", err)
	}
	return items, nil
}

func (r *repository) ListRegions(ctx context.Context, lookup pricing.RegionLookup) ([]pricing.Region, error) {
	if r == nil || r.pool == nil {
		return nil, fmt.Errorf("list pricing regions: repository is unavailable")
	}
	query := `
		SELECT id, parent_id, level, national_code, name, full_name, pinyin, active
		FROM pricing_regions
		WHERE active`
	args := make([]any, 0, 5)
	if lookup.Q != "" {
		args = append(args, likePrefix(lookup.Q))
		placeholder := fmt.Sprintf("$%d", len(args))
		query += " AND (name ILIKE " + placeholder + " || '%' ESCAPE '\\' OR pinyin ILIKE " + placeholder + " || '%' ESCAPE '\\')"
	}
	if lookup.ParentID != nil {
		args = append(args, *lookup.ParentID)
		query += fmt.Sprintf(" AND parent_id = $%d", len(args))
	}
	if lookup.AfterID != uuid.Nil {
		args = append(args, lookup.AfterLevel, lookup.AfterFullName, lookup.AfterID)
		query += fmt.Sprintf(" AND (level, full_name, id) > ($%d, $%d, $%d)", len(args)-2, len(args)-1, len(args))
	}
	args = append(args, lookup.Limit)
	query += fmt.Sprintf(" ORDER BY level ASC, full_name ASC, id ASC LIMIT $%d", len(args))

	rows, err := r.pool.Query(ctx, query, args...)
	if err != nil {
		return nil, fmt.Errorf("query pricing regions: %w", err)
	}
	defer rows.Close()

	items := make([]pricing.Region, 0, lookup.Limit)
	for rows.Next() {
		var region pricing.Region
		if err := rows.Scan(
			&region.ID,
			&region.ParentID,
			&region.Level,
			&region.NationalCode,
			&region.Name,
			&region.FullName,
			&region.Pinyin,
			&region.Active,
		); err != nil {
			return nil, fmt.Errorf("scan pricing region: %w", err)
		}
		items = append(items, region)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("iterate pricing regions: %w", err)
	}
	return items, nil
}

func likePrefix(value string) string {
	replacer := strings.NewReplacer("\\", "\\\\", "%", "\\%", "_", "\\_")
	return replacer.Replace(value)
}
