package history

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// setupReverterTest creates a test fixture with an in-memory filesystem and mock repository.
func setupReverterTest(t *testing.T) (afero.Fs, *mocks.MockBatchFileOperationRepositoryInterface, *Reverter) {
	t.Helper()
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	reverter := NewReverter(fs, mockRepo)
	return fs, mockRepo, reverter
}

// createTestFile creates a file with content in the in-memory filesystem.
func createTestFile(t *testing.T, fs afero.Fs, path, content string) {
	t.Helper()
	dir := filepath.Dir(path)
	require.NoError(t, fs.MkdirAll(dir, 0777))
	require.NoError(t, afero.WriteFile(fs, path, []byte(content), 0666))
}

// --- Test Case 1: Move-mode revert (D-05) ---

func TestRevertFile_MoveMode(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: file was moved from /src/file.mp4 to /dst/file.mp4
	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:            1,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(1), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify file moved back to original path
	exists, err := afero.Exists(fs, "/src/file.mp4")
	assert.NoError(t, err)
	assert.True(t, exists, "file should exist at original path")

	// Verify file no longer at new path
	exists, err = afero.Exists(fs, "/dst/file.mp4")
	assert.NoError(t, err)
	assert.False(t, exists, "file should no longer exist at new path")

	// Verify content preserved
	content, err := afero.ReadFile(fs, "/src/file.mp4")
	assert.NoError(t, err)
	assert.Equal(t, "video-content", string(content))

	mockRepo.AssertExpectations(t)
}

// --- Test Case 2: Copy-mode rejected (D-11) ---

func TestRevertFile_CopyModeRejected(t *testing.T) {
	_, mockRepo, reverter := setupReverterTest(t)

	op := &models.BatchFileOperation{
		ID:            2,
		BatchJobID:    "batch-1",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeCopy,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(2), models.RevertStatusFailed).Return(nil)

	res, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeFailed, res.Outcome)
	assert.Equal(t, models.RevertReasonUnexpectedPathState, res.Reason)
	assert.Contains(t, res.Error, "copy-mode operations cannot be reverted")

	mockRepo.AssertExpectations(t)
}

// --- Test Case 3: Hardlink-mode rejected (D-11) ---

func TestRevertFile_HardlinkModeRejected(t *testing.T) {
	_, mockRepo, reverter := setupReverterTest(t)

	op := &models.BatchFileOperation{
		ID:            3,
		BatchJobID:    "batch-1",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeHardlink,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(3), models.RevertStatusFailed).Return(nil)

	res, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeFailed, res.Outcome)
	assert.Contains(t, res.Error, "copy-mode operations cannot be reverted")

	mockRepo.AssertExpectations(t)
}

// --- Test Case 4: Symlink-mode rejected (D-11) ---

func TestRevertFile_SymlinkModeRejected(t *testing.T) {
	_, mockRepo, reverter := setupReverterTest(t)

	op := &models.BatchFileOperation{
		ID:            4,
		BatchJobID:    "batch-1",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeSymlink,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(4), models.RevertStatusFailed).Return(nil)

	res, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeFailed, res.Outcome)
	assert.Contains(t, res.Error, "copy-mode operations cannot be reverted")

	mockRepo.AssertExpectations(t)
}

// --- Test Case 5: Directory recreation (HIST-06) ---

