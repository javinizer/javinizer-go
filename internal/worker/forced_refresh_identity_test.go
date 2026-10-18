package worker

import (
	"context"
	"errors"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestForcedRefreshConflictDoesNotSurviveDeferredJobPersistence(t *testing.T) {
	ctx := context.Background()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "job.db"), LogLevel: "silent"})
	require.NoError(t, err)
	defer db.Close()
	require.NoError(t, db.RunMigrationsOnStartup(ctx))
	repo := database.NewMovieRepository(db)
	old, err := repo.Upsert(ctx, &models.Movie{ContentID: "old001", ID: "TEST-001", Title: "Saved title"})
	require.NoError(t, err)
	rejected := makeScrapeResult("TEST-001")
	rejected.Movie.ContentID = "other001"
	rejected.Movie.RequireExactContentID = true
	inputs := makeInputs(&stubWorkflow{scrapeResult: rejected})
	inputs.MovieRepo = repo
	NewScrapePhase().Run(ctx, inputs, []string{"fixture.mp4"}, ScrapePhaseConfig{Force: true})
	result := inputs.Updater.(*stubUpdater).getResult("fixture.mp4")
	require.NotNil(t, result)
	assert.Equal(t, models.JobStatusFailed, result.Status)
	assert.Contains(t, result.Error, database.ErrMovieContentIDConflict.Error())
	assert.Nil(t, result.Movie, "a rejected identity must not become editable review input")
	assert.False(t, result.Persisted)
	encoded, err := jobpersist.Encode(jobpersist.Snapshot{ID: "refresh-conflict", Results: map[string]*resultstore.MovieResult{"fixture.mp4": result}})
	require.NoError(t, err)
	decoded, decodeErrors := jobpersist.Decode(encoded)
	require.Empty(t, decodeErrors)
	require.Contains(t, decoded.Results, "fixture.mp4")
	assert.Nil(t, decoded.Results["fixture.mp4"].Movie, "restart must not resurrect the rejected candidate")
	assert.Equal(t, models.JobStatusFailed, decoded.Results["fixture.mp4"].Status)
	after, err := repo.FindByID(ctx, "TEST-001")
	require.NoError(t, err)
	assert.Equal(t, old, after)
	var count int64
	require.NoError(t, db.Model(&models.Movie{}).Count(&count).Error)
	assert.EqualValues(t, 1, count)
}

// The candidate remains private even when persistence never starts or cannot
// finish. Only an accepted DB result may enter the serializable review store.
func TestForcedRefreshCandidatePublishedOnlyAfterPersistence(t *testing.T) {
	for _, missingContent := range []bool{false, true} {
		t.Run(map[bool]string{false: "explicit_content", true: "missing_content"}[missingContent], func(t *testing.T) {
			for _, mode := range []string{"cancel_before_persist", "cancel_during_persist", "query_error", "persist_panic", "success", "ordinary_failure", "no_repository"} {
				t.Run(mode, func(t *testing.T) {
					ctx, cancel := context.WithCancel(context.Background())
					defer cancel()
					db, err := database.New(&database.Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "job.db"), LogLevel: "silent"})
					require.NoError(t, err)
					defer db.Close()
					require.NoError(t, db.RunMigrationsOnStartup(ctx))
					repo := database.NewMovieRepository(db)
					old, err := repo.Upsert(ctx, &models.Movie{ContentID: "old001", ID: "TEST-001", Title: "Saved title"})
					require.NoError(t, err)
					candidate := makeScrapeResult("TEST-001")
					candidate.Movie.ContentID = old.ContentID
					candidate.Movie.Title = "New title"
					candidate.Movie.RequireExactContentID = mode != "ordinary_failure" && !missingContent
					if missingContent {
						candidate.Movie.ContentID = ""
					}
					inputs := makeInputs(&stubWorkflow{scrapeResult: candidate})
					inputs.MovieRepo = repo
					if mode == "no_repository" {
						inputs.MovieRepo = nil
					}
					cfg := ScrapePhaseConfig{Force: mode != "ordinary_failure", OnFileScraped: func(string, string, string) {
						current := inputs.Updater.(*stubUpdater).getResult("fixture.mp4")
						if mode != "ordinary_failure" && mode != "no_repository" {
							assert.Nil(t, current.Movie, "unvalidated candidate cannot enter an active envelope")
							encoded, err := jobpersist.Encode(jobpersist.Snapshot{ID: "active-refresh", Results: map[string]*resultstore.MovieResult{"fixture.mp4": current}})
							require.NoError(t, err)
							decoded, errs := jobpersist.Decode(encoded)
							require.Empty(t, errs)
							assert.Nil(t, decoded.Results["fixture.mp4"].Movie)
						}
						if mode == "cancel_before_persist" {
							cancel()
						}
					}}
					if mode == "query_error" || mode == "persist_panic" || mode == "cancel_during_persist" || mode == "ordinary_failure" {
						require.NoError(t, db.Callback().Query().Before("gorm:query").Register("fail_persist", func(tx *gorm.DB) {
							switch mode {
							case "persist_panic":
								panic("synthetic persist panic")
							case "cancel_during_persist":
								cancel()
								tx.AddError(context.Canceled)
							default:
								tx.AddError(errors.New("synthetic query failure"))
							}
						}))
					}
					NewScrapePhase().Run(ctx, inputs, []string{"fixture.mp4"}, cfg)
					require.NoError(t, db.Callback().Query().Remove("fail_persist"))
					result := inputs.Updater.(*stubUpdater).getResult("fixture.mp4")
					require.NotNil(t, result)
					if mode == "success" {
						require.NotNil(t, result.Movie)
						assert.True(t, result.Persisted)
						assert.False(t, result.Movie.RequireExactContentID)
						assert.Equal(t, "New title", result.Movie.Title)
					} else if mode == "ordinary_failure" || mode == "no_repository" {
						require.NotNil(t, result.Movie, "ordinary failed scrape review semantics are unchanged")
					} else {
						assert.Nil(t, result.Movie)
						assert.False(t, result.Persisted)
						encoded, err := jobpersist.Encode(jobpersist.Snapshot{ID: "refresh", Results: map[string]*resultstore.MovieResult{"fixture.mp4": result}})
						require.NoError(t, err)
						decoded, errs := jobpersist.Decode(encoded)
						require.Empty(t, errs)
						assert.Nil(t, decoded.Results["fixture.mp4"].Movie)
					}
					if mode != "success" {
						after, err := repo.FindByID(context.Background(), old.ID)
						require.NoError(t, err)
						assert.Equal(t, old, after)
					}
				})
			}
		})
	}
}
