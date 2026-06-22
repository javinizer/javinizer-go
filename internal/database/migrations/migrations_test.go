package migrations

import (
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
