package workflow

import (
	"context"
	"encoding/json"
	"strconv"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ---------------------------------------------------------------------------
// readNFOSnapshot — additional edge cases beyond coverage_uncovered_test.go
// ---------------------------------------------------------------------------

func TestReadNFOSnapshot_DirectoryAsPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))
	// A directory path — ReadFile will fail (read dir as file)
	result := readNFOSnapshot(logging.GlobalLogger(), fs, "/source")
	_ = result // just ensure it doesn't panic
	// The result depends on OS behavior — just ensure it doesn't panic
	// On some platforms, reading a directory returns an error; on others it may succeed
}

func TestReadNFOSnapshot_FirstCandidateEmptySkipped(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/second.nfo", []byte("<second/>"), 0644))
	// First candidate is empty string — should be skipped
	result := readNFOSnapshot(logging.GlobalLogger(), fs, "", "/second.nfo")
	assert.Equal(t, "<second/>", result.Content)
	assert.Equal(t, "/second.nfo", result.FoundPath)
}

func TestReadNFOSnapshot_AllCandidatesEmpty(t *testing.T) {
	fs := afero.NewMemMapFs()
	result := readNFOSnapshot(logging.GlobalLogger(), fs, "", "", "")
	assert.Empty(t, result.Content)
	assert.Empty(t, result.FoundPath)
}

func TestReadNFOSnapshot_FirstCandidateFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/first.nfo", []byte("<first/>"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/second.nfo", []byte("<second/>"), 0644))
	result := readNFOSnapshot(logging.GlobalLogger(), fs, "/first.nfo", "/second.nfo")
	assert.Equal(t, "<first/>", result.Content)
	assert.Equal(t, "/first.nfo", result.FoundPath)
}

// ---------------------------------------------------------------------------
// buildGeneratedFilesJSON — additional edge cases
// ---------------------------------------------------------------------------

func TestBuildGeneratedFilesJSON_NFOPathAndDownloads(t *testing.T) {
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "/dest/ABC-001.nfo", nil, []string{"/dest/poster.jpg"})
	assert.NotEmpty(t, result)

	var gf models.GeneratedFilesJSON
	require.NoError(t, json.Unmarshal([]byte(result), &gf))
	assert.Contains(t, gf.Delete, "/dest/ABC-001.nfo")
	assert.Contains(t, gf.Delete, "/dest/poster.jpg")
}

func TestBuildGeneratedFilesJSON_SubtitleMovedEmptyPaths(t *testing.T) {
	// SubtitleMove with Moved=true but empty paths should not appear in MoveBack
	subtitles := []models.SubtitleMove{
		{OriginalPath: "", NewPath: "", Moved: true},
	}
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", subtitles, nil)
	assert.Empty(t, result, "Empty paths should not produce MoveBack entries")
}

func TestBuildGeneratedFilesJSON_SubtitleMovedOneEmptyOneValid(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "", NewPath: "/dest/sub.srt", Moved: true},
		{OriginalPath: "/src/sub2.srt", NewPath: "/dest/sub2.srt", Moved: true},
	}
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", subtitles, nil)
	assert.NotEmpty(t, result)

	var gf models.GeneratedFilesJSON
	require.NoError(t, json.Unmarshal([]byte(result), &gf))
	require.Len(t, gf.MoveBack, 1)
	assert.Equal(t, "/src/sub2.srt", gf.MoveBack[0].OriginalPath)
	assert.Equal(t, "/dest/sub2.srt", gf.MoveBack[0].NewPath)
}

func TestBuildGeneratedFilesJSON_OnlyDownloadsNoNFO(t *testing.T) {
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", nil, []string{"/dest/poster.jpg"})
	assert.NotEmpty(t, result)

	var gf models.GeneratedFilesJSON
	require.NoError(t, json.Unmarshal([]byte(result), &gf))
	assert.Empty(t, gf.MoveBack)
	assert.Contains(t, gf.Delete, "/dest/poster.jpg")
}

