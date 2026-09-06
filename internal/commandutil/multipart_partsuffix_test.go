package commandutil

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Regression pins for the CLI multipart part-suffix drop.
//
// Bug: RunBatchCommand discarded the per-file match metadata that
// ScanAndMatch produced (IsMultiPart / PartNumber / PartSuffix) and never
// seeded ScrapePhaseConfig.FileMatchInfo — unlike the API organize usecase
// (internal/api/batch/usecases.go). The apply phase therefore planned every
// part of a multi-part library onto the SAME destination
// (dest/GOOD-701/GOOD-701.mp4 for GOOD-701-cd1/2/3), and parts cd2/cd3 hit
// "organization validation failed" conflicts even though the matcher had
// detected the explicit -cdN parts correctly.
//
// Fix being pinned: the scan/match metadata map now flows into the batch job
// via ScrapePhaseConfig.FileMatchInfo, so the sort pipeline honors part info
// exactly like batch apply does.
//
// The JAVINIZER_E2E_SCRAPERS seam (Bootstrap substitutes the offline
// e2emock scraper for GOOD-* IDs) keeps these tests deterministic and
// network-free, exercising the real scan → match → scrape → apply chain.
func setupMultipartPartSuffixChain(t *testing.T) (configPath, src, dest string) {
	t.Helper()
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")

	tmpDir := t.TempDir()
	src = filepath.Join(tmpDir, "lib")
	dest = filepath.Join(tmpDir, "dest")
	require.NoError(t, os.MkdirAll(src, 0o700))
	require.NoError(t, os.MkdirAll(dest, 0o700))
	for _, part := range []string{"cd1", "cd2", "cd3"} {
		require.NoError(t, os.WriteFile(
			filepath.Join(src, "GOOD-701-"+part+".mp4"), []byte("fake video"), 0o600))
	}

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(tmpDir, "javinizer.db")
	cfg.Matching.Extensions = []string{".mp4"}
	cfg.Matching.MinSizeMB = 0
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.SubfolderFormat = []string{}
	cfg.Output.Template.FileFormat = "<ID><PARTSUFFIX>"
	cfg.Output.Operation.RenameFile = true
	// e2emock media URLs are non-resolvable (e2e.invalid); downloads are
	// irrelevant to the plan/drop being pinned, so turn them all off.
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false

	configPath = filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))
	return configPath, src, dest
}

// TestRunBatchCommand_MultipartPartSuffix_DryRunPlansDistinctTargets pins the
// dry-run plan leg: all three parts must plan cleanly to distinct targets, so
// the plan render shows zero conflicts and "Would organize 3 file(s)".
func TestRunBatchCommand_MultipartPartSuffix_DryRunPlansDistinctTargets(t *testing.T) {
	configPath, src, dest := setupMultipartPartSuffixChain(t)

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  dest,
		DryRun:       true,
		MoveFiles:    true,
		GenerateNFO:  false,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	out := buf.String()

	// Pre-fix this printed two conflict lines because cd2/cd3 re-planned onto
	// the bare dest/GOOD-701/GOOD-701.mp4 that cd1 claimed.
	assert.NotContains(t, out, "Apply failed",
		"no part may conflict on the planned destination\n%s", out)
	assert.Contains(t, out, "Would organize 3 file(s)",
		"all three -cdN parts must plan\n%s", out)

	// Dry-run is a preview: nothing moved, nothing created at the destination.
	for _, part := range []string{"cd1", "cd2", "cd3"} {
		assert.FileExists(t, filepath.Join(src, "GOOD-701-"+part+".mp4"))
	}
	entries, err := os.ReadDir(dest)
	require.NoError(t, err)
	assert.Empty(t, entries, "dry-run must not create organized output")
}

// TestRunBatchCommand_MultipartPartSuffix_LiveMovesToDistinctTargets pins the
// apply leg: each part lands at its own <ID><PARTSUFFIX> target and the
// sources are consumed by the move.
func TestRunBatchCommand_MultipartPartSuffix_LiveMovesToDistinctTargets(t *testing.T) {
	configPath, src, dest := setupMultipartPartSuffixChain(t)

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   src,
		Destination:  dest,
		DryRun:       false,
		MoveFiles:    true,
		GenerateNFO:  false,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	out := buf.String()

	assert.NotContains(t, out, "Apply failed", "no part may conflict on apply\n%s", out)
	assert.Contains(t, out, "Organized 3 file(s)",
		"all three -cdN parts must apply\n%s", out)

	for _, part := range []string{"cd1", "cd2", "cd3"} {
		assert.FileExists(t, filepath.Join(dest, "GOOD-701", "GOOD-701-"+part+".mp4"),
			"part %s must land at its part-suffixed target\n%s", part, out)
	}
	entries, err := os.ReadDir(src)
	require.NoError(t, err)
	assert.Empty(t, entries, "--move consumed the sources")
}
