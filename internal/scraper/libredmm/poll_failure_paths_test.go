package libredmm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestDirectPollExhaustionAndTransportFailure(t *testing.T) {
	for _, failed := range []bool{false, true} {
		t.Run(map[bool]string{false: "processing exhausted", true: "second request fails"}[failed], func(t *testing.T) {
			s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
			s.pollInterval = 0
			s.client.SetRetryCount(0)
			calls := 0
			unavailable := errors.New("fixture transport unavailable")
			s.client.SetTransport(pollingFixtureTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				if failed && calls == 2 {
					return nil, unavailable
				}
				return &http.Response{StatusCode: http.StatusAccepted, Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r, Body: io.NopCloser(strings.NewReader(`{}`))}, nil
			}))
			result, err := s.ScrapeURL(context.Background(), "https://libredmm.com/movies/ABC-123")
			require.Nil(t, result)
			if failed {
				require.ErrorIs(t, err, unavailable)
				require.ErrorContains(t, err, "failed to query LibreDMM")
				require.Equal(t, 2, calls)
			} else {
				require.EqualError(t, err, "LibreDMM is still processing for ABC-123")
				require.Equal(t, defaultPollAttempts, calls)
			}
		})
	}
}

func TestUnsupportedDetailShapeFallsBackToQueryID(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	calls := 0
	s.client.SetTransport(pollingFixtureTransport(func(r *http.Request) (*http.Response, error) {
		calls++
		require.Contains(t, r.URL.Path, "search")
		require.Equal(t, "ABC-123", r.URL.Query().Get("q"))
		return &http.Response{StatusCode: 200, Header: http.Header{"Content-Type": []string{"application/json"}}, Request: r, Body: io.NopCloser(strings.NewReader(`{"id":"ABC-123","title":"Known title"}`))}, nil
	}))
	result, err := s.ScrapeURL(context.Background(), "https://libredmm.com/other?q=ABC-123")
	require.NoError(t, err)
	require.NotNil(t, result)
	require.Equal(t, "Known title", result.Title)
	require.Equal(t, "ABC-123", result.ID)
	require.Equal(t, 1, calls)
}

func TestInvalidPublicInputsFailBeforeTransport(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	calls := 0
	s.client.SetTransport(pollingFixtureTransport(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("unexpected request") }))
	result, err := s.ScrapeURL(context.Background(), "https://libredmm.com/")
	require.Nil(t, result)
	require.ErrorContains(t, err, "failed to extract ID from URL")
	result, err = s.Search(context.Background(), "")
	require.Nil(t, result)
	require.Error(t, err)
	require.Zero(t, calls)
}
