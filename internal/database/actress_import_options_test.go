package database

import (
	"errors"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestImportOverwriteNamesPolicy(t *testing.T) {
	for _, origin := range []string{ActressOriginImport, ActressOriginUser} {
		for _, overwrite := range []bool{false, true} {
			t.Run(origin+map[bool]string{false: " default", true: " overwrite"}[overwrite], func(t *testing.T) {
				db := newCreditTestDB(t)
				repo := NewActressRepository(db)
				a := models.Actress{DMMID: 700, FirstName: "Old", LastName: "Family", JapaneseName: "旧名", ThumbURL: "keep.jpg", Origin: origin, Verified: true}
				require.NoError(t, db.Create(&a).Error)
				in := models.Actress{ID: a.ID, DMMID: a.DMMID, FirstName: " New ", JapaneseName: "新名", ThumbURL: "replace.jpg"}
				result, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: overwrite})
				require.NoError(t, err)
				if overwrite && origin == ActressOriginImport {
					require.Equal(t, "updated", result.Outcome)
					require.Equal(t, "New", in.FirstName)
					require.Equal(t, "新名", in.JapaneseName)
				} else {
					require.Equal(t, "protected", result.Outcome)
					require.Equal(t, "Old", in.FirstName)
					require.Equal(t, "旧名", in.JapaneseName)
				}
				require.Equal(t, "Family", in.LastName)
				require.Equal(t, "keep.jpg", in.ThumbURL)
				require.Equal(t, a.ID, in.ID)
				require.Equal(t, a.DMMID, in.DMMID)
				require.Equal(t, origin, in.Origin)
				repeat := in
				result, err = repo.ImportUpsertWithOptions(t.Context(), &repeat, ActressImportOptions{OverwriteNames: overwrite})
				require.NoError(t, err)
				require.Equal(t, "unchanged", result.Outcome)
			})
		}
	}
}

func TestImportOverwriteLifecycle(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	a := models.Actress{DMMID: 701, FirstName: "Old", LastName: "Family", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&a).Error)
	tr := models.ActressTranslation{ActressID: a.ID, Language: "en", DisplayName: "Translated"}
	require.NoError(t, db.Create(&tr).Error)
	for _, tc := range []struct {
		id, source string
		override   bool
	}{{"snapshot", "", false}, {"evidence", "fixture", false}, {"manual", "", true}} {
		require.NoError(t, db.Create(&models.Movie{ContentID: tc.id, ID: tc.id, RenderGeneration: 4}).Error)
		require.NoError(t, db.Create(&models.MovieCredit{MovieContentID: tc.id, ActressID: a.ID, CreditedName: "Family Old", Origin: string(models.CreditOriginUser), Source: tc.source, UserOverride: tc.override}).Error)
	}
	in := models.Actress{DMMID: a.DMMID, FirstName: "New"}
	result, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, "updated", result.Outcome)
	var count int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Count(&count).Error)
	require.Zero(t, count)
	alias, err := NewActressAliasRepository(db).FindByAliasName(t.Context(), "Family Old")
	require.NoError(t, err)
	require.Equal(t, "Family New", alias.CanonicalName)
	for _, id := range []string{"snapshot", "evidence", "manual"} {
		var c models.MovieCredit
		require.NoError(t, db.First(&c, "movie_content_id = ?", id).Error)
		if id == "snapshot" {
			require.Equal(t, "Family New", c.CreditedName)
		} else {
			require.Equal(t, "Family Old", c.CreditedName)
		}
		require.Equal(t, a.ID, c.ActressID)
		var m models.Movie
		require.NoError(t, db.First(&m, "content_id = ?", id).Error)
		require.EqualValues(t, 5, m.RenderGeneration)
	}
	result, err = repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, "unchanged", result.Outcome)
	var m models.Movie
	require.NoError(t, db.First(&m, "content_id = ?", "snapshot").Error)
	require.EqualValues(t, 5, m.RenderGeneration)
}

