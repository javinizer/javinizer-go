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

func TestPR260FCA01WritebackLostKnownMovieDoesNotPublish(t *testing.T) {
	db := newActressEditTestDB(t)
	repo := db.Repositories().MovieRepo
	ctx := context.Background()
	movie := models.Movie{ContentID: "pr260-fca01-writeback", ID: "PR260-FCA01-WB", RenderGeneration: 5}
	require.NoError(t, db.Create(&movie).Error)
	persisted, err := repo.FindByID(ctx, movie.ID)
	require.NoError(t, err)
	require.NotNil(t, persisted)
	results := map[string]*resultstore.MovieResult{"/incoming/fca01.mp4": {Movie: &movie}}
	known := refreshApplyMovieIdentity(ctx, repo, results)
	require.True(t, known[movie.ContentID], "successful repository refresh claims prior authority")
	require.NoError(t, db.Where("content_id = ?", movie.ContentID).Delete(&models.Movie{}).Error)
	calls := 0
	results["/incoming/fca01.mp4"].Revision = 7
	before := results["/incoming/fca01.mp4"].Revision
	inputs := applyPhaseInputs{MovieRepo: repo}
	afc := &ApplyFileContext{PublicationGeneration: movie.RenderGeneration, PersistedMovie: true}
	err = withApplyPublicationFence(ctx, inputs, &movie, afc, func() error { calls++; results["/incoming/fca01.mp4"].Revision++; return nil })
	require.ErrorIs(t, err, database.ErrNotFound)
	require.Equal(t, before, results["/incoming/fca01.mp4"].Revision, "lost authority must not create a fresh result revision")
	require.True(t, errors.Is(err, errApplyPublicationFence))
	require.Zero(t, calls)
	afc.PersistedMovie = false
	require.NoError(t, withApplyPublicationFence(ctx, inputs, &movie, afc, func() error { calls++; return nil }))
	require.Equal(t, 1, calls, "legitimately unpersisted ContentID keeps legacy behavior")
}
