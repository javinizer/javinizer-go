package libredmm

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/scraperutil"
	"github.com/stretchr/testify/require"
)

func TestNormalizeMovieURLRetainsParsedSyntaxAndCanonicalOutput(t *testing.T) {
	const base = "https://fixtures.invalid/catalog///"
	for _, tc := range []struct {
		name, raw, want string
		http            bool
	}{
		{"trimmed input, port and query encoding", "  HTTPS://LIBREDMM.COM:9443/search?q=ABC%2B001  ", "https://fixtures.invalid/catalog/search?q=ABC%2B001&format=json", true},
		{"canonical movie suffix", "https://libredmm.com/Movies/ABC%20%E5%BD%B1.json", "https://fixtures.invalid/catalog/movies/ABC%20%E5%BD%B1.json", true},
		{"content ID alias", "http://www.libredmm.com/CID/abc001", "https://fixtures.invalid/catalog/movies/abc001.json", true},
		{"unexpected origin stays unrecognized", "https://fixtures.invalid/movies/abc001", "", true},
		{"missing parsed host", "https:///movies/abc001", "", false},
		{"unsupported scheme", "ftp://fixtures.invalid/movies/abc001", "", false},
		{"invalid URL syntax", "https://fixtures.invalid/movies/%zz", "", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.http, scraperutil.IsHTTPURL(tc.raw))
			got, ok := normalizeMovieURL(tc.raw, base)
			require.Equal(t, tc.want != "", ok)
			require.Equal(t, tc.want, got)
		})
	}
}
