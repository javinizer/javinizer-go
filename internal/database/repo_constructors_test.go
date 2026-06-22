package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestNewGenreRepository(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := newGenreRepository(db)
	assert.NotNil(t, repo)

	// Verify the repository works
	genre, err := repo.FindOrCreate(context.TODO(), "TestGenre")
	require.NoError(t, err)
	assert.Equal(t, "TestGenre", genre.Name)
}

func TestNewBatchFileOperationRepository(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewBatchFileOperationRepository(db)
	assert.NotNil(t, repo)

	// Verify the repository works
	op := &models.BatchFileOperation{
		BatchJobID:    "test-batch",
		MovieID:       "TEST-001",
		OriginalPath:  "/source/test.mp4",
		OperationType: models.OperationTypeMove,
		RevertStatus:  models.RevertStatusApplied,
	}
	require.NoError(t, repo.Create(context.TODO(), op))
	assert.NotZero(t, op.ID)
}

func TestNewActressAliasRepository_Integration(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewActressAliasRepository(db)
	assert.NotNil(t, repo)

	// Test full workflow
	alias := &models.ActressAlias{AliasName: "TestAlias", CanonicalName: "TestCanonical"}
	require.NoError(t, repo.Create(context.TODO(), alias))

	found, err := repo.FindByAliasName(context.TODO(), "TestAlias")
	require.NoError(t, err)
	assert.Equal(t, "TestCanonical", found.CanonicalName)
}

func TestActressAliasRepository_Delete(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewActressAliasRepository(db)

	alias := &models.ActressAlias{AliasName: "ToDelete", CanonicalName: "Canonical"}
	require.NoError(t, repo.Create(context.TODO(), alias))

	require.NoError(t, repo.Delete(context.TODO(), "ToDelete"))
	_, err = repo.FindByAliasName(context.TODO(), "ToDelete")
	assert.Error(t, err)
}

func TestActressAliasRepository_FindByCanonicalName(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewActressAliasRepository(db)

	aliases := []*models.ActressAlias{
		{AliasName: "Alias1", CanonicalName: "Canonical1"},
		{AliasName: "Alias2", CanonicalName: "Canonical1"},
	}
	for _, a := range aliases {
		require.NoError(t, repo.Create(context.TODO(), a))
	}

	found, err := repo.FindByCanonicalName(context.TODO(), "Canonical1")
	require.NoError(t, err)
	assert.Len(t, found, 2)
}

func TestActressAliasRepository_GetAliasMap(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewActressAliasRepository(db)

	require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{AliasName: "A1", CanonicalName: "C1"}))
	require.NoError(t, repo.Create(context.TODO(), &models.ActressAlias{AliasName: "A2", CanonicalName: "C2"}))

	aliasMap, err := repo.GetAliasMap(context.TODO())
	require.NoError(t, err)
	assert.Equal(t, "C1", aliasMap["A1"])
	assert.Equal(t, "C2", aliasMap["A2"])
}

func TestActressTranslationRepository_Integration(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	// Create actress first
	actressRepo := NewActressRepository(db)
	actress := &models.Actress{JapaneseName: "TestActress"}
	require.NoError(t, actressRepo.Create(context.TODO(), actress))

	// Test via movie repo UpsertWithTranslations
	movieRepo := NewMovieRepository(db)
	movie := &models.Movie{ID: "AT-TEST-001", Title: "Actress Translation Test"}
	actressTranslations := []models.ActressTranslationData{
		{ActressIndex: 0, Language: "en", FirstName: "Test", LastName: "Translated"},
	}
	movie.Actresses = []models.Actress{*actress}
	_, err = movieRepo.UpsertWithTranslations(context.TODO(), movie, nil, actressTranslations)
	require.NoError(t, err)
}

func TestGenreTranslationRepository_Integration(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	// Test genre translation via movie repo
	movieRepo := NewMovieRepository(db)

	movie := &models.Movie{
		ID:     "GT-TEST-001",
		Title:  "Genre Translation Test",
		Genres: []models.Genre{{Name: "Action"}},
	}
	genreTranslations := []models.GenreTranslationData{
		{GenreIndex: 0, Language: "en", Name: "Action Movie", SourceName: "Action"},
	}
	created, err := movieRepo.UpsertWithTranslations(context.TODO(), movie, genreTranslations, nil)
	require.NoError(t, err)
	assert.NotNil(t, created)
}

