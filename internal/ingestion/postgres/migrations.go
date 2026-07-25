// Package postgres provides PostgreSQL adapters for the Ingestion module.
package postgres

import (
	"embed"

	"github.com/Y1le/agri-price-crawler/internal/platform/migrate"
)

//go:embed migrations/*.up.sql
var files embed.FS

// Migrations returns the migrations owned by Ingestion.
func Migrations() migrate.Source {
	return migrate.NewSource("ingestion", files, "migrations")
}
