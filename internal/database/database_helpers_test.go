package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func newDatabaseTestDB(t *testing.T) *DB {
	t.Helper()

	cfg := &config.Config{
		Database: config.DatabaseConfig{
			Type: "sqlite",
			DSN:  ":memory:",
		},
		Logging: config.LoggingConfig{
			Level: "error",
		},
	}

	db, err := New(cfg)
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.AutoMigrate())
	return db
}

func TestNormalizeActressSortAndOrderClauses(t *testing.T) {
	tests := []struct {
		name        string
		sortBy      string
		sortOrder   string
		wantSortBy  string
		wantOrder   string
		wantErr     bool
		wantClauses []string
	}{
		{
			name:        "invalid sort returns error",
			sortBy:      "unknown",
			sortOrder:   "desc",
			wantSortBy:  "",
			wantOrder:   "",
			wantErr:     true,
			wantClauses: nil,
		},
		{
			name:        "id sort",
			sortBy:      "id",
			sortOrder:   "desc",
			wantSortBy:  "id",
			wantOrder:   "desc",
			wantClauses: []string{"id desc"},
		},
		{
			name:        "dmm id sort",
			sortBy:      "dmm_id",
			sortOrder:   "asc",
			wantSortBy:  "dmm_id",
			wantOrder:   "asc",
			wantClauses: []string{"dmm_id asc", "id asc"},
		},
		{
			name:        "japanese name sort",
			sortBy:      "japanese_name",
			sortOrder:   "desc",
			wantSortBy:  "japanese_name",
			wantOrder:   "desc",
			wantClauses: []string{"japanese_name desc", "id desc"},
		},
		{
			name:        "first name sort",
			sortBy:      "first_name",
			sortOrder:   "desc",
			wantSortBy:  "first_name",
			wantOrder:   "desc",
			wantClauses: []string{"first_name desc", "last_name desc", "id desc"},
		},
		{
			name:        "last name sort",
			sortBy:      "last_name",
			sortOrder:   "asc",
			wantSortBy:  "last_name",
			wantOrder:   "asc",
			wantClauses: []string{"last_name asc", "first_name asc", "id asc"},
		},
		{
			name:        "created at sort",
			sortBy:      "created_at",
			sortOrder:   "desc",
			wantSortBy:  "created_at",
			wantOrder:   "desc",
			wantClauses: []string{"created_at desc", "id desc"},
		},
		{
			name:        "updated at sort",
			sortBy:      "updated_at",
			sortOrder:   "asc",
			wantSortBy:  "updated_at",
			wantOrder:   "asc",
			wantClauses: []string{"updated_at asc", "id asc"},
		},
		{
			name:        "name alias normalizes order to asc",
			sortBy:      "name",
			sortOrder:   "invalid",
			wantSortBy:  "name",
			wantOrder:   "asc",
			wantClauses: []string{"last_name asc", "first_name asc", "japanese_name asc", "id asc"},
		},
		{
			name:        "empty sort defaults to name asc",
			sortBy:      "",
			sortOrder:   "",
			wantSortBy:  "name",
			wantOrder:   "asc",
			wantClauses: []string{"last_name asc", "first_name asc", "japanese_name asc", "id asc"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gotSortBy, gotOrder, err := normalizeActressSort(tt.sortBy, tt.sortOrder)
			if tt.wantErr {
				assert.Error(t, err)
				return
			}
			assert.NoError(t, err)
			assert.Equal(t, tt.wantSortBy, gotSortBy)
			assert.Equal(t, tt.wantOrder, gotOrder)
			assert.Equal(t, tt.wantClauses, actressOrderClauses(gotSortBy, gotOrder))
		})
	}
}

