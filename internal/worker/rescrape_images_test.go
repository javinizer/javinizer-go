package worker

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Tests below assert the EXPECTED rescrape image-refresh semantics across the
// full edge-case matrix. They drive the fix: a rescrape should bring in the
// scraper's poster/cover for the resolved content, not preserve the existing
// movie's (possibly different-content) images via prefer-nfo.
func existingMovie() *models.Movie {
	m := &models.Movie{ID: "OLD-001", Title: "Existing Title"}
	m.Poster.CoverURL = "https://old.invalid/cover.jpg"
	m.Poster.PosterURL = "https://old.invalid/poster.jpg"
	return m
}

func scrapedNewID() *models.Movie {
	m := &models.Movie{ID: "NEW-001", Title: "Scraped Title"}
	m.Poster.CoverURL = "https://new.invalid/cover.jpg"
	m.Poster.PosterURL = "https://new.invalid/poster.jpg"
	return m
}

// === Content-id change (rescrape resolved a different/corrected ID) ===

func TestRescrapeImage_ContentIDChange_PreferNFO(t *testing.T) {
	merged := mergeRescrapeMovie(existingMovie(), scrapedNewID(), workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.CoverURL)
	assert.Equal(t, "https://new.invalid/poster.jpg", merged.Poster.PosterURL)
	assert.Equal(t, "NEW-001", merged.ID)
}

