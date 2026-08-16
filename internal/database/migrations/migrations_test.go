package migrations

import (
	"io/fs"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestFilesystem(t *testing.T) {
	fs := Filesystem()
	assert.NotNil(t, fs)
}

func TestGoMigrations(t *testing.T) {
	migrations := GoMigrations()
	assert.Empty(t, migrations)
}

// DBC-01: two migration files must never share a goose version prefix —
// that panics at startup. This guards the embedded set so a duplicate
// version cannot merge again (a branch-only 000012 collision already
// forced a rename once).
func TestMigrationVersionsUnique(t *testing.T) {
	seen := make(map[string]string)
	entries, err := fs.ReadDir(Filesystem(), ".")
	assert.NoError(t, err)
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasSuffix(name, ".sql") {
			continue
		}
		version, _, _ := strings.Cut(name, "_")
		if prev, dup := seen[version]; dup {
			t.Fatalf("duplicate migration version %s: %s and %s", version, prev, name)
		}
		seen[version] = name
	}
	assert.Greater(t, len(seen), 0, "expected embedded migrations")
}