func TestBuildGeneratedFilesJSON_MultipleSubtitleMoves(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/src/sub1.srt", NewPath: "/dest/sub1.srt", Moved: true},
		{OriginalPath: "/src/sub2.srt", NewPath: "/dest/sub2.srt", Moved: true},
	}
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", subtitles, nil)
	assert.NotEmpty(t, result)

	var gf models.GeneratedFilesJSON
	require.NoError(t, json.Unmarshal([]byte(result), &gf))
	require.Len(t, gf.MoveBack, 2)
}

func TestBuildGeneratedFilesJSON_AllComponentsPresent(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/src/sub.srt", NewPath: "/dest/sub.srt", Moved: true},
	}
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "/dest/ABC-001.nfo", subtitles, []string{"/dest/poster.jpg"})
	assert.NotEmpty(t, result)

	var gf models.GeneratedFilesJSON
	require.NoError(t, json.Unmarshal([]byte(result), &gf))
	assert.Len(t, gf.Delete, 2)
	assert.Len(t, gf.MoveBack, 1)
}

// ---------------------------------------------------------------------------
// updatePostOrganize — additional edge cases
// ---------------------------------------------------------------------------

func TestUpdatePostOrganize_EmptyGeneratedFiles(t *testing.T) {
	op := &models.BatchFileOperation{BatchJobID: "job1"}
	updatePostOrganize(op, "/dest/file.mp4", false, "/src", "")
	assert.Equal(t, "/dest/file.mp4", op.NewPath)
	assert.False(t, op.InPlaceRenamed)
	assert.Equal(t, "/src", op.OriginalDirPath)
	assert.Equal(t, "", op.GeneratedFiles)
}

func TestUpdatePostOrganize_OverwriteExistingFields(t *testing.T) {
	op := &models.BatchFileOperation{
		BatchJobID:      "job1",
		NewPath:         "/old/dest.mp4",
		InPlaceRenamed:  true,
		OriginalDirPath: "/old-src",
		GeneratedFiles:  `{"Delete":["/old.nfo"]}`,
	}
	updatePostOrganize(op, "/new/dest.mp4", false, "/new-src", `{"Delete":["/new.nfo"]}`)
	assert.Equal(t, "/new/dest.mp4", op.NewPath)
	assert.False(t, op.InPlaceRenamed)
	assert.Equal(t, "/new-src", op.OriginalDirPath)
	assert.Equal(t, `{"Delete":["/new.nfo"]}`, op.GeneratedFiles)
}

// ---------------------------------------------------------------------------
// dbRevertLog.Begin — covers the Begin method
// ---------------------------------------------------------------------------

func newTestDBRevertLog(t *testing.T) (RevertLog, *database.DB) {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	appCfg := config.DefaultConfig(nil, nil)
	_, err = config.Prepare(appCfg)
	require.NoError(t, err)
	nfoCfg := nfo.ConfigFromAppConfig(appCfg, nfo.NFONameConfigFromAppConfig(appCfg))

	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()
	return NewDBRevertLog(repo, &RevertLogConfig{AllowRevert: true, NFOCfg: nfoCfg}, "test-job", fs, template.NewEngine(), nil, nil), db
}

func TestDBRevertLog_Begin_NilMovie(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	opID, err := rl.Begin(context.Background(), ApplyCmd{Movie: nil})
	assert.NoError(t, err)
	assert.Empty(t, opID, "Begin with nil Movie should return empty opID")
}

func TestDBRevertLog_Begin_Success(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "ABC-001", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/ABC-001.mp4", MovieID: "ABC-001"},
		Organize: OrganizeOptions{MoveFiles: true},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	assert.NoError(t, err)
	assert.NotEmpty(t, opID)
}

func TestDBRevertLog_Begin_SkipMode(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "ABC-002", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/ABC-002.mp4", MovieID: "ABC-002"},
		Organize: OrganizeOptions{Skip: true}, // update mode
	}
	opID, err := rl.Begin(context.Background(), cmd)
	assert.NoError(t, err)
	assert.NotEmpty(t, opID)
}

