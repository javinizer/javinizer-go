package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The catalog PUT path (ActressRepository.Update) shares the same refresh wiring;
// it is exercised by the API-layer tests for PUT /actresses/:id.

func TestUpdateCanonicalFieldsRefreshesSnapshotCredits(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Old", LastName: "Name", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)
	snapshot := models.MovieCredit{MovieContentID: "canonical-fields", ActressID: identity.ID, CreditedName: "Name Old", Origin: string(models.CreditOriginUser)}
	require.NoError(t, db.Create(&snapshot).Error)

	require.NoError(t, repos.ActressRepo.UpdateCanonicalFields(context.Background(), identity.ID, "New", "Identity", "", "https://new.test/thumb.jpg"))

	var stored models.MovieCredit
	require.NoError(t, db.First(&stored, snapshot.ID).Error)
	assert.Equal(t, "Identity New", stored.CreditedName, "UpdateCanonicalFields must refresh snapshots as well")
}
