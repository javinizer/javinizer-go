package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPR260ImportKeepsConflictingDMMCandidateDistinct(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{
		DMMID: 111, FirstName: "Same", LastName: "Performer", JapaneseName: "同名",
		Verified: false, Origin: ActressOriginScrape,
		NameKey: models.NormalizeActressNameKey("Performer Same"),
	}
	require.NoError(t, db.Create(&candidate).Error)
	movie := models.Movie{ContentID: "conflicting-dmm-import", ID: "conflicting-dmm-import", RenderGeneration: 4}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "Performer Same"}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldIdentityLink, Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)

	incoming := models.Actress{DMMID: 222, FirstName: "Same", LastName: "Performer", JapaneseName: "同名"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
	require.NotEqual(t, candidate.ID, incoming.ID)
	require.Equal(t, 222, incoming.DMMID)

	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.Equal(t, 111, stored.DMMID)
	require.False(t, stored.Verified)
	require.Equal(t, ActressOriginScrape, stored.Origin)
	require.NoError(t, db.First(&credit, credit.ID).Error)
	require.Equal(t, candidate.ID, credit.ActressID)
	require.NoError(t, db.First(&collision, collision.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, collision.Status)
	var associations int64
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Count(&associations).Error)
	require.Zero(t, associations)
	require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
	require.False(t, movie.RenderDirty)
	require.Equal(t, int64(4), movie.RenderGeneration)
}

func TestPR260ImportPromotionPreservesPreviousNamesAsAliases(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{FirstName: "Credited", LastName: "Old", JapaneseName: "旧芸名", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("Old Credited")}
	require.NoError(t, db.Create(&candidate).Error)
	movie := models.Movie{ContentID: "import-name-promotion", ID: "import-name-promotion"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "Old Credited"}
	require.NoError(t, db.Create(&credit).Error)
	unrelated := models.ActressAlias{AliasName: "Protected Alias", CanonicalName: "Other Person"}
	require.NoError(t, db.Create(&unrelated).Error)

	incoming := models.Actress{ID: candidate.ID, DMMID: 333, FirstName: "Canonical", LastName: "New", JapaneseName: "新芸名"}
	require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))

	for _, scraped := range []models.Actress{
		{FirstName: "Credited", LastName: "Old"},
		{JapaneseName: "旧芸名"},
	} {
		found, outcome, err := ResolveActressIdentityTx(db.DB, &scraped)
		require.NoError(t, err)
		require.Equal(t, ResolutionMatched, outcome)
		require.Equal(t, candidate.ID, found.ID)
	}
	var candidates int64
	require.NoError(t, db.Model(&models.Actress{}).Where("verified = ?", false).Count(&candidates).Error)
	require.Zero(t, candidates)
	require.NoError(t, db.First(&credit, credit.ID).Error)
	require.Equal(t, candidate.ID, credit.ActressID)
	require.NoError(t, db.First(&unrelated, unrelated.ID).Error)
	require.Equal(t, "Other Person", unrelated.CanonicalName)
}

func TestPR260ImportPromotionAliasFailureRollsBack(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	candidate := models.Actress{FirstName: "Before", LastName: "Import", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("Import Before")}
	require.NoError(t, db.Create(&candidate).Error)
	movie := models.Movie{ContentID: "import-alias-rollback", ID: "import-alias-rollback", RenderGeneration: 9}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: candidate.ID, CreditedName: "Import Before"}
	require.NoError(t, db.Create(&credit).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_import_alias BEFORE INSERT ON actress_aliases BEGIN SELECT RAISE(ABORT, 'injected alias failure'); END").Error)

	incoming := models.Actress{ID: candidate.ID, FirstName: "After", LastName: "Import"}
	require.Error(t, repo.ImportUpsert(context.Background(), &incoming))
	var stored models.Actress
	require.NoError(t, db.First(&stored, candidate.ID).Error)
	require.Equal(t, "Before", stored.FirstName)
	require.False(t, stored.Verified)
	var aliases int64
	require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
	require.Zero(t, aliases)
	var associations int64
	require.NoError(t, db.Table("movie_actresses").Where("movie_content_id = ?", movie.ContentID).Count(&associations).Error)
	require.Zero(t, associations)
	require.NoError(t, db.First(&movie, "content_id = ?", movie.ContentID).Error)
	require.False(t, movie.RenderDirty)
	require.Equal(t, int64(9), movie.RenderGeneration)
}

