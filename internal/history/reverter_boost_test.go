package history

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// computeRevertPrimaryPaths tests
// ============================================================================

func TestComputeRevertPrimaryPaths_StandardMove(t *testing.T) {
	op := &models.BatchFileOperation{
		OriginalPath:   filepath.FromSlash("/src/ABC-123/ABC-123.mp4"),
		NewPath:        filepath.FromSlash("/dst/ABC-123/ABC-123.mp4"),
		InPlaceRenamed: false,
	}
	paths := computeRevertPrimaryPaths(op)
	assert.Equal(t, filepath.FromSlash("/dst/ABC-123/ABC-123.mp4"), paths.SourcePath)
	assert.Equal(t, filepath.FromSlash("/src/ABC-123"), paths.TargetDir)
	assert.Equal(t, filepath.FromSlash("/dst"), paths.DestRoot)
}

func TestComputeRevertPrimaryPaths_InPlaceRenamed(t *testing.T) {
	op := &models.BatchFileOperation{
		OriginalPath:    filepath.FromSlash("/src/ABC-123/ABC-001.mp4"),
		NewPath:         filepath.FromSlash("/src/ABC-123-renamed/ABC-123.mp4"),
		InPlaceRenamed:  true,
		OriginalDirPath: filepath.FromSlash("/src/ABC-123"),
	}
	paths := computeRevertPrimaryPaths(op)
	assert.Equal(t, filepath.FromSlash("/src/ABC-123/ABC-123.mp4"), paths.SourcePath)
	assert.Equal(t, filepath.FromSlash("/src/ABC-123"), paths.TargetDir)
	assert.Equal(t, filepath.FromSlash("/src/ABC-123"), paths.OriginalDirPath)
	assert.Equal(t, filepath.FromSlash("/src/ABC-123-renamed"), paths.CurrentDir)
	assert.Equal(t, filepath.FromSlash("/src"), paths.DestRoot)
}

func TestComputeRevertPrimaryPaths_InPlaceRenamedNoOriginalDirPath(t *testing.T) {
	// When InPlaceRenamed is true but OriginalDirPath is empty,
	// falls through to the else branch (standard move)
	op := &models.BatchFileOperation{
		OriginalPath:    filepath.FromSlash("/src/ABC-123/ABC-001.mp4"),
		NewPath:         filepath.FromSlash("/src/ABC-123-renamed/ABC-123.mp4"),
		InPlaceRenamed:  true,
		OriginalDirPath: "",
	}
	paths := computeRevertPrimaryPaths(op)
	// Should fall through to standard move path
	assert.Equal(t, filepath.FromSlash("/src/ABC-123-renamed/ABC-123.mp4"), paths.SourcePath)
	assert.Equal(t, filepath.FromSlash("/src/ABC-123"), paths.TargetDir)
}

// ============================================================================
// cleanupEmptyDirFS tests
// ============================================================================

func TestCleanupEmptyDirFS_RemovesEmptyDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/ABC-123/sub", 0777))
	// sub is empty, should be removed
	cleanupEmptyDirFS(fs, "/out/ABC-123/sub", "/out")
	_, err := fs.Stat("/out/ABC-123/sub")
	assert.True(t, os.IsNotExist(err), "empty subdir should be removed")
}

func TestCleanupEmptyDirFS_StopsAtNonEmptyDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/ABC-123/sub", 0777))
	require.NoError(t, afero.WriteFile(fs, "/out/ABC-123/other.txt", []byte("data"), 0666))
	// sub is empty, but parent ABC-123 is not (has other.txt)
	cleanupEmptyDirFS(fs, "/out/ABC-123/sub", "/out")
	_, err := fs.Stat("/out/ABC-123/sub")
	assert.True(t, os.IsNotExist(err), "empty sub should be removed")
	_, err = fs.Stat("/out/ABC-123")
	assert.NoError(t, err, "non-empty parent should remain")
}

func TestCleanupEmptyDirFS_StopsAtStopAt(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/ABC-123/sub", 0777))
	// stopAt is the parent itself; should not remove it
	cleanupEmptyDirFS(fs, "/out/ABC-123/sub", "/out/ABC-123")
	_, err := fs.Stat("/out/ABC-123/sub")
	assert.True(t, os.IsNotExist(err), "empty sub should be removed")
	// ABC-123 is the stop boundary, should not be removed even if empty
	_, err = fs.Stat("/out/ABC-123")
	assert.NoError(t, err, "stopAt dir should remain even if empty")
}

func TestCleanupEmptyDirFS_WalksUpMultipleLevels(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/a/b/c", 0777))
	// All are empty; should walk up from c → b → a, stopping before /out
	cleanupEmptyDirFS(fs, "/out/a/b/c", "/out")
	for _, p := range []string{"/out/a/b/c", "/out/a/b", "/out/a"} {
		_, err := fs.Stat(p)
		assert.True(t, os.IsNotExist(err), "%s should be removed", p)
	}
	_, err := fs.Stat("/out")
	assert.NoError(t, err, "/out should remain as stopAt")
}

func TestCleanupEmptyDirFS_DirDoesNotExist(t *testing.T) {
	fs := afero.NewMemMapFs()
	// Should not panic on non-existent dir
	cleanupEmptyDirFS(fs, "/nonexistent/path", "/out")
}

