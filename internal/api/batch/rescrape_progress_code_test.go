package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
)

// TestRescrapeProgressCode exercises every branch of rescrapeProgressCode so
// the i18n MessageCode mapping added in PR #144 is fully covered: nil-result
// guard, success (with and without movieID), failed (with and without error),
// and the non-terminal default case.
func TestRescrapeProgressCode(t *testing.T) {
	tests := []struct {
		name     string
		result   *contracts.BulkRescrapeMovieResult
		movieID  string
		wantCode string
		wantArgs map[string]any
	}{
		{
			name:     "nil result returns empty code",
			result:   nil,
			movieID:  "IPX-535",
			wantCode: "",
			wantArgs: nil,
		},
		{
			name:     "success with movie ID stamps SCRAPE_SUCCEEDED",
			result:   &contracts.BulkRescrapeMovieResult{Status: models.RescrapeStatusSuccess},
			movieID:  "IPX-535",
			wantCode: "SCRAPE_SUCCEEDED",
			wantArgs: map[string]any{"movie_id": "IPX-535"},
		},
		{
			name:     "success with empty movie ID omits movie_id arg",
			result:   &contracts.BulkRescrapeMovieResult{Status: models.RescrapeStatusSuccess},
			movieID:  "",
			wantCode: "SCRAPE_SUCCEEDED",
			wantArgs: map[string]any{},
		},
		{
			name:     "failed with error stamps SCRAPE_FAILED and carries error",
			result:   &contracts.BulkRescrapeMovieResult{Status: models.RescrapeStatusFailed, Error: "boom"},
			movieID:  "IPX-535",
			wantCode: "SCRAPE_FAILED",
			wantArgs: map[string]any{"movie_id": "IPX-535", "error": "boom"},
		},
		{
			name:     "failed without error omits error arg",
			result:   &contracts.BulkRescrapeMovieResult{Status: models.RescrapeStatusFailed},
			movieID:  "IPX-535",
			wantCode: "SCRAPE_FAILED",
			wantArgs: map[string]any{"movie_id": "IPX-535"},
		},
		{
			name:     "non-terminal status returns empty code so Message stays authoritative",
			result:   &contracts.BulkRescrapeMovieResult{Status: models.RescrapeStatusGone},
			movieID:  "IPX-535",
			wantCode: "",
			wantArgs: nil,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			code, args := rescrapeProgressCode(tc.result, tc.movieID)
			assert.Equal(t, tc.wantCode, code)
			assert.Equal(t, tc.wantArgs, args)
		})
	}
}
