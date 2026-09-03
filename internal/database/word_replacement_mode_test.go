package database

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

// Task 1.3 / spec: repo normalizes empty to literal, rejects unknown, and the
// widened entries API carries the mode. Real in-memory sqlite via the repo.
func TestWordReplacement_MatchMode_NormalizationAndValidation(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)
	ctx := context.Background()

	// Empty normalizes to literal on Create.
	plain := &models.WordReplacement{Original: "norm-cjk", Replacement: "v1"}
	require.NoError(t, repo.Create(ctx, plain))
	stored, err := repo.FindByOriginal(ctx, "norm-cjk")
	require.NoError(t, err)
	assert.Equal(t, models.MatchModeLiteral, stored.MatchMode)

	// Wildcard persists.
	wild := &models.WordReplacement{Original: "wild-one", Replacement: "v2", MatchMode: models.MatchModeWildcard}
	require.NoError(t, repo.Create(ctx, wild))
	stored, err = repo.FindByOriginal(ctx, "wild-one")
	require.NoError(t, err)
	assert.Equal(t, models.MatchModeWildcard, stored.MatchMode)

	// Unknown mode rejected, nothing stored.
	bad := &models.WordReplacement{Original: "bad-one", Replacement: "v3", MatchMode: "regex"}
	require.Error(t, repo.Create(ctx, bad))
	_, err = repo.FindByOriginal(ctx, "bad-one")
	require.True(t, IsNotFound(err))

	// Upsert with empty mode writes literal (import semantics), and can flip
	// an existing wildcard row back to literal.
	flip := &models.WordReplacement{Original: "wild-one", Replacement: "v2"}
	require.NoError(t, repo.Upsert(ctx, flip))
	stored, err = repo.FindByOriginal(ctx, "wild-one")
	require.NoError(t, err)
	assert.Equal(t, models.MatchModeLiteral, stored.MatchMode)

	// Entries API returns modes.
	entries, err := repo.GetReplacementEntries(ctx)
	require.NoError(t, err)
	byOrig := map[string]string{}
	for _, e := range entries {
		byOrig[e.Original] = e.MatchMode
	}
	assert.Equal(t, models.MatchModeLiteral, byOrig["norm-cjk"])
}

// Upsert validates mode too (the Save path used by import).
func TestWordReplacement_UpsertRejectsUnknownMode(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)
	bad := &models.WordReplacement{Original: "upsert-bad", Replacement: "v", MatchMode: "regex"}
	require.Error(t, repo.Upsert(context.Background(), bad))
	_, err := repo.FindByOriginal(context.Background(), "upsert-bad")
	require.True(t, IsNotFound(err))
}

// Spec scenario "migrate-existing-rows stay literal": rows written before the
// column existed (simulated via raw SQL insert without match_mode) read back
// with the DB-level default 'literal'.
func TestWordReplacement_MatchMode_PreMigrationRowsStayLiteral(t *testing.T) {
	db := newDatabaseTestDB(t)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	_, err = sqlDB.Exec("INSERT INTO word_replacements (original, replacement) VALUES ('legacy-row', 'legacy-repl')")
	require.NoError(t, err)

	repo := NewWordReplacementRepository(db)
	stored, err := repo.FindByOriginal(context.Background(), "legacy-row")
	require.NoError(t, err)
	assert.Equal(t, "legacy-repl", stored.Replacement)
	assert.Equal(t, models.MatchModeLiteral, stored.MatchMode)
}