func TestRescrapeImage_ContentIDChange_PreferScraper(t *testing.T) {
	merged := mergeRescrapeMovie(existingMovie(), scrapedNewID(), workflow.MergeOptions{
		ScalarStrategy: nfo.PreferScraper, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.CoverURL)
	assert.Equal(t, "https://new.invalid/poster.jpg", merged.Poster.PosterURL)
}

func TestRescrapeImage_ContentIDChange_ScrapedHasNoCover(t *testing.T) {
	scraped := scrapedNewID()
	scraped.Poster.CoverURL = ""
	scraped.Poster.PosterURL = ""
	merged := mergeRescrapeMovie(existingMovie(), scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	// New content has no images -> must NOT keep the old (different) content's images.
	assert.Equal(t, "", merged.Poster.CoverURL)
	assert.Equal(t, "", merged.Poster.PosterURL)
}

// === Same content-id (refresh rescrape) ===

func TestRescrapeImage_SameID_PreferScraper_Refreshes(t *testing.T) {
	scraped := &models.Movie{ID: "OLD-001", Title: "Scraped Title"}
	scraped.Poster.CoverURL = "https://new.invalid/cover.jpg"
	scraped.Poster.PosterURL = "https://new.invalid/poster.jpg"
	merged := mergeRescrapeMovie(existingMovie(), scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferScraper, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.CoverURL)
	assert.Equal(t, "https://new.invalid/poster.jpg", merged.Poster.PosterURL)
}

func TestRescrapeImage_SameID_PreferNFO_ScrapedProvidesImage(t *testing.T) {
	scraped := &models.Movie{ID: "OLD-001", Title: "Scraped Title"}
	scraped.Poster.CoverURL = "https://new.invalid/cover.jpg"
	scraped.Poster.PosterURL = "https://new.invalid/poster.jpg"
	merged := mergeRescrapeMovie(existingMovie(), scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	// A rescrape's purpose is to re-fetch; images should refresh from the scraper.
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.CoverURL)
	assert.Equal(t, "https://new.invalid/poster.jpg", merged.Poster.PosterURL)
}

func TestRescrapeImage_SameID_PreferNFO_ScrapedEmptyKeepsExisting(t *testing.T) {
	scraped := &models.Movie{ID: "OLD-001", Title: "Scraped Title"}
	merged := mergeRescrapeMovie(existingMovie(), scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	// Scraper returned no image -> keep existing (don't wipe a valid image).
	assert.Equal(t, "https://old.invalid/cover.jpg", merged.Poster.CoverURL)
	assert.Equal(t, "https://old.invalid/poster.jpg", merged.Poster.PosterURL)
}

func TestRescrapeImage_SameID_PreferNFO_ExistingEmptyFillsFromScraper(t *testing.T) {
	existing := &models.Movie{ID: "OLD-001", Title: "Existing"}
	scraped := &models.Movie{ID: "OLD-001", Title: "Scraped"}
	scraped.Poster.CoverURL = "https://new.invalid/cover.jpg"
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.CoverURL)
}

// === CroppedPosterURL always follows the scraper ===

func TestRescrapeImage_CroppedPosterURL_AlwaysScraped(t *testing.T) {
	existing := existingMovie()
	existing.Poster.CroppedPosterURL = "https://old.invalid/cropped.jpg"
	scraped := scrapedNewID()
	scraped.Poster.CroppedPosterURL = "https://new.invalid/cropped.jpg"
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/cropped.jpg", merged.Poster.CroppedPosterURL)
}

// === Reset baseline (Original*) tracks the scraper, not the prior content ===
// Original* is the revert target the review UI restores on Reset. A rescrape
// establishes a fresh scraper baseline, so Original* must follow the scraped
// movie — never carrying the previous content's URL forward across a
// content-id change (the reported bug).

func TestRescrapeImage_OriginalCoverURL_ResetToScraperOnContentIDChange(t *testing.T) {
	existing := existingMovie()
	existing.Poster.OriginalCoverURL = "https://orig.invalid/cover.jpg" // prior content's baseline
	merged := mergeRescrapeMovie(existing, scrapedNewID(), workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.OriginalCoverURL)
}

func TestRescrapeImage_OriginalPosterURL_ResetToScraperOnContentIDChange(t *testing.T) {
	existing := existingMovie()
	existing.Poster.OriginalPosterURL = "https://orig.invalid/poster.jpg"
	existing.Poster.OriginalCroppedPosterURL = "https://orig.invalid/cropped.jpg"
	scraped := scrapedNewID()
	scraped.Poster.CroppedPosterURL = "https://new.invalid/cropped.jpg"
	scraped.Poster.ShouldCropPoster = true
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/poster.jpg", merged.Poster.OriginalPosterURL)
	assert.Equal(t, "https://new.invalid/cropped.jpg", merged.Poster.OriginalCroppedPosterURL)
	if merged.Poster.OriginalShouldCropPoster == nil || !*merged.Poster.OriginalShouldCropPoster {
		t.Fatal("OriginalShouldCropPoster should be the scraped true baseline")
	}
}

func TestRescrapeImage_OriginalCoverURL_SameIDTracksScraper(t *testing.T) {
	existing := existingMovie()
	existing.Poster.OriginalCoverURL = "https://orig.invalid/cover.jpg"
	scraped := &models.Movie{ID: "OLD-001", Title: "Scraped"}
	scraped.Poster.CoverURL = "https://new.invalid/cover.jpg"
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.OriginalCoverURL)
}

func TestRescrapeImage_OriginalCoverURL_EmptyWhenScraperHasNone(t *testing.T) {
	existing := existingMovie()
	existing.Poster.OriginalCoverURL = "https://orig.invalid/cover.jpg"
	scraped := &models.Movie{ID: "NEW-001", Title: "Scraped"} // no cover
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	// Scraper found no cover for the new content -> baseline is empty, never
	// the previous content's URL.
	assert.Equal(t, "", merged.Poster.OriginalCoverURL)
}

// Asymmetric: scraper provides a poster but no cover on a same-id rescrape.
// The cover is preserved (not wiped), the poster refreshes.
func TestRescrapeImage_Asymmetric_PosterOnly(t *testing.T) {
	existing := existingMovie()
	scraped := &models.Movie{ID: "OLD-001", Title: "Scraped"}
	scraped.Poster.PosterURL = "https://new.invalid/poster.jpg"
	scraped.Poster.CoverURL = ""
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/poster.jpg", merged.Poster.PosterURL)
	assert.Equal(t, "https://old.invalid/cover.jpg", merged.Poster.CoverURL)
}

// Asymmetric on a content-id change: scraper provides a cover but no poster.
// The poster is cleared (different content), the cover refreshes.
func TestRescrapeImage_Asymmetric_ContentIDChange_CoverOnly(t *testing.T) {
	existing := existingMovie()
	scraped := &models.Movie{ID: "NEW-001", Title: "Scraped"}
	scraped.Poster.PosterURL = ""
	scraped.Poster.CoverURL = "https://new.invalid/cover.jpg"
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "", merged.Poster.PosterURL)
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.CoverURL)
}

// Whitespace-only scraper URLs are treated as empty (don't set whitespace).
func TestRescrapeImage_WhitespaceOnlyScraperURL(t *testing.T) {
	existing := existingMovie()
	scraped := &models.Movie{ID: "OLD-001", Title: "Scraped"}
	scraped.Poster.CoverURL = "   "
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://old.invalid/cover.jpg", merged.Poster.CoverURL)
}

