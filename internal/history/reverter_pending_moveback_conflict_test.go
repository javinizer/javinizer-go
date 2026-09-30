package history

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// codex P2 (PRRT_kwDORn9KaM6nD37F): a hydrated pending move intent whose
// replay finds the destination published AND any entry occupying the source
// pathname must take the same destination-conflict branch an occupied regular
// file gets from revertPrimaryFileFS — never the happy rename-back, which on
// POSIX would replace a dangling-symlink occupant that leg's following Stat
// reads as vacant.

// pendingMoveDanglingLinkInfo is the no-follow shape of a dangling symlink: a
// non-regular directory entry carrying ModeSymlink with no resolvable target
// behind a following Stat.
type pendingMoveDanglingLinkInfo struct{ name string }

func (i pendingMoveDanglingLinkInfo) Name() string     { return i.name }
func (pendingMoveDanglingLinkInfo) Size() int64        { return 0 }
func (pendingMoveDanglingLinkInfo) Mode() os.FileMode  { return os.ModeSymlink | 0o777 }
func (pendingMoveDanglingLinkInfo) ModTime() time.Time { return time.Time{} }
func (pendingMoveDanglingLinkInfo) IsDir() bool        { return false }
func (pendingMoveDanglingLinkInfo) Sys() any           { return nil }

// pendingMoveDanglingLinkFs models a dangling symlink over MemMapFs (which has
// no symlink model of its own): the no-follow lookup reports the link as an
// existing directory entry while every following access — including the
// conflict gate's Stat in revertPrimaryFileFS — still reads the pathname as
// vacant. The wrapper mirrors the round-26 symlinkEntryLstatFs pattern.
type pendingMoveDanglingLinkFs struct {
	afero.Fs
	linkPath string
}

func (f *pendingMoveDanglingLinkFs) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	if filepath.Clean(name) == filepath.Clean(f.linkPath) {
		return pendingMoveDanglingLinkInfo{name: filepath.Base(f.linkPath)}, true, nil
	}
	if lst, ok := f.Fs.(afero.Lstater); ok {
		return lst.LstatIfPossible(name)
	}
	info, err := f.Fs.Stat(name)
	return info, false, err
}

func pendingMoveIntentOp(t *testing.T, id uint, movieID, src, dst string) *models.BatchFileOperation {
	t.Helper()
	gf := models.GeneratedFilesJSON{MoveBack: []models.FileMove{{OriginalPath: src, NewPath: dst}}}
	gfJSON, err := json.Marshal(gf)
	require.NoError(t, err)
	return &models.BatchFileOperation{
		ID:             id,
		MovieID:        movieID,
		OriginalPath:   src,
		NewPath:        "",
		OperationType:  models.OperationTypeMove,
		RevertStatus:   models.RevertStatusApplied,
		GeneratedFiles: string(gfJSON),
	}
}

// POSIX shape: a REAL dangling symlink at the source pathname. Without the
// conflict guard the hydration probe reports the entry present, the replay
// falls through, and revertPrimaryFileFS's following Stat reads the pathname
// as vacant — the rename then DESTROYS the symlink entry. The guarded replay
// fails with destination_conflict, retaining both sides.
func TestRevertFile_PendingMoveDanglingSymlinkSourceConflictsRetainsBoth(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	srcDir := filepath.Join(dir, "src")
	require.NoError(t, os.MkdirAll(srcDir, 0o777))
	src := filepath.Join(srcDir, "ABC-123.mp4")
	dst := filepath.Join(dir, "dst", "lib", "ABC-123.mp4")
	require.NoError(t, os.MkdirAll(filepath.Dir(dst), 0o777))
	require.NoError(t, os.WriteFile(dst, []byte("moved bytes"), 0o666), "the deferred publish landed")
	require.NoError(t, os.Symlink(filepath.Join(dir, "gone.mp4"), src), "the source pathname is occupied by a dangling symlink")

	op := pendingMoveIntentOp(t, 811, "ABC-123", src, dst)
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("FindByID", mock.Anything, uint(811)).Return(op, nil)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(811), models.RevertStatusFailed).Return(nil)

	r := NewReverter(fs, mockRepo)
	result, err := r.revertFile(context.Background(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Equal(t, models.RevertReasonDestinationConflict, result.Reason)

	moved, readErr := os.ReadFile(dst)
	require.NoError(t, readErr, "the conflict retains the moved bytes at the destination")
	assert.Equal(t, "moved bytes", string(moved))
	linkInfo, lerr := os.Lstat(src)
	require.NoError(t, lerr, "the dangling symlink entry survives — the rename-back never replaced it")
	assert.NotZero(t, linkInfo.Mode()&os.ModeSymlink)

	// The conflict is retryable rather than terminal: with the occupant
	// cleared, a fresh row (column NewPath empty again, exactly as the
	// database would re-read it) graduates into the ordinary move-back.
	require.NoError(t, os.Remove(src))
	op.NewPath = ""
	op.RevertStatus = models.RevertStatusApplied
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(811), models.RevertStatusReverted).Return(nil)
	retry, err := r.revertFile(context.Background(), op)
	require.NoError(t, err)
	require.NotNil(t, retry)
	assert.Equal(t, models.RevertOutcomeReverted, retry.Outcome)
	restored, readErr := os.ReadFile(src)
	require.NoError(t, readErr, "the cleared occupant lets the move-back land")
	assert.Equal(t, "moved bytes", string(restored))
	if _, statErr := os.Stat(dst); !os.IsNotExist(statErr) {
		t.Fatalf("the move-back vacated the destination: %v", statErr)
	}
}