func TestRevertFile_DirectoryRecreation(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// File at new path; original directory doesn't exist
	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	// Do NOT create /deleted/dir — the reverter must recreate it

	op := &models.BatchFileOperation{
		ID:            5,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/deleted/dir/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(5), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify directory was recreated
	dirExists, err := afero.DirExists(fs, "/deleted/dir")
	assert.NoError(t, err)
	assert.True(t, dirExists, "original directory should be recreated")

	// Verify file at original path
	exists, err := afero.Exists(fs, "/deleted/dir/file.mp4")
	assert.NoError(t, err)
	assert.True(t, exists, "file should exist at original path")

	mockRepo.AssertExpectations(t)
}

// --- Test Case 6: In-place directory rename revert (D-08) ---

func TestRevertFile_InPlaceRenameRevert(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// After organize: directory was renamed from /src/ABC-123 to /src/Studio - Title
	// File is at /src/Studio - Title/renamed.mp4
	createTestFile(t, fs, "/src/Studio - Title/renamed.mp4", "video-content")

	op := &models.BatchFileOperation{
		ID:              6,
		BatchJobID:      "batch-1",
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/file.mp4",
		NewPath:         "/src/Studio - Title/renamed.mp4",
		OperationType:   models.OperationTypeMove,
		RevertStatus:    models.RevertStatusApplied,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	mockRepo.On("UpdateRevertStatus", uint(6), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify directory renamed back to original
	dirExists, err := afero.DirExists(fs, "/src/ABC-123")
	assert.NoError(t, err)
	assert.True(t, dirExists, "original directory should exist after revert")

	// Verify new directory name gone
	dirExists, err = afero.DirExists(fs, "/src/Studio - Title")
	assert.NoError(t, err)
	assert.False(t, dirExists, "renamed directory should no longer exist")

	// Verify file is at the original path
	exists, err := afero.Exists(fs, "/src/ABC-123/file.mp4")
	assert.NoError(t, err)
	assert.True(t, exists, "file should exist at original path within restored directory")

	mockRepo.AssertExpectations(t)
}

// --- Test Case 7: NFO snapshot restoration (D-07) ---

func TestRevertFile_NFOSnapshotRestore(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	nfoContent := `<episodedetails><title>ABC-123</title></episodedetails>`

	op := &models.BatchFileOperation{
		ID:            7,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
		NFOSnapshot:   nfoContent,
		NFOPath:       "/src/ABC-123.nfo",
	}

	mockRepo.On("UpdateRevertStatus", uint(7), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	nfoPath := "/src/ABC-123.nfo"
	exists, err := afero.Exists(fs, nfoPath)
	assert.NoError(t, err)
	assert.True(t, exists, "NFO file should exist at NFOPath")

	content, err := afero.ReadFile(fs, nfoPath)
	assert.NoError(t, err)
	assert.Equal(t, nfoContent, string(content))

	mockRepo.AssertExpectations(t)
}

// --- Test Case 8: Generated files deletion (D-06) ---

func TestRevertFile_GeneratedFilesDeletion(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: file at new path, generated files exist
	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	createTestFile(t, fs, "/dst/poster.jpg", "poster-data")
	createTestFile(t, fs, "/dst/cover.jpg", "cover-data")
	createTestFile(t, fs, "/dst/sub.srt", "subtitle-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	generatedFiles := GeneratedFilesJSON{
		Delete: []string{"/dst/poster.jpg", "/dst/cover.jpg"},
		MoveBack: []FileMove{
			{OriginalPath: "/src/sub.srt", NewPath: "/dst/sub.srt"},
		},
	}
	gfJSON, err := json.Marshal(generatedFiles)
	require.NoError(t, err)

	op := &models.BatchFileOperation{
		ID:             8,
		BatchJobID:     "batch-1",
		MovieID:        "ABC-123",
		OriginalPath:   "/src/file.mp4",
		NewPath:        "/dst/file.mp4",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: string(gfJSON),
	}

	mockRepo.On("UpdateRevertStatus", uint(8), models.RevertStatusReverted).Return(nil)

	_, err = reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify generated files deleted
	exists, _ := afero.Exists(fs, "/dst/poster.jpg")
	assert.False(t, exists, "poster.jpg should be deleted")

	exists, _ = afero.Exists(fs, "/dst/cover.jpg")
	assert.False(t, exists, "cover.jpg should be deleted")

	// Verify subtitle moved back
	exists, _ = afero.Exists(fs, "/src/sub.srt")
	assert.True(t, exists, "subtitle should be moved back to original path")

	exists, _ = afero.Exists(fs, "/dst/sub.srt")
	assert.False(t, exists, "subtitle should no longer exist at new path")

	mockRepo.AssertExpectations(t)
}

// --- Test Case 9: Double-revert guard (D-09) ---

func TestRevertFile_DoubleRevertGuard(t *testing.T) {
	_, _, reverter := setupReverterTest(t)

	op := &models.BatchFileOperation{
		ID:            9,
		BatchJobID:    "batch-1",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusReverted,
	}

	_, err := reverter.revertFile(context.Background(), op)
	assert.ErrorIs(t, err, ErrBatchAlreadyReverted)
}

// --- Test Case 10: Already-failed retry ---

func TestRevertFile_AlreadyFailedRetry(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: a previously failed operation can be retried
	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:            10,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusFailed, // previously failed
	}

	mockRepo.On("UpdateRevertStatus", uint(10), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify file moved back
	exists, _ := afero.Exists(fs, "/src/file.mp4")
	assert.True(t, exists, "file should be moved back on retry")

	mockRepo.AssertExpectations(t)
}

// --- Test Case 11: Successful batch revert (D-02, D-04) ---

func TestRevertBatch_SuccessfulBatch(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: 3 files in a batch
	createTestFile(t, fs, "/dst/file1.mp4", "video1")
	createTestFile(t, fs, "/dst/file2.mp4", "video2")
	createTestFile(t, fs, "/dst/file3.mp4", "video3")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", MovieID: "ABC-001", OriginalPath: "/src/file1.mp4", NewPath: "/dst/file1.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
		{ID: 2, BatchJobID: "batch-1", MovieID: "ABC-002", OriginalPath: "/src/file2.mp4", NewPath: "/dst/file2.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
		{ID: 3, BatchJobID: "batch-1", MovieID: "ABC-003", OriginalPath: "/src/file3.mp4", NewPath: "/dst/file3.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
	}

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", uint(1), models.RevertStatusReverted).Return(nil)
	mockRepo.On("UpdateRevertStatus", uint(2), models.RevertStatusReverted).Return(nil)
	mockRepo.On("UpdateRevertStatus", uint(3), models.RevertStatusReverted).Return(nil)

	result, err := reverter.RevertBatch(context.Background(), "batch-1")
	require.NoError(t, err)

	assert.Equal(t, 3, result.Total)
	assert.Equal(t, 3, result.Succeeded)
	assert.Equal(t, 0, result.Failed)
	assert.Equal(t, 3, len(result.Outcomes))

	mockRepo.AssertExpectations(t)
}

// --- Test Case 12: Batch already reverted (D-09) ---

func TestRevertBatch_AlreadyReverted(t *testing.T) {
	_, mockRepo, reverter := setupReverterTest(t)

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", OriginalPath: "/src/file1.mp4", NewPath: "/dst/file1.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusReverted},
		{ID: 2, BatchJobID: "batch-1", OriginalPath: "/src/file2.mp4", NewPath: "/dst/file2.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusReverted},
	}

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)

	_, err := reverter.RevertBatch(context.Background(), "batch-1")
	assert.ErrorIs(t, err, ErrBatchAlreadyReverted)

	mockRepo.AssertExpectations(t)
}

// --- Test Case 13: Partial failure (D-04) ---

func TestRevertBatch_PartialFailure(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: 5 operations, 2 will succeed (move), 2 will fail (copy/hardlink), 1 will be skipped (anchor missing)
	createTestFile(t, fs, "/dst/file1.mp4", "video1")
	createTestFile(t, fs, "/dst/file3.mp4", "video3")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", MovieID: "ABC-001", OriginalPath: "/src/file1.mp4", NewPath: "/dst/file1.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
		{ID: 2, BatchJobID: "batch-1", MovieID: "ABC-002", OriginalPath: "/src/file2.mp4", NewPath: "/dst/file2.mp4", OperationType: models.OperationTypeCopy, RevertStatus: models.RevertStatusApplied},
		{ID: 3, BatchJobID: "batch-1", MovieID: "ABC-003", OriginalPath: "/src/file3.mp4", NewPath: "/dst/file3.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
		{ID: 4, BatchJobID: "batch-1", MovieID: "ABC-004", OriginalPath: "/src/file4.mp4", NewPath: "/dst/file4.mp4", OperationType: models.OperationTypeHardlink, RevertStatus: models.RevertStatusApplied},
		{ID: 5, BatchJobID: "batch-1", MovieID: "ABC-005", OriginalPath: "/src/file5.mp4", NewPath: "/dst/file5.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
	}

	// IDs 2 and 4 are copy/hardlink — rejected (no UpdateRevertStatus needed since they're handled by result tracking)
	// ID 5: file doesn't exist at NewPath — anchor check skips it (no DB status change, stays applied)
	// IDs 1, 3 succeed

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", uint(1), models.RevertStatusReverted).Return(nil)
	mockRepo.On("UpdateRevertStatus", uint(2), models.RevertStatusFailed).Return(nil)
	mockRepo.On("UpdateRevertStatus", uint(3), models.RevertStatusReverted).Return(nil)
	mockRepo.On("UpdateRevertStatus", uint(4), models.RevertStatusFailed).Return(nil)
	// ID 5: skipped — no UpdateRevertStatus call (stays applied)

	result, err := reverter.RevertBatch(context.Background(), "batch-1")
	require.NoError(t, err) // partial failure is not a fatal error

	assert.Equal(t, 5, result.Total)
	assert.Equal(t, 2, result.Succeeded) // IDs 1 and 3
	assert.Equal(t, 2, result.Failed)    // IDs 2 and 4
	assert.Equal(t, 1, result.Skipped)   // ID 5 (anchor missing)
	assert.Len(t, result.Outcomes, 5)    // All outcomes tracked: 2 succeeded + 2 failed + 1 skipped

	mockRepo.AssertExpectations(t)
}

// --- Test Case 14: Empty batch ---

func TestRevertBatch_EmptyBatch(t *testing.T) {
	_, mockRepo, reverter := setupReverterTest(t)

	mockRepo.On("FindByBatchJobID", "batch-empty").Return([]models.BatchFileOperation{}, nil)

	_, err := reverter.RevertBatch(context.Background(), "batch-empty")
	assert.ErrorIs(t, err, ErrNoOperationsFound)

	mockRepo.AssertExpectations(t)
}

// --- Test Case 15: RevertScrape — single movie (HIST-04) ---

func TestRevertScrape_SingleMovie(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: batch has 3 files, 2 belong to ABC-123
	createTestFile(t, fs, "/dst/abc123-1.mp4", "video1")
	createTestFile(t, fs, "/dst/abc123-2.mp4", "video2")
	createTestFile(t, fs, "/dst/def456.mp4", "video3")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", MovieID: "ABC-123", OriginalPath: "/src/abc123-1.mp4", NewPath: "/dst/abc123-1.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
		{ID: 2, BatchJobID: "batch-1", MovieID: "ABC-123", OriginalPath: "/src/abc123-2.mp4", NewPath: "/dst/abc123-2.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
		{ID: 3, BatchJobID: "batch-1", MovieID: "DEF-456", OriginalPath: "/src/def456.mp4", NewPath: "/dst/def456.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
	}

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", uint(1), models.RevertStatusReverted).Return(nil)
	mockRepo.On("UpdateRevertStatus", uint(2), models.RevertStatusReverted).Return(nil)

	result, err := reverter.RevertScrape(context.Background(), "batch-1", "ABC-123")
	require.NoError(t, err)

	assert.Equal(t, 2, result.Total)
	assert.Equal(t, 2, result.Succeeded)
	assert.Equal(t, 0, result.Failed)

	// Verify DEF-456 was NOT reverted
	exists, _ := afero.Exists(fs, "/dst/def456.mp4")
	assert.True(t, exists, "DEF-456 should not be reverted")

	mockRepo.AssertExpectations(t)
}

// --- Test Case 16: RevertScrape — not found ---

func TestRevertScrape_NotFound(t *testing.T) {
	_, mockRepo, reverter := setupReverterTest(t)

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", MovieID: "ABC-123", OriginalPath: "/src/file1.mp4", NewPath: "/dst/file1.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
	}

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)

	_, err := reverter.RevertScrape(context.Background(), "batch-1", "NOT-FOUND")
	assert.Error(t, err)

	mockRepo.AssertExpectations(t)
}

// --- Test Case 17: Path canonicalization (T-02-01 security) ---

func TestRevertFile_PathCanonicalization(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: file at new path with a path containing ".."
	createTestFile(t, fs, "/dst/file.mp4", "video-content")

	op := &models.BatchFileOperation{
		ID:            17,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/lib/../lib/target/file.mp4", // contains ".."
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(17), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify file moved to the canonicalized original path
	// /lib/../lib/target/file.mp4 cleans to /lib/target/file.mp4
	exists, err := afero.Exists(fs, "/lib/target/file.mp4")
	assert.NoError(t, err)
	assert.True(t, exists, "file should exist at canonicalized original path")

	mockRepo.AssertExpectations(t)
}

// --- Additional test: NFO snapshot empty means no NFO written ---

func TestRevertFile_EmptyNFOSnapshot(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:            100,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
		NFOSnapshot:   "",
		NFOPath:       "",
	}

	mockRepo.On("UpdateRevertStatus", uint(100), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	nfoPath := "/src/ABC-123.nfo"
	exists, _ := afero.Exists(fs, nfoPath)
	assert.False(t, exists, "no NFO should be written when snapshot is empty")

	mockRepo.AssertExpectations(t)
}

// --- Additional test: Generated files — missing delete target is OK ---

func TestRevertFile_GeneratedFilesDeletion_MissingFilesOK(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: file at new path, but generated files don't exist (already deleted)
	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	generatedFiles := GeneratedFilesJSON{
		Delete: []string{"/dst/nonexistent.jpg"}, // doesn't exist — should be OK
	}
	gfJSON, _ := json.Marshal(generatedFiles)

	op := &models.BatchFileOperation{
		ID:             200,
		BatchJobID:     "batch-1",
		MovieID:        "ABC-123",
		OriginalPath:   "/src/file.mp4",
		NewPath:        "/dst/file.mp4",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: string(gfJSON),
	}

	mockRepo.On("UpdateRevertStatus", uint(200), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// File should still be moved back successfully
	exists, _ := afero.Exists(fs, "/src/file.mp4")
	assert.True(t, exists, "file should be moved back even when generated files are missing")

	mockRepo.AssertExpectations(t)
}

// --- Additional test: Invalid GeneratedFiles JSON is handled gracefully (T-02-03) ---

func TestRevertFile_InvalidGeneratedFilesJSON(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:             300,
		BatchJobID:     "batch-1",
		MovieID:        "ABC-123",
		OriginalPath:   "/src/file.mp4",
		NewPath:        "/dst/file.mp4",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: "{invalid json", // malformed JSON
	}

	mockRepo.On("UpdateRevertStatus", uint(300), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// File should still be moved back; generated files cleanup is skipped gracefully
	exists, _ := afero.Exists(fs, "/src/file.mp4")
	assert.True(t, exists, "file should be moved back even when GeneratedFiles JSON is invalid")

	mockRepo.AssertExpectations(t)
}

// --- Test: anchor-missing results in skip, not failure (D-02) ---

func TestRevertFile_AnchorMissing_SkipsOperation(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: file doesn't exist at NewPath — anchor check skips it
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:            400,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/nonexistent.mp4", // doesn't exist — anchor missing
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}

	// No UpdateRevertStatus call — skipped operations stay in applied status

	res, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeSkipped, res.Outcome)
	assert.Equal(t, models.RevertReasonAnchorMissing, res.Reason)
	// Status stays "applied" (not changed)

	mockRepo.AssertExpectations(t)
}

// --- Test: context cancellation ---

func TestRevertBatch_ContextCancelled(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", MovieID: "ABC-001", OriginalPath: "/src/file.mp4", NewPath: "/dst/file.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
	}

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", uint(1), models.RevertStatusReverted).Return(nil)

	ctx, cancel := context.WithCancel(context.Background())
	// Don't cancel — just verify it works with context
	result, err := reverter.RevertBatch(ctx, "batch-1")
	require.NoError(t, err)
	assert.Equal(t, 1, result.Succeeded)
	cancel() // clean up

	mockRepo.AssertExpectations(t)
}

// Helper to verify that the mock has the correct assertions
func TestReverter_NewReverter(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	reverter := NewReverter(fs, mockRepo)

	assert.NotNil(t, reverter)
}

// --- Test: In-place rename with same filename (no file rename needed) ---

func TestRevertFile_InPlaceRenameSameFilename(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// After organize: directory was renamed, but filename stayed the same
	createTestFile(t, fs, "/src/Studio - Title/file.mp4", "video-content")

	op := &models.BatchFileOperation{
		ID:              500,
		BatchJobID:      "batch-1",
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/file.mp4",
		NewPath:         "/src/Studio - Title/file.mp4",
		OperationType:   models.OperationTypeMove,
		RevertStatus:    models.RevertStatusApplied,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	mockRepo.On("UpdateRevertStatus", uint(500), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Directory renamed back, file stays with same name
	exists, _ := afero.Exists(fs, "/src/ABC-123/file.mp4")
	assert.True(t, exists, "file should be at original path after directory rename back")

	mockRepo.AssertExpectations(t)
}

// --- Test Case: Update-mode revert restores NFO and deletes generated files (HIST-05) ---

func TestRevertFile_UpdateMode_RestoresNFOAndDeletesGenerated(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	createTestFile(t, fs, "/src/ABC-123.mp4", "video-content")
	createTestFile(t, fs, "/src/ABC-123.nfo", "new-nfo-content")
	createTestFile(t, fs, "/src/poster.jpg", "poster-data")

	nfoSnapshot := `<episodedetails><title>Original</title></episodedetails>`
	generatedFiles := GeneratedFilesJSON{
		Delete: []string{"/src/ABC-123.nfo", "/src/poster.jpg"},
	}
	gfJSON, err := json.Marshal(generatedFiles)
	require.NoError(t, err)

	op := &models.BatchFileOperation{
		ID:             600,
		BatchJobID:     "batch-1",
		MovieID:        "ABC-123",
		OriginalPath:   "/src/ABC-123.mp4",
		NewPath:        "/src/ABC-123.mp4",
		OperationType:  models.OperationTypeUpdate,
		NFOSnapshot:    nfoSnapshot,
		NFOPath:        "/src/ABC-123.nfo",
		GeneratedFiles: string(gfJSON),
		RevertStatus:   models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(600), models.RevertStatusReverted).Return(nil)

	_, err = reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	exists, _ := afero.Exists(fs, "/src/ABC-123.mp4")
	assert.True(t, exists, "video file should still exist at original path")

	exists, _ = afero.Exists(fs, "/src/poster.jpg")
	assert.False(t, exists, "poster should be deleted (generated file)")

	nfoPath := "/src/ABC-123.nfo"
	exists, _ = afero.Exists(fs, nfoPath)
	assert.True(t, exists, "NFO snapshot should be restored")

	content, err := afero.ReadFile(fs, nfoPath)
	assert.NoError(t, err)
	assert.Equal(t, nfoSnapshot, string(content))

	mockRepo.AssertExpectations(t)
}

// --- Test Case: Update-mode revert with no NFO snapshot just deletes generated files ---

func TestRevertFile_UpdateMode_NoNFOSnapshot(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	createTestFile(t, fs, "/src/ABC-123.mp4", "video-content")
	createTestFile(t, fs, "/src/poster.jpg", "poster-data")

	generatedFiles := GeneratedFilesJSON{
		Delete: []string{"/src/poster.jpg"},
	}
	gfJSON, err := json.Marshal(generatedFiles)
	require.NoError(t, err)

	op := &models.BatchFileOperation{
		ID:             601,
		BatchJobID:     "batch-1",
		MovieID:        "ABC-123",
		OriginalPath:   "/src/ABC-123.mp4",
		NewPath:        "/src/ABC-123.mp4",
		OperationType:  models.OperationTypeUpdate,
		NFOSnapshot:    "",
		NFOPath:        "/src/ABC-123.nfo",
		GeneratedFiles: string(gfJSON),
		RevertStatus:   models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(601), models.RevertStatusReverted).Return(nil)

	_, err = reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	exists, _ := afero.Exists(fs, "/src/poster.jpg")
	assert.False(t, exists, "poster should be deleted")

	exists, _ = afero.Exists(fs, "/src/ABC-123.mp4")
	assert.True(t, exists, "video file should still exist at original path")

	mockRepo.AssertExpectations(t)
}

// --- Empty directory cleanup tests (Phase 07) ---

// TestRevertFile_EmptyDirCleanup_AfterMove verifies that after reverting a move-mode
// operation, the directory the file was moved FROM is removed if empty.
func TestRevertFile_EmptyDirCleanup_AfterMove(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: file was moved from /src/file.mp4 to /dst/ABC-123/ABC-123.mp4
	// After revert, /dst/ABC-123 should be removed because it's empty
	createTestFile(t, fs, "/dst/ABC-123/ABC-123.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:            700,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/ABC-123/ABC-123.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(700), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify file moved back to original path
	exists, _ := afero.Exists(fs, "/src/file.mp4")
	assert.True(t, exists, "file should exist at original path")

	// Verify the empty destination directory was removed
	dirExists, _ := afero.DirExists(fs, "/dst/ABC-123")
	assert.False(t, dirExists, "empty destination directory should be removed after revert")
}

// TestRevertFile_EmptyDirCleanup_DirNotEmpty verifies that after reverting,
// if the destination directory still contains other files, it is NOT removed.
func TestRevertFile_EmptyDirCleanup_DirNotEmpty(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: file was moved from /src/file.mp4 to /dst/ABC-123/ABC-123.mp4
	// Another file remains in /dst/ABC-123 so the directory should NOT be removed
	createTestFile(t, fs, "/dst/ABC-123/ABC-123.mp4", "video-content")
	createTestFile(t, fs, "/dst/ABC-123/other-file.txt", "other-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:            701,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/ABC-123/ABC-123.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(701), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify the destination directory still exists because it has other files
	dirExists, _ := afero.DirExists(fs, "/dst/ABC-123")
	assert.True(t, dirExists, "non-empty destination directory should NOT be removed")
}

// TestRevertFile_EmptyDirCleanup_ParentCleanup verifies that after removing an empty
// child directory, cleanup stops at the destination root boundary.
func TestRevertFile_EmptyDirCleanup_ParentCleanup(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: /dst/ABC-123/ABC-123.mp4 — typical 2-level organize structure
	// destRoot = filepath.Dir(filepath.Dir("/dst/ABC-123/ABC-123.mp4")) = "/dst"
	// After revert, /dst/ABC-123 is empty → removed, but /dst is the stopAt boundary → preserved
	createTestFile(t, fs, "/dst/ABC-123/ABC-123.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	op := &models.BatchFileOperation{
		ID:            702,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/ABC-123/ABC-123.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(702), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Child directory should be removed
	dirExists, _ := afero.DirExists(fs, "/dst/ABC-123")
	assert.False(t, dirExists, "empty child directory should be removed")

	// Destination root should be preserved even if empty (stopAt boundary)
	parentExists, _ := afero.DirExists(fs, "/dst")
	assert.True(t, parentExists, "destination root directory should be preserved (stopAt boundary)")
}

// TestRevertFile_EmptyDirCleanup_GeneratedFilesMakeDirEmpty verifies that after
// generated file deletion empties the directory, it is removed.
func TestRevertFile_EmptyDirCleanup_GeneratedFilesMakeDirEmpty(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: file was moved, and generated files (poster) exist in same dir
	// After revert: video moved back, poster deleted → directory becomes empty → removed
	createTestFile(t, fs, "/dst/ABC-123/ABC-123.mp4", "video-content")
	createTestFile(t, fs, "/dst/ABC-123/poster.jpg", "poster-data")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	generatedFiles := GeneratedFilesJSON{
		Delete: []string{"/dst/ABC-123/poster.jpg"},
	}
	gfJSON, err := json.Marshal(generatedFiles)
	require.NoError(t, err)

	op := &models.BatchFileOperation{
		ID:             703,
		BatchJobID:     "batch-1",
		MovieID:        "ABC-123",
		OriginalPath:   "/src/file.mp4",
		NewPath:        "/dst/ABC-123/ABC-123.mp4",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: string(gfJSON),
	}

	mockRepo.On("UpdateRevertStatus", uint(703), models.RevertStatusReverted).Return(nil)

	_, err = reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify the destination directory was removed (video moved back, poster deleted)
	dirExists, _ := afero.DirExists(fs, "/dst/ABC-123")
	assert.False(t, dirExists, "directory should be removed after video moved back and poster deleted")
}

// TestRevertFile_InPlaceRename_NoDirCleanup verifies that in-place rename revert
// does NOT trigger directory cleanup (the directory was renamed, not left behind).
func TestRevertFile_InPlaceRename_NoDirCleanup(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// After organize: directory was renamed from /src/ABC-123 to /src/Studio - Title
	// In-place rename revert just renames the directory back — no empty directories left
	createTestFile(t, fs, "/src/Studio - Title/renamed.mp4", "video-content")

	op := &models.BatchFileOperation{
		ID:              704,
		BatchJobID:      "batch-1",
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/file.mp4",
		NewPath:         "/src/Studio - Title/renamed.mp4",
		OperationType:   models.OperationTypeMove,
		RevertStatus:    models.RevertStatusApplied,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	mockRepo.On("UpdateRevertStatus", uint(704), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	// Verify directory was renamed back (not cleaned up)
	dirExists, _ := afero.DirExists(fs, "/src/ABC-123")
	assert.True(t, dirExists, "original directory should exist after in-place rename revert")

	// The renamed directory should no longer exist
	renamedExists, _ := afero.DirExists(fs, "/src/Studio - Title")
	assert.False(t, renamedExists, "renamed directory should not exist after revert")
}

// --- Test Case: Update-mode revert with no generated files just restores NFO ---

func TestRevertFile_UpdateMode_NoGeneratedFiles(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	createTestFile(t, fs, "/src/ABC-123.mp4", "video-content")

	nfoSnapshot := `<episodedetails><title>Original</title></episodedetails>`

	op := &models.BatchFileOperation{
		ID:             602,
		BatchJobID:     "batch-1",
		MovieID:        "ABC-123",
		OriginalPath:   "/src/ABC-123.mp4",
		NewPath:        "/src/ABC-123.mp4",
		OperationType:  models.OperationTypeUpdate,
		NFOSnapshot:    nfoSnapshot,
		NFOPath:        "/src/ABC-123.nfo",
		GeneratedFiles: "",
		RevertStatus:   models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(602), models.RevertStatusReverted).Return(nil)

	_, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)

	nfoPath := "/src/ABC-123.nfo"
	exists, _ := afero.Exists(fs, nfoPath)
	assert.True(t, exists, "NFO snapshot should be restored")

	content, err := afero.ReadFile(fs, nfoPath)
	assert.NoError(t, err)
	assert.Equal(t, nfoSnapshot, string(content))

	exists, _ = afero.Exists(fs, "/src/ABC-123.mp4")
	assert.True(t, exists, "video file should still exist at original path")

	mockRepo.AssertExpectations(t)
}

// --- Test: Update-mode anchor missing skips operation (D-02) ---

func TestRevertFile_UpdateMode_AnchorMissing_Skips(t *testing.T) {
	_, mockRepo, reverter := setupReverterTest(t)

	// Video file does NOT exist at OriginalPath for update-mode
	op := &models.BatchFileOperation{
		ID:            700,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/nonexistent.mp4",
		NewPath:       "/src/nonexistent.mp4",
		OperationType: models.OperationTypeUpdate,
		NFOSnapshot:   "<nfo>content</nfo>",
		NFOPath:       "/src/ABC-123.nfo",
		RevertStatus:  models.RevertStatusApplied,
	}

	// No UpdateRevertStatus — anchor missing means operation stays applied

	res, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeSkipped, res.Outcome)
	assert.Equal(t, models.RevertReasonAnchorMissing, res.Reason)

	mockRepo.AssertExpectations(t)
}

// --- Test: Destination conflict in move-mode (D-04) ---

func TestRevertFile_MoveMode_DestinationConflict(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: both NewPath (source) and OriginalPath (destination) exist
	createTestFile(t, fs, "/dst/file.mp4", "video-content")
	createTestFile(t, fs, "/src/file.mp4", "existing-file")

	op := &models.BatchFileOperation{
		ID:            800,
		BatchJobID:    "batch-1",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", uint(800), models.RevertStatusFailed).Return(nil)

	res, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeFailed, res.Outcome)
	assert.Equal(t, models.RevertReasonDestinationConflict, res.Reason)

	// Verify destination file was NOT overwritten
	content, _ := afero.ReadFile(fs, "/src/file.mp4")
	assert.Equal(t, "existing-file", string(content))

	mockRepo.AssertExpectations(t)
}

// --- Test: In-place rename destination conflict (D-04) ---

func TestRevertFile_InPlaceRename_DestinationConflict(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	createTestFile(t, fs, "/src/Studio - Title/renamed.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src/ABC-123", 0777))

	op := &models.BatchFileOperation{
		ID:              801,
		BatchJobID:      "batch-1",
		MovieID:         "ABC-123",
		OriginalPath:    "/src/ABC-123/file.mp4",
		NewPath:         "/src/Studio - Title/renamed.mp4",
		OperationType:   models.OperationTypeMove,
		RevertStatus:    models.RevertStatusApplied,
		InPlaceRenamed:  true,
		OriginalDirPath: "/src/ABC-123",
	}

	mockRepo.On("UpdateRevertStatus", uint(801), models.RevertStatusFailed).Return(nil)

	res, err := reverter.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeFailed, res.Outcome)
	assert.Equal(t, models.RevertReasonDestinationConflict, res.Reason)

	mockRepo.AssertExpectations(t)
}

// --- Test: Skipped operation stays in applied status (D-02) ---

func TestRevertBatch_SkippedOperationStaysApplied(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: 2 move ops, one has anchor missing (skipped)
	createTestFile(t, fs, "/dst/file1.mp4", "video1")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", MovieID: "ABC-001", OriginalPath: "/src/file1.mp4", NewPath: "/dst/file1.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
		{ID: 2, BatchJobID: "batch-1", MovieID: "ABC-002", OriginalPath: "/src/file2.mp4", NewPath: "/dst/file2.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
	}

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", uint(1), models.RevertStatusReverted).Return(nil)
	// ID 2 is skipped — no UpdateRevertStatus call

	result, err := reverter.RevertBatch(context.Background(), "batch-1")
	require.NoError(t, err)

	assert.Equal(t, 2, result.Total)
	assert.Equal(t, 1, result.Succeeded)
	assert.Equal(t, 1, result.Skipped)
	assert.Equal(t, 0, result.Failed)
	// Outcomes contains all results (succeeded + skipped)
	assert.Len(t, result.Outcomes, 2)

	// Find the skipped outcome
	var skippedOutcome *RevertFileResult
	for i := range result.Outcomes {
		if result.Outcomes[i].Outcome == models.RevertOutcomeSkipped {
			skippedOutcome = &result.Outcomes[i]
			break
		}
	}
	require.NotNil(t, skippedOutcome, "expected a skipped outcome")
	assert.Equal(t, models.RevertReasonAnchorMissing, skippedOutcome.Reason)

	mockRepo.AssertExpectations(t)
}

// --- Test: Batch revert cleans up empty destination directory ---
// Verifies that after reverting a batch where all files were in the same
// destination directory, the directory is removed.

func TestRevertBatch_CleansUpEmptyDestinationDir(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: file moved to same destination directory /out/ABC-123/
	createTestFile(t, fs, "/out/ABC-123/ABC-123.mp4", "video-content")
	createTestFile(t, fs, "/out/ABC-123/ABC-123-poster.jpg", "poster-data")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	generatedFiles := GeneratedFilesJSON{
		Delete: []string{"/out/ABC-123/ABC-123-poster.jpg"},
	}
	gfJSON, err := json.Marshal(generatedFiles)
	require.NoError(t, err)

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", MovieID: "ABC-123", OriginalPath: "/src/ABC-123.mp4", NewPath: "/out/ABC-123/ABC-123.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: string(gfJSON)},
	}

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", uint(1), models.RevertStatusReverted).Return(nil)

	result, err := reverter.RevertBatch(context.Background(), "batch-1")
	require.NoError(t, err)

	assert.Equal(t, 1, result.Succeeded)

	// Video moved back
	exists, _ := afero.Exists(fs, "/src/ABC-123.mp4")
	assert.True(t, exists, "video file should be moved back")

	// Generated file deleted
	exists, _ = afero.Exists(fs, "/out/ABC-123/ABC-123-poster.jpg")
	assert.False(t, exists, "poster should be deleted")

	// Destination directory removed (now empty)
	dirExists, _ := afero.DirExists(fs, "/out/ABC-123")
	assert.False(t, dirExists, "empty destination directory should be removed after batch revert")

	mockRepo.AssertExpectations(t)
}

// --- Test: Batch revert cleans up nested empty parent directories ---
// Verifies that after reverting a file from /out/ABP-880/long-named-dir/,
// the parent directory /out/ABP-880 is also removed when empty.
// This is the core bug: multi-level destination directory trees left behind.

func TestRevertBatch_CleansUpNestedEmptyParentDir(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	// Setup: typical organize structure with multi-level destination
	// out/ABP-880/ABP-880 [Studio] - Title/ABP-880.mp4
	innerDir := "/out/ABP-880/ABP-880 [Studio] - Title"
	createTestFile(t, fs, innerDir+"/ABP-880.mp4", "video-content")
	createTestFile(t, fs, innerDir+"/ABP-880.nfo", "nfo-content")
	createTestFile(t, fs, innerDir+"/ABP-880-poster.jpg", "poster-data")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	generatedFiles := GeneratedFilesJSON{
		Delete: []string{
			innerDir + "/ABP-880.nfo",
			innerDir + "/ABP-880-poster.jpg",
		},
	}
	gfJSON, err := json.Marshal(generatedFiles)
	require.NoError(t, err)

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", MovieID: "ABP-880", OriginalPath: "/src/ABP-880.mp4", NewPath: innerDir + "/ABP-880.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied, GeneratedFiles: string(gfJSON)},
	}

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)
	mockRepo.On("UpdateRevertStatus", uint(1), models.RevertStatusReverted).Return(nil)

	result, err := reverter.RevertBatch(context.Background(), "batch-1")
	require.NoError(t, err)

	assert.Equal(t, 1, result.Succeeded)

	// Video moved back
	exists, _ := afero.Exists(fs, "/src/ABP-880.mp4")
	assert.True(t, exists, "video file should be moved back to original path")

	// Generated files deleted
	exists, _ = afero.Exists(fs, innerDir+"/ABP-880.nfo")
	assert.False(t, exists, "NFO should be deleted")
	exists, _ = afero.Exists(fs, innerDir+"/ABP-880-poster.jpg")
	assert.False(t, exists, "poster should be deleted")

	// Inner directory removed (now empty)
	dirExists, _ := afero.DirExists(fs, innerDir)
	assert.False(t, dirExists, "inner destination directory should be removed")

	// Parent directory removed (now empty after inner dir removed)
	dirExists, _ = afero.DirExists(fs, "/out/ABP-880")
	assert.False(t, dirExists, "parent ABP-880 directory should be removed when empty after batch revert")

	mockRepo.AssertExpectations(t)
}

