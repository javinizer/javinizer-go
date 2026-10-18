package workflow

// POSTER-WRITE-HARDENING wave-30 persistence failure coverage: CaptureSnapshot
// remains warn-only on a guarded column failure; CompleteFailed surfaces an
// atomic completion-payload failure and leaves both journal and columns intact.

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

// w30FailNonJournalRepo injects snapshot-column or atomic-completion failures
// while delegating the transaction and rollback semantics to the real repository.
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

func (r *w30FailNonJournalRepo) UpdateJournalInTx(ctx context.Context, id uint, fn database.JournalUpdateFn) error {
	if r.failErr != nil {
		ctx = failAtomicCompletion(ctx, r.failErr)
	}
	return r.BatchFileOperationRepositoryInterface.UpdateJournalInTx(ctx, id, fn)
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
	before, readErr := repo.FindByID(t.Context(), uint(parseOpIDForTest(t, opID)))
	require.NoError(t, readErr)
	err := rl.CompleteFailed(context.Background(), opID, nil)
	require.ErrorIs(t, err, broken.failErr)
	after, readErr := repo.FindByID(t.Context(), before.ID)
	require.NoError(t, readErr)
	require.Equal(t, before, after, "failure must not publish only one half of completion")
}

// The post-mutation CompleteFailed leg: completion-column resolution fails
// inside the authoritative transaction, rolling back its journal contribution.
func TestDBRevertLogW30_CompleteFailedPostMutationPersistFailureSurfaces(t *testing.T) {
	db := newInMemoryDB(t)
	repo := database.NewBatchFileOperationRepository(db)
	fs := afero.NewMemMapFs()

	healthy := NewDBRevertLog(repo, nil, "job-w30-healthy-post", fs, template.NewEngine(), nil, nil)
	opID := w30Begin(t, healthy)

	broken := &w30FailNonJournalRepo{BatchFileOperationRepositoryInterface: repo, failErr: errors.New("replay: non-journal persist wedged")}
	rl := w30RevertLog(t, broken, fs)
	before, readErr := repo.FindByID(t.Context(), uint(parseOpIDForTest(t, opID)))
	require.NoError(t, readErr)
	err := rl.CompleteFailed(context.Background(), opID, &ApplyResult{
		Movie:   &models.Movie{ID: "TEST-001"},
		NFOPath: "/dest/TEST-001.nfo",
	})
	require.ErrorIs(t, err, broken.failErr)
	after, readErr := repo.FindByID(t.Context(), before.ID)
	require.NoError(t, readErr)
	require.Equal(t, before, after, "failure must not publish only one half of completion")
}

func parseOpIDForTest(t *testing.T, opID string) uint64 {
	t.Helper()
	var id uint64
	_, err := fmt.Sscanf(opID, "%d", &id)
	require.NoError(t, err)
	return id
}
