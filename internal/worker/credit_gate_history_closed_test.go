package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func TestHistoricalCreditGateFailedJobsRequireExplicitRecovery(t *testing.T) {
	// These three rows were exported by the actual unpatched startup normalizer.
	// They are interrupted jobs despite looking like terminal credit-gate failures.
	raw, err := os.ReadFile(filepath.Join("testdata", "old-normalized-credit-jobs.json"))
	require.NoError(t, err)
	var rows []*models.Job
	require.NoError(t, json.Unmarshal(raw, &rows))
	require.Len(t, rows, 3)
	// A genuine old gate-only failure is indistinguishable: it must also stay
	// closed until a separately reviewed explicit recovery operation exists.
	now := time.Now()
	path := "/synthetic/SYNTH-001.mkv"
	historical, err := jobpersist.Encode(jobpersist.Snapshot{ID: "genuine-historical-gate", Status: models.JobStatusFailed, TotalFiles: 1, Files: []string{path}, CompletedAt: &now, Failed: 1, Results: map[string]*resultstore.MovieResult{path: {FileMatchInfo: models.FileMatchInfo{Path: path, MovieID: "SYNTH-001"}, Movie: &models.Movie{ID: "SYNTH-001", ContentID: "synthetic-001"}, Status: models.JobStatusFailed, ErrorCode: "blocked", Error: "apply blocked by an open credit collision", EndedAt: &now}}})
	require.NoError(t, err)
	rows = append(rows, historical)
	for _, row := range rows {
		t.Run(row.ID, func(t *testing.T) {
			root := t.TempDir()
			db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(root, "db.sqlite"), LogLevel: "silent"})
			require.NoError(t, err)
			defer db.Close()
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			repo := database.NewJobRepository(db)
			require.NoError(t, repo.Upsert(t.Context(), row))
			for restart := 0; restart < 3; restart++ {
				store := NewJobStore(repo, nil, nil, "", nil, afero.NewMemMapFs())
				job, ok := store.GetJobForControl(row.ID)
				require.True(t, ok)
				wf := &stubApplyWorkflow{applyResult: &workflow.ApplyResult{Movie: &models.Movie{ID: "SYNTH-001", ContentID: "synthetic-001"}}}
				job.SetWorkflow(wf)
				require.Equal(t, models.JobStatusFailed, job.GetJobStatus())
				err = job.StartApply(t.Context(), ApplyPhaseConfig{RetryFilePaths: []string{path}})
				if err == nil {
					require.NoError(t, job.Wait())
				}
				require.ErrorContains(t, err, "expected status completed")
				require.Zero(t, wf.getApplyCalled())
			}
			saved, err := repo.FindByID(t.Context(), row.ID)
			require.NoError(t, err)
			require.Equal(t, models.JobStatusFailed, saved.Status)
			require.Equal(t, row.Results, saved.Results, "matching diagnostics must not cause historical envelope rewrites")
		})
	}
}