// --- Test: Batch revert still cleans up directories when DB status update fails ---
// Regresses: When filesystem revert succeeds but UpdateRevertStatus fails,
// the operation returns sysErr (not RevertOutcomeReverted), but the batch
// sweep should still attempt cleanup since the filesystem changes happened.

func TestRevertBatch_CleansUpNestedDirWhenDBStatusFails(t *testing.T) {
	fs, mockRepo, reverter := setupReverterTest(t)

	innerDir := "/out/ABP-880/ABP-880 [Studio] - Title"
	createTestFile(t, fs, innerDir+"/ABP-880.mp4", "video-content")
	require.NoError(t, fs.MkdirAll("/src", 0777))

	ops := []models.BatchFileOperation{
		{ID: 1, BatchJobID: "batch-1", MovieID: "ABP-880", OriginalPath: "/src/ABP-880.mp4", NewPath: innerDir + "/ABP-880.mp4", OperationType: models.OperationTypeMove, RevertStatus: models.RevertStatusApplied},
	}

	mockRepo.On("FindByBatchJobID", "batch-1").Return(ops, nil)
	// UpdateRevertStatus fails — simulates DB error after filesystem success
	mockRepo.On("UpdateRevertStatus", uint(1), models.RevertStatusReverted).Return(fmt.Errorf("db connection lost"))

	result, err := reverter.RevertBatch(context.Background(), "batch-1")
	// Batch returns no outer error — best-effort processing
	require.NoError(t, err)
	// The operation is counted as failed because DB status persistence failed
	assert.Equal(t, 1, result.Failed)
	assert.Equal(t, 0, result.Succeeded)

	// Video should still be moved back (filesystem succeeded)
	exists, _ := afero.Exists(fs, "/src/ABP-880.mp4")
	assert.True(t, exists, "video file should be moved back even though DB update failed")

	// Nested parent directory should still be cleaned up by the batch sweep,
	// even though the operation outcome was Failed (not Reverted).
	dirExists, _ := afero.DirExists(fs, "/out/ABP-880")
	assert.False(t, dirExists, "parent ABP-880 directory should be removed by batch sweep even when DB status update failed")

	mockRepo.AssertExpectations(t)
}

func TestIsDescendant(t *testing.T) {
	t.Run("same path is descendant", func(t *testing.T) {
		assert.True(t, isDescendant("/out/ABP-880", "/out/ABP-880"))
	})

	t.Run("child is descendant", func(t *testing.T) {
		assert.True(t, isDescendant("/out/ABP-880/ABP-880.mp4", "/out/ABP-880"))
	})

	t.Run("nested child is descendant", func(t *testing.T) {
		assert.True(t, isDescendant("/out/ABP-880/sub/ABP-880.mp4", "/out/ABP-880"))
	})

	t.Run("unrelated path is not descendant", func(t *testing.T) {
		assert.False(t, isDescendant("/out/OTHER-123/OTHER-123.mp4", "/out/ABP-880"))
	})

	t.Run("prefix match without separator is not descendant", func(t *testing.T) {
		assert.False(t, isDescendant("/out/ABP-8800/video.mp4", "/out/ABP-880"))
	})

	t.Run("relative paths work", func(t *testing.T) {
		assert.True(t, isDescendant("out/sub/file.txt", "out/sub"))
	})
}