func TestImportOverwriteConflictsAndRollback(t *testing.T) {
	for _, tc := range []string{"unknown local", "contradictory IDs", "claimed DMM", "alias failure"} {
		t.Run(tc, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			a := models.Actress{DMMID: 702, FirstName: "Old", Origin: ActressOriginImport, Verified: true}
			require.NoError(t, db.Create(&a).Error)
			in := models.Actress{ID: a.ID, FirstName: "New"}
			switch tc {
			case "unknown local":
				in.ID = 999
			case "contradictory IDs":
				in.DMMID = 703
			case "claimed DMM":
				require.NoError(t, db.Model(&a).Update("dmm_id", 0).Error)
				require.NoError(t, db.Create(&models.Actress{DMMID: 703, FirstName: "Other", Verified: true, Origin: ActressOriginImport}).Error)
				in.DMMID = 703
			case "alias failure":
				require.NoError(t, db.Exec("CREATE TRIGGER fail_import_option_alias BEFORE INSERT ON actress_aliases BEGIN SELECT RAISE(ABORT, 'injected alias failure'); END").Error)
			}
			before := in
			result, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
			require.Error(t, err)
			require.Equal(t, before, in, "failed import must not publish uncommitted result")
			if tc != "alias failure" {
				require.ErrorIs(t, err, ErrActressImportIdentityConflict)
				require.Equal(t, "conflicted", result.Outcome)
			}
			require.NoError(t, db.First(&a, a.ID).Error)
			require.Equal(t, "Old", a.FirstName)
		})
	}
}

func TestImportOverwriteRechecksConcurrentManualOwnership(t *testing.T) {
	db, err := New(&Config{Type: "sqlite", DSN: filepath.Join(t.TempDir(), "concurrent.db"), LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { _ = db.Close() })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	a := models.Actress{DMMID: 704, FirstName: "Old", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&a).Error)
	sqlDB, err := db.DB.DB()
	require.NoError(t, err)
	var once sync.Once
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:manual_import_owner", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "actresses" {
			once.Do(func() {
				_, err := sqlDB.ExecContext(t.Context(), "UPDATE actresses SET first_name = ?, origin = ? WHERE id = ?", "Manual", ActressOriginUser, a.ID)
				require.NoError(t, err)
			})
		}
	}))
	in := models.Actress{ID: a.ID, DMMID: a.DMMID, FirstName: "Imported"}
	result, err := NewActressRepository(db).ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, "protected", result.Outcome)
	require.Equal(t, "Manual", in.FirstName)
	require.Equal(t, ActressOriginUser, in.Origin)
}

func TestImportOptionsValidationAndSparseFields(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	for _, in := range []*models.Actress{nil, {}, {DMMID: -1, FirstName: "Negative"}} {
		_, err := repo.ImportUpsertWithOptions(t.Context(), in, ActressImportOptions{OverwriteNames: true})
		require.Error(t, err)
	}
	a := models.Actress{FirstName: "Before", LastName: "Keep", JapaneseName: "保持", ThumbURL: "keep.jpg", Origin: ActressOriginImport}
	require.NoError(t, db.Create(&a).Error)
	in := models.Actress{ID: a.ID, DMMID: 9876, FirstName: "After"}
	result, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, "updated", result.Outcome)
	require.Zero(t, in.DMMID)
	require.Equal(t, "Keep", in.LastName)
	require.Equal(t, "保持", in.JapaneseName)
	require.Equal(t, "keep.jpg", in.ThumbURL)
}

func TestImportOptionsDMMOwnershipQueryFailure(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	a := models.Actress{FirstName: "Before", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&a).Error)
	injectDatabaseCallbackError(t, db, "query", "actresses", 2)
	in := models.Actress{ID: a.ID, DMMID: 9876, FirstName: "After"}
	_, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.ErrorContains(t, err, "injected database error")
}

