package database

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"unicode"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func backupCandidateRepo(t *testing.T) *BatchFileOperationRepository {
	t.Helper()
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	return NewBatchFileOperationRepository(db)
}
func seedBackupCandidate(t *testing.T, repo *BatchFileOperationRepository, raw string) uint {
	t.Helper()
	op := &models.BatchFileOperation{BatchJobID: "candidate-test", OriginalPath: "/source", NewPath: "/unrelated/video", NFOSnapshot: strings.Repeat("large nfo snapshot ", 256), GeneratedFiles: raw}
	require.NoError(t, repo.Create(t.Context(), op))
	return op.ID
}
func backupCandidateJSON(backup string) string {
	return models.MarshalLedgerJSON(models.GeneratedFilesJSON{Replacements: []models.ReplacementEntry{{Backup: backup, Destination: "/unrelated/destination"}}})
}
func TestReplacementBackupCandidatesAliasAndEscapeSuperset(t *testing.T) {
	const token = "0123456789abcdef"
	const backup = "/library/poster.jpg.dlbak." + token + ".1"
	variants := []string{
		backup, "/library/./poster.jpg.dlbak." + token + ".1", "/library/x/../poster.jpg.dlbak." + token + ".1",
		"/library//poster.jpg.dlbak." + token + ".1", `C:\Library/mixed\poster.jpg.dlbak.` + token + ".1",
		strings.ToUpper(backup), "/ÄÖ/é/poster.jpg.dlbak." + token + ".1", "/äö/e\u0301/poster.jpg.dlbak." + token + ".1",
		"/literal%_quote\"\\control\n/poster.jpg.dlbak." + token + ".1",
	}
	repo := backupCandidateRepo(t)
	// The query must not rely on SQLite's configurable LIKE case behavior.
	require.NoError(t, repo.GetDB().Exec("PRAGMA case_sensitive_like=ON").Error)
	var expected []uint
	for _, v := range variants {
		expected = append(expected, seedBackupCandidate(t, repo, backupCandidateJSON(v)))
	}
	expected = append(expected, seedBackupCandidate(t, repo, strings.ReplaceAll(backupCandidateJSON(backup), "/", `\/`)))
	for _, encoded := range []string{`\u0030` + token[1:], token[:10] + `\u0061` + token[11:]} {
		expected = append(expected, seedBackupCandidate(t, repo, strings.ReplaceAll(backupCandidateJSON(backup), token, encoded)))
	}
	expected = append(expected, seedBackupCandidate(t, repo, `{"replacements":[],"replacements":[{"backup":"`+backup+`"}]}`))
	expected = append(expected, seedBackupCandidate(t, repo, `{"unrelated": broken`))
	// JSON-valid but model-invalid rows which could name this backup must
	// still reach the caller's parse-error refusal; SQL validity is not ownership.
	expected = append(expected, seedBackupCandidate(t, repo, `{"undo_started":"bad","replacements":[{"backup":"`+backup+`"}]}`))
	seedBackupCandidate(t, repo, backupCandidateJSON("/other.dlbak.fedcba9876543210.2"))
	// Unlike the old global parse-all gate, irrelevant schema corruption does
	// not block this target: valid JSON with no nonce or Unicode escape cannot
	// contain an equivalent reference. Syntax-invalid rows remain included.
	seedBackupCandidate(t, repo, `{"undo_started":"bad","replacements":[{"backup":"/unrelated"}]}`)
	rows, err := repo.FindReplacementBackupCandidates(t.Context(), backup)
	require.NoError(t, err)
	var got []uint
	for _, row := range rows {
		got = append(got, row.ID)
		require.Empty(t, row.NFOSnapshot)
		require.Empty(t, row.NewPath)
		require.Empty(t, row.BatchJobID)
	}
	require.Equal(t, expected, got)
}
func TestReplacementBackupCandidatesProjectionAndQueryBudget(t *testing.T) {
	repo := backupCandidateRepo(t)
	for i := 0; i < 300; i++ {
		seedBackupCandidate(t, repo, backupCandidateJSON(fmt.Sprintf("/old/%d.nfo.dlbak.%016x.1", i, i+1)))
	}
	const backup = "/current.nfo.dlbak.abcdef0123456789.2"
	id := seedBackupCandidate(t, repo, backupCandidateJSON(backup))
	queries := 0
	require.NoError(t, repo.GetDB().Callback().Query().After("gorm:query").Register("candidate_query_count", func(tx *gorm.DB) {
		queries++
		require.NotContains(t, strings.ToLower(tx.Statement.SQL.String()), "nfo_snapshot")
	}))
	t.Cleanup(func() { _ = repo.GetDB().Callback().Query().Remove("candidate_query_count") })
	rows, err := repo.FindReplacementBackupCandidates(t.Context(), backup)
	require.NoError(t, err)
	require.Equal(t, 1, queries)
	require.Len(t, rows, 1)
	require.Equal(t, id, rows[0].ID)
	require.Empty(t, rows[0].NFOSnapshot)
	// Legacy/unsupported spellings retain every ledger, but still never snapshots.
	rows, err = repo.FindReplacementBackupCandidates(t.Context(), "/legacy.bak")
	require.NoError(t, err)
	require.Equal(t, 2, queries)
	require.Len(t, rows, 301)
	for _, row := range rows {
		require.Empty(t, row.NFOSnapshot)
	}
}
func TestReplacementBackupCandidatesFailureAndNonceFallback(t *testing.T) {
	repo := backupCandidateRepo(t)
	seedBackupCandidate(t, repo, backupCandidateJSON("/any"))
	for _, name := range []string{"/x.dlbak.short", "/x.dlbak.0123456789abcdef.bad-ordinal", "/x.dlbak.0123456789abcdef.1.quarantine", "/x.dlbak.0123456789abcdef/.."} {
		require.Empty(t, replacementBackupNonce(name))
		rows, err := repo.FindReplacementBackupCandidates(t.Context(), name)
		require.NoError(t, err)
		require.Len(t, rows, 1)
	}
	require.Equal(t, "0123456789abcdef", replacementBackupNonce("/x.DLBAK.0123456789ABCDEF.F"))
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	_, err := repo.FindReplacementBackupCandidates(ctx, "/x.dlbak.0123456789abcdef")
	require.ErrorIs(t, err, context.Canceled)
	require.NoError(t, repo.GetDB().Migrator().DropTable(&models.BatchFileOperation{}))
	_, err = repo.FindReplacementBackupCandidates(t.Context(), "/x.dlbak.0123456789abcdef")
	require.Error(t, err)
}

// DestKey uses simple per-rune uppercasing, NOT compatibility/full folding.
// Pin the nonce prefilter assumption independently of parent-path Unicode.
func TestReplacementBackupNonceSimpleCaseInvariant(t *testing.T) {
	for r := rune(128); r <= unicode.MaxRune; r++ {
		upper := unicode.ToUpper(r)
		if upper >= '0' && upper <= '9' || upper >= 'A' && upper <= 'F' {
			t.Fatalf("non-ASCII rune %U aliases a nonce character %U", r, upper)
		}
	}
}
