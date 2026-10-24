package database

import (
	"context"
	"fmt"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRunMigrationsRejectsShortStoredHashWithoutPanic(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	originalHash, err := GetStoredHash(sqlDB, "000001_baseline.sql")
	require.NoError(t, err)
	require.Len(t, originalHash, 64)

	// Public storage and SQLite accept malformed lengths; reporting must not panic.
	for _, length := range []int{1, 11, 12, 13, 64} {
		t.Run(fmt.Sprintf("length_%d", length), func(t *testing.T) {
			malformed := strings.Repeat("x", length)
			require.NoError(t, StoreMigrationHash(sqlDB, "000001_baseline.sql", malformed))
			var migrationErr error
			require.NotPanics(t, func() {
				migrationErr = db.RunMigrationsOnStartup(context.Background())
			})
			expectedPrefix := malformed
			if len(expectedPrefix) > 12 {
				expectedPrefix = expectedPrefix[:12]
			}
			require.EqualError(t, migrationErr, fmt.Sprintf(
				"baseline migration hash mismatch: stored=%s current=%s. Migration file was modified after being applied. Manual intervention required",
				expectedPrefix, originalHash[:12],
			))
			storedHash, err := GetStoredHash(sqlDB, "000001_baseline.sql")
			require.NoError(t, err)
			require.Equal(t, malformed, storedHash, "rejection must preserve stored evidence")
		})
	}
	require.NoError(t, StoreMigrationHash(sqlDB, "000001_baseline.sql", originalHash))
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()), "matching original hash remains accepted")
}
