package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameIdentityFieldsSkipsSnapshotRefreshWithoutCanonicalName(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Keep", LastName: "Me", ThumbURL: "https://old.test/thumb.jpg", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)
	credit := models.MovieCredit{MovieContentID: "no-canonical-name", ActressID: identity.ID, CreditedName: "Me Keep", Source: "legacy", Origin: string(models.CreditOriginUser)}
	require.NoError(t, db.Create(&credit).Error)

	// Clearing every name leaves no canonical name: the snapshot refresh is
	// skipped and the identity + thumbnail edit still commits.
	require.NoError(t, repos.ActressRepo.RenameIdentityFields(context.Background(), identity.ID, "", "", "", "https://new.test/thumb.jpg"))

	var stored models.Actress
	require.NoError(t, db.First(&stored, identity.ID).Error)
	assert.Equal(t, "", stored.FirstName)
	assert.Equal(t, "https://new.test/thumb.jpg", stored.ThumbURL)

	var storedCredit models.MovieCredit
	require.NoError(t, db.First(&storedCredit, credit.ID).Error)
	assert.Equal(t, "Me Keep", storedCredit.CreditedName, "without a canonical name there is nothing to refresh")
}

func TestRenameIdentityFieldsPropagatesSnapshotRefreshFailure(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Boom", LastName: "Case", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)
	credit := models.MovieCredit{MovieContentID: "boom-movie", ActressID: identity.ID, CreditedName: "Case Boom", Source: "legacy", Origin: string(models.CreditOriginUser)}
	require.NoError(t, db.Create(&credit).Error)

	// Force the snapshot refresh UPDATE to fail: the rename must propagate the
	// error and roll the whole transaction back.
	require.NoError(t, db.Exec("CREATE TRIGGER fail_credit_update BEFORE UPDATE ON movie_credits BEGIN SELECT RAISE(ABORT, 'boom'); END;").Error)

	err := repos.ActressRepo.RenameIdentityFields(context.Background(), identity.ID, "New", "Boom", "", "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "identity snapshot credits")

	var stored models.Actress
	require.NoError(t, db.First(&stored, identity.ID).Error)
	assert.Equal(t, "Boom", stored.FirstName, "the identity rename rolls back with the failed refresh")
}
