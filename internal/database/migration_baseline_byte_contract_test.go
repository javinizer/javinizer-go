package database

import (
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"testing"

	dbmigrations "github.com/javinizer/javinizer-go/internal/database/migrations"
	"github.com/stretchr/testify/require"
)

func TestStartupRetainsChecksumOfProviderBaselineAcrossRestart(t *testing.T) {
	const baseline = "000001_baseline.sql"
	b, err := fs.ReadFile(dbmigrations.Filesystem(), baseline)
	require.NoError(t, err)
	sum := sha256.Sum256(b)
	want := hex.EncodeToString(sum[:])

	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	for range 2 {
		require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
		got, err := GetStoredHash(sqlDB, baseline)
		require.NoError(t, err)
		require.Equal(t, want, got)
	}
}