func TestCleanupEmptyDirFS_EmptyStopAt(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/a/b", 0777))
	// With empty stopAt, walks all the way up until non-empty or root
	cleanupEmptyDirFS(fs, "/out/a/b", "")
	for _, p := range []string{"/out/a/b", "/out/a"} {
		_, err := fs.Stat(p)
		assert.True(t, os.IsNotExist(err), "%s should be removed", p)
	}
}

// ============================================================================
// cleanupEmptyDirDownwardFS tests
// ============================================================================

func TestCleanupEmptyDirDownwardFS_RemovesEmptyDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/ABC-123/sub", 0777))
	cleanupEmptyDirDownwardFS(fs, "/out/ABC-123/sub", "/out")
	_, err := fs.Stat("/out/ABC-123/sub")
	assert.True(t, os.IsNotExist(err), "empty dir should be removed")
	// Parent should also be removed since it's empty
	_, err = fs.Stat("/out/ABC-123")
	assert.True(t, os.IsNotExist(err), "empty parent dir should be removed")
}

func TestCleanupEmptyDirDownwardFS_StopsAtNonEmptyDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/ABC-123/sub", 0777))
	require.NoError(t, afero.WriteFile(fs, "/out/other.txt", []byte("data"), 0666))
	cleanupEmptyDirDownwardFS(fs, "/out/ABC-123/sub", "/out")
	// sub removed, ABC-123 empty so removed, /out has other.txt so stays
	_, err := fs.Stat("/out/ABC-123")
	assert.True(t, os.IsNotExist(err), "empty ABC-123 should be removed")
	_, err = fs.Stat("/out/other.txt")
	assert.NoError(t, err, "/out/other.txt should remain")
}

func TestCleanupEmptyDirDownwardFS_StopsAtStopAt(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/out/ABC-123/sub", 0777))
	// stopAt = /out/ABC-123; should remove sub but not ABC-123
	cleanupEmptyDirDownwardFS(fs, "/out/ABC-123/sub", "/out/ABC-123")
	_, err := fs.Stat("/out/ABC-123/sub")
	assert.True(t, os.IsNotExist(err), "empty sub should be removed")
	_, err = fs.Stat("/out/ABC-123")
	assert.NoError(t, err, "stopAt dir should not be removed even if empty")
}

func TestCleanupEmptyDirDownwardFS_NonExistentDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	// Should not panic
	cleanupEmptyDirDownwardFS(fs, "/nonexistent", "/out")
}

// ============================================================================
// cleanupGeneratedFilesFS tests
// ============================================================================

func TestCleanupGeneratedFilesFS_DeletesFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.nfo", []byte("<nfo/>"), 0666))
	require.NoError(t, afero.WriteFile(fs, "/dst/poster.jpg", []byte("img"), 0666))

	gf := models.GeneratedFilesJSON{
		Delete: []string{"/dst/ABC-123.nfo", "/dst/poster.jpg"},
	}
	gfJSON, _ := json.Marshal(gf)

	op := &models.BatchFileOperation{
		GeneratedFiles: string(gfJSON),
	}
	cleanupGeneratedFilesFS(fs, op, "/dst")

	_, err := fs.Stat("/dst/ABC-123.nfo")
	assert.True(t, os.IsNotExist(err), "NFO should be deleted")
	_, err = fs.Stat("/dst/poster.jpg")
	assert.True(t, os.IsNotExist(err), "poster should be deleted")
}

func TestCleanupGeneratedFilesFS_MoveBack(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/src", 0777))
	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/sub.srt", []byte("subs"), 0666))

	gf := models.GeneratedFilesJSON{
		MoveBack: []models.FileMove{
			{OriginalPath: "/src/sub.srt", NewPath: "/dst/sub.srt"},
		},
	}
	gfJSON, _ := json.Marshal(gf)

	op := &models.BatchFileOperation{
		GeneratedFiles: string(gfJSON),
	}
	cleanupGeneratedFilesFS(fs, op, "/dst")

	_, err := fs.Stat("/src/sub.srt")
	assert.NoError(t, err, "file should be moved back to original")
	_, err = fs.Stat("/dst/sub.srt")
	assert.True(t, os.IsNotExist(err), "file should no longer be at new path")
}

func TestCleanupGeneratedFilesFS_EmptyGeneratedFiles(t *testing.T) {
	fs := afero.NewMemMapFs()
	op := &models.BatchFileOperation{GeneratedFiles: ""}
	// Should be a no-op
	cleanupGeneratedFilesFS(fs, op, "/dst")
}

func TestCleanupGeneratedFilesFS_InvalidJSON(t *testing.T) {
	fs := afero.NewMemMapFs()
	op := &models.BatchFileOperation{GeneratedFiles: "not-valid-json"}
	// Should be a no-op (logs error, doesn't panic)
	cleanupGeneratedFilesFS(fs, op, "/dst")
}

func TestCleanupGeneratedFilesFS_DeletesAndCleansEmptyDirs(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/dst/ABC-123", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123/ABC-123.nfo", []byte("<nfo/>"), 0666))

	gf := models.GeneratedFilesJSON{
		Delete: []string{"/dst/ABC-123/ABC-123.nfo"},
	}
	gfJSON, _ := json.Marshal(gf)

	op := &models.BatchFileOperation{
		GeneratedFiles: string(gfJSON),
	}
	cleanupGeneratedFilesFS(fs, op, "/dst")

	_, err := fs.Stat("/dst/ABC-123/ABC-123.nfo")
	assert.True(t, os.IsNotExist(err), "NFO should be deleted")
	// Empty parent dir should also be cleaned up
	_, err = fs.Stat("/dst/ABC-123")
	assert.True(t, os.IsNotExist(err), "empty parent dir should be cleaned up")
}

