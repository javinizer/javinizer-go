package database

import (
	"database/sql"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestComputeMigrationHash(t *testing.T) {
	content := []byte("CREATE TABLE test (id INTEGER);")
	hash := computeMigrationHash(content)

	// SHA256 produces 64 character hex string
	assert.Len(t, hash, 64)
	assert.NotEmpty(t, hash)

	// Same content produces same hash
	hash2 := computeMigrationHash(content)
	assert.Equal(t, hash, hash2)

	// Different content produces different hash
	different := []byte("CREATE TABLE other (id INTEGER);")
	hash3 := computeMigrationHash(different)
	assert.NotEqual(t, hash, hash3)
}

func TestEnsureMigrationHashTable(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	err = ensureMigrationHashTable(db)
	require.NoError(t, err)

	// Verify table exists
	var name string
	err = db.QueryRow(
		"SELECT name FROM sqlite_master WHERE type='table' AND name='schema_migrations_hash'",
	).Scan(&name)
	require.NoError(t, err)
	assert.Equal(t, "schema_migrations_hash", name)

	// Calling again should not error (idempotent)
	err = ensureMigrationHashTable(db)
	require.NoError(t, err)
}

func TestStoreAndGetHash(t *testing.T) {
	db, err := sql.Open("sqlite3", ":memory:")
	require.NoError(t, err)
	defer db.Close()

	err = ensureMigrationHashTable(db)
	require.NoError(t, err)

	// Store hash
	err = StoreMigrationHash(db, "test_migration", "abc123")
	require.NoError(t, err)

	// Retrieve hash
	hash, err := GetStoredHash(db, "test_migration")
	require.NoError(t, err)
	assert.Equal(t, "abc123", hash)

	// Non-existent migration returns empty
	hash, err = GetStoredHash(db, "nonexistent")
	require.NoError(t, err)
	assert.Empty(t, hash)
}
