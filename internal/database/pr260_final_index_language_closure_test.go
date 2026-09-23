package database

import (
	"errors"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

func finalTranslationData(names ...string) []models.ActressTranslationData {
	out := make([]models.ActressTranslationData, len(names))
	for i, name := range names {
		out[i] = models.ActressTranslationData{ActressIndex: i, Language: "en", DisplayName: name}
	}
	return out
}

func finalStoredTranslationNames(t *testing.T, db *DB) map[uint]string {
	t.Helper()
	var rows []models.ActressTranslation
	require.NoError(t, db.Order("actress_id").Find(&rows).Error)
	out := make(map[uint]string, len(rows))
	for _, row := range rows {
		out[row.ActressID] = row.DisplayName
	}
	return out
}

func TestExplicitCreditTranslationOwnershipUsesIdentityEvidence(t *testing.T) {
	t.Run("sparse one of many", func(t *testing.T) {
		db := newCreditTestDB(t)
		movie := &models.Movie{ContentID: "translation-sparse", ID: "translation-sparse", Actresses: []models.Actress{{DMMID: 982001, FirstName: "Alpha"}, {DMMID: 982002, FirstName: "Beta"}}, Credits: []models.MovieCredit{{Scraped: models.Actress{DMMID: 982002, FirstName: "Beta"}}}}
		saved, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, finalTranslationData("Alpha EN", "Beta EN"))
		require.NoError(t, err)
		require.Len(t, saved.Credits, 1)
		require.Equal(t, map[uint]string{saved.Credits[0].ActressID: "Beta EN"}, finalStoredTranslationNames(t, db))
	})

	t.Run("reverse order", func(t *testing.T) {
		db := newCreditTestDB(t)
		movie := &models.Movie{ContentID: "translation-reverse", ID: "translation-reverse", Actresses: []models.Actress{{DMMID: 982011, FirstName: "Alpha"}, {DMMID: 982012, FirstName: "Beta"}}, Credits: []models.MovieCredit{{Scraped: models.Actress{DMMID: 982012, FirstName: "Beta"}}, {Scraped: models.Actress{DMMID: 982011, FirstName: "Alpha"}}}}
		saved, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, finalTranslationData("Alpha EN", "Beta EN"))
		require.NoError(t, err)
		require.Len(t, saved.Credits, 2)
		require.Equal(t, map[uint]string{saved.Credits[0].ActressID: "Beta EN", saved.Credits[1].ActressID: "Alpha EN"}, finalStoredTranslationNames(t, db))
	})

	t.Run("unmatched", func(t *testing.T) {
		db := newCreditTestDB(t)
		movie := &models.Movie{ContentID: "translation-unmatched", ID: "translation-unmatched", Actresses: []models.Actress{{DMMID: 982021, FirstName: "Alpha"}}, Credits: []models.MovieCredit{{Scraped: models.Actress{DMMID: 982022, FirstName: "Other"}}}}
		_, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, finalTranslationData("Alpha EN"))
		require.NoError(t, err)
		require.Empty(t, finalStoredTranslationNames(t, db))
	})

	t.Run("ambiguous shared name", func(t *testing.T) {
		db := newCreditTestDB(t)
		movie := &models.Movie{ContentID: "translation-ambiguous", ID: "translation-ambiguous", Actresses: []models.Actress{{FirstName: "Shared", LastName: "Name"}}, Credits: []models.MovieCredit{{Scraped: models.Actress{DMMID: 982031, FirstName: "Shared", LastName: "Name"}}, {Scraped: models.Actress{DMMID: 982032, FirstName: "Shared", LastName: "Name"}}}}
		_, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, finalTranslationData("Shared EN"))
		require.NoError(t, err)
		require.Empty(t, finalStoredTranslationNames(t, db))
	})

	t.Run("actress pointer evidence", func(t *testing.T) {
		db := newCreditTestDB(t)
		beta := models.Actress{DMMID: 982042, FirstName: "Beta", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&beta).Error)
		movie := &models.Movie{ContentID: "translation-pointer", ID: "translation-pointer", Actresses: []models.Actress{{DMMID: 982041, FirstName: "Alpha"}, beta}, Credits: []models.MovieCredit{{Actress: &beta}}}
		saved, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, finalTranslationData("Alpha EN", "Beta EN"))
		require.NoError(t, err)
		require.Equal(t, map[uint]string{saved.Credits[0].ActressID: "Beta EN"}, finalStoredTranslationNames(t, db))
	})
}

