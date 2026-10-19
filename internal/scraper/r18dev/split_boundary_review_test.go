package r18dev

import (
	"context"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/stretchr/testify/require"
	"path/filepath"
	"strings"
	"testing"
)

// Normal lookup-only fixture: T and T28 are distinct separator-pinned releases.
func TestSplitBoundaryDumpAliasMustNotPublishOtherSeries(t *testing.T) {
	ctx := context.Background()
	path := filepath.Join(t.TempDir(), "fixture.db")
	_, err := r18devdump.Import(ctx, strings.NewReader("COPY public.derived_video (content_id, dvd_id) FROM stdin;\n9t2800123h\tT28-123-HD\n\\.\n"), path, r18devdump.ImportOptions{})
	require.NoError(t, err)
	store, err := r18devdump.Open(path)
	require.NoError(t, err)
	defer store.Close()
	s := &scraper{dumpLookup: store, language: "en"}
	result, _ := s.searchFromDump(ctx, "T-28123H")
	require.Nil(t, result, "T-28123H must not publish the distinct T28-123-HD release")
}