// establishScrapedBaseline is the non-merge (wholesale-replace) rescrape path's
// baseline setter: the scraped movie carries no Original*, so the baseline is
// built from its own poster fields.
func TestEstablishScrapedBaseline_FromScrapedOwnFields(t *testing.T) {
	scraped := &models.Movie{ID: "NEW-001"}
	scraped.Poster.PosterURL = "https://new.invalid/poster.jpg"
	scraped.Poster.CroppedPosterURL = "https://new.invalid/cropped.jpg"
	scraped.Poster.CoverURL = "https://new.invalid/cover.jpg"
	scraped.Poster.ShouldCropPoster = true
	establishScrapedBaseline(scraped, scraped)
	assert.Equal(t, "https://new.invalid/poster.jpg", scraped.Poster.OriginalPosterURL)
	assert.Equal(t, "https://new.invalid/cropped.jpg", scraped.Poster.OriginalCroppedPosterURL)
	assert.Equal(t, "https://new.invalid/cover.jpg", scraped.Poster.OriginalCoverURL)
	if scraped.Poster.OriginalShouldCropPoster == nil || !*scraped.Poster.OriginalShouldCropPoster {
		t.Fatal("OriginalShouldCropPoster should mirror scraped ShouldCropPoster")
	}
}

func TestEstablishScrapedBaseline_NilSafe(t *testing.T) {
	assert.NotPanics(t, func() { establishScrapedBaseline(nil, &models.Movie{}) })
	assert.NotPanics(t, func() { establishScrapedBaseline(&models.Movie{}, nil) })
}

// establishScrapedBaseline trims whitespace-only URLs so a whitespace scraper
// value doesn't become a non-empty baseline that falsely enables Reset.
func TestEstablishScrapedBaseline_TrimsWhitespace(t *testing.T) {
	source := &models.Movie{ID: "NEW-001"}
	source.Poster.PosterURL = "  https://new.invalid/poster.jpg  "
	source.Poster.CroppedPosterURL = "   "
	source.Poster.CoverURL = "  "
	target := &models.Movie{}
	establishScrapedBaseline(target, source)
	assert.Equal(t, "https://new.invalid/poster.jpg", target.Poster.OriginalPosterURL)
	assert.Equal(t, "", target.Poster.OriginalCroppedPosterURL, "whitespace-only should trim to empty")
	assert.Equal(t, "", target.Poster.OriginalCoverURL, "whitespace-only should trim to empty")
}

// Padded scraper URLs on a same-id rescrape are trimmed for both the display
// field and the baseline, so a whitespace-padded scraper value does not leave
// display != baseline and spuriously enable Reset.
func TestRescrapeImage_SameID_PaddedScraperURLTrimmed(t *testing.T) {
	existing := existingMovie()
	scraped := &models.Movie{ID: "OLD-001", Title: "Scraped"}
	scraped.Poster.CoverURL = "  https://new.invalid/cover.jpg  "
	scraped.Poster.PosterURL = "  https://new.invalid/poster.jpg  "
	merged := mergeRescrapeMovie(existing, scraped, workflow.MergeOptions{
		ScalarStrategy: nfo.PreferNFO, ArrayStrategy: true,
	}, "file.mp4")
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.CoverURL)
	assert.Equal(t, "https://new.invalid/poster.jpg", merged.Poster.PosterURL)
	assert.Equal(t, "https://new.invalid/cover.jpg", merged.Poster.OriginalCoverURL)
	assert.Equal(t, "https://new.invalid/poster.jpg", merged.Poster.OriginalPosterURL)
}

// When the scraper found no poster image, establishScrapedBaseline leaves
// OriginalShouldCropPoster nil (not a non-nil false) so the frontend falls
// back to the current field — matching the empty-URL fallback. A non-nil
// false would combine with an edited currentMovie to spuriously enable Reset.
func TestEstablishScrapedBaseline_EmptyPoster_LeavesCropBaselineNil(t *testing.T) {
	target := &models.Movie{Poster: models.PosterState{ShouldCropPoster: true}}
	source := &models.Movie{Poster: models.PosterState{ShouldCropPoster: false}}
	establishScrapedBaseline(target, source)
	assert.Equal(t, "", target.Poster.OriginalPosterURL)
	assert.Equal(t, "", target.Poster.OriginalCroppedPosterURL)
	assert.Nil(t, target.Poster.OriginalShouldCropPoster, "crop baseline must be nil when no poster baseline exists")
	assert.Equal(t, "", target.Poster.OriginalCoverURL)
}

// A scraper with a poster image anchors the crop baseline, mirroring the
// scraped ShouldCropPoster so Reset reflects the rescrape's crop intent.
func TestEstablishScrapedBaseline_WithPoster_AnchorsCropBaseline(t *testing.T) {
	target := &models.Movie{}
	source := &models.Movie{Poster: models.PosterState{PosterURL: "https://x.invalid/p.jpg", ShouldCropPoster: true}}
	establishScrapedBaseline(target, source)
	require.NotNil(t, target.Poster.OriginalShouldCropPoster)
	assert.True(t, *target.Poster.OriginalShouldCropPoster)
}