func TestCleanupGeneratedFilesFS_SkipsDirOutsideBatchRoot(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/other/dir", 0777))
	require.NoError(t, afero.WriteFile(fs, "/other/dir/file.nfo", []byte("<nfo/>"), 0666))

	gf := models.GeneratedFilesJSON{
		Delete: []string{"/other/dir/file.nfo"},
	}
	gfJSON, _ := json.Marshal(gf)

	op := &models.BatchFileOperation{
		GeneratedFiles: string(gfJSON),
	}
	// stopAt is /dst but the file is under /other — dir cleanup should be skipped
	cleanupGeneratedFilesFS(fs, op, "/dst")

	_, err := fs.Stat("/other/dir/file.nfo")
	assert.True(t, os.IsNotExist(err), "file itself should still be deleted")
	// But the parent dir cleanup should be skipped since it's outside the batch root
	_, err = fs.Stat("/other/dir")
	assert.NoError(t, err, "dir outside batch root should not be cleaned up")
}

func TestCleanupGeneratedFilesFS_MissingFileSkipped(t *testing.T) {
	fs := afero.NewMemMapFs()
	gf := models.GeneratedFilesJSON{
		Delete: []string{"/nonexistent/file.nfo"},
	}
	gfJSON, _ := json.Marshal(gf)

	op := &models.BatchFileOperation{
		GeneratedFiles: string(gfJSON),
	}
	// Should not panic on missing file
	cleanupGeneratedFilesFS(fs, op, "/dst")
}

// ============================================================================
// restoreNFOFS tests
// ============================================================================

func TestRestoreNFOFS_NoSnapshot(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	op := &models.BatchFileOperation{NFOSnapshot: ""}
	warning, result := restoreNFOFS(context.Background(), fs, mockRepo, op, false)
	assert.Empty(t, warning)
	assert.Nil(t, result)
}

func TestRestoreNFOFS_WithNFOPath_SoftFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:           100,
		MovieID:      "ABC-123",
		OriginalPath: "/src/ABC-123.mp4",
		NFOPath:      "/src/ABC-123.nfo",
		NFOSnapshot:  "<nfo>data</nfo>",
	}
	warning, result := restoreNFOFS(context.Background(), fs, mockRepo, op, false)
	assert.Empty(t, warning)
	assert.Nil(t, result)

	// Verify NFO was written
	content, err := afero.ReadFile(fs, "/src/ABC-123.nfo")
	require.NoError(t, err)
	assert.Equal(t, "<nfo>data</nfo>", string(content))
}

func TestRestoreNFOFS_WithNFOPath_HardFailure(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	// hardFailure=true but write succeeds — no failRevert called, so no mock needed

	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:           100,
		MovieID:      "ABC-123",
		OriginalPath: "/src/ABC-123.mp4",
		NFOPath:      "/src/ABC-123.nfo",
		NFOSnapshot:  "<nfo>data</nfo>",
	}
	warning, result := restoreNFOFS(context.Background(), fs, mockRepo, op, true)
	assert.Empty(t, warning)
	assert.Nil(t, result)

	content, err := afero.ReadFile(fs, "/src/ABC-123.nfo")
	require.NoError(t, err)
	assert.Equal(t, "<nfo>data</nfo>", string(content))
}

func TestRestoreNFOFS_NoNFOPath_UsesMovieID(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:           101,
		MovieID:      "XYZ-456",
		OriginalPath: "/src/XYZ-456.mp4",
		NFOPath:      "",
		NFOSnapshot:  "<nfo>auto</nfo>",
	}
	warning, result := restoreNFOFS(context.Background(), fs, mockRepo, op, false)
	assert.Empty(t, warning)
	assert.Nil(t, result)

	content, err := afero.ReadFile(fs, "/src/XYZ-456.nfo")
	require.NoError(t, err)
	assert.Equal(t, "<nfo>auto</nfo>", string(content))
}

func TestRestoreNFOFS_NoNFOPath_NoMovieID(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	op := &models.BatchFileOperation{
		ID:           102,
		MovieID:      "",
		OriginalPath: "/src/video.mp4",
		NFOPath:      "",
		NFOSnapshot:  "<nfo>orphan</nfo>",
	}
	// No NFOPath and no MovieID → cannot determine NFO path → returns nil
	warning, result := restoreNFOFS(context.Background(), fs, mockRepo, op, false)
	assert.Empty(t, warning)
	assert.Nil(t, result)
}

func TestRestoreNFOFS_SoftFailure_WriteError(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	op := &models.BatchFileOperation{
		ID:           103,
		MovieID:      "ABC-123",
		OriginalPath: "/src/ABC-123.mp4",
		NFOPath:      "/src/ABC-123.nfo",
		NFOSnapshot:  "<nfo>data</nfo>",
	}
	// Use an fs that blocks .nfo writes
	errorFs := &nfoWriteErrorFs{Fs: fs}
	warning, result := restoreNFOFS(context.Background(), errorFs, mockRepo, op, false)
	assert.Contains(t, warning, "NFO restore failed")
	assert.Nil(t, result, "soft failure should not return a result")
}

