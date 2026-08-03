package javdb

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"testing"

	"github.com/PuerkitoBio/goquery"
	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

type actorRoundTripFunc func(*http.Request) (*http.Response, error)

func (f actorRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func actorResponse(req *http.Request, status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     make(http.Header),
		Body:       io.NopCloser(bytes.NewBufferString(body)),
		Request:    req,
	}
}

func actorTestScraper(transport http.RoundTripper) *scraper {
	client := resty.New()
	client.SetTransport(transport)
	return &scraper{
		client:      client,
		enabled:     true,
		baseURL:     "https://javdb.test",
		rateLimiter: ratelimit.NewLimiter(0),
		settings:    models.ScraperSettings{Enabled: true},
	}
}

func TestResolveActressMetadataCoversFailureAndFallbackPaths(t *testing.T) {
	input := models.ActressInfo{DMMID: 7, JapaneseName: "安倍亜沙美"}
	disabled := actorTestScraper(&staticRoundTripper{})
	disabled.enabled = false
	got, gotErr := disabled.ResolveActressMetadata(context.Background(), input)
	require.NoError(t, gotErr)
	require.Equal(t, models.ActressInfo{DMMID: 7}, got)

	missing := actorTestScraper(&staticRoundTripper{responses: map[string]string{
		"https://javdb.test/actors?locale=en&search=%E5%AE%89%E5%80%8D%E4%BA%9C%E6%B2%99%E7%BE%8E": `<a href="/actors/NO">Other</a>`,
	}})
	gotMiss, errMiss := missing.ResolveActressMetadata(context.Background(), input)
	require.NoError(t, errMiss)
	require.Equal(t, models.ActressInfo{DMMID: 7}, gotMiss)

	failed := actorTestScraper(&errorRoundTripper{err: errors.New("fetch")})
	_, failedErr := failed.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 7, ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"})
	require.Error(t, failedErr)

	oldParser := parseActressProfileHTML
	parseActressProfileHTML = func(string) (*goquery.Document, error) { return nil, errors.New("parse") }
	t.Cleanup(func() { parseActressProfileHTML = oldParser })
	parseFailed := actorTestScraper(&staticRoundTripper{responses: map[string]string{
		"https://javdb.test/actors/ZX?locale=en": "profile",
	}})
	_, parseErr := parseFailed.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 7, ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"})
	require.Error(t, parseErr)

	parseActressProfileHTML = oldParser
	fallback := actorTestScraper(&staticRoundTripper{responses: map[string]string{
		"https://javdb.test/actors/ZX?locale=en": `<html><body><img src="https://c0.jdbstatic.com/avatars/zx/ZX.jpg"></body></html>`,
	}})
	got, fallbackErr := fallback.ResolveActressMetadata(context.Background(), models.ActressInfo{DMMID: 7, JapaneseName: " 安倍亜沙美 ", ThumbURL: "https://c0.jdbstatic.com/avatars/zx/ZX.jpg"})
	require.NoError(t, fallbackErr)
	require.Equal(t, "安倍亜沙美", got.JapaneseName)
}

func TestFindActorIDCoversInputFetchParseAndCandidateBranches(t *testing.T) {
	s := actorTestScraper(&staticRoundTripper{})
	blankID, blankErr := s.findActorID(context.Background(), " \n ")
	require.NoError(t, blankErr)
	require.Empty(t, blankID)

	s.client.SetTransport(&errorRoundTripper{err: errors.New("fetch")})
	_, fetchErr := s.findActorID(context.Background(), "name")
	require.Error(t, fetchErr, "search request failures must surface")

	oldParser := parseActressProfileHTML
	parseActressProfileHTML = func(string) (*goquery.Document, error) { return nil, errors.New("parse") }
	t.Cleanup(func() { parseActressProfileHTML = oldParser })
	s.client.SetTransport(&staticRoundTripper{responses: map[string]string{"https://javdb.test/actors?locale=en&search=name": "body"}})
	_, parseErr := s.findActorID(context.Background(), "name")
	require.Error(t, parseErr, "search-page parse failure must surface")

	parseActressProfileHTML = oldParser
	s.client.SetTransport(&staticRoundTripper{responses: map[string]string{
		"https://javdb.test/actors?locale=en&search=name": `<a href="/actors/NO"></a><a href="/actors/NO">other</a><a href="/movies/1">name</a><a href="/actors/ZX" title="name"></a>`,
	}})
	foundID, foundErr := s.findActorID(context.Background(), "name")
	require.NoError(t, foundErr)
	require.Equal(t, "ZX", foundID)
}

func TestJavDBActorAvatarParsingRejectsMalformedVariants(t *testing.T) {
	cases := []string{
		"%",
		"https://c0.jdbstatic.com/avatars/ZX.jpg",
		"https://c0.jdbstatic.com/images/zx/ZX.jpg",
		"https://c0.jdbstatic.com/avatars/zx/.jpg",
		"https://c0.jdbstatic.com/avatars/zx/ZX.",
		"https://c0.jdbstatic.com/avatars/zx/ZX.gif",
		"https://c0.jdbstatic.com/avatars/zx/!.jpg",
	}
	for _, raw := range cases {
		require.Empty(t, javdbActorIDFromAvatarURL(raw), raw)
	}
	require.False(t, isJavDBActorID("x"))
	require.False(t, isJavDBActorID("bad!"))
}

func TestExtractJavDBActorMetadataCoversSelectorsMetaAndNames(t *testing.T) {
	doc := docFromHTML(t, `<html><head><meta property="og:title" content=""><meta name="twitter:title" content="Solo - JavDB"></head><body><h1></h1><h2></h2><source srcset=","><img data-original="https://example.com/no.jpg"><img srcset="https://c0.jdbstatic.com/avatars/ab/AB.webp 1x, other 2x"></body></html>`)
	got := extractJavDBActorMetadata(doc, 5, "AB")
	require.Equal(t, "Solo", got.JapaneseName)
	require.Equal(t, "https://c0.jdbstatic.com/avatars/ab/AB.webp", got.ThumbURL)

	doc = docFromHTML(t, `<html><head><title>Jane Mary Doe | Javdb</title></head></html>`)
	got = extractJavDBActorMetadata(doc, 6, "CD")
	require.Equal(t, "Jane", got.FirstName)
	require.Equal(t, "Mary Doe", got.LastName)
	require.Equal(t, "https://c0.jdbstatic.com/avatars/cd/CD.jpg", got.ThumbURL)

	require.Equal(t, "Name", cleanJavDBActorName("Name — JavDB"))
	require.Equal(t, "Name", cleanJavDBActorName("Name - Javdb"))
	require.Equal(t, "Name", cleanJavDBActorName("Name | Javdb"))
}

func TestValidateActressThumbnailUsesConfiguredAndDefaultUserAgent(t *testing.T) {
	var userAgents []string
	transport := actorRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		userAgents = append(userAgents, req.Header.Get("User-Agent"))
		return actorResponse(req, http.StatusBadRequest, ""), nil
	})
	s := actorTestScraper(transport)
	_ = s.ValidateActressThumbnail(context.Background(), "https://example.com/image.jpg")
	s.settings.UserAgent = "coverage-agent"
	_ = s.ValidateActressThumbnail(context.Background(), "https://example.com/image.jpg")
	require.Contains(t, userAgents, config.DefaultUserAgent)
	require.Contains(t, userAgents, "coverage-agent")
}