func TestEventRepository_CreateAndFind(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewEventRepository(db)
	assert.NotNil(t, repo)

	// Create event
	e := &models.Event{
		EventType: models.EventCategoryScraper,
		Severity:  models.SeverityInfo,
		Source:    "test",
		Message:   "test event",
	}
	require.NoError(t, repo.Create(context.TODO(), e))
	assert.NotZero(t, e.ID)
}

func TestBaseRepository_NewWithEntity(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	// Test that NewBaseRepository with WithNewEntity option works
	repo := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)
	assert.NotNil(t, repo)
}

func TestJobRepository_CreateAndFind(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewJobRepository(db)
	assert.NotNil(t, repo)

	// Create job with explicit ID
	jobID := "test-job-constructors-1"
	job := &models.Job{
		ID:          jobID,
		Status:      models.JobStatusPending,
		Destination: "/test",
	}
	require.NoError(t, repo.Create(context.TODO(), job))

	// Find by ID
	found, err := repo.FindByID(context.TODO(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusPending, found.Status)
}

func TestJobRepository_Update(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewJobRepository(db)

	jobID := "test-job-constructors-2"
	job := &models.Job{ID: jobID, Status: models.JobStatusPending, Destination: "/test"}
	require.NoError(t, repo.Create(context.TODO(), job))

	job.Status = models.JobStatusCompleted
	require.NoError(t, repo.Update(context.TODO(), job))

	found, err := repo.FindByID(context.TODO(), job.ID)
	require.NoError(t, err)
	assert.Equal(t, models.JobStatusCompleted, found.Status)
}

func TestHistoryRepository_CreateAndFind(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewHistoryRepository(db)
	assert.NotNil(t, repo)

	h := &models.History{
		MovieID:      "TEST-001",
		Operation:    models.HistoryOpScrape,
		OriginalPath: "/src/test.mp4",
		NewPath:      "/dst/test.mp4",
		Status:       models.HistoryStatusSuccess,
	}
	require.NoError(t, repo.Create(context.TODO(), h))
	assert.NotZero(t, h.ID)
}

func TestHistoryRepository_ListByMovieID(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewHistoryRepository(db)

	repo.Create(context.TODO(), &models.History{MovieID: "ABC-001", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess})
	repo.Create(context.TODO(), &models.History{MovieID: "ABC-001", Operation: models.HistoryOpOrganize, Status: models.HistoryStatusSuccess})
	repo.Create(context.TODO(), &models.History{MovieID: "DEF-002", Operation: models.HistoryOpScrape, Status: models.HistoryStatusSuccess})

	results, err := repo.List(context.TODO(), 100, 0)
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 3)
}

func TestWordReplacementRepository_CreateAndFind(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewWordReplacementRepository(db)
	assert.NotNil(t, repo)

	wr := &models.WordReplacement{
		Original:    "TestWord",
		Replacement: "ReplacedWord",
	}
	require.NoError(t, repo.Create(context.TODO(), wr))
	assert.NotZero(t, wr.ID)

	found, err := repo.FindByOriginal(context.TODO(), "TestWord")
	require.NoError(t, err)
	assert.Equal(t, "ReplacedWord", found.Replacement)
}

func TestWordReplacementRepository_GetReplacementMap_Constructors(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	repo := NewWordReplacementRepository(db)

	repo.Upsert(context.TODO(), &models.WordReplacement{Original: "CW1", Replacement: "CR1"})
	repo.Upsert(context.TODO(), &models.WordReplacement{Original: "CW2", Replacement: "CR2"})

	replMap, err := repo.GetReplacementMap(context.TODO())
	require.NoError(t, err)
	assert.Equal(t, "CR1", replMap["CW1"])
	assert.Equal(t, "CR2", replMap["CW2"])
}

func TestIsRecordNotFound(t *testing.T) {
	assert.True(t, IsNotFound(gorm.ErrRecordNotFound))
	assert.False(t, IsNotFound(nil))
}

// TestWrapDBErr is already tested in helpers_test.go
