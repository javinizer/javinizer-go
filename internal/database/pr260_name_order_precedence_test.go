package database

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Two verified actresses can legitimately carry swapped romanized parts. An
// exact scrape must select its exact identity instead of matching both rows and
// degrading into an ambiguous candidate.
func TestExactNameOrderWinsOverSwappedIdentities(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()

	exact := models.Actress{FirstName: "Li", LastName: "Na", Verified: true, Origin: ActressOriginUser}
	swapped := models.Actress{FirstName: "Na", LastName: "Li", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&exact).Error)
	require.NoError(t, db.Create(&swapped).Error)

	movie := creditMovie("order-exact", []models.MovieCredit{{
		CreditedName: "Na Li",
		Scraped:      models.Actress{FirstName: "Li", LastName: "Na"},
	}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(ctx, movie, nil, nil)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	assert.Equal(t, exact.ID, saved.Credits[0].ActressID, "the exact field order must win")

	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	assert.Equal(t, int64(2), count, "no candidate identity for an unambiguous scrape")
}

// When no identity matches the exact order, the swapped order still resolves.
func TestSwappedNameOrderStillMatchesWhenUnique(t *testing.T) {
	db := newCreditTestDB(t)
	repos := db.Repositories()
	ctx := context.Background()

	only := models.Actress{FirstName: "Li", LastName: "Na", Verified: true, Origin: ActressOriginUser}
	require.NoError(t, db.Create(&only).Error)

	movie := creditMovie("order-swapped", []models.MovieCredit{{
		CreditedName: "Li Na",
		Scraped:      models.Actress{FirstName: "Na", LastName: "Li"},
	}})
	saved, err := repos.MovieRepo.UpsertWithTranslations(ctx, movie, nil, nil)
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	assert.Equal(t, only.ID, saved.Credits[0].ActressID, "a unique swapped match still resolves")

	var count int64
	require.NoError(t, db.Model(&models.Actress{}).Count(&count).Error)
	assert.Equal(t, int64(1), count)
}
