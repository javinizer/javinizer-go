package database

import (
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func tagRenderMovie(t *testing.T, db *DB, contentID string) models.Movie {
	t.Helper()
	movie := models.Movie{ContentID: contentID, ID: "DISPLAY-" + contentID}
	require.NoError(t, db.Create(&movie).Error)
	return movie
}

func tagRenderState(t *testing.T, db *DB, contentID string) (bool, int64) {
	t.Helper()
	var movie models.Movie
	require.NoError(t, db.Select("render_dirty", "render_generation").First(&movie, "content_id = ?", contentID).Error)
	return movie.RenderDirty, movie.RenderGeneration
}

func TestMovieTagMutationsInvalidateRenderExactlyOnce(t *testing.T) {
	db := newDatabaseTestDB(t)
	movie := tagRenderMovie(t, db, "tag-render")
	repo := NewMovieTagRepository(db)

	require.NoError(t, repo.AddTag(t.Context(), movie.ContentID, "one"))
	dirty, generation := tagRenderState(t, db, movie.ContentID)
	require.True(t, dirty)
	require.EqualValues(t, 1, generation)

	require.Error(t, repo.AddTag(t.Context(), movie.ContentID, "one"))
	dirty, generation = tagRenderState(t, db, movie.ContentID)
	require.True(t, dirty)
	require.EqualValues(t, 1, generation)

	require.NoError(t, repo.RemoveTag(t.Context(), movie.ContentID, "missing"))
	_, generation = tagRenderState(t, db, movie.ContentID)
	require.EqualValues(t, 1, generation)

	require.NoError(t, repo.RemoveTag(t.Context(), movie.ContentID, "one"))
	_, generation = tagRenderState(t, db, movie.ContentID)
	require.EqualValues(t, 2, generation)

	require.NoError(t, repo.AddTag(t.Context(), movie.ContentID, "two"))
	require.NoError(t, repo.AddTag(t.Context(), movie.ContentID, "three"))
	_, generation = tagRenderState(t, db, movie.ContentID)
	require.EqualValues(t, 4, generation)

	require.NoError(t, repo.RemoveAllTags(t.Context(), movie.ContentID))
	_, generation = tagRenderState(t, db, movie.ContentID)
	require.EqualValues(t, 5, generation)

	require.NoError(t, repo.RemoveAllTags(t.Context(), movie.ContentID))
	_, generation = tagRenderState(t, db, movie.ContentID)
	require.EqualValues(t, 5, generation)
}

func TestMovieTagMutationInvalidationFailureRollsBack(t *testing.T) {
	for _, mutation := range []struct {
		name string
		seed []string
		run  func(*MovieTagRepository, string) error
		want []string
	}{
		{name: "add", run: func(repo *MovieTagRepository, id string) error { return repo.AddTag(context.Background(), id, "new") }, want: []string{}},
		{name: "remove", seed: []string{"old"}, run: func(repo *MovieTagRepository, id string) error {
			return repo.RemoveTag(context.Background(), id, "old")
		}, want: []string{"old"}},
		{name: "remove all", seed: []string{"one", "two"}, run: func(repo *MovieTagRepository, id string) error { return repo.RemoveAllTags(context.Background(), id) }, want: []string{"one", "two"}},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			db := newDatabaseTestDB(t)
			movie := tagRenderMovie(t, db, "tag-failure-"+mutation.name)
			repo := NewMovieTagRepository(db)
			for _, tag := range mutation.seed {
				require.NoError(t, db.Create(&models.MovieTag{MovieID: movie.ContentID, Tag: tag}).Error)
			}
			require.NoError(t, db.Exec("CREATE TRIGGER fail_tag_render_generation BEFORE UPDATE OF render_generation ON movies BEGIN SELECT RAISE(ABORT, 'generation failure'); END").Error)
			require.Error(t, mutation.run(repo, movie.ContentID))
			tags, err := repo.GetTagsForMovie(t.Context(), movie.ContentID)
			require.NoError(t, err)
			require.Equal(t, mutation.want, tags)
			dirty, generation := tagRenderState(t, db, movie.ContentID)
			require.False(t, dirty)
			require.Zero(t, generation)
		})
	}
}

func TestMovieTagConcurrentMutationsDoNotLoseGeneration(t *testing.T) {
	dsn := filepath.Join(t.TempDir(), "tags.sqlite")
	db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "error"})
	require.NoError(t, err)
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	t.Cleanup(func() { _ = db.Close() })
	movie := tagRenderMovie(t, db, "tag-concurrent")
	repo := NewMovieTagRepository(db)

	const mutations = 8
	start := make(chan struct{})
	errs := make(chan error, mutations)
	var wg sync.WaitGroup
	for i := 0; i < mutations; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			errs <- repo.AddTag(context.Background(), movie.ID, fmt.Sprintf("tag-%d", i))
		}(i)
	}
	close(start)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	_, generation := tagRenderState(t, db, movie.ContentID)
	require.EqualValues(t, mutations, generation)
	tags, err := repo.GetTagsForMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, tags, mutations)
}

func TestMovieTagMutationWithoutMoviePreservesLegacyAssociation(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieTagRepository(db)
	require.NoError(t, repo.AddTag(t.Context(), "missing", "orphan"))
	require.NoError(t, repo.RemoveTag(t.Context(), "missing", "orphan"))
	require.NoError(t, repo.RemoveAllTags(t.Context(), "missing"))
}
