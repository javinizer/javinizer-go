package history

import (
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"os"
	"testing"
	"time"
)

func TestPublicationPendingRetainsReplacementAndHistory(t *testing.T) {
	for _, scenario := range []string{"indexed", "fresh-absent", "fresh-present"} {
		t.Run(scenario, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			repo := newP3OpRepo()
			dest := "/out/held/poster.jpg"
			backup := dest + ".dlbak." + p3HexB
			require.NoError(t, fs.MkdirAll("/out/held", 0755))
			writeSweepFile(t, fs, backup, "pre-publication", time.Hour)
			row := journalRow(t, repo, "pending", "held", dest, backup, 1, models.RevertStatusApplied)
			if scenario == "fresh-present" {
				require.NoError(t, afero.WriteFile(fs, dest, []byte("retained destination"), 0644))
				ledger, e := models.ParseGeneratedFiles(row.GeneratedFiles)
				require.NoError(t, e)
				ledger.Replacements[0].RestorePending = true
				row.GeneratedFiles = models.MarshalLedgerJSON(ledger)
				require.NoError(t, repo.Update(t.Context(), row))
			}
			// The index snapshot may predate the guard, so fresh reads also refuse.
			require.NoError(t, repo.UpdateJournalInTx(t.Context(), row.ID, func(current *models.BatchFileOperation) (models.GeneratedFilesJSON, bool, error) {
				ledger, err := models.ParseGeneratedFiles(current.GeneratedFiles)
				ledger.PublicationPending = true
				return ledger, true, err
			}))
			if scenario == "indexed" {
				var e error
				row, e = repo.FindByID(t.Context(), row.ID)
				require.NoError(t, e)
			}
			sweeper := NewReplacementSweeper(fs, repo)
			require.False(t, sweeper.restoreAndConsume(t.Context(), row, backup, dest, sweepSlash(backup), nil))
			if scenario == "fresh-present" {
				body, e := afero.ReadFile(fs, dest)
				require.NoError(t, e)
				require.Equal(t, "retained destination", string(body))
			} else {
				_, statErr := fs.Stat(dest)
				require.ErrorIs(t, statErr, os.ErrNotExist)
			}
			body, err := afero.ReadFile(fs, backup)
			require.NoError(t, err)
			require.Equal(t, "pre-publication", string(body))
			fresh, err := repo.FindByID(t.Context(), row.ID)
			require.NoError(t, err)
			require.True(t, models.PrimaryUndoNeedsRetention(*fresh))
			require.Error(t, sweeper.PruneOperationBackups(t.Context(), []models.BatchFileOperation{*fresh}))
		})
	}
}
