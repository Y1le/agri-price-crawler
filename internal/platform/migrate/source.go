package migrate

import (
	"embed"
	"fmt"
	"io/fs"
	"path"
	"sort"
)

//go:embed sql/*.up.sql
var migrationFiles embed.FS

// Source identifies one module-owned directory of globally versioned migrations.
type Source struct {
	name  string
	files fs.FS
	dir   string
}

// NewSource creates a module-owned migration source.
func NewSource(name string, files fs.FS, dir string) Source {
	return Source{name: name, files: files, dir: dir}
}

// PlatformSource returns the migrations owned by the shared platform module.
func PlatformSource() Source {
	return NewSource("platform", migrationFiles, "sql")
}

// LatestVersion returns the greatest version in the migration catalog.
func LatestVersion(sources ...Source) (int64, error) {
	migrations, err := loadSources(defaultSources(sources))
	if err != nil {
		return 0, err
	}
	if len(migrations) == 0 {
		return 0, nil
	}
	return migrations[len(migrations)-1].version, nil
}

func defaultSources(sources []Source) []Source {
	if len(sources) == 0 {
		return []Source{PlatformSource()}
	}
	return sources
}

func loadSources(sources []Source) ([]migration, error) {
	var migrations []migration
	for _, source := range sources {
		entries, err := fs.ReadDir(source.files, source.dir)
		if err != nil {
			return nil, fmt.Errorf("read migration source %q: %w", source.name, err)
		}

		for _, entry := range entries {
			if entry.IsDir() {
				continue
			}

			name := entry.Name()
			version, err := migrationVersion(name)
			if err != nil {
				return nil, fmt.Errorf("migration source %q: %w", source.name, err)
			}
			contents, err := fs.ReadFile(source.files, path.Join(source.dir, name))
			if err != nil {
				return nil, fmt.Errorf("read migration source %q file %s: %w", source.name, name, err)
			}
			migrations = append(migrations, migration{
				version: version,
				name:    source.name + "/" + name,
				sql:     string(contents),
			})
		}
	}

	sort.Slice(migrations, func(i, j int) bool {
		return migrations[i].version < migrations[j].version
	})
	for index := 1; index < len(migrations); index++ {
		if migrations[index-1].version == migrations[index].version {
			return nil, fmt.Errorf("duplicate migration version %d", migrations[index].version)
		}
	}
	return migrations, nil
}
