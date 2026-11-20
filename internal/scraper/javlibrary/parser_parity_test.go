package javlibrary

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestVideoTilePrefersExactCatalogIDAndSkipsMissingVideoID(t *testing.T) {
	const noVideoID = `<div class="video"><div class="id">ABC-123</div></div>`
	const other = `<div class="video" id="vid_first"><div class="id">DEF-234</div></div>`
	const exact = `<div class="video" id="vid_exact"><div class="id">ABC-123</div></div>`
	const legacy = `<a href="/en/?v=legacy">result</a>`
	s := &scraper{}
	require.Equal(t, "?v=exact", s.extractMovieURLFromHTML(noVideoID+other+exact+legacy, "abc123"))
	require.Equal(t, "?v=first", s.extractMovieURLFromHTML(noVideoID+other+legacy, "abc123"))
	require.Equal(t, "/en/?v=legacy", s.extractMovieURLFromHTML(noVideoID+legacy, "abc123"))
}
