package worker

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func newActressEditTestDB(t *testing.T) *database.DB {
	t.Helper()
	cfg := &database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := database.New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	// A canceled transaction may discard its connection. Keep the shared-memory
	// database alive even when all connections used by the tests are replaced.
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	keepAlive, err := sqlDB.Conn(context.Background())
	require.NoError(t, err)
	t.Cleanup(func() { _ = keepAlive.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	return db
}

func TestActressEditTestDB_SurvivesConnectionReplacement(t *testing.T) {
	db := newActressEditTestDB(t)
	movie := models.Movie{ContentID: "fixture-lifecycle", ID: "FIXTURE-LIFECYCLE", Title: "Retained movie"}
	require.NoError(t, db.Create(&movie).Error)

	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	// Force all pooled connections to close, as can happen after cancellation.
	// The fixture must keep its schema and data when a new connection opens.
	sqlDB.SetMaxIdleConns(0)

	persisted, err := db.Repositories().MovieRepo.FindByID(context.Background(), movie.ID)
	require.NoError(t, err)
	assert.Equal(t, movie.ContentID, persisted.ContentID)
	assert.Equal(t, movie.Title, persisted.Title)
}

func seedCuratedActress(t *testing.T, repo database.ActressRepositoryInterface) models.Actress {
	t.Helper()
	a := &models.Actress{
		DMMID:        12345,
		FirstName:    "Yui",
		LastName:     "Hatano",
		JapaneseName: "波多野結衣",
	}
	require.NoError(t, repo.Create(context.Background(), a))
	require.NotZero(t, a.ID)
	return *a
}

func TestUpdateMovie_ActressRenamesByID(t *testing.T) {
	t.Run("edited existing actress name persists to DB and in-memory (NFO path)", func(t *testing.T) {
		db := newActressEditTestDB(t)
		repos := db.Repositories()
		curated := seedCuratedActress(t, repos.ActressRepo)

		jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Actresses: []models.Actress{curated}},
		})

		ej, ok := jq.GetJobForEdit(job.ID.String())
		require.True(t, ok)

		edited := curated
		edited.FirstName = "Yui-Edited"
		require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
			&models.Movie{ID: "ABC-001", Actresses: []models.Actress{edited}}))

		got, err := repos.ActressRepo.FindByID(context.Background(), curated.ID)
		require.NoError(t, err)
		assert.Equal(t, "Yui-Edited", got.FirstName, "actresses table (Actresses page) must reflect the rename")

		result, _ := job.results.GetMovieResult("file1.mp4")
		require.NotEmpty(t, result.Movie.Actresses)
		assert.Equal(t, "Yui-Edited", result.Movie.Actresses[0].FirstName,
			"in-memory movie (NFO generation) must carry the edited name")
	})

	t.Run("scrape path is untouched: without actressRepo the curated name is preserved", func(t *testing.T) {
		db := newActressEditTestDB(t)
		repos := db.Repositories()
		curated := seedCuratedActress(t, repos.ActressRepo)

		jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil)
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Actresses: []models.Actress{curated}},
		})

		ej, ok := jq.GetJobForEdit(job.ID.String())
		require.True(t, ok)

		edited := curated
		edited.FirstName = "Yui-Edited"
		require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
			&models.Movie{ID: "ABC-001", Actresses: []models.Actress{edited}}))

		got, err := repos.ActressRepo.FindByID(context.Background(), curated.ID)
		require.NoError(t, err)
		assert.Equal(t, "Yui", got.FirstName, "curated actress name must not be clobbered on the scrape/shared path")
	})

	t.Run("in-memory-only path (movieRepo nil) does not mutate the DB", func(t *testing.T) {
		db := newActressEditTestDB(t)
		repos := db.Repositories()
		curated := seedCuratedActress(t, repos.ActressRepo)

		// actressRepo is wired but movieRepo is nil: the in-memory-only edit path
		// must not rename (no DB persistence).
		jq := NewJobStore(nil, nil, nil, "", nil, nil, WithActressRepo(repos.ActressRepo))
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001", Actresses: []models.Actress{curated}},
		})

		ej, ok := jq.GetJobForEdit(job.ID.String())
		require.True(t, ok)

		edited := curated
		edited.FirstName = "Yui-Edited"
		require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
			&models.Movie{ID: "ABC-001", Actresses: []models.Actress{edited}}))

		got, err := repos.ActressRepo.FindByID(context.Background(), curated.ID)
		require.NoError(t, err)
		assert.Equal(t, "Yui", got.FirstName, "in-memory-only path must not mutate the actresses table")
	})

	t.Run("new actress (ID==0) is left to the upserter, not force-renamed", func(t *testing.T) {
		db := newActressEditTestDB(t)
		repos := db.Repositories()

		jq := NewJobStore(nil, nil, repos.MovieRepo, "", nil, nil, WithActressRepo(repos.ActressRepo))
		job := jq.CreateJobBatch([]string{"file1.mp4"})
		job.results.UpdateFileResult("file1.mp4", &resultstore.MovieResult{
			FileMatchInfo: models.FileMatchInfo{Path: "file1.mp4", MovieID: "ABC-001"},
			Status:        models.JobStatusCompleted,
			Movie:         &models.Movie{ID: "ABC-001"},
		})

		ej, ok := jq.GetJobForEdit(job.ID.String())
		require.True(t, ok)

		newActress := models.Actress{FirstName: "Rookie", LastName: "Star", JapaneseName: "新人"}
		require.NoError(t, ej.UpdateMovie(context.Background(), "file1.mp4",
			&models.Movie{ID: "ABC-001", Actresses: []models.Actress{newActress}}))

		found, err := repos.ActressRepo.FindByFirstNameLastName(context.Background(), "Rookie", "Star")
		require.NoError(t, err)
		assert.Equal(t, "Rookie", found.FirstName, "new actress must be created by the upserter, not skipped")
	})
}
