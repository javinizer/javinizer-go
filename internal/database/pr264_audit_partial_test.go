package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A scraper can report the canonical name verbatim. That is still reported
// history and must survive an identity rename.
func TestRenameIdentityFieldsKeepsExactCanonicalScrapeReport(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Old", LastName: "Name", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)
	credit := models.MovieCredit{
		MovieContentID: "exact-canonical-report",
		ActressID:      identity.ID,
		CreditedName:   "Name Old",
		Source:         "dmm",
		Origin:         string(models.CreditOriginScrape),
	}
	require.NoError(t, db.Create(&credit).Error)

	require.NoError(t, repos.ActressRepo.RenameIdentityFields(context.Background(), identity.ID, "New", "Identity", "", ""))

	var stored models.MovieCredit
	require.NoError(t, db.First(&stored, credit.ID).Error)
	assert.Equal(t, "Name Old", stored.CreditedName, "scrape-reported rows are history even when they equal the old canonical name")
}

// A nested Actress pointer without any data is not identity evidence.
func TestUpsertDropsCreditWithEmptyActressPointer(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	movie := creditMovie("empty-actress-pointer", []models.MovieCredit{{Actress: &models.Actress{}}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, saved.Credits)

	var credits, actresses int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&credits).Error)
	require.NoError(t, db.Model(&models.Actress{}).Count(&actresses).Error)
	assert.Equal(t, int64(0), credits, "a bare actress pointer must not create a credit")
	assert.Equal(t, int64(0), actresses, "and must not create a nameless identity")
}

// An alias that agrees with one identity's exact field order must not also
// match a second identity whose parts are swapped.
func TestAliasMatchPrefersExactOrder(t *testing.T) {
	db := newCreditTestDB(t)

	exact := models.Actress{FirstName: "Li", LastName: "Na", Verified: true, Origin: ActressOriginUser}
	swapped := models.Actress{FirstName: "Na", LastName: "Li", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&exact).Error)
	require.NoError(t, db.Create(&swapped).Error)
	require.NoError(t, db.Create(&models.ActressAlias{AliasName: "Na Li", CanonicalName: "Na Li"}).Error)

	matches, err := findVerifiedByAliasTx(db.DB, "", "Li", "Na")
	require.NoError(t, err)
	require.Len(t, matches, 1, "only the exact-order identity may match")
	assert.Equal(t, exact.ID, matches[0].ID)
}