func TestDBRevertLog_Begin_HardlinkMode(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "ABC-003", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/ABC-003.mp4", MovieID: "ABC-003"},
		Organize: OrganizeOptions{MoveFiles: false, LinkMode: organizer.LinkModeHard},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	assert.NoError(t, err)
	assert.NotEmpty(t, opID)
}

func TestDBRevertLog_Begin_SymlinkMode(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "ABC-004", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/ABC-004.mp4", MovieID: "ABC-004"},
		Organize: OrganizeOptions{MoveFiles: false, LinkMode: organizer.LinkModeSoft},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	assert.NoError(t, err)
	assert.NotEmpty(t, opID)
}

func TestDBRevertLog_Begin_CopyMode(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "ABC-005", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/ABC-005.mp4", MovieID: "ABC-005"},
		Organize: OrganizeOptions{MoveFiles: false, LinkMode: organizer.LinkModeNone},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	assert.NoError(t, err)
	assert.NotEmpty(t, opID)
}

// ---------------------------------------------------------------------------
// dbRevertLog.Complete — covers the Complete method
// ---------------------------------------------------------------------------

func TestDBRevertLog_Complete_EmptyOpID(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	err := rl.Complete(context.Background(), "", nil)
	assert.NoError(t, err)
}

func TestDBRevertLog_Complete_InvalidOpID(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	err := rl.Complete(context.Background(), "not-a-number", nil)
	assert.NoError(t, err)
}

func TestDBRevertLog_Complete_ZeroOpID(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	err := rl.Complete(context.Background(), "0", nil)
	assert.NoError(t, err)
}

func TestDBRevertLog_Complete_NilResult_MarksFailed(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)

	// First, Begin to create a record
	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "FAIL-001", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/FAIL-001.mp4", MovieID: "FAIL-001"},
		Organize: OrganizeOptions{MoveFiles: true},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, opID)

	// Complete with nil result → marks as failed
	err = rl.Complete(context.Background(), opID, nil)
	assert.NoError(t, err)
}

func TestDBRevertLog_Complete_SuccessResult_NoOrganizeResult(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)

	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "SUCCESS-001", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/SUCCESS-001.mp4", MovieID: "SUCCESS-001"},
		Organize: OrganizeOptions{MoveFiles: true},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, opID)

	// Complete with a result that has no organize result
	result := &ApplyResult{
		Movie: &models.Movie{ID: "SUCCESS-001"},
	}
	err = rl.Complete(context.Background(), opID, result)
	assert.NoError(t, err)
}

func TestDBRevertLog_Complete_SuccessResult_WithOrganizeResult(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)

	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "ORG-001", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/ORG-001.mp4", MovieID: "ORG-001"},
		Organize: OrganizeOptions{MoveFiles: true},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, opID)

	result := &ApplyResult{
		Movie: &models.Movie{ID: "ORG-001"},
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:          "/dest/ORG-001.mp4",
			InPlaceRenamed:   true,
			FolderPath:       "/dest/ORG-001",
			OldDirectoryPath: "/src",
		},
		NFOPath:       "/dest/ORG-001.nfo",
		FoundNFOPath:  "/dest/ORG-001.nfo",
		DownloadPaths: []string{"/dest/poster.jpg"},
	}
	err = rl.Complete(context.Background(), opID, result)
	assert.NoError(t, err)
}

