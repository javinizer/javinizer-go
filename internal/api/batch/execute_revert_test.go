package batch

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// revertTestMatch returns a default fileMatchInfo for RevertLog seam tests.
func revertTestMatch(movieID string) models.FileMatchInfo {
	return models.FileMatchInfo{
		Path:    fmt.Sprintf("/source/%s.mp4", movieID),
		MovieID: movieID,
		Name:    fmt.Sprintf("%s.mp4", movieID),
	}
}

// TestRevertCorrectness_OrganizePath_SingleRecord verifies that when AllowRevert=true
// and the organize path is used, exactly ONE BatchFileOperation record exists per file
// after Apply — no double-write from both the API hook and the RevertLog seam.
func TestRevertCorrectness_OrganizePath_SingleRecord(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	cfg.Output.Operation.AllowRevert = true

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)
	rl := workflow.NewDBRevertLog(repo, workflow.NewRevertLogConfig(
		cfg.Output.Operation.AllowRevert,
		nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	), "organize-job-1", afero.NewOsFs(), template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
	match := revertTestMatch("TEST-001")

	cmd := workflow.ApplyCmd{
		Movie:    movie,
		Match:    match,
		DestPath: "/dest",
		Organize: workflow.OrganizeOptions{
			MoveFiles: true,
		},
		Download:    true,
		GenerateNFO: true,
	}

	opID, beginErr := rl.Begin(context.Background(), cmd)
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	completeErr := rl.Complete(context.Background(), opID, &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/TEST-001/TEST-001.mp4", InPlaceRenamed: false},
		NFOPath:        "/dest/TEST-001/TEST-001.nfo",
		DownloadPaths:  []string{"/dest/TEST-001/poster.jpg"},
	})
	require.NoError(t, completeErr)

	records, findErr := repo.FindByBatchJobID(context.Background(), "organize-job-1")
	require.NoError(t, findErr)

	assert.Len(t, records, 1, "Exactly ONE record per file — no double-write")
	assert.Equal(t, "TEST-001", records[0].MovieID)
	assert.Equal(t, "organize-job-1", records[0].BatchJobID)
}

// TestRevertCorrectness_UpdatePath_SingleRecord verifies that when AllowRevert=true
// and the update path is used (Organize.Skip=true), exactly ONE BatchFileOperation
// record exists per file after Apply.
func TestRevertCorrectness_UpdatePath_SingleRecord(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	cfg.Output.Operation.AllowRevert = true

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)
	rl := workflow.NewDBRevertLog(repo, workflow.NewRevertLogConfig(
		cfg.Output.Operation.AllowRevert,
		nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	), "update-job-1", afero.NewOsFs(), template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "TEST-002", Title: "Test Movie 2"}
	match := revertTestMatch("TEST-002")

	cmd := workflow.ApplyCmd{
		Movie:    movie,
		Match:    match,
		DestPath: "/source",
		Organize: workflow.OrganizeOptions{
			Skip: true,
		},
		Download:    true,
		GenerateNFO: true,
	}

	opID, beginErr := rl.Begin(context.Background(), cmd)
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	completeErr := rl.Complete(context.Background(), opID, &workflow.ApplyResult{
		NFOPath:       "/source/TEST-002.nfo",
		DownloadPaths: []string{"/source/poster.jpg"},
	})
	require.NoError(t, completeErr)

	records, findErr := repo.FindByBatchJobID(context.Background(), "update-job-1")
	require.NoError(t, findErr)

	assert.Len(t, records, 1, "Exactly ONE record per file in update mode")
	assert.Equal(t, "TEST-002", records[0].MovieID)
	assert.NotEmpty(t, records[0].GeneratedFiles, "GeneratedFiles should be populated by Complete")
}

// TestRevertCorrectness_RevertDisabled_RecordsStillWritten guards the regression where
// completed jobs showed "No operations recorded for this job" when AllowRevert was false.
// AllowRevert must NOT suppress recording — it gates only the revert *action* (enforced by
// the HTTP handlers), not the BatchFileOperation records that back the operations list.
func TestRevertCorrectness_RevertDisabled_RecordsStillWritten(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	cfg.Output.Operation.AllowRevert = false

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	repo := database.NewBatchFileOperationRepository(db)
	rl := workflow.NewRevertLogFromConfig(repo, workflow.NewRevertLogConfig(
		cfg.Output.Operation.AllowRevert,
		nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	), "disabled-job-1", afero.NewOsFs(), nil, nil, nil)

	movie := &models.Movie{ID: "TEST-003", Title: "Test Movie 3"}
	match := revertTestMatch("TEST-003")

	cmd := workflow.ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: workflow.OrganizeOptions{Skip: true},
		Download: true,
	}

	opID, beginErr := rl.Begin(context.Background(), cmd)
	require.NoError(t, beginErr)
	assert.NotEmpty(t, opID, "a record should be created even when AllowRevert=false")

	completeErr := rl.Complete(context.Background(), opID, &workflow.ApplyResult{})
	require.NoError(t, completeErr)

	count, countErr := repo.CountByBatchJobID(context.Background(), "disabled-job-1")
	require.NoError(t, countErr)
	assert.Equal(t, int64(1), count, "Records must be written even when AllowRevert=false (revert action is gated separately)")
}

