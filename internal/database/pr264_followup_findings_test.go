package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// adopt_canonical renames the identity row like a catalog edit, so
// user-origin identity-snapshot credits (their credited_name snapshots the
// canonical name because no scraper ever reported one) must follow the
// adoption, or movies rendered with use_credited_name keep the pre-adoption
// name.
func TestAdoptCanonicalRefreshesIdentitySnapshotCredits(t *testing.T) {
	db := newCreditTestDB(t)
	service := NewCollisionService(db)

	identity := models.Actress{FirstName: "Truth", LastName: "Original", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)
	for _, contentID := range []string{"collision-movie", "snapshot-movie", "legacy-movie", "scrape-movie"} {
		require.NoError(t, db.Create(&models.Movie{ContentID: contentID, ID: contentID, Title: contentID}).Error)
	}

	credit := models.MovieCredit{
		MovieContentID: "collision-movie", ActressID: identity.ID,
		CreditedName: "Reported Person", Source: "dmm", Origin: string(models.CreditOriginScrape),
	}
	require.NoError(t, db.Create(&credit).Error)
	collision := models.CreditCollision{
		CreditID: credit.ID, MovieContentID: credit.MovieContentID,
		Field: models.CreditFieldCreditedName, ReportedValue: "Reported Person", CanonicalValue: "Original Truth",
		Status: models.CollisionStatusOpen, Occurrences: 1, SourcesSeen: "dmm",
	}
	require.NoError(t, db.Create(&collision).Error)

	snapshot := models.MovieCredit{MovieContentID: "snapshot-movie", ActressID: identity.ID, CreditedName: "Original Truth", Origin: string(models.CreditOriginUser)}
	legacy := models.MovieCredit{MovieContentID: "legacy-movie", ActressID: identity.ID, CreditedName: "Original Truth", Source: "legacy", Origin: string(models.CreditOriginUser)}
	reported := models.MovieCredit{MovieContentID: "scrape-movie", ActressID: identity.ID, CreditedName: "Original Truth", Source: "dmm", Origin: string(models.CreditOriginScrape)}
	require.NoError(t, db.Create(&[]models.MovieCredit{snapshot, legacy, reported}).Error)

	_, err := service.Resolve(context.Background(), collision.ID, models.CollisionResolutionAdoptCanonical, 0)
	require.NoError(t, err)

	var storedIdentity models.Actress
	require.NoError(t, db.First(&storedIdentity, identity.ID).Error)
	assert.Equal(t, "Person", storedIdentity.FirstName)
	assert.Equal(t, "Reported", storedIdentity.LastName)

	loadCredit := func(contentID string) models.MovieCredit {
		t.Helper()
		var stored models.MovieCredit
		require.NoError(t, db.Where("movie_content_id = ?", contentID).First(&stored).Error)
		return stored
	}
	assert.Equal(t, "Reported Person", loadCredit("snapshot-movie").CreditedName, "identity snapshot credits follow the adopted canonical name")
	assert.Equal(t, "Reported Person", loadCredit("legacy-movie").CreditedName, "legacy snapshot credits follow the adopted canonical name")
	assert.Equal(t, "Original Truth", loadCredit("scrape-movie").CreditedName, "scraper-reported attribution is history and stays")

	var snapshotMovie models.Movie
	require.NoError(t, db.First(&snapshotMovie, "content_id = ?", "snapshot-movie").Error)
	assert.True(t, snapshotMovie.RenderDirty, "snapshot movies must re-render so use_credited_name picks up the adopted name")
}

// Only adopt_canonical renames the identity row; other resolutions must leave
// snapshot credits alone.
func TestNonAdoptingCollisionResolutionsLeaveSnapshotCredits(t *testing.T) {
	for _, tc := range []struct{ name, resolution string }{
		{"keep_identity", models.CollisionResolutionKeepIdentity},
		{"adopt_alias", models.CollisionResolutionAdoptAlias},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			service := NewCollisionService(db)

			identity := models.Actress{FirstName: "Truth", LastName: "Original", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&identity).Error)
			for _, contentID := range []string{"collision-movie", "snapshot-movie"} {
				require.NoError(t, db.Create(&models.Movie{ContentID: contentID, ID: contentID, Title: contentID}).Error)
			}
			credit := models.MovieCredit{
				MovieContentID: "collision-movie", ActressID: identity.ID,
				CreditedName: "Reported Person", Source: "dmm", Origin: string(models.CreditOriginScrape),
			}
			require.NoError(t, db.Create(&credit).Error)
			collision := models.CreditCollision{
				CreditID: credit.ID, MovieContentID: credit.MovieContentID,
				Field: models.CreditFieldCreditedName, ReportedValue: "Reported Person", CanonicalValue: "Original Truth",
				Status: models.CollisionStatusOpen, Occurrences: 1, SourcesSeen: "dmm",
			}
			require.NoError(t, db.Create(&collision).Error)
			snapshot := models.MovieCredit{MovieContentID: "snapshot-movie", ActressID: identity.ID, CreditedName: "Original Truth", Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&snapshot).Error)

			_, err := service.Resolve(context.Background(), collision.ID, tc.resolution, 0)
			require.NoError(t, err)

			var storedSnapshot models.MovieCredit
			require.NoError(t, db.First(&storedSnapshot, snapshot.ID).Error)
			assert.Equal(t, "Original Truth", storedSnapshot.CreditedName, "%s must not rewrite snapshot credits", tc.resolution)
			var storedIdentity models.Actress
			require.NoError(t, db.First(&storedIdentity, identity.ID).Error)
			assert.Equal(t, "Truth", storedIdentity.FirstName)
			assert.Equal(t, "Original", storedIdentity.LastName)
		})
	}
}

// A scrape credit whose only identity signal is an explicit ActressID is an
// explicit link, not a resolution hint: it must attach to that identity
// without fabricating a nameless candidate row.
func TestUpsertSparseCreditWithOnlyActressIDLinksDirectly(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Linked", LastName: "Identity", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)

	movie := creditMovie("sparse-actress-id", []models.MovieCredit{{ActressID: identity.ID}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	assert.Equal(t, identity.ID, saved.Credits[0].ActressID)

	var credits, actresses int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&credits).Error)
	require.NoError(t, db.Model(&models.Actress{}).Count(&actresses).Error)
	assert.EqualValues(t, 1, credits, "the explicit link must be persisted")
	assert.EqualValues(t, 1, actresses, "no candidate row may be fabricated for an explicit link")
}

// A credit carrying only an override name has no identity to attach to and
// must be dropped instead of fabricating a blank identity row.
func TestUpsertDropsSparseCreditWithOnlyOverrideName(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	movie := creditMovie("sparse-override-only", []models.MovieCredit{{OverrideName: "Pinned Display", UserOverride: true}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, saved.Credits)

	var credits, actresses int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&credits).Error)
	require.NoError(t, db.Model(&models.Actress{}).Count(&actresses).Error)
	assert.EqualValues(t, 0, credits, "an override-only credit must not be persisted")
	assert.EqualValues(t, 0, actresses, "and must not fabricate a blank identity")
}