func TestRestoreNFOFS_HardFailure_WriteError(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(104), models.RevertStatusFailed).Return(nil)

	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:           104,
		MovieID:      "ABC-123",
		OriginalPath: "/src/ABC-123.mp4",
		NFOPath:      "/src/ABC-123.nfo",
		NFOSnapshot:  "<nfo>data</nfo>",
	}
	errorFs := &nfoWriteErrorFs{Fs: fs}
	warning, result := restoreNFOFS(context.Background(), errorFs, mockRepo, op, true)
	assert.Empty(t, warning)
	assert.NotNil(t, result, "hard failure should return a result")
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Equal(t, models.RevertReasonNFORestoreFailed, result.Reason)
	mockRepo.AssertExpectations(t)
}

// ============================================================================
// revertPrimaryFileFS tests
// ============================================================================

func TestRevertPrimaryFileFS_StandardMove(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:             200,
		MovieID:        "ABC-123",
		OriginalPath:   "/src/ABC-123.mp4",
		NewPath:        "/dst/ABC-123.mp4",
		OperationType:  models.OperationTypeMove,
		InPlaceRenamed: false,
	}

	result, err := revertPrimaryFileFS(context.Background(), fs, mockRepo, op, func(dirPath, stopAt string) {})
	require.NoError(t, err)
	assert.Nil(t, result)

	// File should be moved back
	content, readErr := afero.ReadFile(fs, "/src/ABC-123.mp4")
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(content))
}

func TestRevertPrimaryFileFS_DestinationConflict(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(201), models.RevertStatusFailed).Return(nil)

	require.NoError(t, fs.MkdirAll("/src", 0777))
	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123.mp4", []byte("existing"), 0666))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:             201,
		MovieID:        "ABC-123",
		OriginalPath:   "/src/ABC-123.mp4",
		NewPath:        "/dst/ABC-123.mp4",
		OperationType:  models.OperationTypeMove,
		InPlaceRenamed: false,
	}

	result, err := revertPrimaryFileFS(context.Background(), fs, mockRepo, op, func(dirPath, stopAt string) {})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Equal(t, models.RevertReasonDestinationConflict, result.Reason)
	mockRepo.AssertExpectations(t)
}

func TestRevertPrimaryFileFS_InPlaceRenamed_DirAlreadyExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(202), models.RevertStatusFailed).Return(nil)

	// Create both the original dir and renamed dir
	require.NoError(t, fs.MkdirAll("/src/ABC-123", 0777))
	require.NoError(t, fs.MkdirAll("/src/ABC-123-renamed", 0777))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123-renamed/ABC-123.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:              202,
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/ABC-001.mp4",
		NewPath:         "/src/ABC-123-renamed/ABC-123.mp4",
		OperationType:   models.OperationTypeMove,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	result, err := revertPrimaryFileFS(context.Background(), fs, mockRepo, op, func(dirPath, stopAt string) {})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Equal(t, models.RevertReasonDestinationConflict, result.Reason)
	mockRepo.AssertExpectations(t)
}

func TestRevertPrimaryFileFS_InPlaceRenamed_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/src/ABC-123-renamed", 0777))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123-renamed/ABC-123.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:              203,
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/ABC-123.mp4",
		NewPath:         "/src/ABC-123-renamed/ABC-123.mp4",
		OperationType:   models.OperationTypeMove,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	result, err := revertPrimaryFileFS(context.Background(), fs, mockRepo, op, func(dirPath, stopAt string) {})
	require.NoError(t, err)
	assert.Nil(t, result)

	// Dir should be renamed back
	info, statErr := fs.Stat("/src/ABC-123")
	require.NoError(t, statErr)
	assert.True(t, info.IsDir())

	// File should be at the new path under the renamed dir
	content, readErr := afero.ReadFile(fs, "/src/ABC-123/ABC-123.mp4")
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(content))
}

func TestRevertPrimaryFileFS_InPlaceRenamed_FileRenameNeeded(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/src/ABC-123-renamed", 0777))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123-renamed/ABC-001.mp4", []byte("video"), 0666))

	// SourcePath != OriginalPath, so an inner file rename should happen
	op := &models.BatchFileOperation{
		ID:              204,
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/ABC-123.mp4",
		NewPath:         "/src/ABC-123-renamed/ABC-001.mp4",
		OperationType:   models.OperationTypeMove,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	result, err := revertPrimaryFileFS(context.Background(), fs, mockRepo, op, func(dirPath, stopAt string) {})
	require.NoError(t, err)
	assert.Nil(t, result)

	// File should be renamed to original name
	content, readErr := afero.ReadFile(fs, "/src/ABC-123/ABC-123.mp4")
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(content))
}

func TestRevertPrimaryFileFS_InPlaceRenamed_DirRenameError(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(205), models.RevertStatusFailed).Return(nil)

	// Use an fs that fails on Rename
	errorFs := &renameErrorFs{Fs: fs}
	require.NoError(t, fs.MkdirAll("/src/ABC-123-renamed", 0777))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123-renamed/ABC-123.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:              205,
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/ABC-123.mp4",
		NewPath:         "/src/ABC-123-renamed/ABC-123.mp4",
		OperationType:   models.OperationTypeMove,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	result, err := revertPrimaryFileFS(context.Background(), errorFs, mockRepo, op, func(dirPath, stopAt string) {})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	mockRepo.AssertExpectations(t)
}

