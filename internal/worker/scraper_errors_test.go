package worker

import (
	"errors"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

func TestBuildScraperNoResultsError(t *testing.T) {
	t.Run("no failures", func(t *testing.T) {
		msg := buildScraperNoResultsError(nil)
		assert.Equal(t, "Movie lookup failed: no scraper results", msg)
	})

	t.Run("all not found", func(t *testing.T) {
		msg := buildScraperNoResultsError([]scraperFailure{
			{Scraper: "a", Err: models.NewScraperNotFoundError("a", "no match")},
			{Scraper: "b", Err: models.NewScraperNotFoundError("b", "movie not found")},
		})
		assert.Contains(t, msg, "Movie not found on configured scrapers")
		assert.Contains(t, msg, "a:")
		assert.Contains(t, msg, "b:")
	})

	t.Run("availability issues", func(t *testing.T) {
		msg := buildScraperNoResultsError([]scraperFailure{
			{Scraper: "a", Err: models.NewScraperStatusError("a", 502, "bad gateway")},
			{Scraper: "b", Err: models.NewScraperStatusError("b", 429, "slow down")},
		})
		assert.Contains(t, msg, "Movie lookup failed due to source availability issues")
		assert.Contains(t, msg, "bad gateway")
		assert.Contains(t, msg, "slow down")
	})
}

func TestFormatScraperFailure(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		contains   string
		contains2  string
		notContain string
	}{
		{
			name:      "nil error fallback",
			err:       nil,
			contains:  "unknown error",
			contains2: "stub:",
		},
		{
			name:      "typed scraper error raw pass-through",
			err:       models.NewScraperNotFoundError("stub", "  exact typed message  "),
			contains:  "exact typed message",
			contains2: "stub:",
		},
		{
			name:      "not found message",
			err:       models.NewScraperNotFoundError("stub", "movie not found"),
			contains:  "movie not found",
			contains2: "stub:",
		},
		{
			name:      "rate limited with status",
			err:       models.NewScraperHTTPError("stub", 429, "HTTP 429 from upstream"),
			contains:  "rate-limited",
			contains2: "429",
		},
		{
			name:      "blocked with status",
			err:       models.NewScraperHTTPError("stub", 451, "status code 451 denied"),
			contains:  "blocked access",
			contains2: "451",
		},
		{
			name:      "unavailable 502 special text",
			err:       models.NewScraperHTTPError("stub", 502, "HTTP: 502 bad gateway"),
			contains:  "HTTP 502 Bad Gateway",
			contains2: "temporarily unavailable",
		},
		{
			name:      "other with status",
			err:       models.NewScraperHTTPError("stub", 418, "status code 418 teapot"),
			contains:  "scraper request failed",
			contains2: "418",
		},
		{
			name:      "other without status",
			err:       errors.New("socket closed"),
			contains:  "scraper request failed",
			contains2: "details: socket closed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			msg := formatScraperFailure("stub", tt.err)
			assert.Contains(t, msg, tt.contains)
			assert.Contains(t, msg, tt.contains2)
			if tt.notContain != "" {
				assert.NotContains(t, msg, tt.notContain)
			}
		})
	}
}

func TestClassifyScraperError(t *testing.T) {
	t.Run("nil error", func(t *testing.T) {
		kind, code, raw := classifyScraperError(nil)
		assert.Equal(t, scraperErrorOther, kind)
		assert.Equal(t, 0, code)
		assert.Equal(t, "", raw)
	})

	t.Run("typed kind mapping", func(t *testing.T) {
		cases := []struct {
			err      *models.ScraperError
			expected scraperErrorKind
			code     int
		}{
			{models.NewScraperNotFoundError("s", "missing"), scraperErrorNotFound, 0},
			{models.NewScraperStatusError("s", 429, ""), scraperErrorRateLimited, 429},
			{models.NewScraperStatusError("s", 451, ""), scraperErrorBlocked, 451},
			{models.NewScraperStatusError("s", 502, ""), scraperErrorUnavailable, 502},
		}

		for _, tc := range cases {
			kind, code, raw := classifyScraperError(tc.err)
			assert.Equal(t, tc.expected, kind)
			assert.Equal(t, tc.code, code)
			assert.NotEmpty(t, raw)
		}
	})

	t.Run("typed unknown kind uses status fallback", func(t *testing.T) {
		err := &models.ScraperError{Scraper: "s", Kind: models.ScraperErrorKindUnknown, StatusCode: 404}
		kind, code, raw := classifyScraperError(err)
		assert.Equal(t, scraperErrorNotFound, kind)
		assert.Equal(t, 404, code)
		assert.NotEmpty(t, raw)

		err = &models.ScraperError{Scraper: "s", Kind: models.ScraperErrorKindUnknown, StatusCode: 599}
		kind, code, raw = classifyScraperError(err)
		assert.Equal(t, scraperErrorUnavailable, kind)
		assert.Equal(t, 599, code)
		assert.NotEmpty(t, raw)

		err = &models.ScraperError{Scraper: "s", Kind: models.ScraperErrorKindUnknown, StatusCode: 418}
		kind, code, raw = classifyScraperError(err)
		assert.Equal(t, scraperErrorOther, kind)
		assert.Equal(t, 418, code)
		assert.NotEmpty(t, raw)
	})

	t.Run("string matching fallbacks", func(t *testing.T) {
		kind, code, raw := classifyScraperError(models.NewScraperHTTPError("", 429, "HTTP status code 429 from upstream"))
		assert.Equal(t, scraperErrorRateLimited, kind)
		assert.Equal(t, 429, code)
		assert.Equal(t, "HTTP status code 429 from upstream", raw)

		kind, code, raw = classifyScraperError(errors.New("unexpected transport reset"))
		assert.Equal(t, scraperErrorOther, kind)
		assert.Equal(t, 0, code)
		assert.Equal(t, "unexpected transport reset", raw)
	})
}

func TestClassifyScraperError_ScraperHTTPError(t *testing.T) {
	cases := []struct {
		name       string
		err        error
		wantKind   scraperErrorKind
		wantCode   int
		wantRawLen bool
	}{
		{
			name:       "ScraperHTTPError 429 - rate limited",
			err:        models.NewScraperHTTPError("stub", 429, "slow down"),
			wantKind:   scraperErrorRateLimited,
			wantCode:   429,
			wantRawLen: true,
		},
		{
			name:       "ScraperHTTPError 404 - not found",
			err:        models.NewScraperHTTPError("stub", 404, "missing"),
			wantKind:   scraperErrorNotFound,
			wantCode:   404,
			wantRawLen: true,
		},
		{
			name:       "ScraperHTTPError 502 - unavailable",
			err:        models.NewScraperHTTPError("stub", 502, "bad gateway"),
			wantKind:   scraperErrorUnavailable,
			wantCode:   502,
			wantRawLen: true,
		},
		{
			name:       "ScraperHTTPError 418 - other",
			err:        models.NewScraperHTTPError("stub", 418, "teapot"),
			wantKind:   scraperErrorOther,
			wantCode:   418,
			wantRawLen: true,
		},
		{
			name:       "wrapped ScraperHTTPError",
			err:        fmt.Errorf("scrape failed: %w", models.NewScraperHTTPError("stub", 429, "rate limited")),
			wantKind:   scraperErrorRateLimited,
			wantCode:   429,
			wantRawLen: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			kind, code, raw := classifyScraperError(tc.err)
			assert.Equal(t, tc.wantKind, kind)
			assert.Equal(t, tc.wantCode, code)
			if tc.wantRawLen {
				assert.NotEmpty(t, raw)
			}
		})
	}
}
