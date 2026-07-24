// Package postgres provides PostgreSQL adapters for the Pricing module.
package postgres

import (
	"embed"

	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
)

//go:embed migrations/*.up.sql
var files embed.FS

// Migrations returns the migrations owned by Pricing.
func Migrations() migrate.Source {
	return migrate.NewSource("pricing", files, "migrations")
}
