package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenameIdentityFieldsPersistsThumbnailAndRefreshesSnapshotCredits(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()

	identity := models.Actress{
		FirstName: "Old", LastName: "Name", JapaneseName: "旧名",
		ThumbURL: "https://old.test/thumb.jpg", Verified: true, Origin: ActressOriginUser,
	}
	require.NoError(t, db.Create(&identity).Error)

	legacy := models.MovieCredit{MovieContentID: "legacy-movie", ActressID: identity.ID, CreditedName: "Name Old", CreditedJapaneseName: "旧名", ReportedThumbURL: "https://old.test/thumb.jpg", Source: "legacy", Origin: string(models.CreditOriginUser)}
	snapshot := models.MovieCredit{MovieContentID: "snapshot-movie", ActressID: identity.ID, CreditedName: "Name Old", CreditedJapaneseName: "旧名", Origin: string(models.CreditOriginUser)}
	reported := models.MovieCredit{MovieContentID: "scrape-movie", ActressID: identity.ID, CreditedName: "Reported Alias", CreditedJapaneseName: "報告名", Source: "dmm", Origin: string(models.CreditOriginScrape)}
	overridden := models.MovieCredit{MovieContentID: "override-movie", ActressID: identity.ID, CreditedName: "Name Old", OverrideName: "Pinned Display", UserOverride: true, Origin: string(models.CreditOriginUser)}
	require.NoError(t, db.Create(&[]models.MovieCredit{legacy, snapshot, reported, overridden}).Error)

	require.NoError(t, repos.ActressRepo.RenameIdentityFields(ctx, identity.ID, "New", "Identity", "新名", "https://new.test/thumb.jpg"))

	var stored models.Actress
	require.NoError(t, db.First(&stored, identity.ID).Error)
	assert.Equal(t, "New", stored.FirstName)
	assert.Equal(t, "Identity", stored.LastName)
	assert.Equal(t, "新名", stored.JapaneseName)
	assert.Equal(t, "https://new.test/thumb.jpg", stored.ThumbURL)

	loadCredit := func(movieID string) models.MovieCredit {
		t.Helper()
		var credit models.MovieCredit
		require.NoError(t, db.Where("movie_content_id = ?", movieID).First(&credit).Error)
		return credit
	}

	gotLegacy := loadCredit("legacy-movie")
	assert.Equal(t, "Identity New", gotLegacy.CreditedName, "migration snapshot credits follow the rename")
	assert.Equal(t, "新名", gotLegacy.CreditedJapaneseName)

	gotSnapshot := loadCredit("snapshot-movie")
	assert.Equal(t, "Identity New", gotSnapshot.CreditedName, "movie-save snapshot credits follow the rename")

	gotReported := loadCredit("scrape-movie")
	assert.Equal(t, "Reported Alias", gotReported.CreditedName, "reported attribution is history and stays")
	assert.Equal(t, "報告名", gotReported.CreditedJapaneseName)

	gotOverridden := loadCredit("override-movie")
	assert.Equal(t, "Name Old", gotOverridden.CreditedName, "user overrides are never rewritten")
	assert.Equal(t, "Pinned Display", gotOverridden.OverrideName)
}

func TestRenameNameFieldsLeavesThumbnailUntouched(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	identity := models.Actress{FirstName: "Keep", LastName: "Thumb", ThumbURL: "https://keep.test/thumb.jpg", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&identity).Error)

	require.NoError(t, repos.ActressRepo.RenameNameFields(context.Background(), identity.ID, "Renamed", "Only", ""))

	var stored models.Actress
	require.NoError(t, db.First(&stored, identity.ID).Error)
	assert.Equal(t, "Renamed", stored.FirstName)
	assert.Equal(t, "https://keep.test/thumb.jpg", stored.ThumbURL, "name-only renames must not clear the thumbnail")
}
