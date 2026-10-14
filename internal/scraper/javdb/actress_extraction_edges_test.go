package javdb

import (
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func edgeAnchor(t *testing.T, html string) *goquery.Selection {
	t.Helper()
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(html))
	require.NoError(t, err)
	return doc.Find("a").First()
}

func TestJavdbActorIDFromLinkUnparsableHref(t *testing.T) {
	// url.Parse rejects the escape sequence, so the fallback strips query and
	// fragment text before the prefix check.
	anchor := edgeAnchor(t, `<a href="http://%zz/actors/abc?x=1">bad</a>`)
	assert.Equal(t, "", javdbActorIDFromLink(anchor))
}

func TestJavdbAbsoluteImageURLRejectsUnknownShape(t *testing.T) {
	assert.Equal(t, "", javdbAbsoluteImageURL("relative/avatar.jpg"))
}

func TestHasExplicitFemaleMarkerSignals(t *testing.T) {
	assert.False(t, hasExplicitFemaleMarker(nil))
	assert.False(t, hasExplicitFemaleMarker(edgeAnchor(t, `<a href="/actors/x">name</a>`)))
	assert.False(t, hasExplicitFemaleMarker(edgeAnchor(t, `<a class="actor-male" href="/actors/x">name</a>`)))
	assert.True(t, hasExplicitFemaleMarker(edgeAnchor(t, `<a class="actor-female" href="/actors/x">name</a>`)))
	assert.True(t, hasExplicitFemaleMarker(edgeAnchor(t, `<a data-gender="female" href="/actors/x">name</a>`)))
	assert.True(t, hasExplicitFemaleMarker(edgeAnchor(t, `<a title="女優" href="/actors/x">name</a>`)))
}

func TestAdjacentTextHasMaleTokenNilSelection(t *testing.T) {
	assert.False(t, adjacentTextHasMaleToken(nil, true))
	assert.False(t, adjacentTextHasMaleToken(nil, false))
}

func TestHasMaleTokenEmptyText(t *testing.T) {
	assert.False(t, hasMaleToken(""))
	assert.True(t, hasMaleToken("男優"))
}
