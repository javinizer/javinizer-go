package history

import (
	"context"
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

// expectMockUndoAdmission runs the real admission callback against a lean,
// detached row and persists its journal exactly like UpdateJournalInTx.
func expectMockUndoAdmission(t *testing.T, repo *mocks.MockBatchFileOperationRepositoryInterface, stored *models.BatchFileOperation) *mock.Call {
	t.Helper()
	return repo.On("UpdateJournalInTx", mock.Anything, stored.ID, mock.Anything).Return(func(_ context.Context, _ uint, fn database.JournalUpdateFn) error {
		current := &models.BatchFileOperation{ID: stored.ID, GeneratedFiles: stored.GeneratedFiles, RevertStatus: stored.RevertStatus}
		before, err := models.ParseGeneratedFiles(current.GeneratedFiles)
		require.NoError(t, err)
		next, persist, err := fn(current)
		if err != nil {
			return err
		}
		require.Equal(t, !before.UndoStarted, persist, "Undo admission must persist once and remain monotonic on retry")
		before.UndoStarted = true
		require.Equal(t, before, next, "admission must retain the complete inverse journal")
		if persist {
			stored.GeneratedFiles = models.MarshalLedgerJSON(next)
		}
		return nil
	}).Once()
}

// requireOnlyUndoAdmissionChanged retains the entire inverse journal while
// requiring the one durable admission bit that remains set after refusal.
func requireOnlyUndoAdmissionChanged(t *testing.T, before, after string) {
	t.Helper()
	want, err := models.ParseGeneratedFiles(before)
	require.NoError(t, err)
	want.UndoStarted = true
	require.JSONEq(t, models.MarshalLedgerJSON(want), after)
}