func TestPR260CJKCanonicalAdoptionPreservesAllIdentityNames(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{FirstName: "English", LastName: "Name", JapaneseName: "旧日本名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "cjk-canonical-adoption", ID: "cjk-canonical-adoption", RenderGeneration: 2}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "新日本名"}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "新日本名", CanonicalValue: "旧日本名", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&collision).Error)
	jpAlias := models.ActressAlias{AliasName: "Historical Japanese", CanonicalName: "旧日本名"}
	enAlias := models.ActressAlias{AliasName: "Historical English", CanonicalName: "Name English"}
	unrelated := models.ActressAlias{AliasName: "Unrelated Homonym", CanonicalName: "Other Person"}
	for _, alias := range []*models.ActressAlias{&jpAlias, &enAlias, &unrelated} {
		require.NoError(t, db.Create(alias).Error)
	}

	_, err := NewCollisionService(db).Resolve(context.Background(), collision.ID, models.CollisionResolutionAdoptCanonical, 0)
	require.NoError(t, err)
	for _, name := range []models.Actress{{JapaneseName: "旧日本名"}, {JapaneseName: "Historical Japanese"}, {FirstName: "English", LastName: "Name"}, {FirstName: "English", LastName: "Historical"}} {
		found, outcome, resolveErr := ResolveActressIdentityTx(db.DB, &name)
		require.NoError(t, resolveErr)
		require.Equal(t, ResolutionMatched, outcome)
		require.Equal(t, actress.ID, found.ID)
	}
	require.NoError(t, db.First(&jpAlias, jpAlias.ID).Error)
	require.Equal(t, "新日本名", jpAlias.CanonicalName)
	require.NoError(t, db.First(&enAlias, enAlias.ID).Error)
	require.Equal(t, "Name English", enAlias.CanonicalName)
	require.NoError(t, db.First(&unrelated, unrelated.ID).Error)
	require.Equal(t, "Other Person", unrelated.CanonicalName)
}

func TestPR260ImportPromotionAliasControls(t *testing.T) {
	t.Run("unchanged and empty prior names create no aliases", func(t *testing.T) {
		for _, candidate := range []models.Actress{
			{FirstName: "Same", LastName: "Name", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("Name Same")},
			{Origin: ActressOriginScrape},
		} {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			require.NoError(t, db.Create(&candidate).Error)
			incoming := models.Actress{ID: candidate.ID, FirstName: candidate.FirstName, LastName: candidate.LastName}
			if incoming.FullName() == "" {
				incoming.FirstName = "Now Named"
			}
			require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
			var aliases int64
			require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
			require.Zero(t, aliases)
		}
	})

	t.Run("unrelated alias owner is not overwritten", func(t *testing.T) {
		db := newCreditTestDB(t)
		repo := NewActressRepository(db)
		candidate := models.Actress{FirstName: "Taken", Origin: ActressOriginScrape, NameKey: models.NormalizeActressNameKey("Taken")}
		require.NoError(t, db.Create(&candidate).Error)
		protected := models.ActressAlias{AliasName: "Taken", CanonicalName: "Other Person"}
		require.NoError(t, db.Create(&protected).Error)
		incoming := models.Actress{ID: candidate.ID, FirstName: "Replacement"}
		require.NoError(t, repo.ImportUpsert(context.Background(), &incoming))
		require.NoError(t, db.First(&protected, protected.ID).Error)
		require.Equal(t, "Other Person", protected.CanonicalName)
	})
}

func TestTransitionActressCanonicalNamesGuardAndFailureContracts(t *testing.T) {
	t.Run("nil prior identity", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.Nil(t, canonicalActressRepresentations(nil))
		require.NoError(t, transitionActressCanonicalNamesTx(db.DB, 1, nil))
	})

	t.Run("missing and unnamed current identity", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.Error(t, transitionActressCanonicalNamesTx(db.DB, 999999, &models.Actress{FirstName: "Before"}))
		unnamed := models.Actress{Origin: ActressOriginScrape}
		require.NoError(t, db.Create(&unnamed).Error)
		require.NoError(t, transitionActressCanonicalNamesTx(db.DB, unnamed.ID, &models.Actress{FirstName: "Before"}))
	})

	t.Run("alias lookup failure", func(t *testing.T) {
		db := newCreditTestDB(t)
		current := models.Actress{FirstName: "After", Verified: true, Origin: ActressOriginImport}
		require.NoError(t, db.Create(&current).Error)
		injectDatabaseCallbackError(t, db, "query", "actress_aliases", 1)
		require.Error(t, transitionActressCanonicalNamesTx(db.DB, current.ID, &models.Actress{FirstName: "Before"}))
		var aliases int64
		require.NoError(t, db.Model(&models.ActressAlias{}).Count(&aliases).Error)
		require.Zero(t, aliases)
	})

	t.Run("owned alias update failure", func(t *testing.T) {
		db := newCreditTestDB(t)
		current := models.Actress{FirstName: "After", JapaneseName: "新名", Verified: true, Origin: ActressOriginImport}
		require.NoError(t, db.Create(&current).Error)
		alias := models.ActressAlias{AliasName: "旧名", CanonicalName: "Name Before"}
		require.NoError(t, db.Create(&alias).Error)
		require.NoError(t, db.Exec("CREATE TRIGGER fail_owned_alias_update BEFORE UPDATE ON actress_aliases WHEN OLD.alias_name = '旧名' BEGIN SELECT RAISE(ABORT, 'injected owned alias update failure'); END").Error)
		previous := models.Actress{FirstName: "Before", LastName: "Name", JapaneseName: "旧名"}
		require.Error(t, transitionActressCanonicalNamesTx(db.DB, current.ID, &previous))
		require.NoError(t, db.First(&alias, alias.ID).Error)
		require.Equal(t, "Name Before", alias.CanonicalName)
	})
}
