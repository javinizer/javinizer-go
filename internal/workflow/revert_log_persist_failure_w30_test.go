package workflow

// POSTER-WRITE-HARDENING wave-30 — revert-log persist-failure coverage pins
// left over from wave-22: every completion-side persistNonJournalColumns
// failure leg must surface (warn or error) rather than being silently
// dropped. The failure is replayed by wrapping the real sqlite repository
// (the same fixture family as coverage_uncovered_test.go) with an
// UpdateNonJournalFields override that returns a generic error — distinct
// from the wave-15 ErrOperationRowReverted tolerance class.

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/template"
)

// w30FailNonJournalRepo wraps the production repository and fails
// UpdateNonJournalFields with a caller-chosen generic error.
type w30FailNonJournalRepo struct {
	database.BatchFileOperationRepositoryInterface
	failErr error
}

func (r *w30FailNonJournalRepo) UpdateNonJournalFields(ctx context.Context, op *models.BatchFileOperation) error {
	if r.failErr != nil {
		return r.failErr
	}
	return r.BatchFileOperationRepositoryInterface.UpdateNonJournalFields(ctx, op)
}

var _ database.BatchFileOperationRepositoryInterface = (*w30FailNonJournalRepo)(nil)

// w30RevertLog builds a dbRevertLog over the shared in-memory fixture.
func w30RevertLog(t *testing.T, repo database.BatchFileOperationRepositoryInterface, fs afero.Fs) RevertLog {
	t.Helper()
	cfg := config.DefaultConfig(nil, nil)
	_, err := config.Prepare(cfg)
	require.NoError(t, err)
	return NewDBRevertLog(repo, &RevertLogConfig{
		AllowRevert: true,
		NFOCfg:      nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)),
	}, "job-w30-persist-fail", fs, template.NewEngine(),
		nfo.NewNFOImplementor(fs, nfo.ConfigFromAppConfig(cfg, nfo.NFONameConfigFromAppConfig(cfg)), template.NewEngine()), nil)
}

func w30Begin(t *testing.T, rl RevertLog) string {
	t.Helper()
	opID, err := rl.Begin(context.Background(), ApplyCmd{
		Movie: &models.Movie{ID: "TEST-001", Title: "Test Movie"},
		Match: models.FileMatchInfo{Path: "/source/TEST-001.mp4", MovieID: "TEST-001"},
	})
	require.NoError(t, err)
	require.NotEmpty(t, opID)
	return opID
}

// CaptureSnapshot's persist failure is WARN-ONLY (snapshot loss must never
// fail the apply): the call completes and the record keeps its previous
// snapshot data.
func TestDBRevertLogW30_CaptureSnapshotPersistFailureWarns(t *testing.T) {
	db := newInMemoryDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()

	// Begin + snapshot against the healthy repo first.
	healthy := NewDBRevertLog(repo, nil, "job-w30-healthy-snapshot", fs, template.NewEngine(), nil, nil)
	opID := w30Begin(t, healthy)

	broken := &w30FailNonJournalRepo{BatchFileOperationRepositoryInterface: repo, failErr: errors.New("replay: non-journal persist wedged")}
	rl := w30RevertLog(t, broken, fs)
	rl.CaptureSnapshot(context.Background(), opID, ApplyCmd{
		Movie: &models.Movie{ID: "TEST-001", Title: "Test Movie"},
		Match: models.FileMatchInfo{Path: "/source/TEST-001.mp4", MovieID: "TEST-001"},
	})

	recordID64 := parseOpIDForTest(t, opID)
	record, err := repo.FindByID(context.Background(), uint(recordID64))
	require.NoError(t, err)
	require.NotNil(t, record)
	assert.Empty(t, record.NFOSnapshot, "the failed persist never landed snapshot columns")
}

// The nil-result CompleteFailed leg: a persist failure SURFACES as the
// returned error instead of being dropped.
func TestDBRevertLogW30_CompleteFailedNilResultPersistFailureSurfaces(t *testing.T) {
	db := newInMemoryDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()

	healthy := NewDBRevertLog(repo, nil, "job-w30-healthy-nil", fs, template.NewEngine(), nil, nil)
	opID := w30Begin(t, healthy)

	broken := &w30FailNonJournalRepo{BatchFileOperationRepositoryInterface: repo, failErr: errors.New("replay: non-journal persist wedged")}
	rl := w30RevertLog(t, broken, fs)
	err := rl.CompleteFailed(context.Background(), opID, nil)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revert log CompleteFailed: mark record")
}

// The post-mutation CompleteFailed leg: the journal merge lands first
// (UpdateJournalInTx still healthy), then the non-journal persist fails and
// surfaces through the "update failed record" wrap.
func TestDBRevertLogW30_CompleteFailedPostMutationPersistFailureSurfaces(t *testing.T) {
	db := newInMemoryDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()

	healthy := NewDBRevertLog(repo, nil, "job-w30-healthy-post", fs, template.NewEngine(), nil, nil)
	opID := w30Begin(t, healthy)

	broken := &w30FailNonJournalRepo{BatchFileOperationRepositoryInterface: repo, failErr: errors.New("replay: non-journal persist wedged")}
	rl := w30RevertLog(t, broken, fs)
	err := rl.CompleteFailed(context.Background(), opID, &ApplyResult{
		Movie:   &models.Movie{ID: "TEST-001"},
		NFOPath: "/dest/TEST-001.nfo",
	})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "revert log CompleteFailed: update failed record")
}

func parseOpIDForTest(t *testing.T, opID string) uint64 {
	t.Helper()
	var id uint64
	_, err := fmt.Sscanf(opID, "%d", &id)
	require.NoError(t, err)
	return id
}
