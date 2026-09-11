package dmm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
)

func TestRemasterSearchSpellings_NilForUnparseable(t *testing.T) {
	assert.Nil(t, remasterSearchSpellings("plainword"))
	assert.Nil(t, remasterSearchSpellings(""))
}

func TestExtractRemasterContentIDCandidates_Forms(t *testing.T) {
	assert.Empty(t, extractRemasterContentIDCandidates(nil, "rct", "h"))

	doc, derr := goquery.NewDocumentFromReader(strings.NewReader(`<html><body>` +
		`<a>no href</a>` +
		`<a href="/digital/videoa/-/detail/=/cid=1rct00156h/">wanted</a>` +
		`<a href="https://video.dmm.co.jp/av/content/?id=1rct00156h">video form</a>` +
		`<a href="https://www.dmm.co.jp/digital/videoa/-/detail/=/cid=1rct00156h/">full url form</a>` +
		`<a href="/digital/videoa/-/detail/=/cid=1ipx00535h/">wrong series</a>` +
		`<a href="/digital/videoa/-/detail/=/cid=1rct00156/">base no marker</a>` +
		`<a href="/digital/videoa/-/detail/=/cid=ab12zz/">malformed</a>` +
		`<a href="/digital/videoa/-/detail/=/cid=x/">no regex match</a>` +
		`</body></html>`))
	require.NoError(t, derr)
	cands := extractRemasterContentIDCandidates(doc, "rct", "h")
	require.Len(t, cands, 3)
	for _, c := range cands {
		assert.Equal(t, "1rct00156h", c.contentID)
	}
}

func TestResolveRemaster_QueryErrorPaths(t *testing.T) {
	t.Run("transport error", func(t *testing.T) {
		s, _ := newRemasterTestScraper(t)
		s.client.SetTransport(&errTransport{})
		_, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
		require.Error(t, err)
	})

	t.Run("403 status", func(t *testing.T) {
		s, _ := newRemasterTestScraper(t)
		s.client.SetTransport(statusTransport(403))
		_, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
		require.Error(t, err)
	})

	t.Run("500 status", func(t *testing.T) {
		s, _ := newRemasterTestScraper(t)
		s.client.SetTransport(statusTransport(500))
		_, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
		require.Error(t, err)
	})

	t.Run("canceled ctx", func(t *testing.T) {
		s, _ := newRemasterTestScraper(t)
		s.client.SetTransport(statusTransport(200))
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := s.ResolveContentIDCtx(ctx, "RCT-156H")
		require.Error(t, err)
	})
}

type errTransport struct{}

func (t *errTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	return nil, errors.New("boom")
}

func statusTransport(status int) http.RoundTripper {
	return &statusRT{status: status}
}

type statusRT struct{ status int }

func (st *statusRT) RoundTrip(req *http.Request) (*http.Response, error) {
	return &http.Response{
		StatusCode: st.status,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("")),
		Request:    req,
	}, nil
}

func TestResolveRemaster_NoMarkerCandidates(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=1rct00156/">base only</a></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)
	_, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "no matching marker content-id")
}

// Same cleaned cid seen first unprefixed then prefixed: the verbatim
// representative must prefer the catalog-digit form.
func TestResolveRemaster_DedupPrefersPrefixedVerbatim(t *testing.T) {
	s, repo := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<html><body>` +
				`<a href="/digital/videoa/-/detail/=/cid=rct00156h/">digital, no digit</a>` +
				`<a href="/mono/dvd/-/detail/=/cid=1rct00156h/">mono, catalog digit</a>` +
				`</body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)
	cid, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid)
	cached, err := repo.FindBySearchID(context.TODO(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cached.ContentID)
}

