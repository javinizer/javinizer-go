package workflow

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Miss tests for revert_log.go ---
// Focuses on: dbRevertLog.Begin, CaptureSnapshot, Complete error paths,
// noOpRevertLog, NewRevertLogFromConfig, readNFOSnapshot, determineOperationType,
// buildGeneratedFilesJSON, updatePostOrganize

// noOpRevertLog: all methods return zero values
func TestMiss_noOpRevertLog_Begin(t *testing.T) {
	log := noOpRevertLog{}
	opID, err := log.Begin(context.Background(), ApplyCmd{})
	assert.Empty(t, opID)
	assert.NoError(t, err)
}

func TestMiss_noOpRevertLog_CaptureSnapshot(t *testing.T) {
	log := noOpRevertLog{}
	log.CaptureSnapshot(context.Background(), "op1", ApplyCmd{})
	// Should not panic
}

func TestMiss_noOpRevertLog_Complete(t *testing.T) {
	log := noOpRevertLog{}
	err := log.Complete(context.Background(), "op1", nil)
	assert.NoError(t, err)
}

// NewRevertLogFromConfig: nil config returns noOp
func TestMiss_NewRevertLogFromConfig_NilConfig(t *testing.T) {
	log := NewRevertLogFromConfig(nil, nil, "job1", nil, nil, nil, nil)
	_, ok := log.(noOpRevertLog)
	assert.True(t, ok)
}

// NewRevertLogFromConfig: AllowRevert=false still returns dbRevertLog (recording is
// independent of the revert toggle — guards the "No operations recorded" regression).
func TestMiss_NewRevertLogFromConfig_RevertDisabled(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	cfg := &RevertLogConfig{AllowRevert: false}
	log := NewRevertLogFromConfig(mockRepo, cfg, "job1", nil, nil, nil, nil)
	_, ok := log.(*dbRevertLog)
	assert.True(t, ok)
}

// NewRevertLogFromConfig: AllowRevert=true returns dbRevertLog
func TestMiss_NewRevertLogFromConfig_RevertEnabled(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	cfg := &RevertLogConfig{AllowRevert: true}
	log := NewRevertLogFromConfig(mockRepo, cfg, "job1", nil, nil, nil, nil)
	_, ok := log.(*dbRevertLog)
	assert.True(t, ok)
}

// dbRevertLog.Begin: nil Movie returns empty opID
func TestMiss_dbRevertLog_Begin_NilMovie(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	opID, err := log.Begin(context.Background(), ApplyCmd{Movie: nil})
	assert.Empty(t, opID)
	assert.NoError(t, err)
}

// dbRevertLog.Begin: Create error
func TestMiss_dbRevertLog_Begin_CreateError(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	mockRepo.On("Create", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(fmt.Errorf("db error"))

	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001"},
		Match:    models.FileMatchInfo{Path: "/test/file.mp4"},
		Organize: OrganizeOptions{Skip: false, MoveFiles: true},
	}
	opID, err := log.Begin(context.Background(), cmd)
	assert.Empty(t, opID)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "revert log Begin failed")
}

// dbRevertLog.Begin: success with move operation
func TestMiss_dbRevertLog_Begin_SuccessMove(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	mockRepo.On("Create", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(nil).Run(func(args mock.Arguments) {
		op := args.Get(1).(*models.BatchFileOperation)
		op.ID = 42 // Simulate auto-increment
	})

	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-001"},
		Match:    models.FileMatchInfo{Path: "/test/file.mp4"},
		Organize: OrganizeOptions{Skip: false, MoveFiles: true},
	}
	opID, err := log.Begin(context.Background(), cmd)
	assert.NoError(t, err)
	assert.Equal(t, "42", opID)
}

