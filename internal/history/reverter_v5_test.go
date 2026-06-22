package history

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func setupReverterV5(t *testing.T) (afero.Fs, *mocks.MockBatchFileOperationRepositoryInterface, *Reverter) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	reverter := NewReverter(fs, mockRepo)
	return fs, mockRepo, reverter
}

func TestIsDescendant_V5_RelativePaths(t *testing.T) {
	tests := []struct {
		path, parent string
		want         bool
	}{
		{"/a/b/c", "/a/b", true},
		{"/a/b", "/a/b", true},
		{"/a/bc", "/a/b", false},
		{"/x/y", "/a/b", false},
	}

	for _, tt := range tests {
		got := isDescendant(tt.path, tt.parent)
		assert.Equal(t, tt.want, got, "isDescendant(%q, %q)", tt.path, tt.parent)
	}
}

func TestReverter_V5_NewReverterFields(t *testing.T) {
	fs := afero.NewMemMapFs()
	mockRepo := mocks.NewMockBatchFileOperationRepositoryInterface(t)
	r := NewReverter(fs, mockRepo)

	assert.NotNil(t, r)
	assert.Equal(t, fs, r.fs)
	assert.Equal(t, mockRepo, r.batchFileOpRepo)
}
