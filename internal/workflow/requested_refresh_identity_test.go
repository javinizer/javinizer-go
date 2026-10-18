package workflow

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestForceRefreshBindsRequestedIdentityWhenSourceChangesDisplayID(t *testing.T) {
	for _, returnedID := range []string{"DIFFERENT-101", ""} {
		for _, requestedID := range []string{"REFRESH-101", refreshContentID, "  REFRESH-101  "} {
			for _, existing := range []bool{false, true} {
				for _, deferred := range []bool{false, true} {
					t.Run(returnedID+"/"+requestedID+"/"+map[bool]string{false: "new", true: "existing"}[existing]+"/"+map[bool]string{false: "inline", true: "deferred"}[deferred], func(t *testing.T) {
						ctx := context.Background()
						f := newDurableRefreshFixture(t)
						repo := database.NewMovieRepository(f.db)
						var other *models.Movie
						if existing {
							var err error
							other, err = repo.Upsert(ctx, &models.Movie{ID: "OTHER-101", ContentID: "different00101", Title: "Other metadata", Credits: []models.MovieCredit{{Scraped: f.actors[0], CreditedName: "Other choice", OverrideName: "Other alias", UserOverride: true, Origin: "user"}}})
							require.NoError(t, err)
						}
						source := &durableRefreshSource{result: refreshSourceResult()}
						source.result.ID = returnedID
						source.result.ContentID = "different00101"
						source.result.Title = "Rejected metadata"
						cmd := scrape.ScrapeCmd{MovieID: requestedID, ForceRefresh: true, SkipPersist: deferred}
						res, meta, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, cmd)
						if deferred {
							require.NoError(t, err)
							_, err = database.UpsertWithDisplayTitle(ctx, repo, res.Movie.Clone(), nil, nil, meta.RenderDisplayTitle)
						}
						require.ErrorIs(t, err, database.ErrMovieContentIDConflict)
						assert.Equal(t, 1, source.calls)
						f.reopen(t)
						assert.Equal(t, f.before, f.snapshot(t))
						repo = database.NewMovieRepository(f.db)
						after, err := repo.FindByContentID(ctx, "different00101")
						if existing {
							require.NoError(t, err)
							assert.Equal(t, other, after)
						} else {
							assert.True(t, database.IsNotFound(err))
						}
						source.calls = 0
						cached, _, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, scrape.ScrapeCmd{MovieID: "REFRESH-101"})
						require.NoError(t, err)
						assert.True(t, cached.Cached)
						assert.Zero(t, source.calls)
						assert.Equal(t, refreshContentID, cached.Movie.ContentID)
						assert.Equal(t, "Original title", cached.Movie.Title)
					})
				}
			}
		}
	}
}

func TestForceRefreshMissingContentIdentityBoundary(t *testing.T) {
	for _, tc := range []struct {
		name, requested, returned string
		known, conflict, invalid  bool
	}{
		{name: "known_same_display", requested: "REFRESH-101", returned: "REFRESH-101", known: true},
		{name: "known_content_key", requested: refreshContentID, returned: "REFRESH-101", known: true},
		{name: "changed_unowned_display", requested: "REFRESH-101", returned: "NEW-101", known: true, conflict: true},
		{name: "changed_owned_display", requested: "REFRESH-101", returned: "OTHER-101", known: true, conflict: true},
		{name: "known_both_blank", requested: "REFRESH-101", known: true, invalid: true},
		{name: "new_both_blank", requested: "NEW-101", invalid: true},
		{name: "new_manual_request", requested: "NEW-101", returned: "NEW-101"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			ctx := context.Background()
			f := newDurableRefreshFixture(t)
			repo := database.NewMovieRepository(f.db)
			other, err := repo.Upsert(ctx, &models.Movie{ID: "OTHER-101", ContentID: "other00101", Title: "Other metadata", Credits: []models.MovieCredit{{Scraped: f.actors[0], CreditedName: "Other choice", OverrideName: "Other alias", UserOverride: true, Origin: "user"}}})
			require.NoError(t, err)
			source := &durableRefreshSource{result: refreshSourceResult()}
			source.result.ID = tc.returned
			source.result.ContentID = ""
			source.result.Title = "Missing content metadata"
			res, _, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, scrape.ScrapeCmd{MovieID: tc.requested, ForceRefresh: true})
			switch {
			case tc.conflict:
				require.ErrorIs(t, err, database.ErrMovieContentIDConflict)
			case tc.invalid:
				require.ErrorContains(t, err, "content_id is required")
			default:
				require.NoError(t, err)
				assert.Equal(t, "Missing content metadata", res.Movie.Title)
			}
			f.reopen(t)
			afterOther, err := database.NewMovieRepository(f.db).FindByContentID(ctx, other.ContentID)
			require.NoError(t, err)
			assert.Equal(t, other, afterOther)
			if tc.conflict || tc.invalid || !tc.known {
				assert.Equal(t, f.before, f.snapshot(t))
			} else {
				f.assertChoices(t, f.snapshot(t), "Movie Alias")
			}
			if !tc.known && !tc.invalid {
				assert.Equal(t, "new101", res.Movie.ContentID)
				for _, credit := range res.Movie.Credits {
					assert.False(t, credit.UserOverride)
					assert.False(t, credit.Suppressed)
					assert.Empty(t, credit.OverrideName)
				}
			}
		})
	}
}

