package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// EffectiveOrigin treats any non-user origin, including an empty one, as
// scrape-owned. A credit like that is reported attribution, not a snapshot.
func TestRenameIdentityFieldsKeepsUnspecifiedOriginReport(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Old", LastName: "Name", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)
	credit := models.MovieCredit{
		MovieContentID: "unspecified-origin",
		ActressID:      identity.ID,
		CreditedName:   "Name Old",
		Origin:         "",
	}
	require.NoError(t, db.Create(&credit).Error)

	require.NoError(t, repos.ActressRepo.RenameIdentityFields(context.Background(), identity.ID, "New", "Identity", "", ""))

	var stored models.MovieCredit
	require.NoError(t, db.First(&stored, credit.ID).Error)
	assert.Equal(t, "Name Old", stored.CreditedName, "a scrape-owned credit with no origin is not an identity snapshot")
}
