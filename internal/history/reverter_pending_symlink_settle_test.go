package history

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// codex P2 (PRRT_kwDORn9KaM6m7sQ4): an unexecuted pending move whose source
// pathname is a dangling symlink must settle exactly like any occupied source.
// Plain Stat follows the link, misreads the entry as absent, and leaves the
// row anchor-skipping through every batch-revert retry even though the move
// never ran.
func TestRevertFile_UnexecutedMoveIntentDanglingSymlinkSourceSettlesNoOp(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o777))
	src := filepath.Join(srcDir, "ABC-123.mp4")
	dst := filepath.Join(dir, "dst", "lib", "ABC-123.mp4")
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone.mp4"), src), "source occupied by a dangling symlink")

	gf := models.GeneratedFilesJSON{MoveBack: []models.FileMove{{OriginalPath: src, NewPath: dst}}}
	gfJSON, _ := json.Marshal(gf)
	op := &models.BatchFileOperation{
		ID:             810,
		MovieID:        "ABC-123",
		OriginalPath:   src,
		NewPath:        "",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: string(gfJSON),
	}
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("FindByID", mock.Anything, uint(810)).Return(op, nil)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(810), models.RevertStatusNoOp).Return(nil)

	r := NewReverter(fs, mockRepo)
	result, err := r.revertFile(context.Background(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeSkipped, result.Outcome)
	assert.Equal(t, models.RevertReasonAnchorMissing, result.Reason)
	assert.Equal(t, models.RevertStatusNoOp, op.RevertStatus, "the dangling-symlink source settles as an unexecuted move")

	_, lerr := os.Lstat(src)
	assert.NoError(t, lerr, "the settle leaves the directory entry untouched")

	// The settle is terminal — a retry hits the double-revert guard instead of
	// probing the anchor forever.
	_, err = r.revertFile(context.Background(), op)
	assert.ErrorIs(t, err, ErrBatchAlreadyReverted, "settled rows terminate retries")
}