func TestCanonicalActressName(t *testing.T) {
	tests := []struct {
		name    string
		actress *models.Actress
		want    string
	}{
		{
			name: "prefer japanese name",
			actress: &models.Actress{
				JapaneseName: "  波多野結衣 ",
				FirstName:    "Yui",
				LastName:     "Hatano",
			},
			want: "波多野結衣",
		},
		{
			name: "use full name",
			actress: &models.Actress{
				FirstName: "Yui",
				LastName:  "Hatano",
			},
			want: "Hatano Yui",
		},
		{
			name: "fallback first name",
			actress: &models.Actress{
				FirstName: "  Yui  ",
			},
			want: "Yui",
		},
		{
			name: "fallback last name",
			actress: &models.Actress{
				LastName: "  Hatano ",
			},
			want: "Hatano",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.want, canonicalActressName(tt.actress))
		})
	}
}

func TestFilterIdentifiableActresses(t *testing.T) {
	assert.Nil(t, filterIdentifiableActresses(nil))

	input := []models.Actress{
		{},
		{DMMID: 123},
		{JapaneseName: "  女優A  "},
		{FirstName: "A"},
		{LastName: "B"},
		{FirstName: " ", LastName: " "},
	}

	got := filterIdentifiableActresses(input)
	require.Len(t, got, 4)
	assert.Equal(t, 123, got[0].DMMID)
	assert.Equal(t, "  女優A  ", got[1].JapaneseName)
	assert.Equal(t, "A", got[2].FirstName)
	assert.Equal(t, "B", got[3].LastName)
}

func TestMovieRepositoryEnsureGenresExistTx(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existing := models.Genre{Name: "Drama"}
	require.NoError(t, db.DB.Create(&existing).Error)

	genres := []models.Genre{
		{Name: "Drama"},
		{Name: "Comedy"},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.ensureGenresExistTx(tx, genres)
	})
	require.NoError(t, err)
	assert.Equal(t, existing.ID, genres[0].ID)
	assert.NotZero(t, genres[1].ID)

	var count int64
	require.NoError(t, db.DB.Model(&models.Genre{}).Count(&count).Error)
	assert.Equal(t, int64(2), count)
}

func TestMovieRepositoryEnsureActressesExistTx(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewMovieRepository(db)

	existingByDMM := models.Actress{DMMID: 101, JapaneseName: "DMM", FirstName: "A", LastName: "B"}
	existingByJP := models.Actress{JapaneseName: "JP-ONLY", ThumbURL: ""}
	existingByBoth := models.Actress{FirstName: "First", LastName: "Last"}
	existingByFirst := models.Actress{FirstName: "FirstOnly"}
	existingByLast := models.Actress{LastName: "LastOnly"}
	require.NoError(t, db.DB.Create(&existingByDMM).Error)
	require.NoError(t, db.DB.Create(&existingByJP).Error)
	require.NoError(t, db.DB.Create(&existingByBoth).Error)
	require.NoError(t, db.DB.Create(&existingByFirst).Error)
	require.NoError(t, db.DB.Create(&existingByLast).Error)

	actresses := []models.Actress{
		{DMMID: 101, ThumbURL: "https://example.com/a.jpg"},
		{JapaneseName: "JP-ONLY", ThumbURL: "https://example.com/jp.jpg"},
		{FirstName: "First", LastName: "Last", ThumbURL: "https://example.com/both.jpg"},
		{FirstName: "FirstOnly", ThumbURL: "https://example.com/first.jpg"},
		{LastName: "LastOnly", ThumbURL: "https://example.com/last.jpg"},
		{DMMID: 202, JapaneseName: "Created"},
		{},
	}

	err := db.Transaction(func(tx *gorm.DB) error {
		return repo.ensureActressesExistTx(tx, actresses)
	})
	require.NoError(t, err)

	assert.Equal(t, existingByDMM.ID, actresses[0].ID)
	assert.Equal(t, existingByJP.ID, actresses[1].ID)
	assert.Equal(t, existingByBoth.ID, actresses[2].ID)
	assert.Equal(t, existingByFirst.ID, actresses[3].ID)
	assert.Equal(t, existingByLast.ID, actresses[4].ID)
	assert.NotZero(t, actresses[5].ID)
	assert.Zero(t, actresses[6].ID)

	var updatedJP models.Actress
	require.NoError(t, db.DB.First(&updatedJP, existingByJP.ID).Error)
	assert.Equal(t, "https://example.com/jp.jpg", updatedJP.ThumbURL)
}