// Platform-independent memfs shape: the symlink-emulating LstatIfPossible
// wrapper reports the source pathname occupied by a dangling link on any
// host, so the conflict verdict does not depend on real symlink support.
func TestRevertFile_PendingMoveMemfsDanglingLinkSourceConflicts(t *testing.T) {
	base := afero.NewMemMapFs()
	const (
		src = "/src/ABC-123.mp4"
		dst = "/dst/lib/ABC-123.mp4"
	)
	require.NoError(t, base.MkdirAll("/dst/lib", 0o777))
	require.NoError(t, afero.WriteFile(base, dst, []byte("moved bytes"), 0o666))
	fs := &pendingMoveDanglingLinkFs{Fs: base, linkPath: src}

	op := pendingMoveIntentOp(t, 812, "ABC-123", src, dst)
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("FindByID", mock.Anything, uint(812)).Return(op, nil)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(812), models.RevertStatusFailed).Return(nil)

	result, err := NewReverter(fs, mockRepo).revertFile(context.Background(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Equal(t, models.RevertReasonDestinationConflict, result.Reason)

	moved, readErr := afero.ReadFile(base, dst)
	require.NoError(t, readErr)
	assert.Equal(t, "moved bytes", string(moved), "the rename-back against the occupied source never ran")
	if _, statErr := base.Stat(src); !os.IsNotExist(statErr) {
		t.Fatalf("the modeled dangling link stays a mere entry — nothing materialized at the source pathname: %v", statErr)
	}
}

// Occupied REGULAR file at the source (plain memfs, no symlink machinery):
// the hydrated replay fails with the same destination-conflict verdict the
// revertPrimaryFileFS gate gives that shape, retaining both copies
// byte-for-byte.
func TestRevertFile_PendingMoveRegularFileSourceConflicts(t *testing.T) {
	fs := afero.NewMemMapFs()
	const (
		src = "/src/ABC-123.mp4"
		dst = "/dst/lib/ABC-123.mp4"
	)
	require.NoError(t, fs.MkdirAll("/dst/lib", 0o777))
	require.NoError(t, fs.MkdirAll("/src", 0o777))
	require.NoError(t, afero.WriteFile(fs, dst, []byte("moved bytes"), 0o666))
	require.NoError(t, afero.WriteFile(fs, src, []byte("foreign occupant"), 0o666))

	op := pendingMoveIntentOp(t, 813, "ABC-123", src, dst)
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	mockRepo.On("FindByID", mock.Anything, uint(813)).Return(op, nil)
	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(813), models.RevertStatusFailed).Return(nil)

	result, err := NewReverter(fs, mockRepo).revertFile(context.Background(), op)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.Equal(t, models.RevertOutcomeFailed, result.Outcome)
	assert.Equal(t, models.RevertReasonDestinationConflict, result.Reason)

	srcBytes, readErr := afero.ReadFile(fs, src)
	require.NoError(t, readErr)
	assert.Equal(t, "foreign occupant", string(srcBytes), "the occupant is never renamed over")
	dstBytes, readErr := afero.ReadFile(fs, dst)
	require.NoError(t, readErr)
	assert.Equal(t, "moved bytes", string(dstBytes), "the moved bytes stay at the destination")
}
