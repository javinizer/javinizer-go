package libredmm

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

type payloadIdentityTransport func(*http.Request) (*http.Response, error)

func (f payloadIdentityTransport) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// PayloadToResult is the documented parser seam; malformed provenance is not
// asserted to pass the network scraper's URL validation.
func TestPayloadIdentityFallbackForMalformedProvenance(t *testing.T) {
	for _, tc := range []struct{ name, source, normalized, want string }{
		{"malformed provenance", "https://www.libredmm.com/movies/%zz", "", "FIX-001"},
		{"URL before fallback", "https://www.libredmm.com/movies/url-id.json", "", "url-id"},
		{"payload before provenance", "https://www.libredmm.com/movies/%zz", "PAY-002", "PAY-002"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			payload := &moviePayload{Title: "Fixture title", Description: "Fixture description", Volume: 5400, NormalizedID: tc.normalized}
			before := *payload
			calls := 0
			client := &http.Client{Transport: payloadIdentityTransport(func(r *http.Request) (*http.Response, error) {
				calls++
				return nil, fmt.Errorf("unexpected parser HTTP request: %s", r.URL)
			})}
			result := PayloadToResult(payload, tc.source, "FIX-001", client)
			require.Equal(t, tc.want, result.ID)
			require.Equal(t, tc.want, result.ContentID)
			require.Equal(t, "Fixture title", result.Title)
			require.Equal(t, "Fixture description", result.Description)
			require.Equal(t, 90, result.Runtime)
			require.Equal(t, "libredmm", result.Source)
			require.Equal(t, "ja", result.Language)
			require.Equal(t, before, *payload)
			require.Zero(t, calls)
		})
	}
}