// Cache-write failure must not fail resolution.
func TestResolveRemaster_CacheWriteFailureIgnored(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	s.contentIDRepo = &failingCIDRepo{}
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		if strings.Contains(u, "/search/=") {
			return 200, `<html><body><a href="/digital/videoa/-/detail/=/cid=1rct00156h/">remaster</a></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)
	cid, err := s.ResolveContentIDCtx(context.Background(), "RCT-156H")
	require.NoError(t, err)
	assert.Equal(t, "1rct00156h", cid)
}

type failingCIDRepo struct{}

func (f *failingCIDRepo) FindBySearchID(ctx context.Context, searchID string) (*models.ContentIDMapping, error) {
	return nil, errors.New("miss")
}

func (f *failingCIDRepo) Create(ctx context.Context, m *models.ContentIDMapping) error {
	return errors.New("write fail")
}

func (f *failingCIDRepo) GetAllPaginated(ctx context.Context, limit, offset int) ([]models.ContentIDMapping, error) {
	return nil, nil
}

func (f *failingCIDRepo) Delete(ctx context.Context, id string) error { return errors.New("nope") }

func (f *failingCIDRepo) GetAll(ctx context.Context) ([]models.ContentIDMapping, error) {
	return nil, nil
}

func (f *failingCIDRepo) GetAllChunked(ctx context.Context, chunkSize int) ([]models.ContentIDMapping, error) {
	return nil, nil
}

// Per-cid URL combination algebra: disagreeing display pages make a cid
// unverifiable; a verified sibling cid still wins.
func TestResolveRemaster_DisagreeingPagesUnverifiable(t *testing.T) {
	s, _ := newRemasterTestScraper(t)
	rt := &remasterRoundTripper{serve: func(u string) (int, string) {
		switch {
		case strings.Contains(u, "/search/="):
			return 200, `<html><body>` +
				`<a href="/mono/dvd/-/detail/=/cid=abc00999h/">A-dvd</a>` +
				`<a href="/digital/videoa/-/detail/=/cid=abc00999h/">A-digital</a>` +
				`<a href="/digital/videoa/-/detail/=/cid=2abc00999h/">B</a>` +
				`</body></html>`
		case strings.Contains(u, "cid=abc00999h") && strings.Contains(u, "/mono/"):
			return 200, `<html><body><table><tr><td>品番：</td><td>ABC-111-HD</td></tr></table></body></html>`
		case strings.Contains(u, "cid=abc00999h"):
			return 200, `<html><body><table><tr><td>品番：</td><td>ABC-222-HD</td></tr></table></body></html>`
		case strings.Contains(u, "cid=2abc00999h"):
			return 200, `<html><body><table><tr><td>品番：</td><td>ABC-999-HD</td></tr></table></body></html>`
		}
		return 404, ""
	}}
	s.client.SetTransport(rt)
	cid, err := s.ResolveContentIDCtx(context.Background(), "ABC-999H")
	require.NoError(t, err)
	assert.Equal(t, "2abc00999h", cid, "unverifiable cid must not compete; verified cid wins")
}

func TestVerifyCandidateDisplayID_EdgeInputs(t *testing.T) {
	s, _ := newRemasterTestScraper(t)

	st := s.verifyCandidateDisplayID(context.Background(), "rct156h", []string{"", "https://example.com/x"})
	assert.Equal(t, displayUnverifiable, st)

	s2, _ := newRemasterTestScraper(t)
	s2.client.SetTransport(&errTransport{})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st = s2.verifyCandidateDisplayID(ctx, "rct156h", []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/"})
	assert.Equal(t, displayUnverifiable, st)

	s3, _ := newRemasterTestScraper(t)
	s3.client.SetTransport(&errTransport{})
	st = s3.verifyCandidateDisplayID(context.Background(), "rct156h", []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/"})
	assert.Equal(t, displayUnverifiable, st)

	s4, _ := newRemasterTestScraper(t)
	s4.client.SetTransport(statusTransport(404))
	st = s4.verifyCandidateDisplayID(context.Background(), "rct156h", []string{"https://www.dmm.co.jp/mono/dvd/-/detail/=/cid=1rct00156h/"})
	assert.Equal(t, displayUnverifiable, st)
}
