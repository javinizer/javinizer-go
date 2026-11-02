package worker

import (
	"fmt"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// This is the legacy-cast ambiguity from nfo.TestLegacyCastGateSelectionAndStrategy,
// not an injected merge error. The worker promises a wholesale replacement on
// merge failure; these tests exercise that contract, not live scraper behavior.
func rescrapeAmbiguityFixture(t *testing.T, selection string, images bool) (*models.Movie, *models.Movie) {
	t.Helper()
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/legacy.nfo", []byte(`<movie><title>Existing</title><id>TEST-001</id><actor><name>Unclassified Face</name></actor></movie>`), 0644))
	parsed, err := nfo.ParseNFO(fs, "/legacy.nfo")
	require.NoError(t, err)
	actress := models.Actress{ID: 1, FirstName: "Canonical", Verified: true}
	credit := models.MovieCredit{ID: 1, MovieContentID: "test001", ActressID: actress.ID, Actress: &actress, CreditedName: "Reported"}
	if selection == "origin" {
		credit.Origin = "user"
	} else {
		credit.UserOverride = true
		credit.OverrideName = "Selected"
	}
	scraped := &models.Movie{ID: "TEST-001", ContentID: "test001", Title: "Scraped", Actresses: []models.Actress{actress}, Credits: []models.MovieCredit{credit}}
	scraped.Poster.ShouldCropPoster = true
	staleCrop := false
	scraped.Poster.OriginalShouldCropPoster = &staleCrop
	scraped.Poster.OriginalPosterURL = "stale poster"
	scraped.Poster.OriginalCroppedPosterURL = "stale crop"
	scraped.Poster.OriginalCoverURL = "stale cover"
	if images {
		scraped.Poster.PosterURL = " https://images.invalid/poster.jpg "
		scraped.Poster.CroppedPosterURL = " https://images.invalid/crop.jpg "
		scraped.Poster.CoverURL = " https://images.invalid/cover.jpg "
	}
	return parsed.Movie, scraped
}

func requireRescrapeAmbiguity(t *testing.T, existing, scraped *models.Movie) {
	t.Helper()
	// Use independent clones: the prerequisite must not prepare or mutate the
	// actual inputs subsequently exercised by the worker.
	_, err := nfo.MergeMovieMetadataWithOptions(scraped.Clone(), existing.Clone(), nfo.PreferScraper, true)
	var ambiguity *nfo.CastAmbiguityError
	require.ErrorAs(t, err, &ambiguity)
}

func expectedRescrapeFallback(scraped *models.Movie) *models.Movie {
	want := scraped.Clone()
	want.Poster.OriginalPosterURL = strings.TrimSpace(scraped.Poster.PosterURL)
	want.Poster.OriginalCroppedPosterURL = strings.TrimSpace(scraped.Poster.CroppedPosterURL)
	want.Poster.OriginalCoverURL = strings.TrimSpace(scraped.Poster.CoverURL)
	want.Poster.OriginalShouldCropPoster = nil
	if want.Poster.OriginalPosterURL != "" || want.Poster.OriginalCroppedPosterURL != "" {
		crop := scraped.Poster.ShouldCropPoster
		want.Poster.OriginalShouldCropPoster = &crop
	}
	return want
}

func TestMergeRescrapeMovieAmbiguityPreservesFallbackContract(t *testing.T) {
	for _, selection := range []string{"origin", "override"} {
		for _, images := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/images=%v", selection, images), func(t *testing.T) {
				existing, scraped := rescrapeAmbiguityFixture(t, selection, images)
				requireRescrapeAmbiguity(t, existing, scraped)
				beforeExisting := existing.Clone()
				want := expectedRescrapeFallback(scraped)
				got := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{ScalarStrategy: nfo.PreferScraper, ArrayStrategy: true}, "file.mp4")
				require.Same(t, scraped, got, "failure returns the scraped payload, not a partial merge")
				require.Equal(t, want, got, "only the scraped image baseline may change")
				require.Equal(t, beforeExisting, existing, "failed merge must not mutate preserved metadata")
			})
		}
	}
}

func TestRescrapeAmbiguityFallbackCommitsOwnedMovie(t *testing.T) {
	for _, selection := range []string{"origin", "override"} {
		t.Run(selection, func(t *testing.T) {
			existing, scraped := rescrapeAmbiguityFixture(t, selection, true)
			requireRescrapeAmbiguity(t, existing, scraped)
			beforeExisting := existing.Clone()
			want := expectedRescrapeFallback(scraped)
			want.OriginalFileName = "file.mp4" // conversion records the source filename
			store := resultstore.New(1, []string{"file.mp4"})
			store.UpdateFileResult("file.mp4", &resultstore.MovieResult{ResultID: "stable", Movie: existing, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: "file.mp4", MovieID: existing.ID}})
			before, err := store.GetMovieResult("file.mp4")
			require.NoError(t, err)
			wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: scraped, Status: scrape.StatusCompleted}}
			inputs := rescrapePhaseInputs{WF: wf, ResultMap: store, Finder: store, JobID: models.NewJobID()}
			outcome, err := NewRescrapePhase().Rescrape(t.Context(), inputs, RescrapeCmd{MovieID: existing.ID, FilePath: "file.mp4", MergeEnabled: true, Merge: workflow.MergeOptions{ScalarStrategy: nfo.PreferScraper, ArrayStrategy: true}})
			require.NoError(t, err)
			require.NotNil(t, outcome)
			require.Equal(t, models.RescrapeStatusSuccess, outcome.Status)
			require.Equal(t, want, outcome.Movie)
			require.Equal(t, beforeExisting, existing)
			stored, err := store.GetMovieResult("file.mp4")
			require.NoError(t, err)
			require.Equal(t, before.Revision+1, stored.Revision)
			require.Equal(t, before.ResultID, stored.ResultID)
			require.Equal(t, want, stored.Movie)
			outcome.Movie.Credits[0].Actress.FirstName = "changed outside store"
			outcome.Movie.Credits[0].OverrideName = "changed selection"
			outcome.Movie.Actresses[0].FirstName = "changed cast"
			*outcome.Movie.Poster.OriginalShouldCropPoster = false
			again, err := store.GetMovieResult("file.mp4")
			require.NoError(t, err)
			require.Equal(t, stored, again, "response mutation must not alter committed nested state")
			require.Equal(t, beforeExisting, existing)
		})
	}
}