func TestForceRefreshRequestIdentityRecheckedBeforeDeferredCommit(t *testing.T) {
	ctx := context.Background()
	f := newDurableRefreshFixture(t)
	repo := database.NewMovieRepository(f.db)
	other, err := repo.Upsert(ctx, &models.Movie{ID: "OTHER-101", ContentID: "other00101", Title: "Other metadata"})
	require.NoError(t, err)
	source := &durableRefreshSource{result: refreshSourceResult()}
	source.result.ID = "CORRECTED-101"
	res, meta, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, scrape.ScrapeCmd{MovieID: "REFRESH-101", ForceRefresh: true, SkipPersist: true})
	require.NoError(t, err)
	require.NoError(t, f.db.Model(&models.Movie{}).Where("content_id = ?", refreshContentID).Update("id", "FORMER-101").Error)
	require.NoError(t, f.db.Model(&models.Movie{}).Where("content_id = ?", other.ContentID).Update("id", "REFRESH-101").Error)
	before := f.snapshot(t)
	other, err = repo.FindByContentID(ctx, other.ContentID)
	require.NoError(t, err)
	_, err = database.UpsertWithDisplayTitle(ctx, repo, res.Movie.Clone(), nil, nil, meta.RenderDisplayTitle)
	require.ErrorIs(t, err, database.ErrMovieContentIDConflict)
	assert.Equal(t, before, f.snapshot(t))
	after, err := repo.FindByContentID(ctx, other.ContentID)
	require.NoError(t, err)
	assert.Equal(t, other, after)
}

func TestForceRefreshMissingContentRejectsChangedRequestOwnership(t *testing.T) {
	for _, deferred := range []bool{false, true} {
		t.Run(map[bool]string{false: "during_scrape", true: "before_deferred_commit"}[deferred], func(t *testing.T) {
			ctx := context.Background()
			f := newDurableRefreshFixture(t)
			repo := database.NewMovieRepository(f.db)
			_, err := repo.Upsert(ctx, &models.Movie{ID: "OTHER-101", ContentID: "other00101", Title: "Other metadata", Credits: []models.MovieCredit{{Scraped: f.actors[0], CreditedName: "Other choice", OverrideName: "Other alias", UserOverride: true, Origin: "user"}}})
			require.NoError(t, err)
			var before durableRefreshSnapshot
			var other *models.Movie
			changeOwner := func(context.Context) error {
				require.NoError(t, f.db.Model(&models.Movie{}).Where("content_id = ?", refreshContentID).Update("id", "FORMER-101").Error)
				require.NoError(t, f.db.Model(&models.Movie{}).Where("content_id = ?", "other00101").Update("id", "REFRESH-101").Error)
				before = f.snapshot(t)
				other, err = repo.FindByContentID(ctx, "other00101")
				require.NoError(t, err)
				return nil
			}
			source := &durableRefreshSource{result: refreshSourceResult()}
			source.result.ContentID = ""
			source.result.Title = "Wrong-owner metadata"
			if !deferred {
				source.before = changeOwner
			}
			res, meta, err := durableRefreshWorkflow(f.db, source).Scrape(ctx, scrape.ScrapeCmd{MovieID: "REFRESH-101", ForceRefresh: true, SkipPersist: deferred})
			if deferred {
				require.NoError(t, err)
				require.Equal(t, refreshContentID, res.Movie.RefreshExpectedContentID)
				require.NoError(t, changeOwner(ctx))
				_, err = database.UpsertWithDisplayTitle(ctx, repo, res.Movie.Clone(), nil, nil, meta.RenderDisplayTitle)
			}
			require.ErrorIs(t, err, database.ErrMovieContentIDConflict)
			f.reopen(t)
			assert.Equal(t, before, f.snapshot(t))
			after, err := database.NewMovieRepository(f.db).FindByContentID(ctx, other.ContentID)
			require.NoError(t, err)
			assert.Equal(t, other, after)
		})
	}
}
