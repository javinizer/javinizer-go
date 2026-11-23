package migrations

import (
	"embed"
	"io/fs"

	"github.com/pressly/goose/v3"
)

//go:embed *.sql
var sqlMigrations embed.FS

//go:embed 000001_baseline.sql
var baselineSQL string

// BaselineSQL returns the immutable SQL bytes shared with Filesystem. Naming
// this migration in the embed directive makes its presence a build invariant.
func BaselineSQL() string {
	return baselineSQL
}

// Filesystem returns embedded SQL migration files.
func Filesystem() fs.FS {
	return sqlMigrations
}

// GoMigrations returns programmatic migrations that are bundled into the binary.
// All migrations are now in SQL baseline, so no Go migrations needed.
func GoMigrations() []*goose.Migration {
	return []*goose.Migration{}
}