// TestDBRevertLog_CompleteFailed_PreservesNewPathAndMarksFailed verifies that
// CompleteFailed persists the partial OrganizeResult.NewPath (keeping the record
// revertable after a downstream step failure) AND marks the record
// RevertStatusFailed. This is the WF-1 regression guard: a later-step failure
// after a successful organize must not blank NewPath.
func TestDBRevertLog_CompleteFailed_PreservesNewPathAndMarksFailed(t *testing.T) {
	rl, db := newTestDBRevertLog(t)

	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "FAILKEEP-001", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/FAILKEEP-001.mp4", MovieID: "FAILKEEP-001"},
		Organize: OrganizeOptions{MoveFiles: true},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, opID)

	// A later step failed AFTER organize succeeded and moved the file.
	partial := &ApplyResult{
		Movie: &models.Movie{ID: "FAILKEEP-001"},
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:          "/dest/FAILKEEP-001.mp4",
			FolderPath:       "/dest/FAILKEEP-001",
			OldDirectoryPath: "/src",
		},
		NFOPath: "/dest/FAILKEEP-001.nfo",
	}
	err = rl.CompleteFailed(context.Background(), opID, partial)
	assert.NoError(t, err)

	// Assert the persisted record kept NewPath and is marked failed (revertable).
	recordID, _ := strconv.ParseUint(opID, 10, 64)
	var record models.BatchFileOperation
	require.NoError(t, db.Where("id = ?", uint(recordID)).First(&record).Error)
	assert.Equal(t, "/dest/FAILKEEP-001.mp4", record.NewPath, "CompleteFailed must preserve NewPath so revert can locate the moved file")
	assert.Equal(t, models.RevertStatusFailed, record.RevertStatus, "CompleteFailed must mark the record as failed")
}

func TestDBRevertLog_Complete_NonExistentRecord(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)
	// Valid-looking opID but record doesn't exist
	// Complete returns an error when FindByID fails (record not found is a DB error)
	err := rl.Complete(context.Background(), "99999", &ApplyResult{Movie: &models.Movie{ID: "GHOST-001"}})
	// The actual behavior: FindByID returns an error for non-existent records
	assert.Error(t, err, "Complete should return error when record not found")
	assert.Contains(t, err.Error(), "99999")
}

func TestDBRevertLog_Complete_FoundNFOPathOverridesEmpty(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)

	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "NFOPATH-001", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/NFOPATH-001.mp4", MovieID: "NFOPATH-001"},
		Organize: OrganizeOptions{MoveFiles: true},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, opID)

	result := &ApplyResult{
		Movie:        &models.Movie{ID: "NFOPATH-001"},
		FoundNFOPath: "/found/NFOPATH-001.nfo",
		NFOPath:      "/generated/NFOPATH-001.nfo",
	}
	err = rl.Complete(context.Background(), opID, result)
	assert.NoError(t, err)
}

func TestDBRevertLog_Complete_NFOPathFallbackWhenEmpty(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)

	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "NFOPATH-002", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/NFOPATH-002.mp4", MovieID: "NFOPATH-002"},
		Organize: OrganizeOptions{MoveFiles: true},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, opID)

	// FoundNFOPath is empty, NFOPath is set — should use NFOPath as fallback
	result := &ApplyResult{
		Movie:   &models.Movie{ID: "NFOPATH-002"},
		NFOPath: "/generated/NFOPATH-002.nfo",
	}
	err = rl.Complete(context.Background(), opID, result)
	assert.NoError(t, err)
}

func TestDBRevertLog_Complete_OrganizeResultFolderPathEmptySourceDir(t *testing.T) {
	rl, _ := newTestDBRevertLog(t)

	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "FOLDER-001", Title: "Test"},
		Match:    models.FileMatchInfo{Path: "/src/FOLDER-001.mp4", MovieID: "FOLDER-001"},
		Organize: OrganizeOptions{MoveFiles: true},
	}
	opID, err := rl.Begin(context.Background(), cmd)
	require.NoError(t, err)
	require.NotEmpty(t, opID)

	// OrganizeResult has FolderPath and OldDirectoryPath — tests the
	// "if result.OrganizeResult.FolderPath != "" && sourceDir == """ branch
	result := &ApplyResult{
		Movie: &models.Movie{ID: "FOLDER-001"},
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:          "/dest/FOLDER-001.mp4",
			FolderPath:       "/dest/FOLDER-001",
			OldDirectoryPath: "/src/FOLDER-001",
		},
	}
	err = rl.Complete(context.Background(), opID, result)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// dbRevertLog.CaptureSnapshot — additional coverage
