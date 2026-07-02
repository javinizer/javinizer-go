package r18dev

import (
	"context"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
)

// stubDumpLookup is a minimal models.R18DevDumpLookup for testing the
// scraper's dump fast-path. LookupMovie returns lookupMovieResult when set,
// otherwise derives a minimal DumpMovie from dvdToContent. A genuine miss
// returns models.ErrDumpMiss; lookupErr (if set) is returned instead to let
// tests exercise the degraded-dump path.
type stubDumpLookup struct {
	dvdToContent      map[string]string
	lookupMovieResult *models.DumpMovie
	lookupErr         error // when set, every lookup returns this error
}

func (s *stubDumpLookup) LookupByDVDID(ctx context.Context, dvdID string) (string, error) {
	if s.lookupErr != nil {
		return "", s.lookupErr
	}
	cid, ok := s.dvdToContent[normalizeIDWithoutStripping(dvdID)]
	if !ok {
		return "", models.ErrDumpMiss
	}
	return cid, nil
}

func (s *stubDumpLookup) LookupByContentID(ctx context.Context, contentID string) (string, error) {
	if s.lookupErr != nil {
		return "", s.lookupErr
	}
	for did, cid := range s.dvdToContent {
		if cid == contentID {
			return did, nil
		}
	}
	return "", models.ErrDumpMiss
}

func (s *stubDumpLookup) Stats(ctx context.Context) (models.DumpStats, error) {
	return models.DumpStats{RowCount: int64(len(s.dvdToContent))}, nil
}

func (s *stubDumpLookup) LookupMovie(ctx context.Context, dvdID string) (*models.DumpMovie, error) {
	if s.lookupErr != nil {
		return nil, s.lookupErr
	}
	if s.lookupMovieResult != nil {
		return s.lookupMovieResult, nil
	}
	cid, ok := s.dvdToContent[normalizeIDWithoutStripping(dvdID)]
	if !ok {
		return nil, models.ErrDumpMiss
	}
	return &models.DumpMovie{ContentID: cid, DVDID: dvdID}, nil
}

// TestResolveURL_PureHTTPNoDumpConsult verifies that ResolveURL no longer
// consults the dump (Search's searchFromDump is the sole dump entry point).
// With the dump configured but no HTTP server, the resolver falls through to
// HTTP probing and returns false. A recording transport asserts the dump is
// NOT consulted and HTTP IS attempted.
func TestResolveURL_PureHTTPNoDumpConsult(t *testing.T) {
	cfg := createTestSettings(true)
	cfg.Enabled = true
	cfg.RetryCount = 0

	// Dump is configured with a hit, but ResolveURL must NOT use it.
	dump := &stubDumpLookup{dvdToContent: map[string]string{"ipx535": "118ipx00535"}}
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, dump)
	s.client.SetRetryCount(0) // see newScraperWithBlockedHTTP: builder floors 0 -> 3
	rt := &recordingTransport{err: errHTTPBlocked}
	s.client.SetTransport(rt)

	resolver := &r18ContentIDResolver{scraper: s}
	_, ok := resolver.ResolveURL(context.Background(), "IPX-535")
	if ok {
		t.Error("expected resolver to miss with no live HTTP server")
	}
	if rt.count() == 0 {
		t.Error("expected ResolveURL to attempt HTTP after searchFromDump miss")
	}
}

// TestNewScraper_NilDumpLookup verifies the scraper constructs and operates
// without a dump lookup (the default production path before download).
func TestNewScraper_NilDumpLookup(t *testing.T) {
	cfg := createTestSettings(true)
	cfg.Enabled = true
	s := newScraper(&cfg, testGlobalProxy, testGlobalFlareSolverr, nil)
	if s.dumpLookup != nil {
		t.Error("dumpLookup should be nil when not injected")
	}
	// Close must be safe.
	if err := s.Close(); err != nil {
		t.Errorf("Close: %v", err)
	}
}
