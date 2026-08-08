package minnanoav

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperconfig"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Redirecting to a non-MinnanoAV host must be refused.
func TestSearchRedirectToForeignHostRefused(t *testing.T) {
	client := resty.New().SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: http.StatusFound,
			Header:     http.Header{"Location": []string{"http://127.0.0.1:16999/internal"}},
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    req,
		}, nil
	}))
	s := newScraperWithClient(&models.ScraperSettings{Enabled: true}, client)
	_, _, err := s.searchActress(context.Background(), "whatever")
	require.Error(t, err)
	require.Contains(t, err.Error(), "redirect")
}

// Negative rate limits clamp to zero (no throttling), not the 1000ms default.
func TestNegativeRateLimitClampsToZero(t *testing.T) {
	settings := models.ScraperSettings{Enabled: true, RateLimit: -50}
	settings.SetRateLimitPresence(true)
	s := newScraperWithClient(&settings, resty.New())
	require.NoError(t, s.rateLimiter.Wait(context.Background()))
	require.NoError(t, s.rateLimiter.Wait(context.Background()), "no throttle when clamped to zero")
}

func TestMinnanoAVActressFieldsCoverEverything(t *testing.T) {
	got := (&scraper{}).ActressFields()
	for _, want := range []string{"actress", "actress_japanese_name", "actress_first_name", "actress_last_name", "actress_url"} {
		assert.Contains(t, got, want)
	}
}

func TestValidateScraperSettingsRejectsInvalidBaseURL(t *testing.T) {
	err := validateScraperSettings(&models.ScraperSettings{BaseURL: "://bad"})
	require.Error(t, err)
}

func TestBuildClientAppliesDefaultsAndUserAgent(t *testing.T) {
	client := buildClient(&models.ScraperSettings{UserAgent: "  custom-agent  "}, nil)
	require.NotNil(t, client)
	assert.Equal(t, "custom-agent", client.Header.Get("User-Agent"))
	assert.Equal(t, 30*time.Second, client.GetClient().Timeout)

	client = buildClient(&models.ScraperSettings{Timeout: 2, RetryCount: 1}, &models.ProxyConfig{})
	assert.Equal(t, 2*time.Second, client.GetClient().Timeout)
}

func TestBuildClientFallsBackForInvalidProxy(t *testing.T) {
	proxy := &models.ProxyConfig{
		Enabled: true, DefaultProfile: "bad",
		Profiles: map[string]scraperconfig.ProxyProfile{"bad": {URL: "://bad"}},
	}
	client := buildClient(&models.ScraperSettings{}, proxy)
	require.NotNil(t, client)
}

func TestResolveActressMetadataRejectsExcessivelyNestedHTML(t *testing.T) {
	body := strings.Repeat("<div>", 513)
	client := resty.New().SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		profileRequest := req.Clone(req.Context())
		profileRequest.URL, _ = req.URL.Parse("/actress1.html")
		return response(profileRequest, http.StatusOK, body, nil), nil
	}))
	s := newScraperWithClient(&models.ScraperSettings{Enabled: true, BaseURL: "https://www.minnano-av.test"}, client)

	got, err := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 8, JapaneseName: "花子"})
	assert.Error(t, err, "HTML depth-overflow must surface as an error")
	assert.Equal(t, models.ActressInfo{DMMID: 8}, got)
}

func TestParseActressProfileRejectsExcessivelyNestedHTML(t *testing.T) {
	_, err := ParseActressProfile(strings.Repeat("<div>", 513), "https://www.minnano-av.com/actress1.html")
	require.Error(t, err)
}

func TestResolveActressMetadataEarlyReturns(t *testing.T) {
	t.Run("disabled", func(t *testing.T) {
		s := newScraperWithClient(&models.ScraperSettings{}, resty.New())
		got3, err3 := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 3, JapaneseName: "花子"})
		assert.NoError(t, err3)
		assert.Equal(t, models.ActressInfo{DMMID: 3}, got3)
	})
	t.Run("blank name", func(t *testing.T) {
		s := newScraperWithClient(&models.ScraperSettings{Enabled: true}, resty.New())
		got4, err4 := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 4, JapaneseName: "  "})
		assert.NoError(t, err4)
		assert.Equal(t, models.ActressInfo{DMMID: 4}, got4)
	})
	t.Run("search error", func(t *testing.T) {
		client := resty.New().SetTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") }))
		s := newScraperWithClient(&models.ScraperSettings{Enabled: true, BaseURL: "https://www.minnano-av.test"}, client)
		got5, err5 := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 5, JapaneseName: "花子"})
		assert.Error(t, err5, "transport failure must surface as an error")
		assert.Equal(t, models.ActressInfo{DMMID: 5}, got5)
	})
	t.Run("different profile", func(t *testing.T) {
		client := resty.New().SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			profileRequest := req.Clone(req.Context())
			profileRequest.URL, _ = req.URL.Parse("/actress1.html")
			return response(profileRequest, http.StatusOK, `<h1>別人<span>べつじん / Betsu Jin</span></h1>`, nil), nil
		}))
		s := newScraperWithClient(&models.ScraperSettings{Enabled: true, BaseURL: "https://www.minnano-av.test"}, client)
		got6, err6 := s.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 6, JapaneseName: "花子"})
		assert.NoError(t, err6)
		assert.Equal(t, models.ActressInfo{DMMID: 6}, got6)
	})
}

