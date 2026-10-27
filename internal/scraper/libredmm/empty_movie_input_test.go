package libredmm

import (
	"context"
	"fmt"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestScrapeURLRejectsEmptyNormalizedMovieBeforeTransport(t *testing.T) {
	s := newScraper(&models.ScraperSettings{Enabled: true}, nil, models.FlareSolverrConfig{})
	t.Cleanup(func() { require.NoError(t, s.Close()) })
	calls := 0
	s.client.SetTransport(pollingFixtureTransport(func(req *http.Request) (*http.Response, error) {
		calls++
		t.Errorf("empty movie identifier dispatched %s %s", req.Method, req.URL)
		return nil, fmt.Errorf("unexpected request for empty movie identifier")
	}))

	result, err := s.ScrapeURL(context.Background(), "https://www.libredmm.com/movies/.json")
	require.Nil(t, result)
	require.EqualError(t, err, "failed to normalize URL: movie ID cannot be empty")
	require.Zero(t, calls, "invalid input must be rejected before fetching movie JSON")
}