func TestUpsertActressAliases(t *testing.T) {
	db := newDatabaseTestDB(t)

	require.NoError(t, upsertActressAliases(db.DB, []string{"Alias"}, ""))

	existing := models.ActressAlias{
		AliasName:     "AliasA",
		CanonicalName: "Old",
	}
	require.NoError(t, db.DB.Create(&existing).Error)

	aliases := []string{"AliasA", "aliasa", "AliasB", "  AliasC  ", "", "Main"}
	require.NoError(t, upsertActressAliases(db.DB, aliases, "Main"))

	var gotA models.ActressAlias
	require.NoError(t, db.DB.First(&gotA, "alias_name = ?", "AliasA").Error)
	assert.Equal(t, "Main", gotA.CanonicalName)

	var count int64
	require.NoError(t, db.DB.Model(&models.ActressAlias{}).Count(&count).Error)
	assert.Equal(t, int64(3), count)
}

func TestMoveMovieAssociations(t *testing.T) {
	db := newDatabaseTestDB(t)
	movieRepo := NewMovieRepository(db)
	actressRepo := NewActressRepository(db)

	target := &models.Actress{DMMID: 5001, JapaneseName: "Target"}
	source := &models.Actress{DMMID: 5002, JapaneseName: "Source"}
	other := &models.Actress{DMMID: 5003, JapaneseName: "Other"}
	require.NoError(t, actressRepo.Create(target))
	require.NoError(t, actressRepo.Create(source))
	require.NoError(t, actressRepo.Create(other))

	movie1 := createTestMovie("MV-ASSOC-001")
	movie1.Actresses = []models.Actress{*source}
	_, err := movieRepo.Upsert(movie1)

	movie2 := createTestMovie("MV-ASSOC-002")
	movie2.Actresses = []models.Actress{*source, *target}
	_, err = movieRepo.Upsert(movie2)

	movie3 := createTestMovie("MV-ASSOC-003")
	movie3.Actresses = []models.Actress{*other}
	_, err = movieRepo.Upsert(movie3)

	updated, err := moveMovieAssociations(db.DB, source.ID, target.ID)
	require.NoError(t, err)
	assert.Equal(t, 2, updated)

	found1, err := movieRepo.FindByID("MV-ASSOC-001")
	require.NoError(t, err)
	require.Len(t, found1.Actresses, 1)
	assert.Equal(t, target.ID, found1.Actresses[0].ID)

	found2, err := movieRepo.FindByID("MV-ASSOC-002")
	require.NoError(t, err)
	require.Len(t, found2.Actresses, 1)
	assert.Equal(t, target.ID, found2.Actresses[0].ID)

	found3, err := movieRepo.FindByID("MV-ASSOC-003")
	require.NoError(t, err)
	require.Len(t, found3.Actresses, 1)
	assert.Equal(t, other.ID, found3.Actresses[0].ID)
}

func TestActressRepositoryLoadPair(t *testing.T) {
	db := newDatabaseTestDB(t)
	repo := NewActressRepository(db)

	_, _, err := repo.loadPair(0, 1)
	assert.ErrorIs(t, err, ErrActressMergeInvalidID)

	_, _, err = repo.loadPair(1, 1)
	assert.ErrorIs(t, err, ErrActressMergeSameID)

	target := &models.Actress{DMMID: 7001, JapaneseName: "Target"}
	require.NoError(t, repo.Create(target))

	_, _, err = repo.loadPair(target.ID, 99999)
	require.Error(t, err)

	source := &models.Actress{DMMID: 7002, JapaneseName: "Source"}
	require.NoError(t, repo.Create(source))

	gotTarget, gotSource, err := repo.loadPair(target.ID, source.ID)
	require.NoError(t, err)
	assert.Equal(t, target.ID, gotTarget.ID)
	assert.Equal(t, source.ID, gotSource.ID)
}
