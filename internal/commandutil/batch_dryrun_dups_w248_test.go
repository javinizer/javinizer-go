package commandutil

// Regression pins for #248 codex P2 — the dry-run leg of the authorized
// intra-batch duplicate skip. The organizer's dry-run branch returned
// OrganizeResult.Warnings WITHOUT setting DuplicateSkipped (the live leg sets
// it), so the CLI dry-run summary silently reported "Would organize N"
// counting BOTH winner and loser, the skip line never appeared, and the
// duplicate warning text never printed (the post-apply hook gated ALL of it
// behind the dry-run early return instead of gating just the eventlog
// emission). Post-fix the dry-run skip result mirrors the live shape and the
// summary reuses the live arithmetic (completed minus SkippedDuplicates).
//
// Fixtures follow batch_history_w244_test.go conventions: the
// JAVINIZER_E2E_SCRAPERS seam substitutes the offline e2emock scraper, and all
// paths are built with filepath.Join/t.TempDir (never POSIX literals) so the
// tests stay Windows-safe.

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

// setupResidentMoverDryRunBatch writes the codex P2 dry-run fixture: a
// RESIDENT already parked at its computed destination plus a MOVER planning
// onto the identical destination, both below the scanned root (the batch's
// Destination lives inside the scan tree so the resident IS a batch input).
// With ForceUpdate the mover is an AUTHORIZED duplicate (warning + skip);
// without it the mover loses through the ordinary conflict pipeline.
func setupResidentMoverDryRunBatch(t *testing.T) (configPath, scan, moverSrc, residentPath string) {
	t.Helper()
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")

	tmpDir := t.TempDir()
	scan = filepath.Join(tmpDir, "scan")
	dest := filepath.Join(scan, "dest")
	require.NoError(t, os.MkdirAll(filepath.Join(scan, "in"), 0o700))
	residentDir := filepath.Join(dest, "GOOD-700")
	require.NoError(t, os.MkdirAll(residentDir, 0o700))
	moverSrc = filepath.Join(scan, "in", "GOOD-700.mp4")
	residentPath = filepath.Join(residentDir, "GOOD-700.mp4")
	require.NoError(t, os.WriteFile(moverSrc, []byte("mover bytes"), 0o600))
	require.NoError(t, os.WriteFile(residentPath, []byte("resident bytes"), 0o600))

	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = filepath.Join(tmpDir, "javinizer.db")
	cfg.Database.LogLevel = "silent"
	cfg.Matching.Extensions = []string{".mp4"}
	cfg.Matching.MinSizeMB = 0
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.SubfolderFormat = []string{}
	cfg.Output.Template.FileFormat = "<ID>"
	cfg.Output.Operation.RenameFile = true
	// e2emock media URLs are non-resolvable; downloads are irrelevant here.
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false

	configPath = filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))
	return configPath, scan, moverSrc, residentPath
}

// assertDryRunStateUnchanged pins the preview contract on the filesystem: the
// mover never left its source, the resident's bytes stand, no second video
// landed at the destination, and no NFO was generated.
func assertDryRunStateUnchanged(t *testing.T, scan, moverSrc, residentPath string) {
	t.Helper()
	assert.FileExists(t, moverSrc, "the mover's source must stay put in dry-run")
	assert.FileExists(t, residentPath, "the resident's bytes must stand in dry-run")
	count := 0
	walkErr := filepath.Walk(scan, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		switch filepath.Ext(p) {
		case ".mp4":
			count++
		case ".nfo":
			t.Errorf("dry-run generated an NFO at %s", p)
		}
		return nil
	})
	require.NoError(t, walkErr)
	assert.Equal(t, 2, count, "exactly the original two videos exist — no copy/move happened")
}

// TestRunBatchCommand_DryRun_AuthorizedDuplicateSkip_CountsWarnsUnchanged is
// the core pin: a dry run over the resident+mover duplicate fixture with -f
// must report the skip EXACTLY like live — winner/losers excluded from the
// organize totals via the live arithmetic, the skip line counted, and the
// warning text printed — while nothing on disk changes.
func TestRunBatchCommand_DryRun_AuthorizedDuplicateSkip_CountsWarnsUnchanged(t *testing.T) {
	configPath, scan, moverSrc, residentPath := setupResidentMoverDryRunBatch(t)
	dest := filepath.Join(scan, "dest")

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   scan,
		Destination:  dest,
		Recursive:    true,
		DryRun:       true,
		MoveFiles:    true,
		ForceUpdate:  true, // -f authorizes the intra-batch duplicate
		GenerateNFO:  true,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	out := buf.String()

	// Summary truth: the live arithmetic (completed minus skipped) drives the
	// preview — pre-fix this read "Would organize 2 file(s)" with no skip line.
	assert.NotContains(t, out, "Apply failed", "an authorized duplicate dry-run never fails\n%s", out)
	assert.Contains(t, out, "Would organize 1 file(s)", "the skipped loser must not count as would-organize\n%s", out)
	assert.Contains(t, out, "Skipped (authorized duplicates): 1", "the summary must count the dry-run skip\n%s", out)
	assert.Contains(t, out, "Metadata found: 2", "both files scraped and matched\n%s", out)
	assert.Contains(t, out, "Files organized: 1 (dry-run)", "summary totals reuse the live subtraction\n%s", out)
	assert.Contains(t, out, "NFOs generated: 1 (dry-run)", "NFO totals exclude the skipped duplicate\n%s", out)

	// The warning text prints to the console identically to live — pre-fix the
	// dry-run gate swallowed it before the print.
	assert.Contains(t, out, "⚠️", "the duplicate warning must print in dry-run\n%s", out)
	assert.Contains(t, out, "duplicate destination within batch", "warning text must survive to the dry-run console\n%s", out)
	assert.Contains(t, out, "already claimed by")
	assert.Contains(t, out, "overwrite authorized")

	assertDryRunStateUnchanged(t, scan, moverSrc, residentPath)
}

// TestRunBatchCommand_DryRun_UnauthorizedDuplicate_StillConflicts pins the
// unchanged non-force leg: the same fixture WITHOUT -f keeps demoting the
// mover through the ordinary conflict pipeline (a failed apply, no skip
// accounting, no warning print) — exactly the pre-existing dry-run behavior.
func TestRunBatchCommand_DryRun_UnauthorizedDuplicate_StillConflicts(t *testing.T) {
	configPath, scan, moverSrc, residentPath := setupResidentMoverDryRunBatch(t)
	dest := filepath.Join(scan, "dest")

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:   configPath,
		SourcePath:   scan,
		Destination:  dest,
		Recursive:    true,
		DryRun:       true,
		MoveFiles:    true,
		ForceUpdate:  false,
		CommandLabel: "Javinizer Sort",
		ActionVerb:   "Processing files",
		Resolved:     &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err, "per-file failures print but never fail the batch run")
	out := buf.String()

	// The mover fails through the validation pipeline; only the resident
	// completes, so the preview counts exactly the resident.
	assert.Contains(t, out, "Apply failed", "the unauthorized duplicate must surface as a failure\n%s", out)
	assert.Contains(t, out, "organization validation failed")
	assert.Contains(t, out, "Would organize 1 file(s)")
	assert.Contains(t, out, "Metadata found: 1")
	assert.NotContains(t, out, "Skipped (authorized duplicates)", "no authorization, no skip line")
	assert.False(t, strings.Contains(out, "⚠️"), "no warning print without an authorized skip\n%s", out)

	assertDryRunStateUnchanged(t, scan, moverSrc, residentPath)
}
