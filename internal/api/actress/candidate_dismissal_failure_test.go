package actress

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestCandidateDispositionStorageFailurePreservesStateAndRecovers(t *testing.T) {
	for _, action := range []string{"dismiss", "restore"} {
		t.Run(action, func(t *testing.T) {
			db, router := candidateDismissalEnv(t)
			pool, err := db.DB.DB()
			require.NoError(t, err)
			pool.SetMaxOpenConns(1)
			pool.SetMaxIdleConns(1)
			candidate := models.Actress{FirstName: "Candidate", Origin: database.ActressOriginScrape}
			require.NoError(t, db.Create(&candidate).Error)
			repo := database.NewActressRepository(db)
			event := "INSERT"
			row := "NEW"
			if action == "restore" {
				require.NoError(t, repo.SetCandidateDismissed(t.Context(), candidate.ID, true))
				event, row = "DELETE", "OLD"
			}
			snapshot := func() map[string][]map[string]any {
				state := map[string][]map[string]any{}
				for _, table := range []string{"actresses", "actress_candidate_dismissals"} {
					var rows []map[string]any
					require.NoError(t, db.Table(table).Order("rowid").Find(&rows).Error)
					state[table] = rows
				}
				return state
			}
			before := snapshot()
			require.NoError(t, db.Exec(fmt.Sprintf(`CREATE TEMP TRIGGER reject_candidate_disposition BEFORE %s ON actress_candidate_dismissals
WHEN %s.actress_id = %d BEGIN SELECT RAISE(ABORT, 'candidate disposition rejected'); END`, event, row, candidate.ID)).Error)
			t.Cleanup(func() { _ = db.Exec("DROP TRIGGER IF EXISTS reject_candidate_disposition").Error })
			path := "candidates/" + itoa(candidate.ID) + "/" + action
			response := candidateRequest(router, http.MethodPost, path)
			require.Equal(t, http.StatusInternalServerError, response.Code, response.Body.String())
			require.JSONEq(t, `{"error":"candidate disposition rejected"}`, response.Body.String())
			require.Equal(t, before, snapshot(), "failed requests preserve identity and the original queue disposition timestamp")
			require.NoError(t, db.Exec("DROP TRIGGER reject_candidate_disposition").Error)
			response = candidateRequest(router, http.MethodPost, path)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			require.JSONEq(t, `{"ok":true}`, response.Body.String())
			count, err := repo.CountDismissedCandidates(t.Context())
			require.NoError(t, err)
			if action == "dismiss" {
				require.EqualValues(t, 1, count)
			} else {
				require.Zero(t, count)
			}
			var stored models.Actress
			require.NoError(t, db.First(&stored, candidate.ID).Error)
			require.Equal(t, candidate, stored)
		})
	}
}
