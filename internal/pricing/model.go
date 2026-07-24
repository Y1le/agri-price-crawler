// Package pricing owns the public price catalog, snapshots, and trend models.
package pricing

import "github.com/google/uuid"

const (
	DefaultPageLimit = 20
	MaxPageLimit     = 50
)

type Category struct {
	ID        uuid.UUID
	ParentID  *uuid.UUID
	Name      string
	Slug      string
	SortOrder int
	Active    bool
}

type Product struct {
	ID            uuid.UUID
	CategoryID    uuid.UUID
	Name          string
	Slug          string
	Pinyin        string
	Aliases       []string
	CanonicalUnit string
	Active        bool
}

type RegionLevel string

const (
	RegionLevelProvince RegionLevel = "province"
	RegionLevelCity     RegionLevel = "city"
	RegionLevelDistrict RegionLevel = "district"
)

type Region struct {
	ID           uuid.UUID
	ParentID     *uuid.UUID
	Level        RegionLevel
	NationalCode string
	Name         string
	FullName     string
	Pinyin       string
	Active       bool
}

type ProductQuery struct {
	Q          string
	CategoryID *uuid.UUID
	Cursor     string
	Limit      int
}

type RegionQuery struct {
	Q        string
	ParentID *uuid.UUID
	Cursor   string
	Limit    int
}

type ProductPage struct {
	Items      []Product
	NextCursor string
}

type RegionPage struct {
	Items      []Region
	NextCursor string
}

// ProductLookup is an already validated, repository-facing keyset query.
type ProductLookup struct {
	Q          string
	CategoryID *uuid.UUID
	AfterName  string
	AfterID    uuid.UUID
	Limit      int
}

// RegionLookup is an already validated, repository-facing keyset query.
type RegionLookup struct {
	Q             string
	ParentID      *uuid.UUID
	AfterLevel    RegionLevel
	AfterFullName string
	AfterID       uuid.UUID
	Limit         int
}
