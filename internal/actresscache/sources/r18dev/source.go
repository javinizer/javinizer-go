package r18devsource

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/r18devdump"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

const dmmActressImageBase = "https://pics.dmm.co.jp/mono/actjpgs/"

// Lister supplies dump actresses. It matches r18devdump.Store.ListActresses;
// the caller owns the underlying store's lifetime.
type Lister func(ctx context.Context) ([]models.DumpActress, error)

type source struct {
	lister Lister
}

// New returns a source without a lister. Collect on it fails with a
// descriptive error; register the source through sources.RegisterR18Dev
// (or NewFromLister) instead.
func New() actresscache.Source {
	return &source{}
}

// NewFromLister wires a dump lister into the source.
func NewFromLister(lister Lister) actresscache.Source {
	return &source{lister: lister}
}

// Name ...
func (s *source) Name() string {
	return "r18dev"
}

// Collect ...
func (s *source) Collect(ctx context.Context, options actresscache.SourceOptions, emit func(actresscache.Candidate) error) error {
	if s.lister == nil {
		return fmt.Errorf("r18dev source requires a dump lister (use sources.RegisterR18Dev or NewFromLister)")
	}
	actresses, err := s.lister(ctx)
	if err != nil {
		return err
	}
	if options.Limit > 0 && len(actresses) > options.Limit {
		actresses = actresses[:options.Limit]
	}
	candidates := make([]actresscache.Candidate, 0, len(actresses))
	for _, actress := range actresses {
		if strings.TrimSpace(actress.ID) == "" {
			continue
		}
		key := "r18dev:actress:" + strings.TrimSpace(actress.ID)
		if options.MarkSeen != nil {
			options.MarkSeen(key)
		}
		if options.ShouldSkip != nil && options.ShouldSkip(key) {
			continue
		}
		firstName, lastName := splitRomajiName(actress.NameRomaji)
		dmmID, _ := strconv.Atoi(strings.TrimSpace(actress.ID))
		candidates = append(candidates, actresscache.Candidate{
			Key:          key,
			Source:       s.Name(),
			SourceID:     strings.TrimSpace(actress.ID),
			SourceURL:    r18devdump.DumpURLOverride(),
			DMMID:        dmmID,
			FirstName:    firstName,
			LastName:     lastName,
			JapaneseName: scraperutil.CleanString(actress.NameKanji),
			ThumbURL:     thumbnailURL(actress),
		})
	}
	workers := options.Workers
	if workers < 1 {
		workers = 1
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan actresscache.Candidate)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var emitErr error
	worker := func() {
		defer wg.Done()
		for candidate := range jobs {
			if err := emit(candidate); err != nil {
				errOnce.Do(func() {
					emitErr = err
					cancel()
				})
				return
			}
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}
	var enqueueErr error
	for _, candidate := range candidates {
		select {
		case jobs <- candidate:
		case <-ctx.Done():
			enqueueErr = ctx.Err()
		}
		if enqueueErr != nil {
			break
		}
	}
	close(jobs)
	wg.Wait()
	if emitErr != nil {
		return emitErr
	}
	if enqueueErr == nil && options.MarkComplete != nil {
		options.MarkComplete()
	}
	return enqueueErr
}

func splitRomajiName(raw string) (string, string) {
	parts := strings.Fields(strings.TrimSpace(raw))
	if len(parts) == 0 {
		return "", ""
	}
	if len(parts) == 1 {
		return parts[0], ""
	}
	return parts[0], parts[1]
}

func thumbnailURL(actress models.DumpActress) string {
	raw := strings.TrimSpace(actress.ImageURL)
	if raw != "" {
		if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
			return raw
		}
		return dmmActressImageBase + strings.TrimLeft(raw, "/")
	}
	parts := strings.Fields(strings.TrimSpace(actress.NameRomaji))
	if len(parts) == 0 {
		return ""
	}
	filename := strings.ToLower(parts[0])
	if len(parts) > 1 {
		filename = strings.ToLower(parts[1]) + "_" + strings.ToLower(parts[0])
	}
	filename = strings.Map(func(r rune) rune {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_' {
			return r
		}
		return -1
	}, filename)
	if filename == "" {
		return ""
	}
	return dmmActressImageBase + filename + ".jpg"
}
