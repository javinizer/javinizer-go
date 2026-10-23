package javlibrary

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

const overrideCast = `<span class="star"><a href="vl_star.php?s=aa6sc" rel="tag">Yorimoto Shiori</a></span>`

func overrideIdentityDB(t *testing.T) *database.DB {
	t.Helper()
	db, err := database.New(&database.Config{Type: "sqlite", DSN: ":memory:", LogLevel: "silent"})
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, db.Close()) })
	require.NoError(t, db.RunMigrationsOnStartup(t.Context()))
	return db
}

func overriddenActress(t *testing.T) models.Actress {
	t.Helper()
	info := (&scraper{settings: nameOverrideSettings()}).extractActresses(overrideCast)[0]
	require.Equal(t, "Shiori", info.FirstName)
	return models.Actress{FirstName: info.FirstName, LastName: info.LastName}
}

func TestCastNameOverrideIdentityOwners(t *testing.T) {
	for _, tc := range []struct {
		name                        string
		canonical, legacy, verified bool
	}{
		{"canonical", true, false, true},
		{"legacy reversed", false, true, true},
		{"competing verified", true, true, true},
		{"legacy candidate", false, true, false},
		{"new candidate", false, false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			db := overrideIdentityDB(t)
			canonical := models.Actress{FirstName: "Shiori", LastName: "Yorimoto", Verified: true, Origin: database.ActressOriginUser}
			legacy := models.Actress{FirstName: "Yorimoto", LastName: "Shiori", Verified: tc.verified, Origin: database.ActressOriginUser}
			if tc.canonical {
				require.NoError(t, db.Create(&canonical).Error)
			}
			if tc.legacy {
				require.NoError(t, db.Create(&legacy).Error)
			}
			var expected uint
			if tc.canonical {
				expected = canonical.ID
			} else if tc.legacy {
				expected = legacy.ID
			}
			for i := 0; i < 2; i++ {
				incoming := overriddenActress(t)
				found, outcome, err := database.ResolveActressIdentityTx(db.DB, &incoming)
				require.NoError(t, err)
				if expected == 0 {
					expected = found.ID
				}
				require.Equal(t, expected, found.ID)
				if tc.verified {
					require.Equal(t, database.ResolutionMatched, outcome)
				} else {
					require.Equal(t, database.ResolutionCandidateLinked, outcome)
				}
				require.Equal(t, tc.verified, found.Verified, "override must not promote candidate")
			}
			for _, before := range []models.Actress{canonical, legacy} {
				if before.ID == 0 {
					continue
				}
				var after models.Actress
				require.NoError(t, db.First(&after, before.ID).Error)
				require.Equal(t, before.FirstName, after.FirstName)
				require.Equal(t, before.LastName, after.LastName)
				require.Equal(t, before.Verified, after.Verified)
				require.Equal(t, before.DMMID, after.DMMID)
			}
		})
	}
}

func TestCastNameOverrideRescrapePreservesCredit(t *testing.T) {
	db := overrideIdentityDB(t)
	canonical := models.Actress{FirstName: "Shiori", LastName: "Yorimoto", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&canonical).Error)
	upserter := database.NewMovieUpserter(database.NewMovieRepository(db))
	makeMovie := func(a models.Actress) *models.Movie {
		return &models.Movie{ContentID: "override-383", ID: "OVERRIDE-383", Title: "Fixture", Credits: []models.MovieCredit{{
			Scraped: a, CreditedName: a.FullName(), Source: "javlibrary", Origin: string(models.CreditOriginScrape),
		}}}
	}
	saved, err := upserter.Upsert(t.Context(), makeMovie(models.Actress{FirstName: "Yorimoto", LastName: "Shiori"}))
	require.NoError(t, err)
	require.Len(t, saved.Credits, 1)
	originalID := saved.Credits[0].ID
	require.NoError(t, db.Model(&models.MovieCredit{}).Where("id = ?", originalID).Updates(map[string]any{
		"override_name": "Pinned", "user_override": true, "order_pinned": true, "order_index": 7,
	}).Error)
	for i := 0; i < 2; i++ {
		saved, err = upserter.Upsert(t.Context(), makeMovie(overriddenActress(t)))
		require.NoError(t, err)
		require.Len(t, saved.Credits, 1)
		credit := saved.Credits[0]
		require.Equal(t, originalID, credit.ID)
		require.Equal(t, canonical.ID, credit.ActressID)
		require.True(t, credit.UserOverride)
		require.Equal(t, "Pinned", credit.OverrideName)
		require.True(t, credit.OrderPinned)
		require.Equal(t, 7, credit.OrderIndex)
	}
	var stored models.Actress
	require.NoError(t, db.First(&stored, canonical.ID).Error)
	require.Equal(t, canonical, stored)
}

func TestCastNameOverrideDoesNotStealCompetingUserCredit(t *testing.T) {
	db := overrideIdentityDB(t)
	exact := models.Actress{FirstName: "Shiori", LastName: "Yorimoto", Verified: true, Origin: database.ActressOriginUser}
	reversed := models.Actress{FirstName: "Yorimoto", LastName: "Shiori", Verified: true, Origin: database.ActressOriginUser}
	require.NoError(t, db.Create(&exact).Error)
	require.NoError(t, db.Create(&reversed).Error)
	movie := models.Movie{ContentID: "override-owned", ID: "OVERRIDE-OWNED", Title: "Fixture"}
	require.NoError(t, db.Create(&movie).Error)
	owned := models.MovieCredit{MovieContentID: movie.ContentID, ActressID: reversed.ID, Origin: string(models.CreditOriginUser), UserOverride: true, OverrideName: "User choice", OrderPinned: true, OrderIndex: 9}
	require.NoError(t, db.Create(&owned).Error)
	upserter := database.NewMovieUpserter(database.NewMovieRepository(db))
	for i := 0; i < 2; i++ {
		incoming := overriddenActress(t)
		result, err := upserter.Upsert(t.Context(), &models.Movie{ContentID: movie.ContentID, ID: movie.ID, Title: movie.Title, Credits: []models.MovieCredit{{Scraped: incoming, CreditedName: incoming.FullName(), Source: "javlibrary"}}})
		require.NoError(t, err)
		require.Len(t, result.Credits, 2, "keep the user's competing credit rather than silently relink it")
		var after models.MovieCredit
		require.NoError(t, db.First(&after, owned.ID).Error)
		require.Equal(t, owned.ActressID, after.ActressID)
		require.Equal(t, owned.OverrideName, after.OverrideName)
		require.Equal(t, owned.Origin, after.Origin)
		require.True(t, after.UserOverride)
		require.True(t, after.OrderPinned)
		require.Equal(t, owned.OrderIndex, after.OrderIndex)
	}
}
