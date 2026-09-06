package commandutil

// End-to-end pin for #248 codex P2 (F3): a javinizer UPDATE run through the
// real CLI scaffold (RunBatchCommand with SkipOrganize) must report its
// apply-phase audit with the API update path's nfo_gen taxonomy — never a
// file_move/"Organized <id>" success event with an empty new_path.

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestRunBatchCommand_UpdateMode_EventTaxonomy runs a single-file in-place
// metadata update and asserts the persisted eventlog speaks the update
// vocabulary: nfo_gen info event per updated file, zero file_move events.
func TestRunBatchCommand_UpdateMode_EventTaxonomy(t *testing.T) {
	t.Setenv("JAVINIZER_E2E_SCRAPERS", "true")

	tmpDir := t.TempDir()
	src := filepath.Join(tmpDir, "src")
	require.NoError(t, os.MkdirAll(filepath.Join(src, "lib"), 0o700))
	videoPath := filepath.Join(src, "lib", "GOOD-700.mp4")
	require.NoError(t, os.WriteFile(videoPath, []byte("fake video"), 0o600))

	dbPath := filepath.Join(tmpDir, "javinizer.db")
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = dbPath
	cfg.Database.LogLevel = "silent"
	cfg.Matching.Extensions = []string{".mp4"}
	cfg.Matching.MinSizeMB = 0
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.SubfolderFormat = []string{}
	cfg.Output.Template.FileFormat = "<ID>"
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.Output.Download.DownloadActress = false
	configPath := filepath.Join(tmpDir, "config.yaml")
	require.NoError(t, config.Save(cfg, configPath))

	var buf bytes.Buffer
	err := RunBatchCommand(context.Background(), &buf, BatchCommandOptions{
		ConfigFile:        configPath,
		SourcePath:        src,
		Destination:       src, // update mode: files remain in place
		Recursive:         true,
		GenerateNFO:       true,
		SkipOrganize:      true, // javinizer update: never move files
		CommandLabel:      "Javinizer Update",
		ActionVerb:        "Updating metadata",
		CompletionMessage: "Update complete!",
		ModeLine:          "Update (metadata & artwork, files remain in place)",
		Resolved:          &workflow.ResolvedSeamStrings{},
	})
	require.NoError(t, err)
	out := buf.String()
	batchID := batchIDFromOutput(t, out)
	assert.Contains(t, out, "Updated: 1, Failed: 0", "the update summary reports the in-place refresh\n%s", out)
	assert.FileExists(t, videoPath, "update mode never moves the source")

	ctx := context.Background()
	db := openAssertionDB(t, dbPath)
	repos := db.Repositories()

	events, err := repos.EventRepo.FindFiltered(ctx, database.EventFilter{EventType: models.EventCategoryOrganize}, 50, 0)
	require.NoError(t, err)
	require.NotEmpty(t, events, "the update run must journal per-file audit events")
	fileMoveEvents := 0
	nfoGenInfoEvents := 0
	for _, ev := range events {
		if ev.Source == "file_move" {
			fileMoveEvents++
		}
		if ev.Source == "nfo_gen" && ev.Severity == models.SeverityInfo && ev.Message == "Updated GOOD-700" {
			nfoGenInfoEvents++
		}
	}
	assert.Zero(t, fileMoveEvents, "update mode must emit NO file_move events (in-place refresh is not a file move)")
	assert.Equal(t, 1, nfoGenInfoEvents, "exactly one nfo_gen update-success event per updated file")

	// The jobs row and the per-file operation row persist under the same batch
	// identity (the op row journals the in-place update operation type).
	job, err := repos.JobRepo.FindByID(ctx, batchID)
	require.NoError(t, err)
	require.NotNil(t, job)
	ops, err := repos.BatchFileOpRepo.FindByBatchJobID(ctx, batchID)
	require.NoError(t, err)
	require.Len(t, ops, 1)
	assert.Equal(t, models.OperationTypeUpdate, ops[0].OperationType, "the op row journals an in-place update, not a move")
}