func TestRevertPrimaryFileFS_RenameError(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(206), models.RevertStatusFailed).Return(nil)

	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	// Use an fs that fails on Rename
	errorFs := &renameErrorFs{Fs: fs}

	op := &models.BatchFileOperation{
		ID:             206,
		MovieID:        "ABC-123",
		OriginalPath:   "/src/ABC-123.mp4",
		NewPath:        "/dst/ABC-123.mp4",
		OperationType:  models.OperationTypeMove,
		InPlaceRenamed: false,
	}

	result, err := revertPrimaryFileFS(context.Background(), errorFs, mockRepo, op, func(dirPath, stopAt string) {})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	mockRepo.AssertExpectations(t)
}

// renameErrorFs wraps afero.Fs and returns errors on Rename
type renameErrorFs struct {
	afero.Fs
}

func (fs *renameErrorFs) Rename(oldname, newname string) error {
	return fmt.Errorf("rename not allowed")
}

// ============================================================================
// summarizeOutcomes tests
// ============================================================================

func TestSummarizeOutcomes_AllSucceeded(t *testing.T) {
	outcomes := []RevertFileResult{
		{Outcome: models.RevertOutcomeReverted},
		{Outcome: models.RevertOutcomeReverted},
	}
	s, sk, f := summarizeOutcomes(outcomes)
	assert.Equal(t, 2, s)
	assert.Equal(t, 0, sk)
	assert.Equal(t, 0, f)
}

func TestSummarizeOutcomes_Mixed(t *testing.T) {
	outcomes := []RevertFileResult{
		{Outcome: models.RevertOutcomeReverted},
		{Outcome: models.RevertOutcomeSkipped},
		{Outcome: models.RevertOutcomeFailed},
		{Outcome: models.RevertOutcomeReverted},
	}
	s, sk, f := summarizeOutcomes(outcomes)
	assert.Equal(t, 2, s)
	assert.Equal(t, 1, sk)
	assert.Equal(t, 1, f)
}

func TestSummarizeOutcomes_Empty(t *testing.T) {
	s, sk, f := summarizeOutcomes(nil)
	assert.Equal(t, 0, s)
	assert.Equal(t, 0, sk)
	assert.Equal(t, 0, f)
}

// ============================================================================
// collectDestRoots tests
// ============================================================================

func TestCollectDestRoots(t *testing.T) {
	ops := []models.BatchFileOperation{
		{NewPath: filepath.FromSlash("/out/ABC-123/ABC-123.mp4"), InPlaceRenamed: false},
		{NewPath: filepath.FromSlash("/out/DEF-456/DEF-456.mp4"), InPlaceRenamed: false},
		{NewPath: filepath.FromSlash("/out/GHI-789/GHI-789.mp4"), InPlaceRenamed: true}, // skipped
	}
	roots := collectDestRoots(ops)
	assert.True(t, roots[filepath.FromSlash("/out")])
	assert.Len(t, roots, 1) // Both ABC-123 and DEF-456 have same destRoot /out
}

func TestCollectDestRoots_Empty(t *testing.T) {
	roots := collectDestRoots(nil)
	assert.Empty(t, roots)
}

// ============================================================================
// RevertBatch tests
// ============================================================================

func TestRevertBatch_DBError(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(nil, errors.New("db error"))

	result, err := r.RevertBatch(context.Background(), "batch-1")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch batch operations")
	mockRepo.AssertExpectations(t)
}

func TestRevertBatch_NoOperations(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return([]models.BatchFileOperation{}, nil)

	result, err := r.RevertBatch(context.Background(), "batch-1")
	assert.Nil(t, result)
	assert.Equal(t, ErrNoOperationsFound, err)
	mockRepo.AssertExpectations(t)
}

func TestRevertBatch_AllAlreadyReverted(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	ops := []models.BatchFileOperation{
		{ID: 1, RevertStatus: models.RevertStatusReverted},
		{ID: 2, RevertStatus: models.RevertStatusReverted},
	}
	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(ops, nil)

	result, err := r.RevertBatch(context.Background(), "batch-1")
	assert.Nil(t, result)
	assert.Equal(t, ErrBatchAlreadyReverted, err)
	mockRepo.AssertExpectations(t)
}

