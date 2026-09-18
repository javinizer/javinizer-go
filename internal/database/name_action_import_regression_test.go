package database

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestMovieUpsertCanonicalCreditedNameRepresentationsGateOnlyMismatch(t *testing.T) {
	tests := []struct {
		name             string
		actress          models.Actress
		creditedName     string
		creditedJapanese string
		acceptedAlias    string
		aliasCanonical   string
		inlineAlias      string
		wantOpen         bool
	}{
		{name: "last first", actress: models.Actress{DMMID: 8101, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}, creditedName: "Hatano Yui"},
		{name: "first last", actress: models.Actress{DMMID: 8102, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}, creditedName: "Yui Hatano"},
		{name: "japanese", actress: models.Actress{DMMID: 8103, FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣"}, creditedJapanese: "波多野結衣"},
		{name: "single component", actress: models.Actress{DMMID: 8104, FirstName: "Yui"}, creditedName: "Yui"},
		{name: "empty", actress: models.Actress{DMMID: 8105, FirstName: "Yui", LastName: "Hatano"}},
		{name: "normalized whitespace and case", actress: models.Actress{DMMID: 8106, FirstName: "Yui", LastName: "Hatano"}, creditedName: "  yUi   hAtAnO  "},
		{name: "normalized persisted alias", actress: models.Actress{DMMID: 8107, FirstName: "Yui", LastName: "Hatano"}, creditedName: "  stage   NAME  ", acceptedAlias: "Stage Name"},
		{name: "normalized width alias", actress: models.Actress{DMMID: 8108, JapaneseName: "正規名"}, creditedName: "  ＳＴＡＧＥ　ＮＡＭＥ  ", acceptedAlias: "stage name"},
		{name: "normalized composition alias", actress: models.Actress{DMMID: 8112, JapaneseName: "正規名"}, creditedName: "か\u3099", acceptedAlias: "が"},
		{name: "alias owned by another identity", actress: models.Actress{DMMID: 8109, FirstName: "Yui", LastName: "Hatano"}, creditedName: "  shared   name ", acceptedAlias: "Shared Name", aliasCanonical: "Other Person", wantOpen: true},
		{name: "inline alias is not catalog truth", actress: models.Actress{DMMID: 8110, FirstName: "Yui", LastName: "Hatano"}, creditedName: "Stage Name", inlineAlias: "Stage Name", wantOpen: true},
		{name: "true mismatch", actress: models.Actress{DMMID: 8111, FirstName: "Yui", LastName: "Hatano"}, creditedName: "Someone Else", wantOpen: true},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			tc.actress.Verified = true
			tc.actress.Origin = ActressOriginUser
			require.NoError(t, db.Create(&tc.actress).Error)
			if tc.acceptedAlias != "" {
				canonical := tc.aliasCanonical
				if canonical == "" {
					canonical = tc.actress.FullName()
				}
				require.NoError(t, db.Create(&models.ActressAlias{AliasName: tc.acceptedAlias, CanonicalName: canonical}).Error)
			}
			movie := &models.Movie{ContentID: "canonical-gate", ID: "canonical-gate", Credits: []models.MovieCredit{{
				MovieContentID: "canonical-gate", CreditedName: tc.creditedName,
				CreditedJapaneseName: tc.creditedJapanese, Source: "dmm",
				Scraped: models.Actress{DMMID: tc.actress.DMMID, Aliases: tc.inlineAlias},
			}}}
			repo := NewMovieRepository(db)
			_, err := repo.UpsertWithTranslations(context.Background(), movie, nil, nil)
			require.NoError(t, err)
			open, err := NewCreditCollisionRepository(db).ListOpenByMovie(context.Background(), movie.ContentID)
			require.NoError(t, err)
			if tc.wantOpen {
				require.Len(t, open, 1)
				require.Equal(t, models.CreditFieldCreditedName, open[0].Field)
			} else {
				require.Empty(t, open)
			}
		})
	}
}

