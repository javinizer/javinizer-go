package r18dev

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

// A dump row whose content-id and dvd_id disagree about the pinned T/T28
// boundary is internally inconsistent: T28-123-HD's content id would be
// t28123ah/the T28 series form, while t28123h belongs to T-28123H. The dump
// guard must compare separator-pinned identity tuples instead of folded
// strings, the way markerVariationAccept already does for HTTP responses.
func TestSearchFromDump_TT28BoundaryGuard(t *testing.T) {
	t.Run("conflicting dvd_id falls back instead of relabeling", func(t *testing.T) {
		dump := &stubDumpLookup{}
		dump.lookupMovieResult = &models.DumpMovie{
			ContentID: "t28123h",
			DVDID:     "T28-123-HD",
			TitleEn:   "Conflicting Row",
		}
		s, _ := newScraperWithBlockedHTTP(t, dump)
		result, err := s.Search(context.Background(), "T-28123-HD")
		if err == nil && result != nil {
			t.Fatalf("conflicting dump row must not resolve T-28123-HD, got %+v", result)
		}
	})

	t.Run("consistent dvd_id still resolves", func(t *testing.T) {
		dump := &stubDumpLookup{}
		dump.lookupMovieResult = &models.DumpMovie{
			ContentID: "t28123h",
			DVDID:     "T-28123-HD",
			TitleEn:   "Consistent Row",
		}
		s, _ := newScraperWithBlockedHTTP(t, dump)
		result, err := s.Search(context.Background(), "T-28123-HD")
		require.NoError(t, err)
		require.NotNil(t, result, "consistent dump row must resolve")
	})
}
