package database

import (
	"context"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Constructor coverage (33-67%) ---

func TestNewActressAliasRepository_Constructor(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)
	require.NotNil(t, repo)
	require.NotNil(t, repo.BaseRepository)
}

func TestNewBatchFileOperationRepository_Constructor(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBatchFileOperationRepository(db)
	require.NotNil(t, repo)
	require.NotNil(t, repo.BaseRepository)
}

func TestNewGenreRepository_Constructor(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreRepository(db)
	require.NotNil(t, repo)
	require.NotNil(t, repo.BaseRepository)
}

func TestNewBaseRepository_Constructor(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Genre, uint](
		db, "genre",
		func(g models.Genre) string { return g.Name },
		WithNewEntity[models.Genre, uint](func() models.Genre { return models.Genre{} }),
	)
	require.NotNil(t, br)
	assert.Equal(t, db, br.GetDB())
}

func TestNewBaseRepository_WithDefaultOrder(t *testing.T) {
	db := newDatabaseTestDB(t)
	br := NewBaseRepository[models.Event, uint](
		db, "event",
		func(e models.Event) string { return "test" },
		withDefaultOrder[models.Event, uint]("created_at DESC"),
		WithNewEntity[models.Event, uint](func() models.Event { return models.Event{} }),
	)
	require.NotNil(t, br)
	assert.Equal(t, "created_at DESC", br.defaultOrder)
}

func TestNewMovieRepository_Constructor(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	require.NotNil(t, repo)
	require.NotNil(t, repo.BaseRepository)
}

func TestNewEventRepository_Constructor(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewEventRepository(db)
	require.NotNil(t, repo)
	require.NotNil(t, repo.BaseRepository)
}

func TestNewHistoryRepository_Constructor(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)
	require.NotNil(t, repo)
	require.NotNil(t, repo.BaseRepository)
}

func TestNewJobRepository_Constructor(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)
	require.NotNil(t, repo)
	require.NotNil(t, repo.BaseRepository)
}

func TestNewWordReplacementRepository_Constructor(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)
	require.NotNil(t, repo)
	require.NotNil(t, repo.BaseRepository)
}

// --- Close (75%) ---

func TestDB_Close_DoubleClose(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)

	// First close should succeed
	require.NoError(t, db.Close())

	// Second close: database/sql returns nil for a double-close of sql.DB,
	// but the underlying driver may error. Either nil or error is acceptable;
	// we just verify no panic occurs.
	_ = db.Close()
}

// --- UpsertTx concurrent duplicate-key (50%) ---

// --- UpsertTx: existing record update path (the non-not-found branch) ---

func TestActressTranslationRepository_UpsertTx_ExistingRecord(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newActressTranslationRepository(db)

	actress := &models.Actress{DMMID: 77702, JapaneseName: "更新女優"}
	require.NoError(t, db.Create(actress).Error)

	// Create initial translation
	tx := db.Begin()
	translation := &models.ActressTranslation{
		ActressID:    actress.ID,
		Language:     "en",
		FirstName:    "Original",
		LastName:     "Name",
		JapaneseName: "更新女優",
	}
	require.NoError(t, repo.UpsertTx(tx, translation))
	require.NoError(t, tx.Commit().Error)

	// Upsert with updated values (should take the existing-record update path)
	tx = db.Begin()
	updated := &models.ActressTranslation{
		ActressID:    actress.ID,
		Language:     "en",
		FirstName:    "Updated",
		LastName:     "Name",
		JapaneseName: "更新女優",
	}
	require.NoError(t, repo.UpsertTx(tx, updated))
	require.NoError(t, tx.Commit().Error)

	// Verify update
	found, err := repo.FindByActressAndLanguage(context.Background(), actress.ID, "en")
	require.NoError(t, err)
	assert.Equal(t, "Updated", found.FirstName)
	assert.NotZero(t, found.ID, "ID should be preserved from existing record")
}

func TestGenreTranslationRepository_UpsertTx_ExistingRecord(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := newGenreTranslationRepository(db)

	genre := &models.Genre{Name: "Update Genre"}
	require.NoError(t, db.Create(genre).Error)

	// Create initial
	tx := db.Begin()
	translation := &models.GenreTranslation{
		GenreID:    genre.ID,
		Language:   "en",
		Name:       "Original Name",
		SourceName: "src",
	}
	require.NoError(t, repo.UpsertTx(tx, translation))
	require.NoError(t, tx.Commit().Error)

	// Upsert with updated values
	tx = db.Begin()
	updated := &models.GenreTranslation{
		GenreID:    genre.ID,
		Language:   "en",
		Name:       "Updated Name",
		SourceName: "src2",
	}
	require.NoError(t, repo.UpsertTx(tx, updated))
	require.NoError(t, tx.Commit().Error)

	found, err := repo.FindByGenreAndLanguage(context.Background(), genre.ID, "en")
	require.NoError(t, err)
	assert.Equal(t, "Updated Name", found.Name)
	assert.NotZero(t, found.ID, "ID should be preserved from existing record")
}

