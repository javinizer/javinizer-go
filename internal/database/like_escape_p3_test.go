package database

import (
	"context"
	"errors"
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

// codex P3 R12-1: destination matching follows the probed root semantics —
// this regression pins the legacy insensitive behavior explicitly.
func TestFallbackSeam_CaseVariantDestinationMatches(t *testing.T) {
	previous := fsutil.CaseSensitiveProbe
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return false, nil }
	fsutil.ResetCaseSensitivityCache()
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = previous
		fsutil.ResetCaseSensitivityCache()
	})

	raw := `{"replacements":[{"destination":"C:\\Media\\poster.jpg","backup":"C:\\Media\\poster.jpg.dlbak.aaaaaaaaaaaaaaaa","dest_seq":1}]}`
	cand := models.BatchFileOperation{MovieID: "CV-1", GeneratedFiles: raw}
	helper := fallbackSeam{}
	scanCalls := 0
	matched, err := helper.finish(context.Background(), []models.BatchFileOperation{cand}, `c:\media\poster.jpg`, fsutil.DestKey, func(context.Context) ([]models.BatchFileOperation, error) {
		scanCalls++
		return []models.BatchFileOperation{cand}, nil
	})
	require.NoError(t, err)
	// The injected insensitive probe preserves the legacy folded match.
	require.Len(t, matched, 1, "case variants match after the insensitive DestKey probe")
	require.Equal(t, 1, scanCalls, "LIKE on raw still misses the other spelling — folded scan rescues")
}

// codex P3 R14-1: a partial prefilter hit (caller's spelling journaled) must
// NOT hide differently-spelled rows owning the SAME destination — the seam
// unions both sources.
func TestFallbackSeam_UnionDeduplicatesMixedSpellings(t *testing.T) {
	// W12 flip: this earlier R14-1 regression intentionally selects the
	// Windows separator seam; the new POSIX distinction is covered in fsutil
	// and history *_cov_w12_test.go files.
	previous := fsutil.PathBackslashesAreSeparators
	fsutil.PathBackslashesAreSeparators = true
	t.Cleanup(func() { fsutil.PathBackslashesAreSeparators = previous })

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
	// Both journal spellings resolve to the same destination key.
	require.Len(t, matched, 2, "slash and backslash forms match under the Windows seam")
	ids := map[uint]bool{}
	for _, op := range matched {
		ids[op.ID] = true
	}
	require.True(t, ids[7] && ids[9], "deduped union of prefilter + normalized scan")
}

// A dead DB surfaces a query error, not a fake empty result.
func TestFindOperationsByDestination_QueryError(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := NewBatchFileOperationRepository(db)
	require.NoError(t, db.Close())

	_, qerr := repo.FindOperationsByDestination(context.Background(), "/x/poster.jpg")
	require.Error(t, qerr)

	db2, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	defer func() { _ = db2.Close() }()
	require.NoError(t, db2.RunMigrationsOnStartup(context.Background()))
	repo2 := NewBatchFileOperationRepository(db2)
	require.NoError(t, db2.Close())
	_, qerr = repo2.FindOperationsWithLedger(context.Background())
	require.Error(t, qerr)
}

// FindOperationsWithLedger: mixed rows filter to ledger-carrying ones.
func TestFindOperationsWithLedger_Contents(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"})
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := NewBatchFileOperationRepository(db)
	ctx := context.Background()

	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID: "j1", MovieID: "M1", OriginalPath: "/a.mkv", GeneratedFiles: `{"delete":["/x.nfo"]}`,
		RevertStatus: models.RevertStatusApplied,
	}))
	require.NoError(t, repo.Create(ctx, &models.BatchFileOperation{
		BatchJobID: "j1", MovieID: "M2", OriginalPath: "/b.mkv", GeneratedFiles: "",
		RevertStatus: models.RevertStatusApplied,
	}))

	rows, err := repo.FindOperationsWithLedger(ctx)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, "M1", rows[0].MovieID)
}
