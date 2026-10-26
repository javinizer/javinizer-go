package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// No-op operation IDs do not authorize reads, journal writes, or backup cleanup.
// Embedding the real interface allows every unexpected repository call to fail
// immediately instead of silently returning a fabricated successful record.
type completionReadCounter struct {
	database.BatchFileOperationRepositoryInterface
	reads int
}

func (r *completionReadCounter) FindByID(ctx context.Context, id uint) (*models.BatchFileOperation, error) {
	r.reads++
	return r.BatchFileOperationRepositoryInterface.FindByID(ctx, id)
}

func TestRevertCompletionWithoutDatabaseIDDoesNotAccessJournal(t *testing.T) {
	for _, id := range []OperationID{"", "dry-run", "0"} {
		t.Run(string(id), func(t *testing.T) {
			repo := &completionReadCounter{}
			fs := afero.NewMemMapFs()
			const backup = "/synthetic/poster.backup"
			require.NoError(t, afero.WriteFile(fs, backup, []byte("retained backup"), 0o600))
			log := NewDBRevertLog(repo, NewRevertLogConfig(true, nil), "absence-job", fs, nil, nil, nil)
			require.NoError(t, log.Complete(t.Context(), id, &ApplyResult{PublicationFinalized: true}))
			require.NoError(t, log.CompleteFailed(t.Context(), id, &ApplyResult{}))
			require.Zero(t, repo.reads)
			contents, err := afero.ReadFile(fs, backup)
			require.NoError(t, err)
			require.Equal(t, "retained backup", string(contents))
		})
	}
}

func TestRevertCompletionMissingRecordReturnsNotFoundWithoutMutation(t *testing.T) {
	log, _ := newTestDBRevertLog(t)
	repo := log.(*dbRevertLog).repo
	id, err := log.Begin(t.Context(), ApplyCmd{Movie: &models.Movie{ID: "PRESERVE-001"}, Match: models.FileMatchInfo{Path: "/synthetic/PRESERVE-001.mp4"}})
	require.NoError(t, err)
	before := w15Row(t, repo, id)
	for _, fail := range []bool{false, true} {
		if fail {
			err = log.CompleteFailed(t.Context(), "999999", &ApplyResult{})
		} else {
			err = log.Complete(t.Context(), "999999", &ApplyResult{PublicationFinalized: true})
		}
		require.ErrorIs(t, err, database.ErrNotFound)
		require.Equal(t, before, w15Row(t, repo, id), "absent operation must not alter another operation's journal or timestamps")
	}
}