func TestSearchActressErrorAndRedirectBranches(t *testing.T) {
	t.Run("canceled limiter", func(t *testing.T) {
		client := resty.New().SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return response(req, http.StatusOK, "results", nil), nil
		}))
		s := newScraperWithClient(&models.ScraperSettings{Enabled: true, RateLimit: 10000}, client)
		_, _, err := s.searchActress(context.Background(), "first")
		require.NoError(t, err)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, _, err = s.searchActress(ctx, "second")
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("transport error", func(t *testing.T) {
		client := resty.New().SetTransport(roundTripperFunc(func(*http.Request) (*http.Response, error) { return nil, errors.New("offline") }))
		s := newScraperWithClient(&models.ScraperSettings{Enabled: true}, client)
		_, _, err := s.searchActress(context.Background(), "花子")
		require.ErrorContains(t, err, "search failed")
	})
	t.Run("non profile final URL", func(t *testing.T) {
		client := resty.New().SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
			return response(req, http.StatusOK, "results", nil), nil
		}))
		s := newScraperWithClient(&models.ScraperSettings{Enabled: true}, client)
		u, body, err := s.searchActress(context.Background(), "花子")
		require.NoError(t, err)
		assert.Empty(t, u)
		assert.Empty(t, body)
	})
}

func TestParseActressProfileFiltersAliases(t *testing.T) {
	html := `<div class="act-profile"><h2>花子 （はなこ / Yamada Hanako）</h2><table>
<tr><td><span>別名</span><p></p></td></tr>
<tr><td><span>別名</span><p>（はなこ / Yamada Hanako）</p></td></tr>
<tr><td><span>別名</span><p>花子 （はなこ / Yamada Hanako）</p></td></tr>
<tr><td><span>別名</span><p>華子 （はなこ / Yamada Hanako）</p></td></tr>
<tr><td><span>別名</span><p>華子 （はなこ / Yamada Hanako）</p></td></tr>
</table></div>`
	profile, err := ParseActressProfile(html, "https://www.minnano-av.com/actress1.html")
	require.NoError(t, err)
	assert.Equal(t, "花子", profile.JapaneseName)
	assert.Equal(t, []string{"華子"}, profile.Aliases)
}

func TestParsingHelpersCoverEdgeCases(t *testing.T) {
	assert.Equal(t, 0, parseDMMActressID(nil))
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(`<a href="https://example.test/?other=1">x</a><a href="%zz?actress=2">bad escape</a>`))
	require.NoError(t, err)
	assert.Equal(t, 2, parseDMMActressID(doc))

	empty := parseActressPage(nil, "")
	assert.Empty(t, empty.primaryName)
	assert.False(t, empty.containsName("nobody"))
	assert.Empty(t, empty.romajiForName("nobody"))

	jp, reading, romaji := parseNameEntry("花子")
	assert.Equal(t, "花子", jp)
	assert.Empty(t, reading)
	assert.Empty(t, romaji)
	reading, romaji = parseReadingRomaji("")
	assert.Empty(t, reading)
	assert.Empty(t, romaji)
	reading, romaji = parseReadingRomaji("はなこ")
	assert.Equal(t, "はなこ", reading)
	assert.Empty(t, romaji)

	first, last, ok := splitRomajiName("")
	assert.False(t, ok)
	assert.Empty(t, first)
	assert.Empty(t, last)
	_, _, ok = splitRomajiName("Single")
	assert.False(t, ok)

	assert.Empty(t, stripQuery("  "))
	assert.Equal(t, "%", stripQuery("%"))
	assert.Empty(t, directText(nil))

	doc, err = goquery.NewDocumentFromReader(strings.NewReader(`<h1> Direct <span>nested</span> Text </h1>`))
	require.NoError(t, err)
	assert.Equal(t, " Direct  Text ", directText(doc.Find("h1")))
}

func TestValidateActressThumbnailExercisesClientSelection(t *testing.T) {
	err := (*scraper)(nil).ValidateActressThumbnail(context.Background(), "://bad")
	require.Error(t, err)

	s := newScraperWithClient(&models.ScraperSettings{UserAgent: "custom"}, resty.New())
	require.Error(t, s.ValidateActressThumbnail(context.Background(), "://bad"))
	s.settings.UserAgent = ""
	require.Error(t, s.ValidateActressThumbnail(context.Background(), "://bad"))
}

func TestRegisterConstructor(t *testing.T) {
	registry := scraperutil.NewScraperRegistry()
	Register(registry)
	registration, ok := registry.Get("minnanoav")
	require.True(t, ok)
	created, err := registration.Constructor(scraperutil.ScraperDeps{Settings: registration.Defaults})
	require.NoError(t, err)
	assert.Equal(t, "minnanoav", created.Name())
}

func TestSearchActressWithResponseMissingRequestURL(t *testing.T) {
	client := resty.New().SetTransport(roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader("body")), Header: make(http.Header)}, nil
	}))
	s := newScraperWithClient(&models.ScraperSettings{Enabled: true}, client)
	_, _, err := s.searchActress(context.Background(), "花子")
	// net/http attaches the original request when a transport omits it; this assertion documents the safe outcome.
	require.NoError(t, err)
}