func TestRevertBatch_SuccessfulRevert(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	ops := []models.BatchFileOperation{
		{
			ID:            300,
			BatchJobID:    "batch-1",
			MovieID:       "ABC-123",
			OriginalPath:  "/src/ABC-123.mp4",
			NewPath:       "/dst/ABC-123.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
	}
	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(300), models.RevertStatusReverted).Return(nil)

	r := NewReverter(fs, mockRepo)
	result, err := r.RevertBatch(context.Background(), "batch-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 0, result.Skipped)
	assert.Equal(t, 0, result.Failed)
	mockRepo.AssertExpectations(t)
}

func TestRevertBatch_MixedStatuses(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	ops := []models.BatchFileOperation{
		{
			ID:            301,
			BatchJobID:    "batch-1",
			MovieID:       "ABC-123",
			OriginalPath:  "/src/ABC-123.mp4",
			NewPath:       "/dst/ABC-123.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
		{
			ID:            302,
			BatchJobID:    "batch-1",
			MovieID:       "DEF-456",
			OriginalPath:  "/src/DEF-456.mp4",
			NewPath:       "/dst/DEF-456.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusReverted, // already reverted
		},
	}
	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(301), models.RevertStatusReverted).Return(nil)

	r := NewReverter(fs, mockRepo)
	result, err := r.RevertBatch(context.Background(), "batch-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total) // Only processable ops counted
	assert.Equal(t, 1, result.Succeeded)
	mockRepo.AssertExpectations(t)
}

// ============================================================================
// RevertScrape tests
// ============================================================================

func TestRevertScrape_DBError(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(nil, errors.New("db error"))

	result, err := r.RevertScrape(context.Background(), "batch-1", "ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
	mockRepo.AssertExpectations(t)
}

func TestRevertScrape_NoOperations(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return([]models.BatchFileOperation{}, nil)

	result, err := r.RevertScrape(context.Background(), "batch-1", "ABC-123")
	assert.Nil(t, result)
	assert.Equal(t, ErrNoOperationsFound, err)
	mockRepo.AssertExpectations(t)
}

func TestRevertScrape_NoMatchingMovieID(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	ops := []models.BatchFileOperation{
		{
			ID:            400,
			BatchJobID:    "batch-1",
			MovieID:       "DEF-456",
			OriginalPath:  "/src/DEF-456.mp4",
			NewPath:       "/dst/DEF-456.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
	}
	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(ops, nil)

	result, err := r.RevertScrape(context.Background(), "batch-1", "ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no processable operations found")
	mockRepo.AssertExpectations(t)
}

func TestRevertScrape_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	ops := []models.BatchFileOperation{
		{
			ID:            401,
			BatchJobID:    "batch-1",
			MovieID:       "ABC-123",
			OriginalPath:  "/src/ABC-123.mp4",
			NewPath:       "/dst/ABC-123.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
		{
			ID:            402,
			BatchJobID:    "batch-1",
			MovieID:       "DEF-456",
			OriginalPath:  "/src/DEF-456.mp4",
			NewPath:       "/dst/DEF-456.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
	}
	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(401), models.RevertStatusReverted).Return(nil)

	r := NewReverter(fs, mockRepo)
	result, err := r.RevertScrape(context.Background(), "batch-1", "ABC-123")
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Succeeded)
	mockRepo.AssertExpectations(t)
}

// ============================================================================
// revertOperations tests (via RevertBatch)
// ============================================================================

func TestRevertOperations_SystemError(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	// An op with Applied status passes RevertScrape filter,
	// but Copy operation type triggers failRevert from guardDoubleRevert,
	// which returns a nil error (not a system error). To trigger a true
	// system error, use UpdateRevertStatus returning an error from
	// revertFile when the DB persist fails after successful revert.
	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	ops := []models.BatchFileOperation{
		{
			ID:            500,
			BatchJobID:    "batch-1",
			MovieID:       "ABC-123",
			OriginalPath:  "/src/ABC-123.mp4",
			NewPath:       "/dst/ABC-123.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusApplied,
		},
	}
	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(500), models.RevertStatusReverted).Return(errors.New("db persist failed"))

	r := NewReverter(fs, mockRepo)

	result, err := r.RevertScrape(context.Background(), "batch-1", "ABC-123")
	// The DB persist error from revertFile is a system error caught by revertOperations
	require.NoError(t, err)
	assert.Equal(t, 1, result.Total)
	assert.Equal(t, 1, result.Failed)
	assert.Contains(t, result.Outcomes[0].Error, "db persist failed")
	mockRepo.AssertExpectations(t)
}

// ============================================================================
// guardDoubleRevert tests
// ============================================================================

func TestGuardDoubleRevert_AlreadyReverted(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	op := &models.BatchFileOperation{
		RevertStatus:  models.RevertStatusReverted,
		OperationType: models.OperationTypeMove,
	}
	result, err := r.guardDoubleRevert(context.Background(), op)
	assert.Nil(t, result)
	assert.Equal(t, ErrBatchAlreadyReverted, err)
}

func TestGuardDoubleRevert_UnexpectedStatus(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	op := &models.BatchFileOperation{
		RevertStatus:  "pending",
		OperationType: models.OperationTypeMove,
	}
	result, err := r.guardDoubleRevert(context.Background(), op)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected revert status")
}

func TestGuardDoubleRevert_CopyModeNotRevertible(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(600), models.RevertStatusFailed).Return(nil)
	r := NewReverter(fs, mockRepo)

	op := &models.BatchFileOperation{
		ID:            600,
		RevertStatus:  models.RevertStatusApplied,
		OperationType: models.OperationTypeCopy,
	}
	result, err := r.guardDoubleRevert(context.Background(), op)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Equal(t, models.RevertReasonUnexpectedPathState, result.Reason)
	mockRepo.AssertExpectations(t)
}

func TestGuardDoubleRevert_Ok(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	op := &models.BatchFileOperation{
		RevertStatus:  models.RevertStatusApplied,
		OperationType: models.OperationTypeMove,
	}
	result, err := r.guardDoubleRevert(context.Background(), op)
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// ============================================================================
// checkAnchor tests
// ============================================================================

func TestCheckAnchor_FileExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:            700,
		NewPath:       "/dst/ABC-123.mp4",
		OperationType: models.OperationTypeMove,
	}
	result, err := r.checkAnchor(context.Background(), op)
	assert.Nil(t, result)
	assert.NoError(t, err)
}

func TestCheckAnchor_FileMissing(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	op := &models.BatchFileOperation{
		ID:            701,
		MovieID:       "ABC-123",
		OriginalPath:  "/src/ABC-123.mp4",
		NewPath:       "/dst/ABC-123.mp4",
		OperationType: models.OperationTypeMove,
	}
	result, err := r.checkAnchor(context.Background(), op)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeSkipped, result.Outcome)
	assert.Equal(t, models.RevertReasonAnchorMissing, result.Reason)
}

