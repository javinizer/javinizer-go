package update

import (
	"errors"
	"net/http"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCheckForUpdatePreservesLookupFailureWithoutAvailability(t *testing.T) {
	cause := errors.New("release endpoint unavailable")
	for _, prerelease := range []bool{false, true} {
		name := "stable only"
		if prerelease {
			name = "prereleases allowed"
		}
		t.Run(name, func(t *testing.T) {
			checker := newGitHubCheckerWithBaseURL("fixture/repo", "https://releases.invalid")
			checker.envLookup = func(string) string { return "" }
			calls := 0
			checker.httpClient = &http.Client{Transport: roundTripFunc(func(req *http.Request) (*http.Response, error) {
				calls++
				require.Equal(t, http.MethodGet, req.Method)
				require.Equal(t, "https://releases.invalid/repos/fixture/repo/releases/latest", req.URL.String())
				return nil, cause
			})}

			latest, available, err := checkForUpdateWithChecker(t.Context(), "v1.0.0", prerelease, checker)

			require.ErrorIs(t, err, cause)
			require.ErrorContains(t, err, "failed to fetch releases")
			require.Nil(t, latest, "a failed lookup must not invent a release")
			require.False(t, available, "a failed lookup must not advertise an update")
			require.Equal(t, 1, calls, "lookup failures must not trigger a release-list fallback")
		})
	}
}