// dbRevertLog.Begin: update mode
func TestMiss_dbRevertLog_Begin_UpdateMode(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	mockRepo.On("Create", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(nil).Run(func(args mock.Arguments) {
		op := args.Get(1).(*models.BatchFileOperation)
		op.ID = 43
	})

	cmd := ApplyCmd{
		Movie:    &models.Movie{ID: "TEST-002"},
		Match:    models.FileMatchInfo{Path: "/test/file.mp4"},
		Organize: OrganizeOptions{Skip: true}, // Update mode
	}
	opID, err := log.Begin(context.Background(), cmd)
	assert.NoError(t, err)
	assert.Equal(t, "43", opID)
}

// dbRevertLog.CaptureSnapshot: empty opID
func TestMiss_dbRevertLog_CaptureSnapshot_EmptyOpID(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	// Should not call repo — empty opID
	log.CaptureSnapshot(context.Background(), "", ApplyCmd{})
}

// dbRevertLog.CaptureSnapshot: nil Movie
func TestMiss_dbRevertLog_CaptureSnapshot_NilMovie(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	// Should not call repo — nil Movie
	log.CaptureSnapshot(context.Background(), "1", ApplyCmd{Movie: nil})
}

// dbRevertLog.CaptureSnapshot: invalid opID
func TestMiss_dbRevertLog_CaptureSnapshot_InvalidOpID(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	// Should not call repo — invalid opID
	log.CaptureSnapshot(context.Background(), "not-a-number", ApplyCmd{Movie: &models.Movie{ID: "TEST"}})
}

// dbRevertLog.CaptureSnapshot: FindByID returns error
func TestMiss_dbRevertLog_CaptureSnapshot_FindError(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	mockRepo.On("FindByID", mock.Anything, uint(1)).Return(nil, fmt.Errorf("db error"))

	log.CaptureSnapshot(context.Background(), "1", ApplyCmd{
		Movie: &models.Movie{ID: "TEST"},
		Match: models.FileMatchInfo{Path: "/test/file.mp4"},
	})
}

// dbRevertLog.CaptureSnapshot: FindByID returns nil
func TestMiss_dbRevertLog_CaptureSnapshot_FindNil(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	mockRepo.On("FindByID", mock.Anything, uint(1)).Return(nil, nil)

	log.CaptureSnapshot(context.Background(), "1", ApplyCmd{
		Movie: &models.Movie{ID: "TEST"},
		Match: models.FileMatchInfo{Path: "/test/file.mp4"},
	})
}

// dbRevertLog.Complete: empty opID returns nil
func TestMiss_dbRevertLog_Complete_EmptyOpID(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	err := log.Complete(context.Background(), "", nil)
	assert.NoError(t, err)
}

// dbRevertLog.Complete: invalid opID returns nil
func TestMiss_dbRevertLog_Complete_InvalidOpID(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	err := log.Complete(context.Background(), "not-a-number", nil)
	assert.NoError(t, err)
}

// dbRevertLog.Complete: zero opID returns nil
func TestMiss_dbRevertLog_Complete_ZeroOpID(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	err := log.Complete(context.Background(), "0", nil)
	assert.NoError(t, err)
}

// dbRevertLog.Complete: FindByID returns error
func TestMiss_dbRevertLog_Complete_FindError(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	mockRepo.On("FindByID", mock.Anything, uint(1)).Return(nil, fmt.Errorf("db error"))

	err := log.Complete(context.Background(), "1", &ApplyResult{})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "revert log Complete: find record")
}

// dbRevertLog.Complete: FindByID returns nil
func TestMiss_dbRevertLog_Complete_FindNil(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	mockRepo.On("FindByID", mock.Anything, uint(1)).Return(nil, nil)

	err := log.Complete(context.Background(), "1", &ApplyResult{})
	assert.NoError(t, err)
}

