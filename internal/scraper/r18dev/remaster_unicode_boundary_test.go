package r18dev

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"unicode"

	"github.com/stretchr/testify/require"
)

func TestUnicodeWhitespaceRemasterBoundary(t *testing.T) {
	// Test every whitespace rune Go recognizes, not a hand-picked subset.
	for r := rune(0); r <= unicode.MaxRune; r++ {
		if !unicode.IsSpace(r) {
			continue
		}
		for _, tc := range []struct{ display, cid string }{
			{"T28 123 HD", "9t28123h"}, {"T 28123 HD", "55t28123h"},
			{"T28 123 E AI", "9t28899eai"}, {"T 28123 Z HD", "55t28123zh"},
		} {
			query := strings.ReplaceAll(tc.display, " ", string(r))
			t.Run(fmt.Sprintf("U%04X/%s", r, tc.display), func(t *testing.T) {
				require.Equal(t, canonicalRemasterDisplayID(tc.display), canonicalRemasterDisplayID(query))
				for _, mode := range []string{"http", "store"} {
					server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
						w.Header().Set("Content-Type", "application/json")
						_, _ = w.Write(prefixedT28Body(tc.cid, tc.display))
					}))
					s := newR18TestScraper(server, true, "en")
					if mode == "store" {
						s.dumpLookup = prefixedT28EvidenceStore(t, tc.cid, tc.display)
					}
					res, err := s.Search(context.Background(), query)
					server.Close()
					require.NoError(t, err, mode)
					require.Equal(t, tc.cid, res.ContentID)
					require.Equal(t, canonicalRemasterDisplayID(tc.display), res.ID)
				}
			})
		}
	}
}
