package database

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func TestLegacyMovieUpsertCanonicalMutationInvalidatesTranslations(t *testing.T) {
	for _, field := range []string{"first", "last", "japanese"} {
		t.Run(field, func(t *testing.T) {
			db := newCreditTestDB(t)
			existing := models.Actress{DMMID: 882211, Verified: true, Origin: ActressOriginUser}
			incoming := models.Actress{DMMID: existing.DMMID}
			switch field {
			case "first":
				existing.LastName, incoming.FirstName, incoming.LastName = "Person", "New", "Person"
			case "last":
				existing.FirstName, incoming.FirstName, incoming.LastName = "New", "New", "Person"
			case "japanese":
				incoming.JapaneseName = "新名"
			}
			require.NoError(t, db.Create(&existing).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: existing.ID, Language: "en", DisplayName: "stale", SourceName: "translation:test"}).Error)
			movie := &models.Movie{ContentID: "legacy-" + field, ID: "LEGACY-" + field, DisplayTitle: "legacy", Actresses: []models.Actress{incoming}}
			_, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, nil)
			require.NoError(t, err)
			var count int64
			require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", existing.ID).Count(&count).Error)
			require.Zero(t, count)
		})
	}
}

func TestLegacyMovieUpsertThumbOnlyAndUnchangedPreserveTranslations(t *testing.T) {
	for _, thumb := range []string{"", "new.jpg"} {
		t.Run(thumb, func(t *testing.T) {
			db := newCreditTestDB(t)
			actress := models.Actress{DMMID: 882212, FirstName: "Same", LastName: "Person", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&actress).Error)
			translation := models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Same EN", SourceName: "translation:test"}
			require.NoError(t, db.Create(&translation).Error)
			movie := &models.Movie{ContentID: "legacy-preserve-" + thumb, ID: "LEGACY-PRESERVE", Actresses: []models.Actress{{DMMID: actress.DMMID, FirstName: actress.FirstName, LastName: actress.LastName, ThumbURL: thumb}}}
			_, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, nil)
			require.NoError(t, err)
			var stored models.ActressTranslation
			require.NoError(t, db.First(&stored, translation.ID).Error)
			require.Equal(t, "translation:test", stored.SourceName)
		})
	}
}

func TestLegacyMovieUpsertTranslationDeleteFailureRollsBackMovieAndCanonicalMutation(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{DMMID: 882213, LastName: "Person", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	translation := models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Before EN"}
	require.NoError(t, db.Create(&translation).Error)
	original := models.Movie{ContentID: "legacy-rollback", ID: "LEGACY-ROLLBACK", DisplayTitle: "before", Actresses: []models.Actress{actress}}
	_, err := NewMovieRepository(db).Upsert(t.Context(), &original)
	require.NoError(t, err)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_legacy_translation_delete BEFORE DELETE ON actress_translations BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)

	changed := models.Movie{ContentID: original.ContentID, ID: original.ID, DisplayTitle: "after", Actresses: []models.Actress{{DMMID: actress.DMMID, FirstName: "New", LastName: actress.LastName}}}
	_, err = NewMovieRepository(db).UpsertWithTranslations(t.Context(), &changed, nil, nil)
	require.Error(t, err)
	var storedActress models.Actress
	require.NoError(t, db.First(&storedActress, actress.ID).Error)
	require.Empty(t, storedActress.FirstName)
	var storedMovie models.Movie
	require.NoError(t, db.First(&storedMovie, "content_id = ?", original.ContentID).Error)
	require.Equal(t, "before", storedMovie.DisplayTitle)
	require.NoError(t, db.First(&translation, translation.ID).Error)
}

func TestResolveActressGroupRaceRetryFoundInvalidatesTranslations(t *testing.T) {
	db := newCreditTestDB(t)
	existing := models.Actress{DMMID: 882214, LastName: "Person"}
	require.NoError(t, db.Create(&existing).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: existing.ID, Language: "en", DisplayName: "stale"}).Error)
	incoming := []models.Actress{{DMMID: existing.DMMID, FirstName: "Race", LastName: existing.LastName}}
	calls := 0
	lookup := func(tx *gorm.DB, _ *models.Actress) (models.Actress, bool, error) {
		calls++
		if calls == 1 {
			return models.Actress{}, false, nil
		}
		var found models.Actress
		err := tx.First(&found, existing.ID).Error
		return found, err == nil, err
	}
	name := "test:legacy-race-duplicate"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "actresses" {
			_ = tx.AddError(gorm.ErrDuplicatedKey)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(name) })
	err := db.Transaction(func(tx *gorm.DB) error {
		return NewMovieRepository(db).upserter.resolveActressGroup(tx, incoming, []actressGroupEntry{{index: 0, act: &incoming[0]}}, lookup)
	})
	require.NoError(t, err)
	var count int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", existing.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestResolveActressGroupRaceRetryDeleteFailureRollsBackCanonicalMutation(t *testing.T) {
	db := newCreditTestDB(t)
	existing := models.Actress{DMMID: 882215, LastName: "Person"}
	require.NoError(t, db.Create(&existing).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: existing.ID, Language: "en", DisplayName: "stale"}).Error)
	require.NoError(t, db.Exec("CREATE TRIGGER fail_race_translation_delete BEFORE DELETE ON actress_translations BEGIN SELECT RAISE(ABORT, 'injected'); END").Error)
	incoming := []models.Actress{{DMMID: existing.DMMID, FirstName: "Race", LastName: existing.LastName}}
	calls := 0
	lookup := func(tx *gorm.DB, _ *models.Actress) (models.Actress, bool, error) {
		calls++
		if calls == 1 {
			return models.Actress{}, false, nil
		}
		var found models.Actress
		err := tx.First(&found, existing.ID).Error
		return found, err == nil, err
	}
	name := "test:legacy-race-delete-failure"
	require.NoError(t, db.Callback().Create().Before("gorm:create").Register(name, func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Schema != nil && tx.Statement.Schema.Table == "actresses" {
			_ = tx.AddError(gorm.ErrDuplicatedKey)
		}
	}))
	t.Cleanup(func() { _ = db.Callback().Create().Remove(name) })
	err := db.Transaction(func(tx *gorm.DB) error {
		return NewMovieRepository(db).upserter.resolveActressGroup(tx, incoming, []actressGroupEntry{{index: 0, act: &incoming[0]}}, lookup)
	})
	require.Error(t, err)
	var stored models.Actress
	require.NoError(t, db.First(&stored, existing.ID).Error)
	require.Empty(t, stored.FirstName)
	var count int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", existing.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
}

