package minnanoavsource

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestParseProfileFallbackWhenBaseURLBroken(t *testing.T) {
	page := `<html><head><meta property="og:image" content="https://www.minnano-av.com/abs.jpg?v=9"></head><body><h1>名</h1></body></html>`
	profile, err := ParseProfile([]byte(page), "http://[::1") // unparseable base -> fallback path
	require.NoError(t, err)
	assert.Equal(t, "https://www.minnano-av.com/abs.jpg", profile.ThumbURL)
}

func TestParseProfileFallbackWhenImageURLBroken(t *testing.T) {
	page := "<html><head><meta property=\"og:image\" content=\"http://exa%zz/x.jpg\"></head><body><h1>名</h1></body></html>"
	profile, err := ParseProfile([]byte(page), "https://www.minnano-av.com/x.html")
	require.NoError(t, err)
	assert.Equal(t, "http://exa%zz/x.jpg", profile.ThumbURL, "unparseable og:image passes through unmodified")
}

func TestParseProfileProtocolRelativeImage(t *testing.T) {
	page := `<html><head><meta property="og:image" content="//cdn.minnano-av.com/a.jpg"></head><body><h1>名</h1></body></html>`
	profile, err := ParseProfile([]byte(page), "https://www.minnano-av.com/x.html")
	require.NoError(t, err)
	assert.Equal(t, "https://cdn.minnano-av.com/a.jpg", profile.ThumbURL)
}
