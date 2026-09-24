package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// BuildCreditsFromScrape can emit scrape-owned credits with an empty Source, so
// the snapshot refresh must not key off source alone: their credited_name is
// reported attribution and has to survive an identity rename.
func TestRenameIdentityFieldsKeepsSourcelessScrapeReportedNames(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Report", LastName: "Owner", JapaneseName: "報告", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)

	reported := models.MovieCredit{
		MovieContentID:       "sourceless-scrape",
		ActressID:            identity.ID,
		CreditedName:         "Different Reported Name",
		CreditedJapaneseName: "別名",
		Origin:               string(models.CreditOriginScrape),
	}
	require.NoError(t, db.Create(&reported).Error)

	require.NoError(t, repos.ActressRepo.RenameIdentityFields(context.Background(), identity.ID, "New", "Owner", "新報告", ""))

	var stored models.MovieCredit
	require.NoError(t, db.First(&stored, reported.ID).Error)
	assert.Equal(t, "Different Reported Name", stored.CreditedName, "reported names must never be rewritten by a rename")
	assert.Equal(t, "別名", stored.CreditedJapaneseName)
}
