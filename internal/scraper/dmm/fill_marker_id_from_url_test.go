package dmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// fillMarkerIDFromURL derives a direct URL scrape's canonical display spelling
// from the URL cid when the page publishes no 品番 row — but only for H/HD
// marker cids, whose number matches the display number. AI marker cids do not
// encode the display number, so they never derive. These direct unit tests
// pin every guard of the fill (ScrapeURL-flow coverage lives in
// parse_html_scrapeurl_fill_test.go).
func TestFillMarkerIDFromURL(t *testing.T) {
	t.Run("h marker cid fills the canonical display id", func(t *testing.T) {
		res := &models.ScraperResult{}
		fillMarkerIDFromURL(res, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/")
		assert.Equal(t, "RCT-00156H", res.ID, "the empty page id is filled from the URL cid's canonical spelling")
	})

	t.Run("ai marker cid leaves the id empty", func(t *testing.T) {
		res := &models.ScraperResult{}
		fillMarkerIDFromURL(res, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=dv00899ai/")
		assert.Empty(t, res.ID, "AI cid numbers are unrelated to display numbers (dv00899ai maps to DV-818AI), so only the page 品番 may publish the id")
	})

	t.Run("non-marker cid leaves the id empty", func(t *testing.T) {
		res := &models.ScraperResult{}
		fillMarkerIDFromURL(res, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=abp1234/")
		assert.Empty(t, res.ID, "a cid without a remaster marker must not fabricate a display id")
	})

	t.Run("url without an extractable cid leaves the id empty", func(t *testing.T) {
		res := &models.ScraperResult{}
		fillMarkerIDFromURL(res, "https://www.dmm.co.jp/digital/videoa/-/list/=/sort=rank/")
		assert.Empty(t, res.ID, "no cid in the URL means nothing to derive the id from")
	})

	t.Run("existing id wins over the fill", func(t *testing.T) {
		res := &models.ScraperResult{ID: "DV-818AI"}
		fillMarkerIDFromURL(res, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/")
		assert.Equal(t, "DV-818AI", res.ID, "a page-parsed id is never overwritten by the URL-derived spelling")
	})

	t.Run("nil result is a no-op", func(t *testing.T) {
		fillMarkerIDFromURL(nil, "https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/")
	})
}