func TestReassignedCreditTranslationOwnershipFollowsScrapedIdentity(t *testing.T) {
	db := newCreditTestDB(t)
	source := models.Actress{DMMID: 982061, FirstName: "Source", Origin: ActressOriginScrape}
	target := models.Actress{DMMID: 982062, FirstName: "Target", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&source).Error)
	require.NoError(t, db.Create(&target).Error)
	movie := &models.Movie{ContentID: "translation-reassigned", ID: "translation-reassigned", Actresses: []models.Actress{{DMMID: source.DMMID, FirstName: source.FirstName}}, Credits: []models.MovieCredit{{Scraped: models.Actress{DMMID: source.DMMID, FirstName: source.FirstName}}}}
	require.NoError(t, db.Create(&models.MovieCreditReassignment{MovieContentID: movie.ContentID, SourceActressID: source.ID, TargetActressID: target.ID}).Error)
	saved, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, finalTranslationData("Source EN"))
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	require.Equal(t, target.ID, saved.Credits[0].ActressID)
	require.Equal(t, map[uint]string{target.ID: "Source EN"}, finalStoredTranslationNames(t, db))
}

func TestGeneratedCreditTranslationOwnershipAAndB(t *testing.T) {
	db := newCreditTestDB(t)
	movie := &models.Movie{ContentID: "translation-generated", ID: "translation-generated", Actresses: []models.Actress{{DMMID: 982051, FirstName: "Alpha"}, {DMMID: 982052, FirstName: "Beta"}}, Credits: []models.MovieCredit{{Scraped: models.Actress{DMMID: 982051, FirstName: "Alpha"}}, {Scraped: models.Actress{DMMID: 982052, FirstName: "Beta"}}}}
	saved, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, finalTranslationData("Alpha EN", "Beta EN"))
	require.NoError(t, err)
	require.Equal(t, map[uint]string{saved.Credits[0].ActressID: "Alpha EN", saved.Credits[1].ActressID: "Beta EN"}, finalStoredTranslationNames(t, db))
}

func TestFinalTranslationIdentityMappingFailureBranches(t *testing.T) {
	t.Run("deduplicates resolved ids", func(t *testing.T) {
		db := newCreditTestDB(t)
		actress := models.Actress{DMMID: 982071, FirstName: "Same"}
		require.NoError(t, db.Create(&actress).Error)
		mapped, err := actressTranslationIDsByIdentityTx(db.DB, []models.Actress{actress}, []models.MovieCredit{{Scraped: models.Actress{FirstName: "Other"}}, {ActressID: actress.ID, Scraped: actress}, {ActressID: actress.ID, Scraped: actress}})
		require.NoError(t, err)
		require.Nil(t, mapped)
	})
	t.Run("resolved identity reload", func(t *testing.T) {
		db := newCreditTestDB(t)
		require.NoError(t, db.Migrator().DropTable(&models.Actress{}))
		_, err := actressTranslationIDsByIdentityTx(db.DB, []models.Actress{{DMMID: 1}}, []models.MovieCredit{{ActressID: 1}})
		require.Error(t, err)
	})
	t.Run("upsert propagation", func(t *testing.T) {
		db := newCreditTestDB(t)
		verified := models.Actress{DMMID: 982072, FirstName: "Verified", Verified: true, Origin: ActressOriginUser}
		require.NoError(t, db.Create(&verified).Error)
		name := "final:translation-identity-reload"
		require.NoError(t, db.Callback().Query().Before("gorm:query").Register(name, func(tx *gorm.DB) {
			if _, ok := tx.Statement.Dest.(*[]models.Actress); ok {
				_ = tx.AddError(errors.New("resolved identity reload"))
			}
		}))
		t.Cleanup(func() { _ = db.Callback().Query().Remove(name) })
		movie := &models.Movie{ContentID: "identity-reload-error", ID: "identity-reload-error", Actresses: []models.Actress{verified}, Credits: []models.MovieCredit{{Scraped: verified}}}
		_, err := NewMovieRepository(db).UpsertWithTranslations(t.Context(), movie, nil, finalTranslationData("Verified EN"))
		require.Error(t, err)
	})
}

