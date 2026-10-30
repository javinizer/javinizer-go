package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestMovieTagAddNormalizationFailureRollsBackAndCanRetry(t *testing.T) {
	db := newDatabaseTestDB(t)
	movie := tagRenderMovie(t, db, "normalization-rollback")
	repo := NewMovieTagRepository(db)

	// Normalization first removes a duplicate legacy row, then moves the
	// remaining legacy-only row. Reject the latter after the deletion has run.
	seed := []models.MovieTag{
		{MovieID: movie.ContentID, Tag: "shared"},
		{MovieID: movie.ID, Tag: "shared"},
		{MovieID: movie.ID, Tag: "legacy-only"},
	}
	require.NoError(t, db.Create(&seed).Error)
	var before []models.MovieTag
	require.NoError(t, db.Order("id ASC").Find(&before).Error)
	var movieBefore models.Movie
	require.NoError(t, db.First(&movieBefore, "content_id = ?", movie.ContentID).Error)
	require.NoError(t, db.Exec(`CREATE TRIGGER reject_tag_normalization_update
		BEFORE UPDATE OF movie_id ON movie_tags
		WHEN NOT EXISTS (SELECT 1 FROM movie_tags
			WHERE movie_id = 'DISPLAY-normalization-rollback' AND tag = 'shared')
		BEGIN SELECT RAISE(ABORT, 'owned normalization update rejected'); END`).Error)

	err := repo.AddTag(t.Context(), movie.ID, "new")
	require.ErrorContains(t, err, "normalize movie tag key "+movie.ID)
	require.ErrorContains(t, err, "owned normalization update rejected")

	// Check physical rows, not only GetTagsForMovie: its canonicalized view
	// would hide the loss of the duplicate legacy row if rollback were broken.
	var after []models.MovieTag
	require.NoError(t, db.Order("id ASC").Find(&after).Error)
	require.Equal(t, before, after, "restore deleted legacy duplicate and do not add the requested tag")
	var movieAfter models.Movie
	require.NoError(t, db.First(&movieAfter, "content_id = ?", movie.ContentID).Error)
	require.Equal(t, movieBefore, movieAfter, "failed normalization must not change movie or render state")

	// Removing only the owned fault permits the same public operation to
	// complete, canonicalizing old rows and invalidating rendering once.
	require.NoError(t, db.Exec("DROP TRIGGER reject_tag_normalization_update").Error)
	require.NoError(t, repo.AddTag(t.Context(), movie.ID, "new"))
	var retried []models.MovieTag
	require.NoError(t, db.Order("tag ASC").Find(&retried).Error)
	require.Len(t, retried, 3)
	for _, tag := range retried {
		require.Equal(t, movie.ContentID, tag.MovieID)
	}
	require.Equal(t, "legacy-only", retried[0].Tag)
	require.Equal(t, seed[2].ID, retried[0].ID, "legacy-only row is moved, not recreated")
	require.Equal(t, "new", retried[1].Tag)
	require.Equal(t, "shared", retried[2].Tag)
	require.Equal(t, seed[0].ID, retried[2].ID, "canonical duplicate survives normalization")
	tags, err := repo.GetTagsForMovie(t.Context(), movie.ID)
	require.NoError(t, err)
	require.Equal(t, []string{"legacy-only", "new", "shared"}, tags)
	dirty, generation := tagRenderState(t, db, movie.ContentID)
	require.True(t, dirty)
	require.Equal(t, movieBefore.RenderGeneration+1, generation)
}
