package worker

import (
	"context"
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestLegacyFamilySaveRendersDisplayTitle(t *testing.T) {
	for _, withRepository := range []bool{false, true} {
		t.Run(map[bool]string{false: "in memory", true: "persisted"}[withRepository], func(t *testing.T) {
			const path = "synthetic.mp4"
			store := resultstore.New(1, []string{path})
			seedFamilyResult(store, path, "title-result", "TITLE-005", "title005")
			var repo database.MovieRepositoryInterface
			if withRepository {
				db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
				require.NoError(t, err)
				t.Cleanup(func() { _ = db.Close() })
				require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
				repo = database.NewMovieRepository(db)
			}
			pe := NewPosterEditor(store, store, repo)
			movie := &models.Movie{ID: "TITLE-005", ContentID: "title005", Title: "After", DisplayTitle: "Stale"}
			_, _, err := pe.UpdateMovieFamilyWithEcho(t.Context(), movie.ID, "title-result", movie, FamilySaveOptions{
				RenderDisplayTitle: func(_ context.Context, loaded *models.Movie) string { return loaded.ID + " " + loaded.Title },
			})
			require.NoError(t, err)
			published, _, found := store.GetFileResultByResultID("title-result")
			require.True(t, found)
			require.Equal(t, "TITLE-005 After", published.Movie.DisplayTitle)
			if withRepository {
				stored, err := repo.FindByID(t.Context(), movie.ID)
				require.NoError(t, err)
				require.Equal(t, published.Movie.DisplayTitle, stored.DisplayTitle)
			}
		})
	}
}

func TestEditCommitterRendersCreditDisplayTitleBeforeEnvelope(t *testing.T) {
	for _, failEnvelope := range []bool{false, true} {
		t.Run(map[bool]string{false: "commit", true: "rollback"}[failEnvelope], func(t *testing.T) {
			db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
			require.NoError(t, err)
			t.Cleanup(func() { _ = db.Close() })
			require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
			repo := database.NewMovieRepository(db)
			canonical := models.Actress{FirstName: "Canonical", Verified: true, Origin: "user"}
			require.NoError(t, db.Create(&canonical).Error)
			original, err := repo.Upsert(t.Context(), &models.Movie{ID: "TITLE-006", Title: "Before", DisplayTitle: "Before derived", Actresses: []models.Actress{canonical}})
			require.NoError(t, err)
			require.NoError(t, database.NewMovieCreditRepository(db).UpdateOverride(t.Context(), original.Credits[0].ID, "Chosen", true))
			candidate := original.Clone()
			candidate.Title = "After"
			candidate.Credits = nil // ordinary API edit discards read-only credits
			published := false
			envelopeErr := errors.New("injected envelope failure")
			err = NewEditCommitter(db, nil, "", nil).Commit(t.Context(), &EditCommitPlan{
				UpsertMovie: candidate,
				RenderDisplayTitle: func(_ context.Context, loaded *models.Movie) string {
					require.Len(t, loaded.Credits, 1)
					require.NotNil(t, loaded.Credits[0].Actress)
					return loaded.Credits[0].RenderName(true, "Canonical") + " - " + loaded.Title
				},
				EnvelopeFn: func() (*models.Job, error) {
					require.Equal(t, "Chosen - After", candidate.DisplayTitle)
					if failEnvelope {
						return nil, envelopeErr
					}
					return nil, nil
				},
				Publish: func() error { published = true; return nil },
			})
			stored, readErr := repo.FindByID(t.Context(), original.ID)
			require.NoError(t, readErr)
			if failEnvelope {
				require.ErrorIs(t, err, envelopeErr)
				require.False(t, published)
				require.Equal(t, original.Title, stored.Title)
				require.Equal(t, original.DisplayTitle, stored.DisplayTitle)
			} else {
				require.NoError(t, err)
				require.True(t, published)
				require.Equal(t, candidate.DisplayTitle, stored.DisplayTitle)
				require.Equal(t, candidate.RenderGeneration, stored.RenderGeneration)
			}
		})
	}
}
