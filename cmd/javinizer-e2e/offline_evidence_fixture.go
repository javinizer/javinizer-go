package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/javinizer/javinizer-go/internal/scraper/r18dev"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

const offlineEvidenceMovieID = "EVIDENCE-001"
const offlineEvidenceContentID = "evidence00001"

// Both synthetic supplied files use exact derived_actress keys, but disagree on
// names so the genuine-source browser test detects accidental canonical writes.
// Source romaji is NULL because the real adapter otherwise derives image URLs.
const offlineEvidenceMovieDump = "COPY public.derived_video (content_id, dvd_id, title_en) FROM stdin;\n" +
	"evidence00001\tEVIDENCE-001\tOffline evidence fixture movie\n\\.\n" +
	"COPY public.derived_actress (id, name_romaji, name_kanji) FROM stdin;\n" +
	"7\t\\N\tOriginal dump actor\n007\t\\N\tOriginal leading-zero actor\n\\.\n" +
	"COPY public.derived_video_actress (content_id, actress_id, ordinality) FROM stdin;\n" +
	"evidence00001\t7\t1\nevidence00001\t007\t2\n\\.\n"

const offlineEvidenceActorDump = "COPY public.derived_actress (id, name_romaji, name_kanji) FROM stdin;\n" +
	"7\tSupplemental whole alias\tSupplemental evidence name\n" +
	"007\tSupplemental leading-zero alias\tSupplemental leading-zero name\n\\.\n"

// prepareOfflineEvidenceFixture is only for the dedicated opt-in E2E harness.
// It uses the actual importer, adapter, converter and publisher, never a private
// raw-reference setter. Call before Finalize and clean up after runtime shutdown.
func prepareOfflineEvidenceFixture(ctx context.Context, directory string, cfg *config.Config, reg *scraperutil.ScraperRegistry) (models.R18DevDumpLookup, func(), error) {
	if ctx == nil || cfg == nil || reg == nil || !filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return nil, nil, fmt.Errorf("invalid offline evidence fixture arguments")
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	root, err := os.MkdirTemp(directory, "offline-evidence-")
	if err != nil {
		return nil, nil, fmt.Errorf("create offline evidence fixture: %w", err)
	}
	state := &offlineEvidenceFixtureState{}
	var once sync.Once
	cleanup := func() {
		once.Do(func() {
			state.mu.Lock()
			defer state.mu.Unlock()
			state.closed = true
			if state.store != nil {
				_ = state.store.Close()
			}
			_ = os.RemoveAll(root)
		})
	}
	ready := false
	defer func() {
		if !ready {
			cleanup()
		}
	}()
	dumpPath := filepath.Join(root, "source.db")
	if _, err := r18devdump.Import(ctx, strings.NewReader(offlineEvidenceMovieDump), dumpPath, r18devdump.ImportOptions{RequireAllRows: true}); err != nil {
		return nil, nil, fmt.Errorf("import offline evidence fixture: %w", err)
	}
	state.store, err = r18devdump.OpenContext(ctx, dumpPath)
	if err != nil {
		return nil, nil, fmt.Errorf("open offline evidence fixture: %w", err)
	}
	artifactDirectory := filepath.Join(root, "artifact")
	if err := publishOfflineEvidenceFixture(ctx, artifactDirectory); err != nil {
		return nil, nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, nil, err
	}
	r18dev.Register(offlineEvidenceFixtureRegistrar{reg: reg, state: state})
	cfg.Scrapers.Priority = []string{"r18dev"}
	cfg.Metadata.OfflineScrapeEvidence = config.OfflineScrapeEvidenceConfig{
		Enabled: true, ArtifactDirectory: artifactDirectory, MaxAgeHours: 168,
	}
	cfg.Output.Download.DownloadActress = false
	cfg.Output.Download.DownloadCover = false
	cfg.Output.Download.DownloadPoster = false
	cfg.Output.Download.DownloadExtrafanart = false
	cfg.Output.Download.DownloadTrailer = false
	cfg.System.VersionCheckEnabled = false
	ready = true
	return state.store, cleanup, nil
}

