package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// The catalog PUT path (ActressRepository.Update) must refresh identity-snapshot
// credits on a canonical rename, exactly like the review rename path.
func TestCatalogUpdateRefreshesSnapshotCredits(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()

	identity := models.Actress{FirstName: "Old", LastName: "Name", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)

	// The credit references a real movie: render invalidation resolves it.
	movie := models.Movie{ContentID: "catalog-update-movie", ID: "catalog-update-movie", Title: "Catalog Update"}
	require.NoError(t, db.Create(&movie).Error)
	require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{identity}))

	// A pure identity snapshot: user origin, credited name equal to the canonical one.
	snapshot := models.MovieCredit{
		MovieContentID:       movie.ContentID,
		ActressID:            identity.ID,
		CreditedName:         "Name Old",
		CreditedJapaneseName: "旧名",
		Origin:               string(models.CreditOriginUser),
	}
	require.NoError(t, db.Create(&snapshot).Error)

	loaded, err := repos.ActressRepo.FindByID(ctx, identity.ID)
	require.NoError(t, err)
	loaded.FirstName = "New"
	loaded.LastName = "Identity"
	require.NoError(t, repos.ActressRepo.Update(ctx, loaded))

	var stored models.MovieCredit
	require.NoError(t, db.First(&stored, snapshot.ID).Error)
	assert.Equal(t, "Identity New", stored.CreditedName, "a catalog rename refreshes identity snapshots")
	assert.Equal(t, "旧名", stored.CreditedJapaneseName)
}

func TestCatalogSnapshotRefreshFailureRollsBack(t *testing.T) {
	for _, tc := range []struct {
		name   string
		update func(context.Context, *ActressRepository, *models.Actress) error
	}{
		{
			name: "update",
			update: func(ctx context.Context, repo *ActressRepository, actress *models.Actress) error {
				actress.FirstName = "New"
				actress.LastName = "Identity"
				return repo.Update(ctx, actress)
			},
		},
		{
			name: "canonical fields",
			update: func(ctx context.Context, repo *ActressRepository, actress *models.Actress) error {
				return repo.UpdateCanonicalFields(ctx, actress.ID, "New", "Identity", "新名", actress.ThumbURL)
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			repo := NewActressRepository(db)
			identity := models.Actress{FirstName: "Old", LastName: "Name", JapaneseName: "旧名", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&identity).Error)
			movie := models.Movie{ContentID: "refresh-failure-" + tc.name, ID: "refresh-failure-" + tc.name, Title: "Refresh Failure"}
			require.NoError(t, db.Create(&movie).Error)
			require.NoError(t, db.Model(&movie).Association("Actresses").Replace([]models.Actress{identity}))
			credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: identity.ID, CreditedName: "Name Old", CreditedJapaneseName: "旧名", Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)
			injectDatabaseCallbackError(t, db, "update", "movie_credits", 1)

			err := tc.update(t.Context(), repo, &identity)
			require.Error(t, err)

			var storedActress models.Actress
			require.NoError(t, db.First(&storedActress, identity.ID).Error)
			assert.Equal(t, "Name Old", storedActress.FullName())
			var storedCredit models.MovieCredit
			require.NoError(t, db.First(&storedCredit, credit.ID).Error)
			assert.Equal(t, "Name Old", storedCredit.CreditedName)
			assert.Equal(t, "旧名", storedCredit.CreditedJapaneseName)
		})
	}
}
