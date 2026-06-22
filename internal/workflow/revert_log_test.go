package workflow

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/stretchr/testify/assert"
)

func TestNoOpRevertLog_Begin(t *testing.T) {
	log := noOpRevertLog{}
	id, err := log.Begin(nil, ApplyCmd{})
	assert.Empty(t, id)
	assert.NoError(t, err)
}

func TestNoOpRevertLog_CaptureSnapshot(t *testing.T) {
	log := noOpRevertLog{}
	// Should not panic
	log.CaptureSnapshot(nil, "op1", ApplyCmd{})
}

func TestNoOpRevertLog_Complete(t *testing.T) {
	log := noOpRevertLog{}
	err := log.Complete(nil, "op1", nil)
	assert.NoError(t, err)
}

func TestDetermineOperationType_Move(t *testing.T) {
	assert.Equal(t, models.OperationTypeMove, determineOperationType(true, organizer.LinkModeNone, false))
}

func TestDetermineOperationType_Hardlink(t *testing.T) {
	assert.Equal(t, models.OperationTypeHardlink, determineOperationType(false, organizer.LinkModeHard, false))
}

func TestDetermineOperationType_Symlink(t *testing.T) {
	assert.Equal(t, models.OperationTypeSymlink, determineOperationType(false, organizer.LinkModeSoft, false))
}

func TestDetermineOperationType_Copy(t *testing.T) {
	assert.Equal(t, models.OperationTypeCopy, determineOperationType(false, organizer.LinkModeNone, false))
}

func TestDetermineOperationType_Update(t *testing.T) {
	assert.Equal(t, models.OperationTypeUpdate, determineOperationType(true, organizer.LinkModeNone, true))
}

func TestNewPreOrganizeRecord(t *testing.T) {
	rec := newPreOrganizeRecord("job1", "ABC-001", "/src/file.mp4", "nfo-content", "/src/file.nfo", "/src", models.OperationTypeMove, false)
	assert.Equal(t, "job1", rec.BatchJobID)
	assert.Equal(t, "ABC-001", rec.MovieID)
	assert.Equal(t, "/src/file.mp4", rec.OriginalPath)
	assert.Equal(t, "nfo-content", rec.NFOSnapshot)
	assert.Equal(t, "/src/file.nfo", rec.NFOPath)
	assert.Equal(t, "/src", rec.OriginalDirPath)
	assert.Equal(t, models.RevertStatusApplied, rec.RevertStatus)
	assert.Equal(t, models.OperationTypeMove, rec.OperationType)
	assert.False(t, rec.InPlaceRenamed)
}