func publishOfflineEvidenceFixture(ctx context.Context, directory string) error {
	limits := offline.DefaultLimits()
	snapshot, receipt, err := r18dump.Convert(ctx, strings.NewReader(offlineEvidenceActorDump), actresscache.Provenance{
		SourceURL:  "https://e2e.invalid/synthetic-derived-actress.sql",
		ObservedAt: time.Now().UTC(), ParserRevision: "synthetic-e2e-r18dump-v1",
	}, limits.Source)
	if err != nil {
		return fmt.Errorf("convert offline evidence fixture: %w", err)
	}
	if err := os.Mkdir(directory, 0o700); err != nil {
		return fmt.Errorf("create offline evidence artifact directory: %w", err)
	}
	store, err := artifactstore.Open(directory, limits.Store)
	if err != nil {
		return fmt.Errorf("open offline evidence artifact: %w", err)
	}
	defer func() { _ = store.Close() }()
	if _, err := store.Publish(ctx, bytes.NewReader(snapshot.Bytes()), receipt); err != nil {
		// Unsupported platforms fail closed rather than fabricating a manifest.
		return fmt.Errorf("publish offline evidence fixture: %w", err)
	}
	return nil
}

type offlineEvidenceFixtureState struct {
	mu     sync.RWMutex
	store  *r18devdump.Store
	closed bool
}

type offlineEvidenceFixtureRegistrar struct {
	reg   *scraperutil.ScraperRegistry
	state *offlineEvidenceFixtureState
}

func (r offlineEvidenceFixtureRegistrar) Register(registration scraperutil.ScraperRegistration) {
	constructor := registration.Constructor
	registration.Constructor = func(deps scraperutil.ScraperDeps) (models.Scraper, error) {
		deps.R18DevDump = offlineEvidenceFailClosedLookup{R18DevDumpLookup: r.state.store}
		delegate, err := constructor(deps)
		if err != nil {
			return nil, err
		}
		return &offlineEvidenceFixtureScraper{Scraper: delegate, state: r.state}, nil
	}
	r.reg.Register(registration)
}

// Only the base interface is exposed, not optional live URL-resolution seams.
type offlineEvidenceFixtureScraper struct {
	models.Scraper
	state *offlineEvidenceFixtureState
}

func (s *offlineEvidenceFixtureScraper) Search(ctx context.Context, id string) (*models.ScraperResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if id != offlineEvidenceMovieID && id != offlineEvidenceContentID {
		return nil, fmt.Errorf("r18dev: offline evidence fixture rejects unsupported query %q", id)
	}
	s.state.mu.RLock()
	defer s.state.mu.RUnlock()
	if s.state.closed {
		return nil, fmt.Errorf("r18dev: offline evidence fixture is closed")
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	return s.Scraper.Search(context.WithValue(ctx, offlineEvidenceCancelKey{}, cancel), id)
}

func (s *offlineEvidenceFixtureScraper) GetURL(context.Context, string) (string, error) {
	return "", fmt.Errorf("r18dev: offline evidence fixture disables URL resolution")
}

type offlineEvidenceCancelKey struct{}

type offlineEvidenceFailClosedLookup struct {
	models.R18DevDumpLookup
}

func (l offlineEvidenceFailClosedLookup) LookupMovie(ctx context.Context, id string) (*models.DumpMovie, error) {
	movie, err := l.R18DevDumpLookup.LookupMovie(ctx, id)
	if err != nil || movie == nil {
		// The real scraper checks cancellation before any HTTP fallback. Even an
		// unexpected SQLite failure must remain local in this test-only mode.
		if cancel, ok := ctx.Value(offlineEvidenceCancelKey{}).(context.CancelFunc); ok {
			cancel()
		}
	}
	return movie, err
}
