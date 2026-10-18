package dmm

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestExtractRemasterContentIDCandidates_WideNumeric(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`
		<html><body>
			<a href="/digital/videoa/-/detail/=/cid=1abc123456h/">six digit</a>
			<a href="/digital/videoa/-/detail/=/cid=1abc001/">no marker</a>
			<a href="/digital/videoa/-/detail/=/cid=1xyz123456h/">other series</a>
		</body></html>`))
	require.NoError(t, err)
	cands := extractRemasterContentIDCandidates(doc, "abc", "h", "")
	require.Len(t, cands, 1)
	assert.Equal(t, "1abc123456h", cands[0].contentID)
	assert.Equal(t, "abc123456h", cands[0].cleanID)

	zdoc, err := goquery.NewDocumentFromReader(strings.NewReader(
		`<a href="/digital/videoa/-/detail/=/cid=1ipx00535zhd/">z variant</a>`))
	require.NoError(t, err)
	zcands := extractRemasterContentIDCandidates(zdoc, "ipx", "h", "z")
	require.Len(t, zcands, 1)
	assert.Equal(t, "1ipx00535zhd", zcands[0].contentID)
}