func TestNormalizedAliasMigrationBackfillsAndSurvivesRestart(t *testing.T) {
	ctx := context.Background()
	dsn := filepath.Join(t.TempDir(), "alias.db")
	open := func() *DB {
		db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
		require.NoError(t, err)
		return db
	}

	legacy := open()
	sqlDB, err := legacy.DB.DB()
	require.NoError(t, err)
	_, err = newMigrationProvider(t, sqlDB).UpTo(ctx, 18)
	require.NoError(t, err)
	require.NoError(t, legacy.Exec(`INSERT INTO actresses (id, dmm_id, first_name, last_name, verified, origin) VALUES (41, 8141, 'Yui', 'Hatano', 1, 'user')`).Error)
	require.NoError(t, legacy.Exec(`INSERT INTO actress_aliases (alias_name, canonical_name) VALUES ('Stage Name', 'Hatano Yui')`).Error)
	require.NoError(t, legacy.Close())

	for index, credited := range []string{"  stage   NAME ", "STAGE NAME"} {
		db := open()
		require.NoError(t, db.RunMigrationsOnStartup(ctx))
		var aliasKey, canonicalKey string
		require.NoError(t, db.Raw("SELECT alias_name_key, canonical_name_key FROM actress_aliases WHERE alias_name = ?", "Stage Name").Row().Scan(&aliasKey, &canonicalKey))
		require.Equal(t, models.NormalizeActressNameKey("Stage Name"), aliasKey)
		require.Equal(t, models.NormalizeActressNameKey("Hatano Yui"), canonicalKey)
		movieID := fmt.Sprintf("alias-restart-%d", index)
		movie := &models.Movie{ContentID: movieID, ID: movieID, Credits: []models.MovieCredit{{
			MovieContentID: movieID, CreditedName: credited, Source: "dmm", Scraped: models.Actress{DMMID: 8141},
		}}}
		_, err := NewMovieRepository(db).UpsertWithTranslations(ctx, movie, nil, nil)
		require.NoError(t, err)
		openRows, err := NewCreditCollisionRepository(db).ListOpenByMovie(ctx, movieID)
		require.NoError(t, err)
		require.Empty(t, openRows)
		require.NoError(t, db.Close())
	}
}

func TestImportUpsertMatchesUnionOfCanonicalRepresentations(t *testing.T) {
	t.Run("changed Japanese with stable English", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		existing := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true, Origin: ActressOriginImport}
		require.NoError(t, db.Create(&existing).Error)
		incoming := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "新表記"}
		require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
		require.Equal(t, existing.ID, incoming.ID)
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
		require.Equal(t, int64(1), count)
	})

	t.Run("reversed English order", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		existing := models.Actress{FirstName: "Yui", LastName: "Hatano", Verified: true, Origin: ActressOriginImport}
		require.NoError(t, db.Create(&existing).Error)
		incoming := models.Actress{FirstName: "Hatano", LastName: "Yui"}
		require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
		require.Equal(t, existing.ID, incoming.ID)
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
		require.Equal(t, int64(1), count)
	})

	t.Run("conflicting representations are ambiguous without mutation", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		japaneseOwner := models.Actress{FirstName: "Other", LastName: "Person", JapaneseName: "競合名", Verified: true, Origin: ActressOriginUser}
		englishOwner := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "別名", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&japaneseOwner).Error)
		require.NoError(t, db.Create(&englishOwner).Error)
		incoming := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "競合名"}
		err := repo.ImportUpsert(context.Background(), &incoming)
		require.ErrorContains(t, err, "ambiguous import match")
		require.Zero(t, incoming.ID)
		var actressCount, aliasCount int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&actressCount).Error)
		require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliasCount).Error)
		require.Equal(t, int64(2), actressCount)
		require.Zero(t, aliasCount)
	})
}

