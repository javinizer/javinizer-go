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