func TestActressTranslationRepositoryNormalizesLanguageBoundaries(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{FirstName: "Language"}
	require.NoError(t, db.Create(&actress).Error)
	repo := newActressTranslationRepository(db)
	first := models.ActressTranslation{ActressID: actress.ID, Language: " EN ", DisplayName: "first", SourceName: "provider-a"}
	require.NoError(t, repo.Upsert(t.Context(), &first))
	second := models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "second", SourceName: "provider-b"}
	require.NoError(t, repo.Upsert(t.Context(), &second))
	var count int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", actress.ID).Count(&count).Error)
	require.Equal(t, int64(1), count)
	found, err := repo.FindByActressAndLanguage(t.Context(), actress.ID, " En ")
	require.NoError(t, err)
	require.Equal(t, "en", found.Language)
	require.Equal(t, "second", found.DisplayName)
	batch, err := repo.FindByActressIDsAndLanguage(t.Context(), []uint{actress.ID}, " EN ")
	require.NoError(t, err)
	require.Len(t, batch[actress.ID], 1)
	require.Equal(t, "en", batch[actress.ID][0].Language)
	all, err := repo.FindAllByActress(t.Context(), actress.ID)
	require.NoError(t, err)
	require.Len(t, all, 1)
	require.Equal(t, "en", all[0].Language)
	require.NoError(t, repo.Delete(t.Context(), actress.ID, " eN "))
	_, err = repo.FindByActressAndLanguage(t.Context(), actress.ID, "en")
	require.Error(t, err)
}

func TestFreshTranslationsByActressCompatibilityReturnsProviderRows(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{FirstName: "Canonical", LastName: "Name"}
	require.NoError(t, db.Create(&actress).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actress.ID, Language: "en", DisplayName: "Translated", SourceName: "provider-label"}).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: actress.ID, Language: "ja", DisplayName: "別名", SourceName: "unrelated-canonical-value"}).Error)

	translations, err := NewActressRepository(db).FreshTranslationsByActress(t.Context(), actress.ID)
	require.NoError(t, err)
	require.Len(t, translations, 2)
	require.ElementsMatch(t, []string{"provider-label", "unrelated-canonical-value"}, []string{translations[0].SourceName, translations[1].SourceName})
}

func TestFreshTranslationsByActressCompatibilityReturnsFindError(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Exec("DROP TABLE actress_translations").Error)
	translations, err := NewActressRepository(db).FreshTranslationsByActress(t.Context(), 42)
	require.Error(t, err)
	require.Nil(t, translations)
}

func TestMergeTranslationOverlapUsesNormalizedLanguageAndTargetProvenance(t *testing.T) {
	db := newCreditTestDB(t)
	target := models.Actress{FirstName: "Same"}
	source := models.Actress{FirstName: "Same"}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: target.ID, Language: "EN", DisplayName: "target", SourceName: "provider-target"}).Error)
	require.NoError(t, db.Create(&models.ActressTranslation{ActressID: source.ID, Language: " en ", DisplayName: "source", SourceName: "provider-source"}).Error)
	require.NoError(t, reconcileMergedActressTranslationsTx(db.DB, target.ID, source.ID, "same", "same", "same"))
	var rows []models.ActressTranslation
	require.NoError(t, db.Order("id").Find(&rows).Error)
	require.Len(t, rows, 1)
	require.Equal(t, target.ID, rows[0].ActressID)
	require.Equal(t, "en", rows[0].Language)
	require.Equal(t, "target", rows[0].DisplayName)
	require.Equal(t, "provider-target", rows[0].SourceName)
}

func TestNormalizeLanguageHelperMatchesEstablishedPolicy(t *testing.T) {
	require.Equal(t, "zh-hant-tw", normalizeActressTranslationLanguage(" ZH-Hant-TW "))
	require.Equal(t, "", normalizeActressTranslationLanguage("  "))
}
