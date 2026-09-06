package workflow

// PR #248 codex P1 (F1) workflow-level pins: when the repository's
// write-atomic prune fence rejects the revert-log pre-record (the owning
// job's retention claim is committed), RevertLog.Begin must surface
// database.ErrJobPruning with NO OperationID — apply aborts before any
// filesystem mutation (beginRevertLog), so the failure mode "Apply proceeds
// with mutations and NO durable revert ledger" can never arise.

import (
	"context"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w248FenceRejectRepo is the force-ordered stub: its Create reports the
// committed retention claim unconditionally — the sweep-flip-before-insert
// ordering made deterministic. Every other method delegates to the (nil)
// embedded interface: Begin touches Create only, so nothing else is
// reachable.
type w248FenceRejectRepo struct {
	database.BatchFileOperationRepositoryInterface
}

func (w248FenceRejectRepo) Create(context.Context, *models.BatchFileOperation) error {
	return database.ErrJobPruning
}

// TestDBRevertLog_Begin_FenceRejectionYieldsNoOperationID pins the contract
// on the stubbed seam: a fence-rejected pre-record aborts Begin before any
// operation identity exists.
func TestDBRevertLog_Begin_FenceRejectionYieldsNoOperationID(t *testing.T) {
	rl := NewDBRevertLog(w248FenceRejectRepo{}, NewRevertLogConfig(true, nil), "job-w248", afero.NewMemMapFs(), nil, nil, nil)
	opID, err := rl.Begin(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "W248-001", Title: "Fence"},
		Match:    models.FileMatchInfo{Path: "/src/W248-001.mp4", MovieID: "W248-001"},
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.ErrorIs(t, err, database.ErrJobPruning)
	assert.Empty(t, opID, "a fenced Begin must mint no operation identity — apply aborts before mutations")
}

// TestDBRevertLog_Begin_RealRepoPrunedJob drives the same contract through
// the REAL sqlite-backed repository (newTestDBRevertLog wires jobID
// "test-job"): with the claim committed, Begin's single-statement
// fence rejects and zero batch_file_operations rows land.
func TestDBRevertLog_Begin_RealRepoPrunedJob(t *testing.T) {
	rl, db := newTestDBRevertLog(t)

	jobRepo := database.NewJobRepository(db)
	organizedAt := time.Now().UTC().Add(-48 * time.Hour)
	require.NoError(t, jobRepo.Create(context.Background(), &models.Job{
		ID:          "test-job",
		Status:      models.JobStatusOrganized,
		Files:       "[]",
		StartedAt:   organizedAt,
		OrganizedAt: &organizedAt,
	}))
	// Commit the retention claim (mirrors DeleteOrganizedOlderThan's flip;
	// pruningJobStatus is unexported, so bind the durable wire value).
	require.NoError(t, db.DB.Exec("UPDATE jobs SET status = ? WHERE id = ?", "pruning", "test-job").Error)

	opID, err := rl.Begin(context.Background(), ApplyCmd{
		Movie:    &models.Movie{ID: "W248-002", Title: "Real Fence"},
		Match:    models.FileMatchInfo{Path: "/src/W248-002.mp4", MovieID: "W248-002"},
		Organize: OrganizeOptions{MoveFiles: true},
	})
	require.ErrorIs(t, err, database.ErrJobPruning)
	assert.Empty(t, opID)

	count, countErr := database.NewBatchFileOperationRepository(db).CountByBatchJobID(context.Background(), "test-job")
	require.NoError(t, countErr)
	assert.Zero(t, count, "no ledger row may survive a fence-rejected Begin")
}