// TestRevertCorrectness_OrganizePath_PostApplyStatePersisted verifies that after
// Apply through the organize path with AllowRevert=true, the revert record contains
// post-apply state sufficient to undo the operation.
func TestRevertCorrectness_OrganizePath_PostApplyStatePersisted(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	cfg.Output.Operation.AllowRevert = true

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)
	rl := workflow.NewDBRevertLog(repo, workflow.NewRevertLogConfig(
		cfg.Output.Operation.AllowRevert,
		nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	), "organize-job-2", afero.NewOsFs(), template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "TEST-004", Title: "Test Movie 4"}
	match := revertTestMatch("TEST-004")

	cmd := workflow.ApplyCmd{
		Movie:    movie,
		Match:    match,
		DestPath: "/dest",
		Organize: workflow.OrganizeOptions{
			MoveFiles: true,
		},
		Download:    true,
		GenerateNFO: true,
	}

	opID, beginErr := rl.Begin(context.Background(), cmd)
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	completeErr := rl.Complete(context.Background(), opID, &workflow.ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{NewPath: "/dest/TEST-004/TEST-004.mp4", InPlaceRenamed: false},
		NFOPath:        "/dest/TEST-004/TEST-004.nfo",
		DownloadPaths:  []string{"/dest/TEST-004/poster.jpg", "/dest/TEST-004/fanart.jpg"},
	})
	require.NoError(t, completeErr)

	records, findErr := repo.FindByBatchJobID(context.Background(), "organize-job-2")
	require.NoError(t, findErr)
	require.Len(t, records, 1)

	record := records[0]
	assert.Equal(t, "/dest/TEST-004/TEST-004.mp4", record.NewPath, "NewPath should be populated")
	assert.NotEmpty(t, record.GeneratedFiles, "GeneratedFiles should be non-empty")
	assert.False(t, record.InPlaceRenamed, "InPlaceRenamed should be false for organize mode")
	assert.Equal(t, models.RevertStatusApplied, record.RevertStatus)
}

// TestRevertCorrectness_UpdatePath_NFOSnapshotPopulated verifies that the update
// path captures an NFO snapshot in the revert record.
func TestRevertCorrectness_UpdatePath_NFOSnapshotPopulated(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	cfg.Output.Operation.AllowRevert = true

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)
	rl := workflow.NewDBRevertLog(repo, workflow.NewRevertLogConfig(
		cfg.Output.Operation.AllowRevert,
		nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	), "update-job-2", afero.NewOsFs(), template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "TEST-005", Title: "Test Movie 5"}
	match := revertTestMatch("TEST-005")

	cmd := workflow.ApplyCmd{
		Movie:    movie,
		Match:    match,
		DestPath: "/source",
		Organize: workflow.OrganizeOptions{
			Skip: true,
		},
		Download:    true,
		GenerateNFO: true,
	}

	opID, beginErr := rl.Begin(context.Background(), cmd)
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	completeErr := rl.Complete(context.Background(), opID, &workflow.ApplyResult{
		NFOPath:       "/source/TEST-005.nfo",
		DownloadPaths: []string{"/source/poster.jpg"},
	})
	require.NoError(t, completeErr)

	records, findErr := repo.FindByBatchJobID(context.Background(), "update-job-2")
	require.NoError(t, findErr)
	require.Len(t, records, 1)

	record := records[0]
	assert.Equal(t, models.OperationTypeUpdate, record.OperationType,
		"Update mode should have OperationTypeUpdate")
	assert.NotEmpty(t, record.GeneratedFiles, "GeneratedFiles should be populated by Complete")
}

// TestRevertCorrectness_MultipleFiles_NoDoubleWrite verifies that when multiple
// files are processed through the RevertLog seam, each file gets exactly one record.
func TestRevertCorrectness_MultipleFiles_NoDoubleWrite(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	cfg.Output.Operation.AllowRevert = true

	db, err := database.New(&database.Config{Type: cfg.Database.Type, DSN: cfg.Database.DSN, LogLevel: cfg.Database.LogLevel})
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	_, err = config.Prepare(cfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)

	for _, movieID := range []string{"FILE-001", "FILE-002", "FILE-003"} {
		rl := workflow.NewDBRevertLog(repo, workflow.NewRevertLogConfig(
			cfg.Output.Operation.AllowRevert,
			nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
		), "multi-job-1", afero.NewOsFs(), template.NewEngine(), nil, nil)
		match := revertTestMatch(movieID)
		cmd := workflow.ApplyCmd{
			Movie:    &models.Movie{ID: movieID, Title: fmt.Sprintf("Movie %s", movieID)},
			Match:    match,
			DestPath: "/dest",
			Organize: workflow.OrganizeOptions{MoveFiles: true},
			Download: true,
		}

		opID, beginErr := rl.Begin(context.Background(), cmd)
		require.NoError(t, beginErr)

		completeErr := rl.Complete(context.Background(), opID, &workflow.ApplyResult{
			OrganizeResult: &organizer.OrganizeResult{NewPath: fmt.Sprintf("/dest/%s/%s.mp4", movieID, movieID), InPlaceRenamed: false},
			NFOPath:        fmt.Sprintf("/dest/%s/%s.nfo", movieID, movieID),
			DownloadPaths:  []string{fmt.Sprintf("/dest/%s/poster.jpg", movieID)},
		})
		require.NoError(t, completeErr)
	}

	records, findErr := repo.FindByBatchJobID(context.Background(), "multi-job-1")
	require.NoError(t, findErr)

	assert.Len(t, records, 3, "One record per file, no double-write")
	movieIDs := make(map[string]bool)
	for _, r := range records {
		assert.False(t, movieIDs[r.MovieID], "No duplicate records for movie %s", r.MovieID)
		movieIDs[r.MovieID] = true
	}
}
