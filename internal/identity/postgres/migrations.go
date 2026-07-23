// Package postgres provides the PostgreSQL adapter for the Identity module.
package postgres

import (
	"embed"

	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
)

//go:embed migrations/*.up.sql
var files embed.FS

// Migrations returns the migrations owned by the Identity module.
func Migrations() migrate.Source {
	return migrate.NewSource("identity", files, "migrations")
}