func TestCheckAnchor_UpdateOperation(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	require.NoError(t, fs.MkdirAll("/src", 0777))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123.mp4", []byte("video"), 0666))

	// For update operations, anchor is OriginalPath
	op := &models.BatchFileOperation{
		ID:            702,
		OriginalPath:  "/src/ABC-123.mp4",
		NewPath:       "/src/ABC-123.mp4",
		OperationType: models.OperationTypeUpdate,
	}
	result, err := r.checkAnchor(context.Background(), op)
	assert.Nil(t, result)
	assert.NoError(t, err)
}

// ============================================================================
// revertFile tests (full integration)
// ============================================================================

func TestRevertFile_UpdateOperation(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/src", 0777))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123.mp4", []byte("video"), 0666))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123.nfo", []byte("<old/>"), 0666))

	op := &models.BatchFileOperation{
		ID:            800,
		MovieID:       "ABC-123",
		OriginalPath:  "/src/ABC-123.mp4",
		NewPath:       "/src/ABC-123.mp4",
		OperationType: models.OperationTypeUpdate,
		RevertStatus:  models.RevertStatusApplied,
		NFOSnapshot:   "<original/>",
		NFOPath:       "/src/ABC-123.nfo",
	}
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(800), models.RevertStatusReverted).Return(nil)

	r := NewReverter(fs, mockRepo)
	result, err := r.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeReverted, result.Outcome)
	mockRepo.AssertExpectations(t)
}

func TestRevertFile_MoveOperation(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:            801,
		MovieID:       "ABC-123",
		OriginalPath:  "/src/ABC-123.mp4",
		NewPath:       "/dst/ABC-123.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(801), models.RevertStatusReverted).Return(nil)

	r := NewReverter(fs, mockRepo)
	result, err := r.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeReverted, result.Outcome)

	// File should be moved back
	content, readErr := afero.ReadFile(fs, "/src/ABC-123.mp4")
	require.NoError(t, readErr)
	assert.Equal(t, "video", string(content))
	mockRepo.AssertExpectations(t)
}

func TestRevertFile_AlreadyReverted(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	op := &models.BatchFileOperation{
		ID:            802,
		RevertStatus:  models.RevertStatusReverted,
		OperationType: models.OperationTypeMove,
	}
	result, err := r.revertFile(context.Background(), op)
	assert.Nil(t, result)
	assert.Equal(t, ErrBatchAlreadyReverted, err)
}

func TestRevertFile_AnchorMissing(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	op := &models.BatchFileOperation{
		ID:            803,
		MovieID:       "ABC-123",
		OriginalPath:  "/src/ABC-123.mp4",
		NewPath:       "/dst/ABC-123.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	result, err := r.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeSkipped, result.Outcome)
	assert.Equal(t, models.RevertReasonAnchorMissing, result.Reason)
}

func TestRevertFile_DBPersistFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:            804,
		MovieID:       "ABC-123",
		OriginalPath:  "/src/ABC-123.mp4",
		NewPath:       "/dst/ABC-123.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(804), models.RevertStatusReverted).Return(errors.New("db write failed"))

	r := NewReverter(fs, mockRepo)
	result, err := r.revertFile(context.Background(), op)
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to persist revert status")
	mockRepo.AssertExpectations(t)
}

func TestRevertFile_WithNFOWarning(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:            805,
		MovieID:       "ABC-123",
		OriginalPath:  "/src/ABC-123.mp4",
		NewPath:       "/dst/ABC-123.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
		NFOSnapshot:   "<nfo/>",
		NFOPath:       "/src/ABC-123.nfo",
	}
	// NFO write will fail due to nfoWriteErrorFs, but it's a soft failure in move mode
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(805), models.RevertStatusReverted).Return(nil)

	errorFs := &nfoWriteErrorFs{Fs: fs}
	r := NewReverter(errorFs, mockRepo)
	result, err := r.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeReverted, result.Outcome)
	assert.Contains(t, result.Error, "NFO restore failed")
	mockRepo.AssertExpectations(t)
}

// ============================================================================
// failRevert tests
// ============================================================================

func TestFailRevert_DBUpdateFails(t *testing.T) {
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(900), models.RevertStatusFailed).Return(errors.New("db error"))

	op := &models.BatchFileOperation{
		ID:           900,
		MovieID:      "ABC-123",
		OriginalPath: "/src/ABC-123.mp4",
		NewPath:      "/dst/ABC-123.mp4",
	}
	result := failRevert(context.Background(), mockRepo, op, models.RevertReasonAccessDenied, "test error")
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Equal(t, models.RevertReasonAccessDenied, result.Reason)
	assert.Equal(t, "test error", result.Error)
	mockRepo.AssertExpectations(t)
}

// ============================================================================
// Logger.Count tests
// ============================================================================

func TestLogger_Count(t *testing.T) {
	db, cleanup := setupTestDB(t)
	defer cleanup()

	logger := NewLogger(database.NewHistoryRepository(db))
	repo := database.NewHistoryRepository(db)

	// Initially zero
	count, err := logger.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(0), count)

	// Add records
	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(context.Background(), &models.History{
			MovieID:   fmt.Sprintf("IPX-%d", i),
			Operation: models.HistoryOpOrganize,
			Status:    models.HistoryStatusSuccess,
		}))
	}

	count, err = logger.Count(context.Background())
	require.NoError(t, err)
	assert.Equal(t, int64(3), count)
}

