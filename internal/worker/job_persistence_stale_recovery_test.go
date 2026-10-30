package worker

import (
	"bytes"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestDBJobPersistenceUpsertRejectsStaleEnvelopeAndRecovers(t *testing.T) {
	db := newHistoryTestDB(t)
	repo := database.NewJobRepository(db)
	initialPlan := `{"phase":"initial"}`
	job := models.Job{
		ID: "persistence-stale-envelope", Status: models.JobStatusRunning,
		TotalFiles: 2, StartedAt: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC),
		ApplyPlan: &initialPlan, Files: `["initial.mp4"]`, Results: `{}`,
		Excluded: `{}`, FileMatchInfo: `{}`,
	}
	require.NoError(t, repo.Create(t.Context(), &job))
	stale := job
	accepted := job
	acceptedPlan := `{"phase":"accepted"}`
	accepted.ApplyPlan = &acceptedPlan
	accepted.Files = `["accepted.mp4"]`
	accepted.Results = `{"accepted.mp4":{"status":"completed"}}`
	accepted.Excluded = `{"excluded.mp4":true}`
	accepted.FileMatchInfo = `{"accepted.mp4":{"movie_id":"ACCEPTED-1"}}`
	generation, err := repo.CommitEnvelope(t.Context(), &accepted, job.EnvelopeGeneration)
	require.NoError(t, err)
	require.Equal(t, job.EnvelopeGeneration+1, generation)
	snapshot := func() []models.Job {
		t.Helper()
		var rows []models.Job
		require.NoError(t, db.WithContext(t.Context()).Order("id ASC").Find(&rows).Error)
		return rows
	}
	before := snapshot()
	require.Len(t, before, 1)
	stale.Status = models.JobStatusFailed
	stale.Completed = 99
	stale.Results = `{"stale":"must not land"}`
	callerBefore := stale
	var logs bytes.Buffer
	t.Cleanup(logging.SetOutput(&logs))

	// This tests the exported persistence API, not manager wiring. Its real
	// repository must retain generation fencing; no fake repository is used.
	persistence := NewDBJobPersistence(repo)
	err = persistence.UpsertJob(&stale)
	require.ErrorIs(t, err, database.ErrStaleEnvelopeGeneration)
	require.EqualError(t, err, "upsert job persistence-stale-envelope: stale envelope generation")
	require.Contains(t, logs.String(), "level=warning")
	require.Contains(t, logs.String(), "Failed to upsert job persistence-stale-envelope in database: "+err.Error())
	require.Equal(t, callerBefore, stale)
	require.Equal(t, before, snapshot(), "rejected metadata and envelope must not change any durable field")

	fresh, err := repo.FindByID(t.Context(), job.ID)
	require.NoError(t, err)
	fresh.Completed = 1
	fresh.Progress = 50
	// Even a current-generation metadata save does not own envelope columns.
	// The supported wrapper delegates to the concrete repository's fences.
	unownedPlan := `{"phase":"must-not-replace"}`
	fresh.ApplyPlan = &unownedPlan
	fresh.Files = `[]`
	fresh.Results = `{}`
	fresh.Excluded = `{}`
	fresh.FileMatchInfo = `{}`
	want := before[0]
	want.Completed = 1
	want.Progress = 50
	want.EnvelopeGeneration++
	want.PruneVersion++
	logs.Reset()
	require.NoError(t, persistence.UpsertJob(fresh))
	require.Equal(t, want, *fresh, "caller receives the accepted durable envelope and new fences")
	require.Equal(t, []models.Job{want}, snapshot())
	require.Empty(t, logs.String(), "successful retry must not repeat the failure warning")
}
