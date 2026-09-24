package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertMatchesIdentityWhenRomanizedNameOrderIsReversed(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()

	existing := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&existing).Error)

	movie := &models.Movie{
		ContentID: "rev-legacy", ID: "rev-legacy", Title: "Reversed Name Order",
		Actresses: []models.Actress{{FirstName: "Hatano", LastName: "Yui"}},
	}
	saved, err := repos.MovieRepo.UpsertWithTranslations(ctx, movie, nil, nil)
	require.NoError(t, err)
	require.Len(t, saved.Actresses, 1)
	assert.Equal(t, existing.ID, saved.Actresses[0].ID, "reversed order must resolve to the existing identity")

	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	assert.Equal(t, int64(1), count, "no duplicate identity for a reversed romanized name")
}

func TestUpsertCreditsMatchIdentityWhenRomanizedNameOrderIsReversed(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()

	existing := models.Actress{FirstName: "Yui", LastName: "Hatano", JapaneseName: "波多野結衣", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&existing).Error)

	movie := creditMovie("rev-credit", []models.MovieCredit{{
		CreditedName: "Yui Hatano",
		Scraped:      models.Actress{FirstName: "Hatano", LastName: "Yui"},
	}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(ctx, movie, nil, nil)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	assert.Equal(t, existing.ID, saved.Credits[0].ActressID, "credit resolution must match a reversed romanized name")

	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	assert.Equal(t, int64(1), count, "no candidate created for the same person")
}