func TestImportUpsertMatchesCandidateUnionWithoutDMM(t *testing.T) {
	t.Run("stable English promotes changed Japanese candidate", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		candidate := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "旧表記", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("旧表記")}
		require.NoError(t, db.Create(&candidate).Error)

		incoming := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "新表記"}
		require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
		require.Equal(t, candidate.ID, incoming.ID)
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
		require.Equal(t, int64(1), count)
	})

	t.Run("reversed English promotes candidate", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		candidate := models.Actress{FirstName: "Yui", LastName: "Hatano", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("Hatano Yui")}
		require.NoError(t, db.Create(&candidate).Error)
		incoming := models.Actress{FirstName: "Hatano", LastName: "Yui"}
		require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
		require.Equal(t, candidate.ID, incoming.ID)
	})

	t.Run("candidate and verified representation conflict is ambiguous", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		candidate := models.Actress{JapaneseName: "競合名", FirstName: "Other", LastName: "Candidate", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("競合名")}
		verified := models.Actress{JapaneseName: "別名", FirstName: "Yui", LastName: "Hatano", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&candidate).Error)
		require.NoError(t, db.Create(&verified).Error)
		incoming := models.Actress{JapaneseName: "競合名", FirstName: "Yui", LastName: "Hatano"}
		err := repo.ImportUpsert(context.Background(), &incoming)
		require.ErrorContains(t, err, "ambiguous import match")
		require.Zero(t, incoming.ID)
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
		require.Equal(t, int64(2), count)
	})
}

func TestImportUpsertCandidateUnionPromotionLifecycle(t *testing.T) {
	t.Run("file restart preserves credit alias projection and gate", func(t *testing.T) {
		ctx := context.Background()
		dsn := filepath.Join(t.TempDir(), "candidate.db")
		open := func() *DB {
			db, err := New(&Config{Type: "sqlite", DSN: dsn, LogLevel: "silent"})
			require.NoError(t, err)
			require.NoError(t, db.RunMigrationsOnStartup(ctx))
			return db
		}
		db := open()
		candidate := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "旧表記", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("旧表記"), AmbiguityQuarantined: true}
		require.NoError(t, db.Create(&candidate).Error)
		movie := models.Movie{ContentID: "candidate-union-restart", ID: "candidate-union-restart"}
		require.NoError(t, db.Create(&movie).Error)
		credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "Hatano Yui", Source: "dmm"}
		require.NoError(t, db.Create(&credit).Error)
		collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, ReportedValue: "Hatano Yui", CanonicalValue: "旧表記", Status: models.CollisionStatusOpen}
		require.NoError(t, db.Create(&collision).Error)
		require.NoError(t, db.Close())

		db = open()
		incoming := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "新表記"}
		require.NoError(t, NewActressRepository(db).ImportUpsert(ctx, &incoming))
		require.Equal(t, candidate.ID, incoming.ID)
		var actressCount int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&actressCount).Error)
		require.Equal(t, int64(1), actressCount)
		var stored models.Actress
		require.NoError(t, db.First(&stored, candidate.ID).Error)
		require.True(t, stored.Verified)
		require.False(t, stored.AmbiguityQuarantined)
		var storedCredit models.MovieCredit
		require.NoError(t, db.First(&storedCredit, credit.ID).Error)
		require.Equal(t, candidate.ID, storedCredit.ActressID)
		var storedCollision models.CreditCollision
		require.NoError(t, db.First(&storedCollision, collision.ID).Error)
		require.Equal(t, models.CollisionStatusResolved, storedCollision.Status)
		openGate, err := NewCreditCollisionRepository(db).HasOpenForMovie(ctx, movie.ContentID)
		require.NoError(t, err)
		require.False(t, openGate)
		var projected []uint
		require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Pluck("actress_id", &projected).Error)
		require.Equal(t, []uint{candidate.ID}, projected)
		var aliases []models.ActressAlias
		require.NoError(t, db.Where("alias_name_key = ?", models.NormalizeActressNameKey("旧表記")).Find(&aliases).Error)
		require.NotEmpty(t, aliases)
		require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
		require.True(t, movie.RenderDirty)
		require.Greater(t, movie.RenderGeneration, int64(0))
		require.NoError(t, db.Close())
	})

	t.Run("union promotion rollback is atomic", func(t *testing.T) {
		db := newCreditTestDB(t)
		candidate := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "旧表記", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("旧表記"), AmbiguityQuarantined: true}
		require.NoError(t, db.Create(&candidate).Error)
		movie := models.Movie{ContentID: "candidate-union-rollback", ID: "candidate-union-rollback"}
		require.NoError(t, db.Create(&movie).Error)
		credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID}
		require.NoError(t, db.Create(&credit).Error)
		collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}
		require.NoError(t, db.Create(&collision).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_union_cleanup BEFORE UPDATE ON credit_collisions BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
		incoming := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "新表記"}
		require.Error(t, NewActressRepository(db).ImportUpsert(t.Context(), &incoming))
		var stored models.Actress
		require.NoError(t, db.First(&stored, candidate.ID).Error)
		require.False(t, stored.Verified)
		require.True(t, stored.AmbiguityQuarantined)
		require.Equal(t, "旧表記", stored.JapaneseName)
		require.NoError(t, db.First(&collision, collision.ID).Error)
		require.Equal(t, models.CollisionStatusOpen, collision.Status)
		var count int64
		require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
		require.Equal(t, int64(1), count)
	})
}