// dbRevertLog.Complete: nil result → mark as failed
func TestMiss_dbRevertLog_Complete_NilResult(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	preRecord := &models.BatchFileOperation{
		ID:           1,
		BatchJobID:   "job1",
		RevertStatus: models.RevertStatusApplied,
	}
	mockRepo.On("FindByID", mock.Anything, uint(1)).Return(preRecord, nil)
	mockRepo.On("Update", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(nil)

	err := log.Complete(context.Background(), "1", nil)
	assert.NoError(t, err)
}

// dbRevertLog.Complete: nil result, Update returns error
func TestMiss_dbRevertLog_Complete_NilResultUpdateError(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	preRecord := &models.BatchFileOperation{
		ID:           1,
		BatchJobID:   "job1",
		RevertStatus: models.RevertStatusApplied,
	}
	mockRepo.On("FindByID", mock.Anything, uint(1)).Return(preRecord, nil)
	mockRepo.On("Update", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(fmt.Errorf("db error"))

	err := log.Complete(context.Background(), "1", nil)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "mark record")
}

// dbRevertLog.Complete: success result
func TestMiss_dbRevertLog_Complete_SuccessResult(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	preRecord := &models.BatchFileOperation{
		ID:           1,
		BatchJobID:   "job1",
		RevertStatus: models.RevertStatusApplied,
	}
	mockRepo.On("FindByID", mock.Anything, uint(1)).Return(preRecord, nil)
	mockRepo.On("Update", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(nil)

	result := &ApplyResult{
		OrganizeResult: &organizer.OrganizeResult{
			NewPath:        "/output/file.mp4",
			InPlaceRenamed: false,
		},
		NFOPath:      "/output/file.nfo",
		FoundNFOPath: "/output/file.nfo",
	}
	err := log.Complete(context.Background(), "1", result)
	assert.NoError(t, err)
}

// dbRevertLog.Complete: success result, Update returns error
func TestMiss_dbRevertLog_Complete_SuccessResultUpdateError(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	log := NewDBRevertLog(mockRepo, nil, "job1", nil, nil, nil, nil)

	preRecord := &models.BatchFileOperation{
		ID:           1,
		BatchJobID:   "job1",
		RevertStatus: models.RevertStatusApplied,
	}
	mockRepo.On("FindByID", mock.Anything, uint(1)).Return(preRecord, nil)
	mockRepo.On("Update", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(fmt.Errorf("db error"))

	result := &ApplyResult{}
	err := log.Complete(context.Background(), "1", result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "update post-apply record")
}

// readNFOSnapshot: empty path
func TestMiss_readNFOSnapshot_EmptyPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	result := readNFOSnapshot(logging.GlobalLogger(), fs, "")
	assert.Equal(t, "", result.Content)
	assert.Equal(t, "", result.FoundPath)
}

// readNFOSnapshot: file found
func TestMiss_readNFOSnapshot_FileFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/test/movie.nfo", []byte("<nfo>test</nfo>"), 0644))

	result := readNFOSnapshot(logging.GlobalLogger(), fs, "/test/movie.nfo")
	assert.Equal(t, "<nfo>test</nfo>", result.Content)
	assert.NotEmpty(t, result.FoundPath)
}

// readNFOSnapshot: file not found
func TestMiss_readNFOSnapshot_FileNotFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	result := readNFOSnapshot(logging.GlobalLogger(), fs, "/test/nonexistent.nfo")
	assert.Equal(t, "", result.Content)
	assert.Equal(t, "", result.FoundPath)
}

// readNFOSnapshot: multiple candidates, first found wins
func TestMiss_readNFOSnapshot_MultipleCandidates(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/test/second.nfo", []byte("<nfo>second</nfo>"), 0644))

	result := readNFOSnapshot(logging.GlobalLogger(), fs, "/test/first.nfo", "/test/second.nfo")
	assert.Equal(t, "<nfo>second</nfo>", result.Content)
}

// determineOperationType: all variants
func TestMiss_determineOperationType(t *testing.T) {
	tests := []struct {
		name      string
		moveFiles bool
		linkMode  organizer.LinkMode
		isUpdate  bool
		expected  models.OperationTypeEnum
	}{
		{"update", true, organizer.LinkModeNone, true, models.OperationTypeUpdate},
		{"move", true, organizer.LinkModeNone, false, models.OperationTypeMove},
		{"hardlink", false, organizer.LinkModeHard, false, models.OperationTypeHardlink},
		{"symlink", false, organizer.LinkModeSoft, false, models.OperationTypeSymlink},
		{"copy", false, organizer.LinkModeNone, false, models.OperationTypeCopy},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := determineOperationType(tt.moveFiles, tt.linkMode, tt.isUpdate)
			assert.Equal(t, tt.expected, result)
		})
	}
}

// buildGeneratedFilesJSON: empty paths
func TestMiss_buildGeneratedFilesJSON_EmptyPaths(t *testing.T) {
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", nil, nil)
	assert.Equal(t, "", result)
}

// buildGeneratedFilesJSON: with nfoPath
func TestMiss_buildGeneratedFilesJSON_WithNFOPath(t *testing.T) {
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "/output/movie.nfo", nil, nil)
	assert.NotEmpty(t, result)
	assert.Contains(t, result, "movie.nfo")
}

// buildGeneratedFilesJSON: with downloadPaths
func TestMiss_buildGeneratedFilesJSON_WithDownloadPaths(t *testing.T) {
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "/output/movie.nfo", nil, []string{"/output/poster.jpg"})
	assert.NotEmpty(t, result)
}

// buildGeneratedFilesJSON: with subtitle moves
func TestMiss_buildGeneratedFilesJSON_WithSubtitleMoves(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/src/sub.srt", NewPath: "/output/sub.srt", Moved: true},
	}
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "/output/movie.nfo", subtitles, nil)
	assert.NotEmpty(t, result)
}

