package dmm

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/ratelimit"
)

// Wait failure in the variation loop surfaces as a wrapped error.
func TestRemaster_RateLimitWaitFailure(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.rateLimiter = ratelimit.NewLimiter(time.Hour)
	s.client.SetTransport(statusTransport(200))
	require.NoError(t, s.rateLimiter.Wait(context.Background()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.ResolveContentIDCtx(ctx, "RCT-156H")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "rate limit wait failed")
}

// Wait failure during verification breaks the URL loop.
func TestRemaster_VerifyRateLimitWaitFailure(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.rateLimiter = ratelimit.NewLimiter(time.Hour)
	require.NoError(t, s.rateLimiter.Wait(context.Background()))
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st, err := s.verifyCandidateDisplayID(ctx, "rct156h", []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/"})
	assert.ErrorIs(t, err, context.Canceled)
	assert.Equal(t, displayUnverifiable, st)
}

// hrefs whose cid/id params don't match the extraction regex produce no raw cid.
func TestExtractRemaster_RawEmptySkipped(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body>` +
		`<a href="/mono/dvd/-/detail/=/cid=/">empty cid</a>` +
		`<a href="https://video.dmm.co.jp/av/content/?id=">empty id</a>` +
		`<a href="/digital/videoa/-/detail/=/cid=1rct00156h/">wanted</a>` +
		`</body></html>`))
	require.NoError(t, err)
	cands := extractRemasterContentIDCandidates(doc, "rct", "h", "")
	require.Len(t, cands, 1)
	assert.Equal(t, "1rct00156h", cands[0].contentID)
}

// Display extraction: label rows with too few cells or an empty normalized
// value never produce identities.
func TestExtractDisplayID_DisplayBranches(t *testing.T) {
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<html><body><table>` +
		`<tr><td>品番：</td></tr>` +
		`<tr><td>品番：</td><td>ーーー</td></tr>` +
		`<tr><td>品番：</td><td>DV-818AI</td></tr>` +
		`</table></body></html>`))
	require.NoError(t, err)
	assert.Equal(t, "dv-818ai", extractDisplayID(doc))
}