func TestAllowedCollisionResolutionMatrixMatchesValidator(t *testing.T) {
	all := []string{
		models.CollisionResolutionKeepIdentity,
		models.CollisionResolutionAdoptCanonical,
		models.CollisionResolutionAdoptAlias,
		models.CollisionResolutionReassign,
	}
	tests := []struct {
		name     string
		field    string
		status   string
		verified bool
		want     []string
	}{
		{name: "credited verified", field: models.CreditFieldCreditedName, status: models.CollisionStatusOpen, verified: true, want: all},
		{name: "credited candidate", field: models.CreditFieldCreditedName, status: models.CollisionStatusOpen, want: all},
		{name: "thumb verified", field: models.CreditFieldReportedThumb, status: models.CollisionStatusOpen, verified: true, want: []string{models.CollisionResolutionKeepIdentity, models.CollisionResolutionAdoptCanonical, models.CollisionResolutionReassign}},
		{name: "thumb candidate", field: models.CreditFieldReportedThumb, status: models.CollisionStatusOpen, want: []string{models.CollisionResolutionKeepIdentity, models.CollisionResolutionAdoptCanonical, models.CollisionResolutionReassign}},
		{name: "identity verified", field: models.CreditFieldIdentityLink, status: models.CollisionStatusOpen, verified: true, want: []string{models.CollisionResolutionKeepIdentity, models.CollisionResolutionAdoptCanonical, models.CollisionResolutionReassign}},
		{name: "identity candidate", field: models.CreditFieldIdentityLink, status: models.CollisionStatusOpen, want: []string{models.CollisionResolutionAdoptCanonical, models.CollisionResolutionReassign}},
		{name: "closed row", field: models.CreditFieldCreditedName, status: models.CollisionStatusResolved, verified: true, want: []string{}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			collision := &models.CreditCollision{Field: tc.field, Status: tc.status}
			credit := &models.MovieCredit{Actress: &models.Actress{Verified: tc.verified}}
			allowed := AllowedCollisionResolutions(collision, credit)
			require.Equal(t, tc.want, allowed)
			allowedSet := make(map[string]bool, len(allowed))
			for _, resolution := range allowed {
				allowedSet[resolution] = true
			}
			for _, resolution := range all {
				targetID := uint(0)
				if resolution == models.CollisionResolutionReassign {
					targetID = 999
				}
				err := validateCollisionResolution(collision, credit, resolution, targetID)
				if allowedSet[resolution] {
					require.NoError(t, err, resolution)
				} else {
					require.Error(t, err, resolution)
				}
			}
		})
	}
}

