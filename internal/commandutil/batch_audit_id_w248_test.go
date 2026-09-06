package commandutil

// Regression pins for #248 codex P2 (R2): the console must never print a
// queryable-looking "Batch Job: <id>" for a run whose jobs row does not
// exist — `history list --batch <id>` would answer 'batch job not found'.
// 30ac1625 fixed the dry-run header (preview label); this file pins the
// remaining LIVE legs: the scan early-exits (zero files / zero matched IDs)
// render the identical no-audit sentence dry runs use, while a normal live
// run prints its identity strictly AFTER the runtime persisted the jobs row.
//
// All paths are built with filepath.Join / t.TempDir (never POSIX literals)
// so the tests stay Windows-safe.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupLiveBatchConfig writes an offline-live batch config plus empty
// source/destination trees. Mirrors batch_history_w244's setupDuplicateBatch
// config minus the fixture files, so each test plants exactly the files its
// leg needs. Downloads are disabled — e2emock media URLs are non-resolvable.
func setupLiveBatchConfig(t *testing.T) (configPath, src, dest, dbPath string) {
	t.Helper()

	tmpDir := t.TempDir()
	src = filepath.Join(tmpDir, "src")
	dest = filepath.Join(tmpDir, "dest")
	require.NoError(t, os.MkdirAll(src, 0o700))
	require.NoError(t, os.MkdirAll(dest, 0o700))

	dbPath = filepath.Join(tmpDir, "javinizer.db")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	cfg.Database.LogLevel = "silent"
	cfg.Matching.Extensions = []string{".mp4"}
	cfg.Matching.MinSizeMB = 0
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.SubfolderFormat = []string{}
	cfg.Output.Template.FileFormat = "<ID>"
	cfg.Output.Operation.RenameFile = true
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false

	configPath = filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))
	return configPath, src, dest, dbPath
}

// runLiveBatch drives RunBatchCommand in LIVE mode against the fixture with
// the default (real) presenter so the console surface under test is real.
func runLiveBatch(t *testing.T, configPath, src, dest string) string {
	t.Helper()
	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:        configPath,
		SourcePath:        src,
		Destination:       dest,
		Recursive:         true,
		CommandLabel:      "Javinizer Sort",
		ActionVerb:        "Processing files",
		CompletionMessage: "Sort complete!",
		Resolved:          &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	return buf.String()
}

// assertNoJobsRow re-opens the run database and proves nothing persisted a
// jobs row — the other half of the 'no audit ID offered' contract.
func assertNoJobsRow(t *testing.T, dbPath string) {
	t.Helper()
	db := openAssertionDB(t, dbPath)
	jobs, err := db.Repositories().JobRepo.List(context.Background())
	require.NoError(t, err)
	assert.Empty(t, jobs, "a scan early-exit persist no jobs row")
}

// TestRunBatchCommand_LiveZeroFiles_PrintsNoBatchJobToken — leg (a): a live
// run whose scan finds no video files exits before the runtime exists; the
// console must offer NO 'Batch Job:' token and instead render the identical
// no-audit sentence the dry-run header prints.
func TestRunBatchCommand_LiveZeroFiles_PrintsNoBatchJobToken(t *testing.T) {
	configPath, src, dest, dbPath := setupLiveBatchConfig(t)

	out := runLiveBatch(t, configPath, src, dest)

	assert.NotContains(t, out, "Batch Job:", "zero-file live runs offer no queryable audit id\n%s", out)
	assert.Contains(t, out, "Preview (not persisted; no audit ID)",
		"the early-exit note matches the dry-run variant exactly\n%s", out)
	assert.Contains(t, out, "No files to process", "sanity: the zero-file leg ran\n%s", out)
	assert.NotContains(t, out, "Processing files", "the runtime leg never started\n%s", out)
	assertNoJobsRow(t, dbPath)
}

// TestRunBatchCommand_LiveUnmatchedFiles_PrintsNoBatchJobToken — leg (b): a
// live run whose scan finds video files but matches NO content IDs exits
// before the runtime exists; same no-audit contract as leg (a).
func TestRunBatchCommand_LiveUnmatchedFiles_PrintsNoBatchJobToken(t *testing.T) {
	configPath, src, dest, dbPath := setupLiveBatchConfig(t)

	// A video file whose name matches no ID pattern: scanned, never matched.
	require.NoError(t, os.WriteFile(filepath.Join(src, "family-recital.mp4"), []byte("fake video"), 0o600))

	out := runLiveBatch(t, configPath, src, dest)

	assert.NotContains(t, out, "Batch Job:", "unmatched live runs offer no queryable audit id\n%s", out)
	assert.Contains(t, out, "Preview (not persisted; no audit ID)",
		"the early-exit note matches the dry-run variant exactly\n%s", out)
	assert.NotContains(t, out, "Processing files", "the runtime leg never started\n%s", out)
	assertNoJobsRow(t, dbPath)
}

// TestRunBatchCommand_LiveRun_PrintsIDAfterPersist — leg (c): a normal live
// run still prints its batch identity, and the ordering evidence holds —
// the printed token resolves to a jobs row on disk under that exact id
// (post-persist console placement: the identity renders after processing
// started, i.e. after runtime construction wrote the row).
func TestRunBatchCommand_LiveRun_PrintsIDAfterPersist(t *testing.T) {
	// One matchable file on the e2emock scraper seam (offline) — the
	// batch_update_mode_w248 fixture.
	configPath, src, dest, dbPath := setupSingleFileBatch(t, "GOOD-700")

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:        configPath,
		SourcePath:        src,
		Destination:       dest,
		Recursive:         true,
		MoveFiles:         true,
		GenerateNFO:       true,
		CommandLabel:      "Javinizer Sort",
		ActionVerb:        "Processing files",
		CompletionMessage: "Sort complete!",
		Resolved:          &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	out := buf.String()

	// The printed identity parses and post-dates the processing banner —
	// console evidence the id is rendered only within the persisted leg.
	batchID := batchIDFromOutput(t, out)
	idxProcessing := strings.Index(out, "Processing files")
	idxBatchID := strings.Index(out, "Batch Job: ")
	require.GreaterOrEqual(t, idxProcessing, 0, "the processing banner printed\n%s", out)
	assert.Greater(t, idxBatchID, idxProcessing,
		"the audit id renders strictly after runtime construction (post-persist)\n%s", out)

	// The jobs row exists on disk under the printed id — the token is
	// queryable via `history list --batch <id>`, never dangling.
	db := openAssertionDB(t, dbPath)
	job, err := db.Repositories().JobRepo.FindByID(context.Background(), batchID)
	require.NoError(t, err, "the printed id must resolve to a persisted jobs row")
	require.NotNil(t, job)
	assert.Equal(t, batchID, job.ID)
}

// TestSilentBatchCommandPresenter_OnAuditID pins the silent seam for the new
// presenter lifecycle hook (W-5: no stdout side effects in tests).
func TestSilentBatchCommandPresenter_OnAuditID(t *testing.T) {
	p := &SilentBatchCommandPresenter{}
	var buf bytes.Buffer
	p.OnAuditID(&buf, BatchCommandOptions{BatchJobID: "job-1"}, true)
	p.OnAuditID(&buf, BatchCommandOptions{}, false)
	assert.Empty(t, buf.String())
}