// --- Merge (78.8%) ---

func TestActressRepository_Merge_SimpleNoConflicts(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := NewActressRepository(db)

	target := &models.Actress{
		DMMID:        60001,
		FirstName:    "Target",
		LastName:     "Actress",
		JapaneseName: "ターゲット",
	}
	source := &models.Actress{
		DMMID:        60002,
		FirstName:    "Source",
		LastName:     "Actress",
		JapaneseName: "ソース",
		ThumbURL:     "https://example.com/thumb.jpg",
	}
	require.NoError(t, repo.Create(context.TODO(), target))
	require.NoError(t, repo.Create(context.TODO(), source))

	// Merge with source winning thumb_url (only conflict)
	result, err := repo.Merge(context.TODO(), target.ID, source.ID, map[string]string{
		"thumb_url": "source",
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	assert.Equal(t, source.ID, result.MergedFromID)
	assert.GreaterOrEqual(t, result.ConflictsResolved, 1, "at least one conflict should be resolved")

	merged, err := repo.FindByID(context.TODO(), target.ID)
	require.NoError(t, err)
	assert.Equal(t, "https://example.com/thumb.jpg", merged.ThumbURL)

	_, err = repo.FindByID(context.TODO(), source.ID)
	require.Error(t, err, "source should be deleted after merge")
}

func TestActressRepository_Merge_WithAliasConsolidation(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
	repo := NewActressRepository(db)

	target := &models.Actress{
		DMMID:        61001,
		FirstName:    "Tgt",
		LastName:     "A",
		JapaneseName: "目標",
		Aliases:      "TgtAlias",
	}
	source := &models.Actress{
		DMMID:        61002,
		FirstName:    "Src",
		LastName:     "A",
		JapaneseName: "原典",
		Aliases:      "SrcAlias1|SrcAlias2",
	}
	require.NoError(t, repo.Create(context.TODO(), target))
	require.NoError(t, repo.Create(context.TODO(), source))

	result, err := repo.Merge(context.TODO(), target.ID, source.ID, nil)
	require.NoError(t, err)
	require.NotNil(t, result)
	assert.GreaterOrEqual(t, result.AliasesAdded, 2, "source aliases should be added")

	merged, err := repo.FindByID(context.TODO(), target.ID)
	require.NoError(t, err)
	assert.Contains(t, merged.Aliases, "SrcAlias1")
	assert.Contains(t, merged.Aliases, "SrcAlias2")

	// Verify alias records created
	aliasRepo := NewActressAliasRepository(db)
	aliases, err := aliasRepo.FindByCanonicalName(context.TODO(), "目標")
	require.NoError(t, err)
	aliasNames := make(map[string]bool)
	for _, a := range aliases {
		aliasNames[a.AliasName] = true
	}
	assert.True(t, aliasNames["SrcAlias1"], "SrcAlias1 should be in alias records")
	assert.True(t, aliasNames["SrcAlias2"], "SrcAlias2 should be in alias records")
}

// --- ensureGenresExistTx (73.9%) ---

func TestMovieRepository_EnsureGenresExistTx_NewGenres(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	genres := []models.Genre{
		{Name: "Action"},
		{Name: "Drama"},
		{Name: "Comedy"},
	}

	err := repo.upserter.ensureGenresExistTx(db.DB, genres)
	require.NoError(t, err)

	// All genres should have IDs assigned
	for i, g := range genres {
		assert.NotZero(t, g.ID, "genre %q should have an ID after ensureGenresExistTx", genres[i].Name)
	}

	// Verify genres exist in DB
	genreRepo := newGenreRepository(db)
	for _, g := range genres {
		found, err := genreRepo.FindOrCreate(context.Background(), g.Name)
		require.NoError(t, err)
		assert.Equal(t, g.Name, found.Name)
	}
}

func TestMovieRepository_EnsureGenresExistTx_ExistingGenresNotDuplicated(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)
	genreRepo := newGenreRepository(db)

	// Pre-create genres
	existing1, err := genreRepo.FindOrCreate(context.Background(), "Horror")
	require.NoError(t, err)
	existing2, err := genreRepo.FindOrCreate(context.Background(), "Sci-Fi")
	require.NoError(t, err)

	// Call ensureGenresExistTx with the same names
	genres := []models.Genre{
		{Name: "Horror"},
		{Name: "Sci-Fi"},
		{Name: "Thriller"}, // new genre
	}
	err = repo.upserter.ensureGenresExistTx(db.DB, genres)
	require.NoError(t, err)

	// Existing genres should reuse IDs
	assert.Equal(t, existing1.ID, genres[0].ID, "existing genre Horror should reuse ID")
	assert.Equal(t, existing2.ID, genres[1].ID, "existing genre Sci-Fi should reuse ID")
	// New genre should get a new ID
	assert.NotZero(t, genres[2].ID, "new genre Thriller should get an ID")

	// Verify no duplicate genres in DB
	allGenres, err := genreRepo.List(context.Background())
	require.NoError(t, err)
	names := make(map[string]int)
	for _, g := range allGenres {
		names[g.Name]++
	}
	for name, count := range names {
		assert.Equal(t, 1, count, "genre %q should appear exactly once", name)
	}
}

func TestMovieRepository_EnsureGenresExistTx_EmptySlice(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	err := repo.upserter.ensureGenresExistTx(db.DB, []models.Genre{})
	require.NoError(t, err)
}

// --- SeedDefaultWordReplacements (75%) ---

func TestSeedDefaultWordReplacements_Boost(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	// Seed defaults
	SeedDefaultWordReplacements(context.Background(), repo)

	// Verify at least some defaults were seeded
	replacements, err := repo.List(context.Background())
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(replacements), 10, "should seed default word replacements")

	// Verify a known default exists
	_, err = repo.FindByOriginal(context.Background(), "R**e")
	require.NoError(t, err, "R**e should be seeded as a default replacement")
}

