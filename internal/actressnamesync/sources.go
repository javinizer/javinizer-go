package actressnamesync

import (
	"context"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/artifactstore"
	"github.com/javinizer/javinizer-go/internal/actresscache/offline"
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
	store, err := artifactstore.Open(s.opts.ArtifactDirectory, offline.DefaultLimits().Store)
	if err != nil {
		s.unavailable()
		return
	}
	defer func() { _ = store.Close() }()
	view, err := store.Load(s.ctx)
	if err != nil {
		s.unavailable()
		return
	}
	records := view.Snapshot.Records()
	index := make([]indexedSource, 0, len(records))
	for _, e := range records {
		if s.ctx.Err() != nil {
			s.unavailable()
			return
		}
		c := sourceChoice(e)
		index = append(index, indexedSource{choice: c, search: strings.ToLower(c.SourceID + "\x00" + c.JapaneseName + "\x00" + strings.Join(c.Aliases, "\x00"))})
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.snapshot = view.Snapshot
	s.sources = index
	s.info.Available = true
	s.info.Code = ""
	s.info.ArtifactDigest = view.Snapshot.Digest()
	s.info.RecordCount = len(index)
	s.info.Recovered = view.Recovered
}
func (s *Service) unavailable() { s.mu.Lock(); s.info.Code = "snapshot_unavailable"; s.mu.Unlock() }

// Sources searches bounded source metadata for human selection, never identity matching.
func (s *Service) Sources(ctx context.Context, query string, offset, limit int) (*SourcePage, error) {
	ctx, done, err := s.request(ctx)
	if err != nil {
		return nil, err
	}
	defer done()
	if len(query) > 128 || !utf8.ValidString(query) || offset < 0 || limit < 1 || limit > 100 {
		return nil, ErrInvalid
	}
	snapshot, err := s.current()
	if err != nil {
		return nil, err
	}
	s.mu.RLock()
	index := s.sources
	s.mu.RUnlock()
	if offset > len(index) {
		return nil, ErrInvalid
	}
	q := strings.ToLower(query)
	page := &SourcePage{Items: make([]SourceChoice, 0, limit), ArtifactDigest: snapshot.Digest()}
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
