package r18dev

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// A compact five-digit display query (ABC12345H) must resolve against the
// server's catalog-prefixed cid (1abc12345h): without zero-padding evidence
// the query is not raw, so acceptance falls to the marker/series guard and
// the display identity instead of demanding literal cid equality.
func TestSearchCompactFiveDigitDisplayResolvesPrefixedCID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"content_id":"1abc12345h","dvd_id":"ABC-12345-HD","title_en":"Five-digit remaster"}`))
	}))
	defer server.Close()

	s := newR18TestScraper(server, true, "en")
	res, err := s.Search(context.Background(), "ABC12345H")
	require.NoError(t, err, "the compact five-digit display query must resolve via display acceptance")
	require.NotNil(t, res)
	assert.Equal(t, "1abc12345h", res.ContentID)
	assert.Equal(t, "ABC-12345H", res.ID)
}

// The mirrored five-digit boundary: zero-padded forms and the prefix-free
// t28 tail stay raw; non-padded five-digit display spellings do not.
func TestIsRawRemasterContentIDQueryFiveDigitPaddingBoundary(t *testing.T) {
	for _, id := range []string{"abc01234h", "lulu00441ai", "t28123h", "t2800123hd"} {
		assert.True(t, isRawRemasterContentIDQuery(id), id)
	}
	for _, id := range []string{"abc12345h", "abc12345", "ABC12345H"} {
		assert.False(t, isRawRemasterContentIDQuery(id), id)
	}
}
