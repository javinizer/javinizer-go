package database

import (
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDelegatedFindByID_ReturnsErrNotFound_ForUintIDRepo(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	_, err := repo.FindByID(99999)
	require.Error(t, err, "FindByID on non-existent record should return error")
	assert.True(t, errors.Is(err, ErrNotFound),
		"delegated FindByID should wrap gorm.ErrRecordNotFound as ErrNotFound, got: %v", err)
}

func TestDelegatedFindByID_ReturnsErrNotFound_ForStringIDRepo(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	_, err := repo.FindByID("nonexistent-job-id")
	require.Error(t, err, "FindByID on non-existent string ID should return error")
	assert.True(t, errors.Is(err, ErrNotFound),
		"delegated FindByID for string ID should wrap gorm.ErrRecordNotFound as ErrNotFound, got: %v", err)
}

func TestDelegatedFindByID_StringIDTypeSwitch_QueriesCorrectly(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	job := &models.Job{ID: "job-string-id-123", Status: "running", Files: "[]"}
	require.NoError(t, repo.Create(job))

	found, err := repo.FindByID("job-string-id-123")
	require.NoError(t, err)
	assert.Equal(t, "job-string-id-123", found.ID,
		"delegated FindByID for string ID should use 'id = ?' clause via type-switch")
}

func TestDelegatedErrorMessage_IncludesEntityName(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	_, err := repo.FindByID(99999)
	require.Error(t, err)

	errMsg := err.Error()
	assert.Contains(t, errMsg, "history",
		"delegated FindByID error should include entity name 'history', got: %s", errMsg)
}

func TestDelegatedErrorMessage_IncludesEntityLabel(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewGenreReplacementRepository(db)

	replacement := &models.GenreReplacement{Original: "BigTits", Replacement: "Large"}
	require.NoError(t, repo.Create(replacement))

	duplicate := &models.GenreReplacement{Original: "BigTits", Replacement: "Other"}
	err := repo.Create(duplicate)
	if err != nil {
		errMsg := err.Error()
		assert.Contains(t, errMsg, "genre replacement",
			"delegated Create error should include entity name 'genre replacement', got: %s", errMsg)
	}
}

func TestDelegatedListAll_GenreRepo(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewGenreRepository(db)

	names := []string{"Action", "Drama", "Comedy"}
	for _, name := range names {
		_, err := repo.FindOrCreate(name)
		require.NoError(t, err)
	}

	genres, err := repo.List()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(genres), 3,
		"delegated ListAll should return all records via ListAll()")
}

func TestDelegatedListAll_GenreReplacementRepo(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewGenreReplacementRepository(db)

	replacements := []*models.GenreReplacement{
		{Original: "Test1", Replacement: "Replaced1"},
		{Original: "Test2", Replacement: "Replaced2"},
	}
	for _, r := range replacements {
		require.NoError(t, repo.Create(r))
	}

	list, err := repo.List()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list), 2,
		"delegated ListAll should return all genre replacements via ListAll()")
}

func TestDelegatedListAll_ActressAliasRepo(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressAliasRepository(db)

	aliases := []*models.ActressAlias{
		{AliasName: "Alias1", CanonicalName: "Canonical1"},
		{AliasName: "Alias2", CanonicalName: "Canonical2"},
	}
	for _, a := range aliases {
		require.NoError(t, repo.Create(a))
	}

	list, err := repo.List()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list), 2,
		"delegated ListAll should return all actress aliases via ListAll()")
}

func TestDelegatedListAll_JobRepo(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	jobs := []*models.Job{
		{ID: "job-list-1", Status: "completed", Files: "[]"},
		{ID: "job-list-2", Status: "running", Files: "[]"},
	}
	for _, j := range jobs {
		require.NoError(t, repo.Create(j))
	}

	list, err := repo.List()
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(list), 2,
		"delegated ListAll should return all jobs via ListAll()")
}

func TestDelegatedGetDB_ReturnsValidDB(t *testing.T) {
	db := newDatabaseTestDB(t)

	repos := []struct {
		name  string
		getDB func() *DB
	}{
		{"HistoryRepository", func() *DB { return NewHistoryRepository(db).GetDB() }},
		{"ActressRepository", func() *DB { return NewActressRepository(db).GetDB() }},
		{"EventRepository", func() *DB { return NewEventRepository(db).GetDB() }},
		{"BatchFileOperationRepository", func() *DB { return NewBatchFileOperationRepository(db).GetDB() }},
		{"GenreRepository", func() *DB { return NewGenreRepository(db).GetDB() }},
		{"JobRepository", func() *DB { return NewJobRepository(db).GetDB() }},
		{"MovieRepository", func() *DB { return NewMovieRepository(db).GetDB() }},
		{"GenreReplacementRepository", func() *DB { return NewGenreReplacementRepository(db).GetDB() }},
		{"ActressAliasRepository", func() *DB { return NewActressAliasRepository(db).GetDB() }},
	}

	for _, tc := range repos {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.getDB()
			assert.NotNil(t, got, "GetDB() should return non-nil DB for %s", tc.name)
		})
	}
}

func TestDelegatedGetDB_RepoSpecificMethod(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	history := &models.History{MovieID: "TEST-GETDB", Operation: "organize", Status: "success"}
	require.NoError(t, repo.Create(history))

	results, err := repo.FindByMovieID("TEST-GETDB")
	require.NoError(t, err)
	assert.GreaterOrEqual(t, len(results), 1,
		"repo-specific method using GetDB() should work correctly")
}

func TestBaseRepositoryList_ZeroLimitReturnsZeroRecords(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewBaseRepository[models.History, uint](
		db, "history",
		func(h models.History) string { return fmt.Sprintf("%d", h.ID) },
		WithDefaultOrder[models.History, uint]("created_at DESC"),
		WithNewEntity[models.History, uint](func() models.History { return models.History{} }),
	)

	for i := 0; i < 3; i++ {
		require.NoError(t, repo.Create(&models.History{
			MovieID: fmt.Sprintf("ZL-%03d", i), Operation: "test", Status: "success",
		}))
	}

	results, err := repo.List(0, 0)
	require.NoError(t, err)
	assert.Len(t, results, 0,
		"List(0,0) with GORM returns 0 records (use ListAll() for all records)")
}

func TestDelegatedDelete_UintIDRepo(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewHistoryRepository(db)

	history := &models.History{MovieID: "DEL-TEST", Operation: "organize", Status: "success"}
	require.NoError(t, repo.Create(history))

	require.NoError(t, repo.Delete(history.ID))

	_, err := repo.FindByID(history.ID)
	assert.True(t, errors.Is(err, ErrNotFound),
		"deleted record should return ErrNotFound via delegated FindByID")
}

func TestDelegatedDelete_StringIDRepo(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewJobRepository(db)

	job := &models.Job{ID: "job-to-delete", Status: "running", Files: "[]"}
	require.NoError(t, repo.Create(job))

	require.NoError(t, repo.Delete("job-to-delete"))

	_, err := repo.FindByID("job-to-delete")
	assert.True(t, errors.Is(err, ErrNotFound),
		"deleted string-ID record should return ErrNotFound via delegated FindByID")
}