func TestSeedDefaultWordReplacements_Idempotent_Boost(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewWordReplacementRepository(db)

	// Seed twice
	SeedDefaultWordReplacements(context.Background(), repo)
	SeedDefaultWordReplacements(context.Background(), repo)

	// Should not create duplicates
	replacements, err := repo.List(context.Background())
	require.NoError(t, err)
	origCount := make(map[string]int)
	for _, r := range replacements {
		origCount[r.Original]++
	}
	for orig, count := range origCount {
		assert.Equal(t, 1, count, "original %q should appear exactly once after double seed", orig)
	}
}

func TestIsDefaultWordReplacement_Boost(t *testing.T) {
	assert.True(t, IsDefaultWordReplacement("R**e"), "R**e is a default")
	assert.True(t, IsDefaultWordReplacement("F***"), "F*** is a default")
	assert.False(t, IsDefaultWordReplacement("NotADefault"), "non-default should return false")
}

// --- Lock/Unlock (66.7%) ---

func TestProcessMigrationLocker_LockUnlock(t *testing.T) {
	locker := processMigrationLocker{}

	// Lock should succeed
	require.NoError(t, locker.Lock(context.Background(), nil))

	// Unlock should succeed
	require.NoError(t, locker.Unlock(context.Background(), nil))
}

func TestProcessMigrationLocker_LockIsBlocking(t *testing.T) {
	locker := processMigrationLocker{}

	require.NoError(t, locker.Lock(context.Background(), nil))

	// A second lock attempt should block since processMigrationLocker uses sync.Mutex
	// We test this with a goroutine + channel to avoid deadlocking the test
	locked := make(chan struct{})
	go func() {
		_ = locker.Lock(context.Background(), nil)
		close(locked)
	}()

	// Should NOT be locked yet because the mutex is held
	select {
	case <-locked:
		t.Fatal("second lock should block while first is held")
	case <-time.After(50 * time.Millisecond):
		// Expected: still blocked
	}

	// Unlock should unblock the second goroutine
	require.NoError(t, locker.Unlock(context.Background(), nil))

	select {
	case <-locked:
		// Second lock acquired successfully after unlock
	case <-time.After(2 * time.Second):
		t.Fatal("second lock should have been acquired after unlock")
	}

	// Clean up: unlock the second lock
	require.NoError(t, locker.Unlock(context.Background(), nil))
}

// --- RunMigrationsOnStartup (70.2%) ---

func TestRunMigrationsOnStartup_NilContext(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// nil context should be handled gracefully (replaced with context.Background internally)
	require.NoError(t, db.RunMigrationsOnStartup(nil))
}

func TestRunMigrationsOnStartup_Idempotent(t *testing.T) {
	cfg := &Config{Type: "sqlite", DSN: ":memory:", LogLevel: "error"}
	db, err := New(cfg)
	require.NoError(t, err)
	defer func() { _ = db.Close() }()

	// First run
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))

	// Second run should be safe (no pending migrations)
	require.NoError(t, db.RunMigrationsOnStartup(context.Background()))
}
