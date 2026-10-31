package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/stretchr/testify/require"
)

// These are internal constructor/Execute contracts, not claims that the public
// workflow factory admits incomplete dependencies. No dependency is replaced
// after construction, and failure must precede parsing, scanning, or scraping.
func TestOrchestratorsRejectMissingFilesystem(t *testing.T) {
	t.Run("compare", func(t *testing.T) {
		cmd := CompareCmd{MovieID: "TEST-001", NFOPath: "/owned/movie.nfo", SelectedScrapers: []string{"r18dev"}}
		before := cmd
		before.SelectedScrapers = append([]string(nil), cmd.SelectedScrapers...)
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		orch := newCompareOrchestrator(nil, nil, nil, nil)

		result, err := orch.Execute(ctx, cmd)

		require.EqualError(t, err, "filesystem not configured")
		require.Nil(t, result)
		require.Equal(t, before, cmd)
		require.NoError(t, ctx.Err())
	})
	t.Run("scan and match", func(t *testing.T) {
		cmd := ScanAndMatchCmd{Directory: "/owned/videos", Recursive: true, Filter: "TEST", MaxFiles: 10, TimeoutSeconds: 30}
		before := cmd
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		orch := newScanAndMatchOrchestrator(nil, scanner.Config{}, nil, nil, 100, nil)

		result, err := orch.Execute(ctx, cmd)

		require.EqualError(t, err, "filesystem not configured")
		require.Nil(t, result)
		require.Equal(t, before, cmd)
		require.NoError(t, ctx.Err()) // Execute must not cancel its caller's context.
	})
}