// buildGeneratedFilesJSON: subtitle not moved
func TestMiss_buildGeneratedFilesJSON_SubtitleNotMoved(t *testing.T) {
	subtitles := []models.SubtitleMove{
		{OriginalPath: "/src/sub.srt", NewPath: "/output/sub.srt", Moved: false},
	}
	result := buildGeneratedFilesJSON(logging.GlobalLogger(), "", subtitles, nil)
	assert.Equal(t, "", result) // Not moved, no nfoPath, no downloads → empty
}

// updatePostOrganize: updates fields
func TestMiss_updatePostOrganize(t *testing.T) {
	op := &models.BatchFileOperation{
		ID:           1,
		BatchJobID:   "job1",
		RevertStatus: models.RevertStatusApplied,
	}
	updatePostOrganize(op, "/new/path.mp4", true, "/original/dir", `{"delete":["/new/movie.nfo"]}`)

	assert.Equal(t, "/new/path.mp4", op.NewPath)
	assert.True(t, op.InPlaceRenamed)
	assert.Equal(t, "/original/dir", op.OriginalDirPath)
	assert.Equal(t, `{"delete":["/new/movie.nfo"]}`, op.GeneratedFiles)
}

// NewRevertLogConfig
func TestMiss_NewRevertLogConfig(t *testing.T) {
	cfg := NewRevertLogConfig(true, &nfo.Config{})
	assert.True(t, cfg.AllowRevert)
	assert.NotNil(t, cfg.NFOCfg)
}

// RevertLogConfig.ToNFONameConfig with nil NFOCfg
func TestMiss_RevertLogConfig_ToNFONameConfig_NilNFOCfg(t *testing.T) {
	cfg := &RevertLogConfig{AllowRevert: true, NFOCfg: nil}
	result := cfg.ToNFONameConfig(true, "-pt1")
	assert.True(t, result.IsMultiPart)
	assert.Equal(t, "-pt1", result.PartSuffix)
}

// RevertLogConfig.ToNFONameConfig with NFOCfg
func TestMiss_RevertLogConfig_ToNFONameConfig_WithNFOCfg(t *testing.T) {
	cfg := &RevertLogConfig{AllowRevert: true, NFOCfg: &nfo.Config{FilenameTemplate: "<ID>"}}
	result := cfg.ToNFONameConfig(false, "")
	assert.False(t, result.IsMultiPart)
}

// CaptureSnapshot: with NFO file found
func TestMiss_dbRevertLog_CaptureSnapshot_NFOFound(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/src/TEST-001.nfo", []byte("<nfo>snapshot</nfo>"), 0644))

	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	cfg := &RevertLogConfig{AllowRevert: true, NFOCfg: &nfo.Config{FilenameTemplate: "<ID>"}}
	engine := template.NewEngine()
	log := NewDBRevertLog(mockRepo, cfg, "job1", fs, engine, nil, nil)

	preRecord := &models.BatchFileOperation{
		ID:           1,
		BatchJobID:   "job1",
		RevertStatus: models.RevertStatusApplied,
	}
	mockRepo.On("FindByID", mock.Anything, uint(1)).Return(preRecord, nil)
	mockRepo.On("Update", mock.Anything, mock.AnythingOfType("*models.BatchFileOperation")).Return(nil)

	cmd := ApplyCmd{
		Movie: &models.Movie{ID: "TEST-001"},
		Match: models.FileMatchInfo{Path: "/src/TEST-001.mp4"},
	}
	log.CaptureSnapshot(context.Background(), "1", cmd)
}

// newPreOrganizeRecord: verify fields
func TestMiss_newPreOrganizeRecord(t *testing.T) {
	op := newPreOrganizeRecord("job1", "MOV-001", "/src/file.mp4", "nfo-snapshot", "/src/file.nfo", "/src", models.OperationTypeMove, false)
	assert.Equal(t, "job1", op.BatchJobID)
	assert.Equal(t, "MOV-001", op.MovieID)
	assert.Equal(t, "/src/file.mp4", op.OriginalPath)
	assert.Equal(t, "", op.NewPath)
	assert.Equal(t, models.OperationTypeMove, op.OperationType)
	assert.Equal(t, "nfo-snapshot", op.NFOSnapshot)
	assert.Equal(t, models.RevertStatusApplied, op.RevertStatus)
}
