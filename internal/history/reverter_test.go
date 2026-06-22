package history

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
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
