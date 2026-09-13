package dmm

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRemasterWidePrefixContentID(t *testing.T) {
	for _, cid := range []string{"118abc00123h", "1234abc00123hd", "12345abc00123ai"} {
		t.Run(cid, func(t *testing.T) {
			marker, series, _, raw := classifyRemasterQuery(cid)
			require.NotEmpty(t, marker)
			assert.Equal(t, "abc", series)
			require.True(t, raw)
			s, _ := newRemasterTestScraper(t)
			rt := &remasterRoundTripper{serve: func(u string) (int, string) {
				if strings.Contains(u, "cid="+cid) {
					return 200, `<html><h1 id="title" class="item">Remaster</h1><table><tr><td>品番：</td><td>ABC-123H</td></tr></table></html>`
				}
				return 404, ""
			}}
			s.client.SetTransport(rt)
			resolved, err := s.ResolveContentID(cid)
			require.NoError(t, err)
			assert.Equal(t, cid, resolved)
			assert.Zero(t, rt.searchN)
			result, err := s.Search(context.Background(), cid)
			require.NoError(t, err)
			assert.Equal(t, cid, result.ContentID)
		})
	}
}

func TestRemasterBrowserCandidateVerification(t *testing.T) {
	for _, tc := range []struct {
		name, display string
		fail          bool
		want          displayStatus
	}{
		{"matching", "RCT-156-HD", false, displayVerified},
		{"conflicting", "RCT-999-HD", false, displayRejected},
		{"missing", "", false, displayUnverifiable},
		{"fetch_error", "", true, displayUnverifiable},
	} {
		t.Run(tc.name, func(t *testing.T) {
			s, _ := newRemasterTestScraper(t)
			s.useBrowser = true
			calls := 0
			s.browserFetch = func(ctx context.Context, u string) (string, error) {
				calls++
				assert.Equal(t, "https://video.dmm.co.jp/av/content/?id=1rct00156h", u)
				if tc.fail {
					return "", errors.New("browser unavailable")
				}
				return `<table><tr><td>品番：</td><td>` + tc.display + `</td></tr></table>`, nil
			}
			got, err := s.verifyCandidateDisplayID(context.Background(), "rct156h", []string{"https://video.dmm.co.jp/av/content/?id=1rct00156h"})
			require.NoError(t, err)
			assert.Equal(t, tc.want, got)
			assert.Equal(t, 1, calls)
		})
	}
}

func TestRemasterBrowserOnlyResolution(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.useBrowser = true
	s.client.SetTransport(&remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<a href="https://video.dmm.co.jp/av/content/?id=1rct00156h">Remaster</a>`
		}
		return 404, ""
	}})
	calls := 0
	s.browserFetch = func(ctx context.Context, u string) (string, error) {
		calls++
		return `<table><tr><td>品番：</td><td>RCT-156-HD</td></tr></table>`, nil
	}
	cid, err := s.ResolveContentID("RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid)
	assert.Positive(t, calls)
	result, err := s.Search(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", result.ContentID)
	assert.Equal(t, "RCT-156H", result.ID)
	assert.Contains(t, result.SourceURL, "video.dmm.co.jp")
}
