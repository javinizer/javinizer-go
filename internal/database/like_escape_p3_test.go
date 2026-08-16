package database

import (
	"context"
	"errors"
	"runtime"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// codex P3 R5-1: destinations are matched against the JSON-encoded column —
// Windows paths contain backslashes that JSON escapes, so the LIKE prefilter
// must be built from the JSON spelling or Windows lookups return nothing and
// dest sequences restart at 1.
func TestFindOperationsByDestination_WindowsJSONEscaping(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	winDest := `D:\media\library\ABC-001\poster.jpg`
	raw := `{"replacements":[{"destination":"D:\\media\\library\\ABC-001\\poster.jpg","backup":"D:\\media\\library\\ABC-001\\poster.jpg.dlbak.aaaaaaaaaaaaaaaa","dest_seq":1}]}`
	op := &models.BatchFileOperation{
		BatchJobID: "job-1", MovieID: "ABC-001", OriginalPath: "C:\\rip\\abc.mkv",
		OperationType: models.OperationTypeCopy, GeneratedFiles: raw,
		RevertStatus: models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(ctx, op))

	hits, err := repo.FindOperationsByDestination(ctx, winDest)
	require.NoError(t, err)
	require.Len(t, hits, 1, "Windows destination must round-trip the JSON LIKE prefilter")
	require.Equal(t, "ABC-001", hits[0].MovieID)

	// Substring destinations must NOT match.
	misses, err := repo.FindOperationsByDestination(ctx, `D:\media\library\ABC-001\poster2.jpg`)
	require.NoError(t, err)
	require.Empty(t, misses)
}

// codex P3 R7-2: the cross-form ownership fallback must propagate query
// failures — destructive callers treat empty-with-error as keep-and-retry.
func TestFindOperationsByDestination_FallbackErrorPropagates(t *testing.T) {
	// The fallback seam is exercised directly: a failing ledger scan must
	// surface as an error, never as an empty result.
	helper := fallbackSeam{}
	sentinel := errors.New("db wedged")
	identity := func(p string) string { return p }
	type ctxKey struct{}
	ctx := context.WithValue(context.Background(), ctxKey{}, "marker")
	gotCtx := context.Context(nil)
	_, err := helper.finish(ctx, nil, "/slash/form/poster.jpg", identity, func(c context.Context) ([]models.BatchFileOperation, error) {
		gotCtx = c
		return nil, sentinel
	})
	require.ErrorIs(t, err, sentinel, "fallback failure must surface, not masquerade as absence")
	require.Equal(t, "marker", gotCtx.Value(ctxKey{}), "R10-1: the caller's context rides the fallback scan")
}

// codex P3 R12-1: destination matching follows platform case semantics —
// Windows/macOS fold case, so a differently-cased apply must match.
func TestFallbackSeam_CaseVariantDestinationMatches(t *testing.T) {
	raw := `{"replacements":[{"destination":"C:\\Media\\poster.jpg","backup":"C:\\Media\\poster.jpg.dlbak.aaaaaaaaaaaaaaaa","dest_seq":1}]}`
	cand := models.BatchFileOperation{MovieID: "CV-1", GeneratedFiles: raw}
	helper := fallbackSeam{}
	scanCalls := 0
	matched, err := helper.finish(context.Background(), []models.BatchFileOperation{cand}, `c:\media\poster.jpg`, fsutil.DestKey, func(context.Context) ([]models.BatchFileOperation, error) {
		scanCalls++
		return []models.BatchFileOperation{cand}, nil
	})
	require.NoError(t, err)
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		require.Len(t, matched, 1, "case variants match on case-insensitive platforms")
	} else {
		require.Empty(t, matched, "case differs = different file on case-sensitive filesystems")
		require.Equal(t, 1, scanCalls, "prefilter miss fell back to the full scan")
	}
}

// codex P3 R14-1: a partial prefilter hit (caller's spelling journaled) must
// NOT hide differently-spelled rows owning the SAME destination — the seam
// unions both sources.
func TestFallbackSeam_UnionDeduplicatesMixedSpellings(t *testing.T) {
	backslash := `{"replacements":[{"destination":"C:\\Media\\poster.jpg","backup":"C:\\Media\\poster.jpg.dlbak.aaaaaaaaaaaaaaaa","dest_seq":1}]}`
	slash := `{"replacements":[{"destination":"C:/Media/poster.jpg","backup":"C:/Media/poster.jpg.dlbak.bbbbbbbbbbbbbbbb","dest_seq":2}]}`
	candBackslash := models.BatchFileOperation{ID: 7, MovieID: "CV-BS", GeneratedFiles: backslash}
	candSlash := models.BatchFileOperation{ID: 9, MovieID: "CV-SL", GeneratedFiles: slash}

	helper := fallbackSeam{}
	query := `C:/Media/poster.jpg` // slash-form query — the SQL prefilter (raw form) misses all
	matched, err := helper.finish(context.Background(), []models.BatchFileOperation{candBackslash}, query, fsutil.DestKey, func(context.Context) ([]models.BatchFileOperation, error) {
		return []models.BatchFileOperation{candBackslash, candSlash}, nil
	})
	require.NoError(t, err)
	require.Len(t, matched, 2, "both spellings of one destination must remain visible")
	ids := map[uint]bool{}
	for _, op := range matched {
		ids[op.ID] = true
	}
	require.True(t, ids[7] && ids[9], "deduped union of prefilter + normalized scan")
}