func TestAllowedResolutionsErrorAndEmptyContracts(t *testing.T) {
	t.Run("empty", func(t *testing.T) {
		db := newCreditTestDB(t)
		got, err := NewCollisionService(db).AllowedResolutions(context.Background(), nil)
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("query failure", func(t *testing.T) {
		db := newCreditTestDB(t)
		service := NewCollisionService(db)
		require.NoError(t, db.Close())
		_, err := service.AllowedResolutions(context.Background(), []models.CreditCollision{{ID: 1, CreditID: 1, Status: models.CollisionStatusOpen}})
		require.Error(t, err)
	})

	t.Run("missing credit", func(t *testing.T) {
		db := newCreditTestDB(t)
		_, err := NewCollisionService(db).AllowedResolutions(context.Background(), []models.CreditCollision{{ID: 1, CreditID: 999, Status: models.CollisionStatusOpen}})
		require.ErrorIs(t, err, ErrNotFound)
	})
}

func TestNormalizedAliasLookupFailureContracts(t *testing.T) {
	t.Run("empty alias does not query", func(t *testing.T) {
		db := newCreditTestDB(t)
		matched, err := aliasMatchesCanonicalTx(db.DB, "  ", &models.Actress{FirstName: "Canonical"})
		require.NoError(t, err)
		require.False(t, matched)
	})

	t.Run("backfill query failure", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Close())
		require.Error(t, backfillActressAliasNameKeys(t.Context(), db.DB))
	})

	t.Run("backfill update failure rolls back", func(t *testing.T) {
		db := newCreditTestDB(t)
		alias := models.ActressAlias{AliasName: "Legacy Alias", CanonicalName: "Canonical"}
		require.NoError(t, db.Create(&alias).Error)
		require.NoError(t, db.Exec("UPDATE actress_aliases SET alias_name_key = '' WHERE id = ?", alias.ID).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_alias_key_backfill BEFORE UPDATE OF alias_name_key ON actress_aliases BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
		require.Error(t, backfillActressAliasNameKeys(t.Context(), db.DB))
		var key string
		require.NoError(t, db.Raw("SELECT alias_name_key FROM actress_aliases WHERE id = ?", alias.ID).Scan(&key).Error)
		require.Empty(t, key)
	})

	t.Run("startup reports backfill failure", func(t *testing.T) {
		db, err := New(&Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
		require.NoError(t, err)
		t.Cleanup(func() { _ = db.Close() })
		injectDatabaseCallbackError(t, db, "query", "actress_aliases", 1)
		err = db.RunMigrationsOnStartup(t.Context())
		require.ErrorContains(t, err, "backfill normalized actress alias keys")
	})
}

func TestNormalizedAliasConflictingOwnersFailClosedWithoutMutation(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressAliasRepository(db)
	first := models.ActressAlias{AliasName: "Stage Name", CanonicalName: "Owner A"}
	second := models.ActressAlias{AliasName: "  stage   name ", CanonicalName: "Owner B"}
	require.NoError(t, db.Create(&first).Error)
	require.NoError(t, db.Create(&second).Error)

	assertUnchanged := func(t *testing.T) {
		t.Helper()
		var aliases []models.ActressAlias
		require.NoError(t, db.Where("alias_name_key = ?", models.NormalizeActressNameKey("stage name")).Order("id").Find(&aliases).Error)
		require.Len(t, aliases, 2)
		require.Equal(t, []string{"Owner A", "Owner B"}, []string{aliases[0].CanonicalName, aliases[1].CanonicalName})
	}

	_, err := repo.FindByAliasName(t.Context(), " STAGE NAME ")
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	_, err = repo.GetAliasMap(t.Context())
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	_, err = findVerifiedByAliasTx(db.DB, "", "stage", "name")
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	_, err = aliasMatchesCanonicalTx(db.DB, "stage name", &models.Actress{JapaneseName: "Owner A"})
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	_, err = NewActressRepository(db).FindVerifiedByAlias(t.Context(), "stage name")
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	err = repo.Create(t.Context(), &models.ActressAlias{AliasName: "stage name", CanonicalName: "Owner C"})
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	err = repo.Upsert(t.Context(), &models.ActressAlias{AliasName: "stage name", CanonicalName: "Owner C"})
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	err = db.Transaction(func(tx *gorm.DB) error {
		return repo.UpsertTx(tx, &models.ActressAlias{AliasName: "stage name", CanonicalName: "Owner C"})
	})
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	err = db.Transaction(func(tx *gorm.DB) error {
		return upsertAliasTx(tx, &models.ActressAlias{AliasName: "stage name", CanonicalName: "Owner C"})
	})
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	err = db.Transaction(func(tx *gorm.DB) error {
		return upsertActressAliases(tx, []string{"stage name"}, "Owner C")
	})
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	require.ErrorIs(t, repo.Delete(t.Context(), "stage name"), ErrActressAliasAmbiguous)
	assertUnchanged(t)

	actress := models.Actress{DMMID: 91991, JapaneseName: "New Owner", Verified: true}
	require.NoError(t, db.Create(&actress).Error)
	err = db.Transaction(func(tx *gorm.DB) error {
		return transitionActressCanonicalNamesTx(tx, actress.ID, &models.Actress{JapaneseName: "Owner A"})
	})
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	assertUnchanged(t)
}

func TestNormalizedAliasEquivalentOwnersRetargetTogether(t *testing.T) {
	db := newCreditTestDB(t)
	aliases := []models.ActressAlias{
		{AliasName: "Stage Name", CanonicalName: "Owner A"},
		{AliasName: " stage   name ", CanonicalName: " owner   a "},
		{AliasName: "Other Stage", CanonicalName: " OWNER A "},
	}
	for i := range aliases {
		require.NoError(t, db.Create(&aliases[i]).Error)
	}
	repo := NewActressAliasRepository(db)
	for range 5 {
		aliasMap, err := repo.GetAliasMap(t.Context())
		require.NoError(t, err)
		require.Len(t, aliasMap, 2)
		require.Equal(t, "Owner A", aliasMap[models.NormalizeActressNameKey("stage name")])
	}
	group, err := repo.GetAliasGroup(t.Context(), " OWNER   A ")
	require.NoError(t, err)
	require.Equal(t, "Owner A", group.Canonical)
	require.ElementsMatch(t, []string{"Owner A", "Stage Name", " stage   name ", "Other Stage"}, group.Names)

	actress := models.Actress{DMMID: 91992, JapaneseName: "Owner B", Verified: true}
	require.NoError(t, db.Create(&actress).Error)
	require.NoError(t, db.Transaction(func(tx *gorm.DB) error {
		return transitionActressCanonicalNamesTx(tx, actress.ID, &models.Actress{JapaneseName: "owner a"})
	}))
	var stored []models.ActressAlias
	require.NoError(t, db.Order("id").Find(&stored).Error)
	require.Len(t, stored, 4)
	for i := range stored {
		require.Equal(t, "Owner B", stored[i].CanonicalName)
	}
	found, err := NewActressAliasRepository(db).FindByAliasName(t.Context(), "STAGE NAME")
	require.NoError(t, err)
	require.Equal(t, aliases[0].ID, found.ID)
}

func TestNormalizedAliasLegacyConflictSurvivesMigrationRestartFailClosed(t *testing.T) {
	ctx := t.Context()
	path := filepath.Join(t.TempDir(), "alias-conflict.db")
	cfg := &Config{Type: "sqlite", DSN: path, LogLevel: "silent"}
	legacy, err := New(cfg)
	require.NoError(t, err)
	sqlDB, err := legacy.DB.DB()
	require.NoError(t, err)
	provider := newMigrationProvider(t, sqlDB)
	_, err = provider.UpTo(ctx, 18)
	require.NoError(t, err)
	require.NoError(t, legacy.Exec("INSERT INTO actress_aliases (alias_name, canonical_name) VALUES (?, ?), (?, ?)",
		"Legacy Stage", "Owner A", " legacy   stage ", "Owner B").Error)
	require.NoError(t, legacy.Close())

	for attempt := 0; attempt < 2; attempt++ {
		reopened, openErr := New(cfg)
		require.NoError(t, openErr)
		require.NoError(t, reopened.RunMigrationsOnStartup(ctx))
		repo := NewActressAliasRepository(reopened)
		_, findErr := repo.FindByAliasName(ctx, "LEGACY STAGE")
		require.ErrorIs(t, findErr, ErrActressAliasAmbiguous)
		_, mapErr := repo.GetAliasMap(ctx)
		require.ErrorIs(t, mapErr, ErrActressAliasAmbiguous)
		var aliases []models.ActressAlias
		require.NoError(t, reopened.Where("alias_name_key = ?", models.NormalizeActressNameKey("legacy stage")).Order("id").Find(&aliases).Error)
		require.Len(t, aliases, 2)
		require.Equal(t, []string{"Owner A", "Owner B"}, []string{aliases[0].CanonicalName, aliases[1].CanonicalName})
		require.NoError(t, reopened.Close())
	}
}

func TestNormalizedAliasRepositoryFailureAndNoopContracts(t *testing.T) {
	t.Run("empty canonical has no aliases", func(t *testing.T) {
		db := newCreditTestDB(t)
		aliases, err := normalizedAliasesForCanonicalTx(db.DB, "  ")
		require.NoError(t, err)
		require.Empty(t, aliases)
	})

	t.Run("create reuses equivalent owner and rejects theft", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressAliasRepository(db)
		first := &models.ActressAlias{AliasName: "Create Alias", CanonicalName: "Owner A"}
		require.NoError(t, repo.Create(t.Context(), first))
		equivalent := &models.ActressAlias{AliasName: " create   alias ", CanonicalName: " owner   a "}
		require.NoError(t, repo.Create(t.Context(), equivalent))
		require.Equal(t, first.ID, equivalent.ID)
		err := repo.Create(t.Context(), &models.ActressAlias{AliasName: "CREATE ALIAS", CanonicalName: "Owner B"})
		require.ErrorIs(t, err, ErrActressAliasOwnershipConflict)
	})

	t.Run("create failure is wrapped", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_alias_create BEFORE INSERT ON actress_aliases BEGIN SELECT RAISE(ABORT, 'injected create failure'); END").Error)
		err := NewActressAliasRepository(db).Create(t.Context(), &models.ActressAlias{AliasName: "Create Failure", CanonicalName: "Owner"})
		require.ErrorContains(t, err, "create actress alias")
	})

	t.Run("delete missing is idempotent", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, NewActressAliasRepository(db).Delete(t.Context(), "missing"))
	})

	t.Run("delete failure is wrapped", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressAliasRepository(db)
		require.NoError(t, repo.Create(t.Context(), &models.ActressAlias{AliasName: "Delete Failure", CanonicalName: "Owner"}))
		require.NoError(t, db.Exec("CREATE TRIGGER fail_alias_delete BEFORE DELETE ON actress_aliases BEGIN SELECT RAISE(ABORT, 'injected delete failure'); END").Error)
		err := repo.Delete(t.Context(), "delete failure")
		require.ErrorContains(t, err, "delete actress alias")
	})

	t.Run("alias map skips empty normalized key", func(t *testing.T) {
		db := newCreditTestDB(t)
		alias := models.ActressAlias{AliasName: "Temporary", CanonicalName: "Owner"}
		require.NoError(t, db.Create(&alias).Error)
		require.NoError(t, db.Exec("UPDATE actress_aliases SET alias_name = '', alias_name_key = '' WHERE id = ?", alias.ID).Error)
		got, err := NewActressAliasRepository(db).GetAliasMap(t.Context())
		require.NoError(t, err)
		require.Empty(t, got)
	})

	t.Run("verified canonical homonym is ambiguous", func(t *testing.T) {
		db := newCreditTestDB(t)
		for _, dmmID := range []int{91993, 91994} {
			require.NoError(t, db.Create(&models.Actress{DMMID: dmmID, JapaneseName: "Shared Owner", Verified: true}).Error)
		}
		require.NoError(t, NewActressAliasRepository(db).Create(t.Context(), &models.ActressAlias{AliasName: "Shared Alias", CanonicalName: "Shared Owner"}))
		_, err := NewActressRepository(db).FindVerifiedByAlias(t.Context(), "shared alias")
		require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	})

	t.Run("canonical transition wraps post-retarget lookup failure", func(t *testing.T) {
		db := newCreditTestDB(t)
		actress := models.Actress{DMMID: 91995, JapaneseName: "New Owner", Verified: true}
		require.NoError(t, db.Create(&actress).Error)
		injectDatabaseCallbackError(t, db, "query", "actress_aliases", 2)
		err := db.Transaction(func(tx *gorm.DB) error {
			return transitionActressCanonicalNamesTx(tx, actress.ID, &models.Actress{JapaneseName: "Old Owner"})
		})
		require.ErrorContains(t, err, "find actress alias Old Owner")
	})
}