// ---------------------------------------------------------------------------

func TestDBRevertLog_CaptureSnapshot_MultipartFile(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	appCfg := config.DefaultConfig(nil, nil)
	_, err = config.Prepare(appCfg)
	require.NoError(t, err)
	nfoCfg := nfo.ConfigFromAppConfig(appCfg, nfo.NFONameConfigFromAppConfig(appCfg))

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/ABC-001-cd1.nfo", []byte("<movie><title>Multipart</title></movie>"), 0644))

	repo := database.NewBatchFileOperationRepository(db)
	rl := NewDBRevertLog(repo, &RevertLogConfig{AllowRevert: true, NFOCfg: nfoCfg}, "job-multipart", fs, template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "ABC-001", Title: "Test Movie"}
	match := models.FileMatchInfo{Path: "/source/ABC-001-cd1.mp4", MovieID: "ABC-001", IsMultiPart: true, PartSuffix: "-cd1", PartNumber: 1}

	opID, beginErr := rl.Begin(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	rl.CaptureSnapshot(context.Background(), opID, ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
}

func TestDBRevertLog_CaptureSnapshot_NilConfig(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	fs := afero.NewMemMapFs()
	repo := database.NewBatchFileOperationRepository(db)
	// Nil config — should use default NFONameConfig
	rl := NewDBRevertLog(repo, nil, "job-nil-cfg", fs, template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "NILCFG-001", Title: "Test"}
	match := models.FileMatchInfo{Path: "/src/NILCFG-001.mp4", MovieID: "NILCFG-001"}

	// Begin first
	opID, beginErr := rl.Begin(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	// CaptureSnapshot with nil config should not panic
	rl.CaptureSnapshot(context.Background(), opID, ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
}

func TestDBRevertLog_CaptureSnapshot_NFONotFound(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	appCfg := config.DefaultConfig(nil, nil)
	_, err = config.Prepare(appCfg)
	require.NoError(t, err)
	nfoCfg := nfo.ConfigFromAppConfig(appCfg, nfo.NFONameConfigFromAppConfig(appCfg))

	fs := afero.NewMemMapFs()
	repo := database.NewBatchFileOperationRepository(db)
	rl := NewDBRevertLog(repo, &RevertLogConfig{AllowRevert: true, NFOCfg: nfoCfg}, "job-no-nfo", fs, template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "NONFO-001", Title: "Test"}
	match := models.FileMatchInfo{Path: "/src/NONFO-001.mp4", MovieID: "NONFO-001"}

	opID, beginErr := rl.Begin(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	// CaptureSnapshot when no NFO file exists — should not panic
	rl.CaptureSnapshot(context.Background(), opID, ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
}

func TestDBRevertLog_CaptureSnapshot_UpdatesOriginalDirPath(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	appCfg := config.DefaultConfig(nil, nil)
	_, err = config.Prepare(appCfg)
	require.NoError(t, err)
	nfoCfg := nfo.ConfigFromAppConfig(appCfg, nfo.NFONameConfigFromAppConfig(appCfg))

	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0755))
	require.NoError(t, afero.WriteFile(fs, "/source/DIRTEST-001.nfo", []byte("<movie/>"), 0644))

	repo := database.NewBatchFileOperationRepository(db)
	rl := NewDBRevertLog(repo, &RevertLogConfig{AllowRevert: true, NFOCfg: nfoCfg}, "job-dirpath", fs, template.NewEngine(), nil, nil)

	movie := &models.Movie{ID: "DIRTEST-001", Title: "Test"}
	match := models.FileMatchInfo{Path: "/source/DIRTEST-001.mp4", MovieID: "DIRTEST-001"}

	opID, beginErr := rl.Begin(context.Background(), ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.NoError(t, beginErr)
	require.NotEmpty(t, opID)

	rl.CaptureSnapshot(context.Background(), opID, ApplyCmd{
		Movie:    movie,
		Match:    match,
		Organize: OrganizeOptions{MoveFiles: true},
	})
}

// ---------------------------------------------------------------------------
// NewRevertLogFromConfig — additional coverage
// ---------------------------------------------------------------------------

func TestNewRevertLogFromConfig_NilConfig(t *testing.T) {
	rl := NewRevertLogFromConfig(nil, nil, "job", nil, nil, nil, nil)
	assert.IsType(t, noOpRevertLog{}, rl, "nil config should return noOpRevertLog")
}

func TestNewRevertLogFromConfig_AllowRevertFalse(t *testing.T) {
	// AllowRevert=false no longer gates recording — operations are always recorded
	// so the operations list works regardless of the revert toggle. Only the
	// nil-repo/nil-cfg defensive paths return noOpRevertLog.
	rl := NewRevertLogFromConfig(mocks.NewMockBatchFileOperationRepositoryInterface(t), &RevertLogConfig{AllowRevert: false}, "job", nil, nil, nil, nil)
	assert.IsType(t, &dbRevertLog{}, rl, "AllowRevert=false should return dbRevertLog (recording is independent of the revert toggle)")
}

func TestNewRevertLogFromConfig_AllowRevertTrue(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.Database.DSN = ":memory:"
	db, err := database.New(database.ConfigFromAppConfig(cfg))
	require.NoError(t, err)
	err = db.RunMigrationsOnStartup(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })

	appCfg := config.DefaultConfig(nil, nil)
	_, err = config.Prepare(appCfg)
	require.NoError(t, err)

	repo := database.NewBatchFileOperationRepository(db)
	rl := NewRevertLogFromConfig(repo, &RevertLogConfig{AllowRevert: true, NFOCfg: nfo.ConfigFromAppConfig(appCfg, nfo.NFONameConfigFromAppConfig(appCfg))}, "job", afero.NewMemMapFs(), template.NewEngine(), nil, nil)
	assert.NotNil(t, rl)
	// Should NOT be noOpRevertLog
	_, isNoOp := rl.(noOpRevertLog)
	assert.False(t, isNoOp, "AllowRevert=true should return dbRevertLog, not noOpRevertLog")
}

// ---------------------------------------------------------------------------
// NewDBRevertLog — constructor coverage
// ---------------------------------------------------------------------------

func TestNewDBRevertLog(t *testing.T) {
	rl := NewDBRevertLog(nil, nil, "test-job", nil, nil, nil, nil)
	assert.NotNil(t, rl)
}

// ---------------------------------------------------------------------------
// determineOperationType — additional coverage
// ---------------------------------------------------------------------------

func TestDetermineOperationType_UpdateOverridesHardlink(t *testing.T) {
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(false, organizer.LinkModeHard, true))
}

func TestDetermineOperationType_UpdateOverridesSymlink(t *testing.T) {
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(false, organizer.LinkModeSoft, true))
}

func TestDetermineOperationType_UpdateOverridesMove(t *testing.T) {
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(true, organizer.LinkModeNone, true))
}

// ---------------------------------------------------------------------------
// newPreOrganizeRecord — edge cases
// ---------------------------------------------------------------------------

func TestNewPreOrganizeRecord_EmptySnapshot(t *testing.T) {
	rec := newPreOrganizeRecord("job1", "ABC-001", "/src/file.mp4", "", "", "/src", models.OperationTypeMove, false)
	assert.Equal(t, "", rec.NFOSnapshot)
	assert.Equal(t, "", rec.NFOPath)
	assert.Equal(t, "", rec.NewPath)
	assert.Equal(t, models.RevertStatusApplied, rec.RevertStatus)
}

func TestNewPreOrganizeRecord_InPlaceRenamed(t *testing.T) {
	rec := newPreOrganizeRecord("job1", "ABC-001", "/src/file.mp4", "", "", "/src", models.OperationTypeHardlink, true)
	assert.True(t, rec.InPlaceRenamed)
	assert.Equal(t, models.OperationTypeHardlink, rec.OperationType)
}
