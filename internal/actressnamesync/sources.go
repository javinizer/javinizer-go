package actressnamesync

import (
	"context"
	"net/url"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
	"github.com/javinizer/javinizer-go/internal/actressmetadata"
)

func sourceChoice(e actresscache.Evidence) SourceChoice {
	key, _ := actresscache.SourceKey(e.Source, e.SourceID)
	displayURL := ""
	if u, err := url.Parse(e.Provenance.SourceURL); err == nil {
		u.User = nil
		u.RawQuery = ""
		u.Fragment = ""
		displayURL = u.String()
	}
	return SourceChoice{SourceKey: key, SourceID: e.SourceID, JapaneseName: e.Metadata.JapaneseName, Aliases: append([]string{}, e.Metadata.Aliases...), SourceURL: displayURL, ObservedAt: e.Provenance.ObservedAt}
}
func (s *Service) initialize() {
	defer close(s.ready)
	if !s.opts.Enabled {
		s.mu.Lock()
		s.info.Code = "disabled"
		s.mu.Unlock()
		return
	}
	g, err := s.loadGeneration(s.ctx)
	if err != nil {
		s.mu.Lock()
		s.info.Code = "snapshot_unavailable"
		s.mu.Unlock()
		return
	}
	s.mu.Lock()
	s.generation = g
	s.info = g.info
	s.mu.Unlock()
}

func (s *Service) loadGeneration(ctx context.Context) (*generation, error) {
	store, err := artifactstore.Open(s.opts.ArtifactDirectory, offline.DefaultLimits().Store)
	if err != nil {
		return nil, err
	}
	defer func() { _ = store.Close() }()
	view, err := store.Load(ctx)
	if err != nil {
		return nil, err
	}
	records := view.Snapshot.Records()
	index := make([]indexedSource, 0, len(records))
	for _, e := range records {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		c := sourceChoice(e)
		index = append(index, indexedSource{choice: c, search: strings.ToLower(c.SourceID + "\x00" + c.JapaneseName + "\x00" + strings.Join(c.Aliases, "\x00"))})
	}
	provider, err := actressmetadata.NewSnapshotProvider(view.Snapshot, "r18dev")
	if err != nil {
		return nil, err
	}
	return &generation{options: s.opts, snapshot: view.Snapshot, sources: index, provider: provider, info: SourceInfo{Enabled: true, Available: true, ArtifactDigest: view.Snapshot.Digest(), RecordCount: len(index), Recovered: view.Recovered, MaxAgeSeconds: int64(s.opts.MaxAge / time.Second), RestartRequiredForConfigChanges: true}}, nil
}

// Reload reads only the restart-pinned directory. It never submits work or
// refreshes observation times. Publication shares final confirmation admission.
func (s *Service) Reload(ctx context.Context) (SourceInfo, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return SourceInfo{}, err
	}
	defer done()
	if !s.opts.Enabled {
		return SourceInfo{}, ErrDisabled
	}
	select {
	case s.reload <- struct{}{}:
		defer func() { <-s.reload }()
	default:
		return SourceInfo{}, ErrReloadBusy
	}
	select {
	case <-s.ready:
	case <-ctx.Done():
		return s.reloadProblem("reload_cancelled"), ctx.Err()
	}
	g, err := s.loadGeneration(ctx)
	if err != nil || g.info.Recovered {
		code := "reload_failed"
		if ctx.Err() != nil {
			code = "reload_cancelled"
		}
		info := s.reloadProblem(code)
		if ctx.Err() != nil {
			return info, ctx.Err()
		}
		return info, ErrReloadFailed
	}
	select {
	case s.confirm <- struct{}{}:
		defer func() { <-s.confirm }()
	case <-ctx.Done():
		return s.reloadProblem("reload_cancelled"), ctx.Err()
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := ctx.Err(); err != nil {
		s.info.ReloadProblemCode = "reload_cancelled"
		return s.info, err
	}
	if s.generation == nil || s.generation.snapshot.Digest() != g.snapshot.Digest() {
		s.generation = g
		clear(s.tickets)
	}
	workerProblem := s.info.WorkerProblemCode
	s.info = g.info
	s.info.WorkerProblemCode = workerProblem
	return s.info, nil
}

func (s *Service) reloadProblem(code string) SourceInfo {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.info.ReloadProblemCode = code
	return s.info
}

// Sources searches bounded source metadata for human selection, never identity matching.
func (s *Service) Sources(ctx context.Context, query string, offset, limit int) (*SourcePage, error) {
	return s.SourcesAt(ctx, query, offset, limit, "")
}

// SourcesAt treats the displayed digest only as an equality precondition.
func (s *Service) SourcesAt(ctx context.Context, query string, offset, limit int, digest string) (*SourcePage, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if len(query) > 128 || !utf8.ValidString(query) || offset < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	select {
	case s.readSlots <- struct{}{}:
		defer func() { <-s.readSlots }()
	default:
		return nil, ErrReviewCapacity
	}
	g, err := s.currentGeneration(digest)
	if err != nil {
		return nil, err
	}
	index := g.sources
	if offset > len(index) {
		return nil, ErrInvalid
	}
	q := strings.ToLower(query)
	page := &SourcePage{Items: make([]SourceChoice, 0, limit), ArtifactDigest: g.snapshot.Digest()}
	for i := offset; i < len(index); i++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if !strings.Contains(index[i].search, q) {
			continue
		}
		if len(page.Items) == limit {
			next := i
			page.NextOffset = &next
			break
		}
		c := index[i].choice
		c.Aliases = append([]string{}, c.Aliases...)
		page.Items = append(page.Items, c)
	}
	return page, nil
}
