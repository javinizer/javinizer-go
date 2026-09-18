package database

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestPR260SuppressionRestoreMatchesEveryCanonicalRepresentation(t *testing.T) {
	for i, tc := range []struct {
		name     string
		reported string
		wantOpen bool
	}{
		{name: "family given", reported: "Family Given"},
		{name: "given family", reported: "Given Family"},
		{name: "Japanese", reported: "日本名"},
		{name: "normalized", reported: "  fAmIlY   gIvEn  "},
		{name: "mismatch", reported: "Different Person", wantOpen: true},
		{name: "blank", reported: "   ", wantOpen: false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := newCreditTestDB(t)
			actress := models.Actress{FirstName: "Given", LastName: "Family", JapaneseName: "日本名", Aliases: "Scraped Name", Verified: true, Origin: ActressOriginUser}
			require.NoError(t, db.Create(&actress).Error)
			movieID := fmt.Sprintf("canonical-restore-%d", i)
			movie := models.Movie{ContentID: movieID, ID: movieID}
			require.NoError(t, db.Create(&movie).Error)
			credit := models.MovieCredit{MovieContentID: movieID, ActressID: actress.ID, CreditedName: tc.reported, Origin: string(models.CreditOriginUser)}
			require.NoError(t, db.Create(&credit).Error)

			service := NewCollisionService(db)
			require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
			require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, false))

			open, err := service.Collisions.ListOpenByMovie(context.Background(), movieID)
			require.NoError(t, err)
			if !tc.wantOpen {
				require.Empty(t, open)
				return
			}
			require.Len(t, open, 1)
			require.Equal(t, models.CreditFieldCreditedName, open[0].Field)
			require.Equal(t, tc.reported, open[0].ReportedValue)
			require.Equal(t, "日本名", open[0].CanonicalValue)
			require.False(t, open[0].UserPinned)
		})
	}
}

func TestPR260SuppressionRestoreDoesNotTrustScrapedAliasField(t *testing.T) {
	db := newCreditTestDB(t)
	actress := models.Actress{FirstName: "Given", LastName: "Family", JapaneseName: "日本名", Aliases: "Scraped Name", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "canonical-restore-scraped-alias", ID: "canonical-restore-scraped-alias"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Scraped Name", Origin: string(models.CreditOriginUser)}
	require.NoError(t, db.Create(&credit).Error)
	service := NewCollisionService(db)
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, true))
	require.NoError(t, service.SetCreditSuppressed(context.Background(), credit.ID, false))
	open, err := service.Collisions.ListOpenByMovie(context.Background(), movie.ContentID)
	require.NoError(t, err)
	require.Len(t, open, 1)
	require.Equal(t, "日本名", open[0].CanonicalValue)
}

func TestPR260ReviewRenameReconcilesEveryCurrentCanonicalRepresentation(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	actress := models.Actress{FirstName: "Old", LastName: "Name", JapaneseName: "日本名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&actress).Error)
	movie := models.Movie{ContentID: "canonical-rename", ID: "canonical-rename"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: actress.ID, CreditedName: "Given Family"}
	require.NoError(t, db.Create(&credit).Error)

	values := []struct {
		reported string
		pinned   bool
		matches  bool
	}{
		{reported: "Family Given", matches: true},
		{reported: " given   FAMILY ", matches: true},
		{reported: "日本名", matches: true},
		{reported: "Different Person"},
		{reported: ""},
		{reported: "Given Family", pinned: true, matches: true},
	}
	collisions := make([]models.CreditCollision, len(values))
	for i, value := range values {
		collisions[i] = models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: value.reported, CanonicalValue: "stale", Status: models.CollisionStatusOpen, UserPinned: value.pinned}
		require.NoError(t, db.Create(&collisions[i]).Error)
	}

	require.NoError(t, repo.RenameNameFields(context.Background(), actress.ID, "Given", "Family", "日本名"))
	for i, value := range values {
		require.NoError(t, db.First(&collisions[i], collisions[i].ID).Error)
		require.Equal(t, "日本名", collisions[i].CanonicalValue)
		if value.matches && !value.pinned {
			require.Equal(t, models.CollisionStatusResolved, collisions[i].Status)
			require.Equal(t, models.CollisionResolutionAdoptCanonical, collisions[i].Resolution)
		} else {
			require.Equal(t, models.CollisionStatusOpen, collisions[i].Status)
			require.Empty(t, collisions[i].Resolution)
		}
	}
}

func TestPR260MergeReconcilesEnglishRepresentationWithJapaneseDisplay(t *testing.T) {
	db := newCreditTestDB(t)
	repo := NewActressRepository(db)
	target := models.Actress{FirstName: "Old", LastName: "Name", JapaneseName: "日本名", Verified: true, Origin: ActressOriginUser}
	source := models.Actress{FirstName: "Given", LastName: "Family", JapaneseName: "日本名", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&target).Error)
	require.NoError(t, db.Create(&source).Error)
	movie := models.Movie{ContentID: "canonical-merge", ID: "canonical-merge"}
	require.NoError(t, db.Create(&movie).Error)
	credit := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: target.ID, CreditedName: "Given Family"}
	require.NoError(t, db.Create(&credit).Error)
	matching := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Given Family", CanonicalValue: "stale", Status: models.CollisionStatusOpen}
	other := models.CreditCollision{CreditID: credit.ID, MovieContentID: movie.ContentID, Field: models.CreditFieldCreditedName, ReportedValue: "Different Person", CanonicalValue: "stale", Status: models.CollisionStatusOpen}
	require.NoError(t, db.Create(&matching).Error)
	require.NoError(t, db.Create(&other).Error)

	_, err := repo.Merge(context.Background(), target.ID, source.ID, map[string]string{"first_name": "source", "last_name": "source"})
	require.NoError(t, err)
	require.NoError(t, db.First(&matching, matching.ID).Error)
	require.Equal(t, models.CollisionStatusResolved, matching.Status)
	require.Equal(t, models.CollisionResolutionAdoptCanonical, matching.Resolution)
	require.Equal(t, "日本名", matching.CanonicalValue)
	require.NoError(t, db.First(&other, other.ID).Error)
	require.Equal(t, models.CollisionStatusOpen, other.Status)
	require.Equal(t, "日本名", other.CanonicalValue)
}
