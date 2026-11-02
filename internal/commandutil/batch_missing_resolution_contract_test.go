package commandutil

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/require"
)

// Public callers can omit the seam-resolution step. Admission must return an
// actionable error before any file processing, rather than dereference nil.
func TestRunBatchCommandMissingResolutionRejectsBeforeProcessing(t *testing.T) {
	configPath, src, dest, dbPath := setupLiveBatchConfig(t)
	input := filepath.Join(src, "ABC-001.mp4")
	original := []byte("owned unchanged video fixture")
	require.NoError(t, os.WriteFile(input, original, 0600))
	var output bytes.Buffer
	err := RunBatchCommand(t.Context(), &output, BatchCommandOptions{
		ConfigFile: configPath, SourcePath: src, Destination: dest, Recursive: true,
		DryRun: true, MoveFiles: true, GenerateNFO: true, CommandLabel: "Javinizer Sort", ActionVerb: "Processing files",
		// Resolved intentionally omitted through the public options contract.
	})
	require.EqualError(t, err, "batch command requires resolved seam strings: opts.Resolved is nil")
	require.Contains(t, output.String(), "Preview (not persisted; no audit ID)")
	require.NotContains(t, output.String(), "Batch Job:")
	require.NotContains(t, output.String(), "Applied")
	actual, readErr := os.ReadFile(input)
	require.NoError(t, readErr)
	require.Equal(t, original, actual)
	entries, readErr := os.ReadDir(dest)
	require.NoError(t, readErr)
	require.Empty(t, entries)
	db := openAssertionDB(t, dbPath)
	repos := db.Repositories()
	jobs, readErr := repos.JobRepo.List(t.Context())
	require.NoError(t, readErr)
	require.Empty(t, jobs)
	history, readErr := repos.HistoryRepo.Count(t.Context())
	require.NoError(t, readErr)
	require.Zero(t, history)
	events, readErr := repos.EventRepo.Count(t.Context())
	require.NoError(t, readErr)
	require.Zero(t, events)
}
