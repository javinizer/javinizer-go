package jav321

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- CanHandleURL ---

func TestMiss2_CanHandleURL(t *testing.T) {
	s := newScraper(testSettings("https://jp.jav321.com"), nil, models.FlareSolverrConfig{})
	assert.True(t, s.CanHandleURL("https://jp.jav321.com/video/abc123"))
	assert.False(t, s.CanHandleURL("https://example.com/test"))
	assert.False(t, s.CanHandleURL("://bad-url"))
}

// --- ScrapeURL: unhandled URL ---

func TestMiss2_ScrapeURL_UnhandledURL(t *testing.T) {
	s := newScraper(testSettings("https://jp.jav321.com"), nil, models.FlareSolverrConfig{})
	_, err := s.ScrapeURL(context.Background(), "https://example.com/test")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "not handled")
}

// --- ScrapeURL: various statuses tested via Search instead ---
// (CanHandleURL checks hostname, so httptest server URLs won't pass)

// --- Search: disabled ---

func TestMiss2_Search_Disabled(t *testing.T) {
	settings := testSettings("https://jp.jav321.com")
	settings.Enabled = false
	s := newScraper(settings, nil, models.FlareSolverrConfig{})
	_, err := s.Search(context.Background(), "ABC-123")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "disabled")
}

// --- Search: cancelled context ---

func TestMiss2_Search_CancelledContext(t *testing.T) {
	s := newScraper(testSettings("https://jp.jav321.com"), nil, models.FlareSolverrConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.Search(ctx, "ABC-123")
	require.Error(t, err)
}

// --- getURLCtx: empty ID ---

func TestMiss2_GetURLCtx_EmptyID(t *testing.T) {
	s := newScraper(testSettings("https://jp.jav321.com"), nil, models.FlareSolverrConfig{})
	_, err := s.getURLCtx(context.Background(), "")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "cannot be empty")
}

// --- getURLCtx: URL input ---

func TestMiss2_GetURLCtx_URLInput(t *testing.T) {
	s := newScraper(testSettings("https://jp.jav321.com"), nil, models.FlareSolverrConfig{})
	url, err := s.getURLCtx(context.Background(), "https://jp.jav321.com/video/abc123")
	require.NoError(t, err)
	assert.Equal(t, "https://jp.jav321.com/video/abc123", url)
}

// --- fetchPageCtx: cancelled context ---

func TestMiss2_FetchPageCtx_CancelledContext(t *testing.T) {
	s := newScraper(testSettings("https://jp.jav321.com"), nil, models.FlareSolverrConfig{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, _, err := s.fetchPageCtx(ctx, "https://jp.jav321.com/video/abc123")
	require.Error(t, err)
}

// --- ResolveDownloadProxyForHost ---

func TestMiss2_ResolveDownloadProxyForHost(t *testing.T) {
	s := newScraper(testSettings("https://jp.jav321.com"), nil, models.FlareSolverrConfig{})

	dp, sp, ok := s.ResolveDownloadProxyForHost("jav321.com")
	assert.True(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("")
	assert.False(t, ok)
	dp, sp, ok = s.ResolveDownloadProxyForHost("example.com")
	assert.False(t, ok)
	_ = dp
	_ = sp
}
