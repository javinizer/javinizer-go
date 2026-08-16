package history

import (
	"context"
	"fmt"
	"os"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// --- Miss-line coverage tests for reverter.go ---
// Target miss lines: 126-128, 141-143, 179-180, 196-201, 208-216,
// 226-228, 236-238, 271-273, 278-280, 288-291, 321-324, 327-330,
// 352-354, 401-403, 410-416, 451, 552-562, 570-571

func TestMiss_CheckAnchor_AccessDenied(t *testing.T) {
	fs, mockRepo, _ := setupReverterTest(t)

	// Create a file at the anchor path, then use a read-only fs wrapper
	// to trigger a non-NotExist, non-nil error
	createTestFile(t, fs, "/dst/file.mp4", "video")

	// Use a custom Fs that returns a generic error on Stat for our path
	errorFs := &statErrorFs{Fs: fs, errorPath: "/dst/file.mp4"}
	errorReverter := NewReverter(errorFs, mockRepo)

	op := &models.BatchFileOperation{
		ID:            9070,
		BatchJobID:    "batch-miss",
		MovieID:       "ABC-123",
		OriginalPath:  "/src/file.mp4",
		NewPath:       "/dst/file.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}

	mockRepo.On("UpdateRevertStatus", mock.Anything, uint(9070), models.RevertStatusFailed).Return(nil)

	res, err := errorReverter.revertFile(context.Background(), op)
	require.NoError(t, err)
	assert.Equal(t, models.RevertOutcomeFailed, res.Outcome)
	assert.Equal(t, models.RevertReasonAccessDenied, res.Reason)

	mockRepo.AssertExpectations(t)
}

// statErrorFs wraps afero.Fs and returns a generic error for Stat on a specific path
type statErrorFs struct {
	afero.Fs
	errorPath string
}

func (fs *statErrorFs) Stat(name string) (os.FileInfo, error) {
	if name == fs.errorPath {
		return nil, fmt.Errorf("permission denied")
	}
	return fs.Fs.Stat(name)
}

// This specifically hits the `sourcePath != op.OriginalPath` check being false