// ============================================================================
// InPlaceRenamed move with cleanupEmptyDir called
// ============================================================================

func TestRevertPrimaryFileFS_CallsCleanupDirFn(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)

	require.NoError(t, fs.MkdirAll("/dst", 0777))
	require.NoError(t, afero.WriteFile(fs, "/dst/ABC-123.mp4", []byte("video"), 0666))

	cleanupCalled := false
	cleanupFn := func(dirPath, stopAt string) {
		cleanupCalled = true
		assert.Equal(t, filepath.Dir("/dst/ABC-123.mp4"), dirPath)
	}

	op := &models.BatchFileOperation{
		ID:             950,
		MovieID:        "ABC-123",
		OriginalPath:   "/src/ABC-123.mp4",
		NewPath:        "/dst/ABC-123.mp4",
		OperationType:  models.OperationTypeMove,
		InPlaceRenamed: false,
	}

	result, err := revertPrimaryFileFS(context.Background(), fs, mockRepo, op, cleanupFn)
	require.NoError(t, err)
	assert.Nil(t, result)
	assert.True(t, cleanupCalled, "cleanupDirFn should be called for standard move")
}

// ============================================================================
// InPlaceRenamed with file rename MkdirAll error
// ============================================================================

func TestRevertPrimaryFileFS_InPlaceRenamed_FileRenameMkdirAllError(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(951), models.RevertStatusFailed).Return(nil)

	require.NoError(t, fs.MkdirAll("/src/ABC-123-renamed", 0777))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123-renamed/ABC-001.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:              951,
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/ABC-123.mp4",
		NewPath:         "/src/ABC-123-renamed/ABC-001.mp4",
		OperationType:   models.OperationTypeMove,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	// Use fs that succeeds on dir Rename but fails on MkdirAll
	errorFs := &mkdirAllErrorFs{Fs: fs, failPath: filepath.Dir("/src/ABC-123/ABC-123.mp4")}

	result, err := revertPrimaryFileFS(context.Background(), errorFs, mockRepo, op, func(dirPath, stopAt string) {})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Contains(t, result.Error, "failed to create directory for file rename")
	mockRepo.AssertExpectations(t)
}

// mkdirAllErrorFs wraps afero.Fs and returns errors on MkdirAll for a specific path
type mkdirAllErrorFs struct {
	afero.Fs
	failPath string
}

func (fs *mkdirAllErrorFs) MkdirAll(path string, perm os.FileMode) error {
	if path == fs.failPath {
		return fmt.Errorf("mkdirall blocked")
	}
	return fs.Fs.MkdirAll(path, perm)
}

// ============================================================================
// InPlaceRenamed with inner file rename error
// ============================================================================

func TestRevertPrimaryFileFS_InPlaceRenamed_InnerRenameError(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(952), models.RevertStatusFailed).Return(nil)

	require.NoError(t, fs.MkdirAll("/src/ABC-123-renamed", 0777))
	require.NoError(t, afero.WriteFile(fs, "/src/ABC-123-renamed/ABC-001.mp4", []byte("video"), 0666))

	op := &models.BatchFileOperation{
		ID:              952,
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/ABC-123.mp4",
		NewPath:         "/src/ABC-123-renamed/ABC-001.mp4",
		OperationType:   models.OperationTypeMove,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	// Use fs that fails on second Rename (inner file rename after dir rename succeeds)
	errorFs := &renameAfterDirFs{Fs: fs}

	result, err := revertPrimaryFileFS(context.Background(), errorFs, mockRepo, op, func(dirPath, stopAt string) {})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Contains(t, result.Error, "failed to rename file within directory")
	mockRepo.AssertExpectations(t)
}

// renameAfterDirFs: first Rename succeeds, second fails
type renameAfterDirFs struct {
	afero.Fs
	renameCount int
}

func (fs *renameAfterDirFs) Rename(oldname, newname string) error {
	fs.renameCount++
	if fs.renameCount > 1 {
		return fmt.Errorf("rename blocked after first")
	}
	return fs.Fs.Rename(oldname, newname)
}

// ============================================================================
// RevertBatch with no processable ops (all unexpected status)
// ============================================================================

func TestRevertBatch_NoProcessableOps_UnexpectedStatus(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	ops := []models.BatchFileOperation{
		{ID: 1, RevertStatus: "pending", OperationType: models.OperationTypeMove},
	}
	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(ops, nil)

	result, err := r.RevertBatch(context.Background(), "batch-1")
	// pending is not Applied/Failed/Reverted, so no processable ops
	assert.Nil(t, result)
	assert.Equal(t, ErrNoOperationsFound, err)
	mockRepo.AssertExpectations(t)
}

// ============================================================================
// RevertScrape with already-reverted movie
// ============================================================================

func TestRevertScrape_MovieAlreadyReverted(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	ops := []models.BatchFileOperation{
		{
			ID:            403,
			BatchJobID:    "batch-1",
			MovieID:       "ABC-123",
			OriginalPath:  "/src/ABC-123.mp4",
			NewPath:       "/dst/ABC-123.mp4",
			OperationType: models.OperationTypeMove,
			RevertStatus:  models.RevertStatusReverted,
		},
	}
	mockRepo.On("FindByBatchJobID", mock.Anything, "batch-1").Return(ops, nil)

	result, err := r.RevertScrape(context.Background(), "batch-1", "ABC-123")
	assert.Nil(t, result)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "no processable operations found")
	mockRepo.AssertExpectations(t)
}