func TestFinalMergeTranslationNormalizationFailures(t *testing.T) {
	for _, test := range []struct {
		name                                      string
		targetLanguage, sourceLanguage, operation string
		occurrence                                int
	}{
		{name: "target normalize update", targetLanguage: " EN ", sourceLanguage: "ja", operation: "update", occurrence: 1},
		{name: "source list", targetLanguage: "en", sourceLanguage: "ja", operation: "query", occurrence: 2},
		{name: "source normalize update", targetLanguage: "en", sourceLanguage: " JA ", operation: "update", occurrence: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			target := models.Actress{FirstName: "Same"}
			source := models.Actress{FirstName: "Same"}
			require.NoError(t, db.Create(&target).Error)
			require.NoError(t, db.Create(&source).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: target.ID, Language: test.targetLanguage}).Error)
			require.NoError(t, db.Create(&models.ActressTranslation{ActressID: source.ID, Language: test.sourceLanguage}).Error)
			injectDatabaseCallbackError(t, db, test.operation, "actress_translations", test.occurrence)
			require.Error(t, reconcileMergedActressTranslationsTx(db.DB, target.ID, source.ID, "same", "same", "same"))
		})
	}
}

func TestFinalSaveMergedActressWriteFailure(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Close())
	previous := models.Actress{ID: 1, FirstName: "Before"}
	merged := models.Actress{ID: 1, FirstName: "After"}
	require.Error(t, saveMergedActressTx(db.DB, &previous, &merged))
}

func TestFinalMovieReloadFailureAfterPersistence(t *testing.T) {
	db := newCreditTestDB(t)
	injectDatabaseCallbackError(t, db, "query", "movies", 3)
	movie := &models.Movie{ContentID: "final-reload-error", ID: "final-reload-error"}
	_, err := NewMovieRepository(db).Upsert(t.Context(), movie)
	require.Error(t, err)
}

func TestActressTranslationUpsertRejectsEmptyNormalizedLanguageWithoutMutation(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{FirstName: "Language"}
	require.NoError(t, db.Create(&actress).Error)
	repo := newActressTranslationRepository(db)
	for _, language := range []string{"", " \t\n "} {
		record := models.ActressTranslation{ActressID: actress.ID, Language: language, DisplayName: "invalid"}
		err := repo.Upsert(t.Context(), &record)
		require.ErrorIs(t, err, ErrInvalidLookup)
		require.Equal(t, language, record.Language)
		require.Zero(t, record.ID)
	}
	var count int64
	require.NoError(t, db.Model(&models.ActressTranslation{}).Where("actress_id = ?", actress.ID).Count(&count).Error)
	require.Zero(t, count)
}

func TestActressTranslationUpsertTxRejectsEmptyBeforeDatabaseOperation(t *testing.T) {
	db := newCreditTestDB(t)
	require.NoError(t, db.Close())
	record := models.ActressTranslation{ActressID: 1, Language: "  "}
	err := newActressTranslationRepository(db).UpsertTx(db.DB, &record)
	require.ErrorIs(t, err, ErrInvalidLookup)
	require.False(t, errors.Is(err, ErrNotFound))
	require.Equal(t, "  ", record.Language)
}
