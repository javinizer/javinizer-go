package database

import (
	"context"
	"testing"

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
