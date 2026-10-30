package caribbeancom

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/go-resty/resty/v2"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/ratelimit"
	"github.com/stretchr/testify/require"
)

type charsetContractTransport func(*http.Request) (*http.Response, error)

func (f charsetContractTransport) RoundTrip(req *http.Request) (*http.Response, error) { return f(req) }

func charsetContractUTF16LE(s string) []byte {
	out := []byte{0xff, 0xfe}
	for _, unit := range utf16.Encode([]rune(s)) {
		var encoded [2]byte
		binary.LittleEndian.PutUint16(encoded[:], unit)
		out = append(out, encoded[:]...)
	}
	return out
}

func charsetContractScraper(body []byte, contentType string, status int, calls *int) *scraper {
	return &scraper{
		enabled: true, baseURL: "https://www.caribbeancom.com", language: "ja",
		rateLimiter: ratelimit.NewLimiter(0),
		client: resty.New().SetRetryCount(0).SetTransport(charsetContractTransport(func(req *http.Request) (*http.Response, error) {
			(*calls)++
			return &http.Response{StatusCode: status, Header: http.Header{"Content-Type": []string{contentType}}, Request: req, Body: io.NopCloser(bytes.NewReader(body))}, nil
		})),
	}
}

func TestCharsetFallbackContractPublicMetadata(t *testing.T) {
	// The Japanese title is literal Shift-JIS, not an injected decoder result.
	sjisTitle := "\x83\x65\x83\x58\x83\x67"
	page := func(meta, title string) string {
		return "<html><head>" + meta + "<title>" + title + "</title></head><body><h1 itemprop=\"name\">" + title + "</h1></body></html>"
	}
	for _, tc := range []struct {
		name, contentType string
		body              []byte
		title             string
	}{
		{"header beats meta", "text/html; charset=Shift_JIS", []byte(page(`<meta charset="utf-8">`, sjisTitle)), "テスト"},
		{"meta selects Shift-JIS", "text/html", []byte(page(`<meta charset="Shift_JIS">`, sjisTitle)), "テスト"},
		{"BOM beats header", "text/html; charset=Shift_JIS", charsetContractUTF16LE(page("", "雪の映画")), "雪の映画"},
		{"unknown label defaults to Windows-1252", "text/html; charset=not-a-charset", []byte(page("", "Price \x80")), "Price €"},
		{"malformed Shift-JIS is replacement data", "text/html; charset=Shift_JIS", []byte(page("", sjisTitle+"\x82")), "テスト�"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			calls := 0
			s := charsetContractScraper(tc.body, tc.contentType, http.StatusOK, &calls)
			result, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/120614-753/index.html")
			require.NoError(t, err)
			require.NotNil(t, result)
			require.Equal(t, "120614-753", result.ID)
			require.Equal(t, tc.title, result.Title)
			require.Equal(t, tc.title, result.OriginalTitle)
			require.Equal(t, "caribbeancom", result.Source)
			require.Equal(t, 1, calls)
		})
	}
}

func TestCharsetFallbackContractDecodedChallengeKeepsStatus(t *testing.T) {
	challenge := charsetContractUTF16LE(`<title>Just a moment...</title><div id="cf-challenge-running"></div>`)
	for _, status := range []int{http.StatusOK, http.StatusForbidden, http.StatusUnavailableForLegalReasons, http.StatusNotFound} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			calls := 0
			s := charsetContractScraper(challenge, "text/html; charset=Shift_JIS", status, &calls)
			result, err := s.ScrapeURL(context.Background(), "https://www.caribbeancom.com/moviepages/120614-753/index.html")
			require.Nil(t, result)
			classified, ok := models.AsScraperError(err)
			require.True(t, ok, "expected classified scraper error: %v", err)
			if status == http.StatusNotFound {
				require.Equal(t, models.ScraperErrorKindNotFound, classified.Kind)
			} else {
				require.Equal(t, models.ScraperErrorKindBlocked, classified.Kind)
			}
			if status == http.StatusForbidden || status == http.StatusUnavailableForLegalReasons {
				require.Equal(t, status, classified.StatusCode)
			}
			require.Equal(t, 1, calls)
		})
	}
}

func TestCharsetFallbackContractSuccessfulDecodePreservesWhitespaceAndTail(t *testing.T) {
	t.Run("plain UTF8 preserves surrounding whitespace", func(t *testing.T) {
		want := " \n<p>雪</p>\t "
		calls := 0
		s := charsetContractScraper([]byte(want), "text/html; charset=UTF-8", http.StatusOK, &calls)
		got, status, err := s.fetchPageCtx(context.Background(), "https://www.caribbeancom.com/moviepages/120614-753/index.html")
		require.NoError(t, err)
		require.Equal(t, want, got)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, 1, calls)
	})
	t.Run("empty response", func(t *testing.T) {
		calls := 0
		s := charsetContractScraper(nil, "text/html; charset=UTF-16LE", http.StatusOK, &calls)
		got, status, err := s.fetchPageCtx(context.Background(), "https://www.caribbeancom.com/moviepages/120614-753/index.html")
		require.NoError(t, err)
		require.Empty(t, got)
		require.Equal(t, http.StatusOK, status)
		require.Equal(t, 1, calls)
	})
	// Unlike the error fallback's Response.String(), successful decoding must
	// not trim the response. An odd trailing UTF-16 byte is replacement data.
	want := " \n<p>雪</p>\t �"
	body := append(charsetContractUTF16LE(strings.TrimSuffix(want, "�")), 0x41)
	calls := 0
	s := charsetContractScraper(body, "text/html; charset=UTF-16LE", http.StatusOK, &calls)
	got, status, err := s.fetchPageCtx(context.Background(), "https://www.caribbeancom.com/moviepages/120614-753/index.html")
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, status)
	// The selected IgnoreBOM decoder retains the BOM as U+FEFF.
	require.Equal(t, "\ufeff"+want, got)
	require.Equal(t, 1, calls)
}

func TestCharsetFallbackContractEmptyBody(t *testing.T) {
	calls := 0
	s := charsetContractScraper(nil, "text/html; charset=Shift_JIS", http.StatusOK, &calls)
	got, status, err := s.fetchPageCtx(context.Background(), "https://www.caribbeancom.com/moviepages/120614-753/index.html")
	require.NoError(t, err)
	require.Empty(t, got)
	require.Equal(t, http.StatusOK, status)
	require.Equal(t, 1, calls)
}
