package dmm

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// A T-series AI query clears the CID-derived ID (AI cids do not encode the
// display number), so the page 品番 is the only identity source. The compacted
// page value of T-28123-AI reads as t28/123 under the strict tail split, so
// pageRemasterDisplayID must apply the prefix-free T/T28 disambiguation
// instead of rejecting it and leaving an empty ID.
func TestPageRemasterDisplayIDT28AISeries(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(
		"<html><table><tr><td>品番：</td><td>T-28123-AI</td></tr></table></html>"))
	assert.NoError(t, err)
	assert.Equal(t, "T-28123AI", pageRemasterDisplayID(doc, "t", "ai", ""))
	assert.Equal(t, "T-28123H", pageRemasterDisplayID(doc2(t, "T-28123-HD"), "t", "h", ""))
	assert.Equal(t, "T28-123H", pageRemasterDisplayID(doc2(t, "T28-123-HD"), "t28", "h", ""))

	s := &scraper{}
	result := &models.ScraperResult{}
	s.extractIdentifiers(result, doc,
		"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=t28123ai/", true)
	assert.Equal(t, "T-28123AI", result.ID)
	assert.Equal(t, "t28123ai", result.ContentID)

	// A genuinely different series on the page stays rejected.
	assert.Equal(t, "", pageRemasterDisplayID(doc2(t, "DV-818-AI"), "t", "ai", ""))
}

func doc2(t *testing.T, display string) *goquery.Document {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(
		"<html><table><tr><td>品番：</td><td>" + display + "</td></tr></table></html>"))
	assert.NoError(t, err)
	return doc
}
