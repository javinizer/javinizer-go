package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestUpsertDropsCreditsWithoutIdentityEvidence(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	movie := creditMovie("nameless-credit", []models.MovieCredit{{ReportedThumbURL: "https://example.test/only-thumb.jpg"}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, saved.Credits, "a credit with no name evidence must not be persisted")

	var credits int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&credits).Error)
	assert.Equal(t, int64(0), credits)
	var actresses int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&actresses).Error)
	assert.Equal(t, int64(0), actresses, "no nameless ghost identity")
}

func TestUpsertDropsNamelessActressEntries(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()

	movie := &models.Movie{
		ContentID: "nameless-actress", ID: "nameless-actress", Title: "Nameless Entry",
		Actresses: []models.Actress{{ThumbURL: "https://example.test/only-thumb.jpg"}},
	}
	saved, err := repos.MovieRepo.UpsertWithTranslations(context.Background(), movie, nil, nil)
	require.NoError(t, err)
	assert.Empty(t, saved.Credits)

	var credits int64
	require.NoError(t, db.Model(&models.MovieCredit{}).Count(&credits).Error)
	assert.Equal(t, int64(0), credits, "nameless entries must not create credits")
	var actresses int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&actresses).Error)
	assert.Equal(t, int64(0), actresses)
}