func TestImportOverwriteDoesNotTrustOrStealAliases(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	owner := models.Actress{DMMID: 705, FirstName: "Owner", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&owner).Error)
	a := models.Actress{DMMID: 706, FirstName: "Old", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&a).Error)
	alias := models.ActressAlias{AliasName: "Old", CanonicalName: "Owner"}
	require.NoError(t, db.Create(&alias).Error)
	in := models.Actress{ID: a.ID, FirstName: "New", Aliases: "Owner"}
	result, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, "updated", result.Outcome)
	require.NoError(t, db.First(&alias, alias.ID).Error)
	require.Equal(t, "Owner", alias.CanonicalName)
	aliasOnly := models.Actress{FirstName: "Alias-only", Aliases: "Owner|New"}
	result, err = repo.ImportUpsertWithOptions(t.Context(), &aliasOnly, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, "created", result.Outcome)
	require.NotEqual(t, owner.ID, aliasOnly.ID)
	require.NotEqual(t, a.ID, aliasOnly.ID)
}

func TestImportOverwriteQuarantineFailureRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	a := models.Actress{DMMID: 707, FirstName: "Old", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&a).Error)
	require.NoError(t, db.Callback().Query().After("gorm:query").Register("test:import_quarantine_failure", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == "actresses" && strings.Contains(tx.Statement.SQL.String(), "WHERE verified = ? ORDER BY id") {
			_ = tx.AddError(errors.New("quarantine unavailable"))
		}
	}))
	in := models.Actress{ID: a.ID, FirstName: "New"}
	_, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.ErrorContains(t, err, "quarantine unavailable")
	require.NoError(t, db.First(&a, a.ID).Error)
	require.Equal(t, "Old", a.FirstName)
	var count int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Count(&count).Error)
	require.Zero(t, count)
}

func TestImportOverwriteReconcilesCollisions(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	a := models.Actress{DMMID: 708, FirstName: "Old", JapaneseName: "旧名", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&a).Error)
	movie, err := NewMovieRepository(db).Upsert(t.Context(), creditMovie("overwrite-collision", []models.MovieCredit{{CreditedName: "新名", Source: "fixture", Scraped: models.Actress{DMMID: a.DMMID, JapaneseName: "新名"}}}))
	require.NoError(t, err)
	collisions, err := NewCreditCollisionRepository(db).ListOpenByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.NotEmpty(t, collisions)
	in := models.Actress{DMMID: a.DMMID, JapaneseName: "新名"}
	result, err := repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	require.Equal(t, "updated", result.Outcome)
	collisions, err = NewCreditCollisionRepository(db).ListOpenByMovie(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Empty(t, collisions)
	after, err := NewMovieRepository(db).FindByContentID(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.Equal(t, "新名", after.Credits[0].CreditedName)
	require.Equal(t, a.ID, after.Credits[0].ActressID)
	require.Equal(t, movie.RenderGeneration+1, after.RenderGeneration)
}

func TestImportOverwriteRecomputesCandidateQuarantine(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	a := models.Actress{FirstName: "Canonical", JapaneseName: "旧名", Origin: ActressOriginImport, Verified: true}
	require.NoError(t, db.Create(&a).Error)
	movie, err := NewMovieRepository(db).Upsert(t.Context(), creditMovie("overwrite-quarantine", []models.MovieCredit{{CreditedName: "新名", Source: "fixture", Scraped: models.Actress{DMMID: 709, JapaneseName: "新名"}}}))
	require.NoError(t, err)
	require.False(t, movie.Credits[0].Actress.AmbiguityQuarantined)
	in := models.Actress{ID: a.ID, JapaneseName: "新名"}
	_, err = repo.ImportUpsertWithOptions(t.Context(), &in, ActressImportOptions{OverwriteNames: true})
	require.NoError(t, err)
	after, err := NewMovieRepository(db).FindByContentID(t.Context(), movie.ContentID)
	require.NoError(t, err)
	require.True(t, after.Credits[0].Actress.AmbiguityQuarantined)
	require.Equal(t, movie.Credits[0].ActressID, after.Credits[0].ActressID)
	require.False(t, after.Credits[0].Actress.Verified)
	require.Equal(t, movie.RenderGeneration+1, after.RenderGeneration)
}
