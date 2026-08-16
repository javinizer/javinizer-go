package minnanoavsource

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseDMMActressIDChainDescentEndsAtLeaf(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="https://al.dmm.co.jp/?lurl=https%3A%2F%2Fvideo.dmm.co.jp%2Fav%2Flist%2F%3D%2Farticle%3Dactress%2Fid%3D998877%2F">one-hop</a><a href="https://al.dmm.co.jp/?lurl=x%2Fdeeper%253Fother%253Dval">deep-2</a><a href="https://www.dmm.co.jp/?x=y">direct</a>`))
	require.NoError(t, err)
	assert.Equal(t, 998877, parseDMMActressID(doc))
}

func TestParseDMMActressIDPreservesEncodedInnerQuery(t *testing.T) {
	// The affiliate target carries its own query with the actress param
	// percent-encoded inside lurl=; decoding the whole outer href before
	// parsing truncates the target at the inner & and loses the ID.
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="https://al.dmm.co.jp/?lurl=https%3A%2F%2Fvideo.dmm.co.jp%2Fav%2Flist%2F%3Ffoo%3Dbar%26actress%3D28262">affiliate</a>`))
	require.NoError(t, err)
	assert.Equal(t, 28262, parseDMMActressID(doc))
}

func TestResolveAffiliateChainDecodesWholeEncodedHref(t *testing.T) {
	// A fully percent-encoded href hides its query from url.Parse until one
	// whole-string decode layer exposes the lurl parameter.
	encoded := "https%3A%2F%2Fal.dmm.co.jp%2F%3Flurl%3Dhttps%253A%252F%252Fvideo.dmm.co.jp%252F%253Factress%253D42"
	assert.Equal(t, "https://video.dmm.co.jp/?actress=42", resolveAffiliateChain(encoded))
}

func TestResolveAffiliateChainKeepsUndecodableHref(t *testing.T) {
	// An invalid %-encoding must not be mangled: unescaping errors and the
	// original href is returned for downstream matching.
	bad := "https://dmm.co.jp/?x=%zz"
	assert.Equal(t, bad, resolveAffiliateChain(bad))
}

func TestResolveAffiliateChainStopsAtNonURLRedirect(t *testing.T) {
	// lurl extracts to a value that is not a URL target: descent stops and
	// the wrapper href is what downstream matching sees.
	href := "https://al.dmm.co.jp/?lurl=not-an-url"
	assert.Equal(t, href, resolveAffiliateChain(href))
}