func TestMergeAliasAmbiguityRollsBackBeforeSourceDeletion(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{DMMID: 91996, JapaneseName: "Merge Target", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{DMMID: 91997, JapaneseName: "Merge Source", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	for _, alias := range []models.ActressAlias{
		{AliasName: "Blocked Alias", CanonicalName: "Other Owner A"},
		{AliasName: " blocked   alias ", CanonicalName: "Other Owner B"},
	} {
		require.NoError(t, db.Create(&alias).Error)
	}
	plan := &MergePlan{
		TargetID:              target.ID,
		SourceID:              source.ID,
		OriginalCanonicalName: target.JapaneseName,
		Merged:                target,
		CanonicalName:         target.JapaneseName,
		SourceAliasUpserts:    []string{"BLOCKED ALIAS"},
	}
	_, err := repo.merger.ExecuteMerge(t.Context(), plan, db)
	require.ErrorIs(t, err, ErrActressAliasAmbiguous)
	require.NoError(t, db.First(&models.Actress{}, source.ID).Error)
	var aliases []models.ActressAlias
	require.NoError(t, db.Where("alias_name_key = ?", models.NormalizeActressNameKey("blocked alias")).Order("id").Find(&aliases).Error)
	require.Equal(t, []string{"Other Owner A", "Other Owner B"}, []string{aliases[0].CanonicalName, aliases[1].CanonicalName})
}

func TestAllowedResolutionsPropagatesActionContextFailure(t *testing.T) {
	t.Run("maps non-empty action context", func(t *testing.T) {
		_, service, _, collision := collisionFixture(t)
		got, err := service.AllowedResolutions(t.Context(), []models.CreditCollision{collision})
		require.NoError(t, err)
		require.NotEmpty(t, got[collision.ID])
	})

	db := newCreditTestDB(t)
	service := NewCollisionService(db)
	require.NoError(t, db.Close())
	rows := []models.CreditCollision{{ID: 1, CreditID: 1, Status: models.CollisionStatusOpen}}
	_, err := service.ActionContexts(t.Context(), rows)
	require.Error(t, err)
	_, err = service.AllowedResolutions(t.Context(), rows)
	require.Error(t, err)
}

func TestResolveCollisionUpdateFailureRollsBack(t *testing.T) {
	db, service, _, collision := collisionFixture(t)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_allowed_resolution_update BEFORE UPDATE ON credit_collisions BEGIN SELECT RAISE(ABORT, 'injected collision update failure'); END").Error)
	_, err := service.resolveTx(db.DB, collision.ID, models.CollisionResolutionKeepIdentity, 0)
	require.Error(t, err)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
}

func TestResolveCollisionLosesOpenStatusBeforeConditionalUpdate(t *testing.T) {
	db, service, _, collision := collisionFixture(t)
	callbackName := "test:close_collision_before_conditional_update"
	fired := false
	require.NoError(t, db.Callback().Update().Before("gorm:update").Register(callbackName, func(tx *gorm.DB) {
		if fired || tx.Statement.Table != "credit_collisions" {
			return
		}
		fired = true
		_ = tx.Exec("UPDATE credit_collisions SET status = ? WHERE id = ?", models.CollisionStatusResolved, collision.ID).Error
	}))
	t.Cleanup(func() { _ = db.Callback().Update().Remove(callbackName) })

	_, err := service.resolveTx(db.DB, collision.ID, models.CollisionResolutionKeepIdentity, 0)
	require.ErrorIs(t, err, ErrCollisionNotOpen)
	require.True(t, fired)
}
