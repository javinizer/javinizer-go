package minnanoavsource

import (
	"context"
	"encoding/xml"
	"fmt"
	"net/url"
	"path"
	"regexp"
	"sort"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

const (
	defaultSitemapURL = "https://www.minnano-av.com/sitemap.xml"
	maxSitemapBytes   = 16 << 20
	maxProfileBytes   = 8 << 20
)

var profilePathPattern = regexp.MustCompile(`^/actress([0-9]+)\.html$`)

type source struct{}

type sitemapIndex struct {
	Sitemaps []struct {
		Location string `xml:"loc"`
	} `xml:"sitemap"`
}

type sitemapURLSet struct {
	URLs []struct {
		Location string `xml:"loc"`
	} `xml:"url"`
}

// New ...
func New() actresscache.Source {
	return &source{}
}

// Name ...
func (s *source) Name() string {
	return "minnanoav"
}

// Collect ...
func (s *source) Collect(ctx context.Context, options actresscache.SourceOptions, emit func(actresscache.Candidate) error) error {
	if options.Fetcher == nil {
		return fmt.Errorf("minnanoav source requires a fetcher")
	}
	sitemapURL := strings.TrimSpace(options.SitemapURL)
	if sitemapURL == "" && options.Parameters != nil {
		sitemapURL = strings.TrimSpace(options.Parameters["minnanoav.sitemap"])
	}
	if sitemapURL == "" && options.Parameters != nil {
		sitemapURL = strings.TrimSpace(options.Parameters["sitemap"])
	}
	if sitemapURL == "" {
		sitemapURL = defaultSitemapURL
	}
	profileURLs, err := discoverProfileURLs(ctx, options.Fetcher, sitemapURL)
	if err != nil {
		return err
	}
	truncated := false
	if options.Limit > 0 && len(profileURLs) > options.Limit {
		profileURLs = profileURLs[:options.Limit]
		truncated = true
	}
	workers := options.Workers
	if workers < 1 {
		workers = 1
	}
	if workers > len(profileURLs) && len(profileURLs) > 0 {
		workers = len(profileURLs)
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	jobs := make(chan string)
	var wg sync.WaitGroup
	var errOnce sync.Once
	var sourceErr error
	setError := func(err error) {
		errOnce.Do(func() {
			sourceErr = err
			cancel()
		})
	}
	worker := func() {
		defer wg.Done()
		for profileURL := range jobs {
			id, ok := profileID(profileURL)
			if !ok {
				setError(fmt.Errorf("invalid MinnanoAV profile URL: %s", profileURL))
				return
			}
			key := "minnanoav:actress:" + id
			if options.MarkSeen != nil {
				options.MarkSeen(key)
			}
			if options.ShouldSkip != nil && options.ShouldSkip(key) {
				continue
			}
			candidate := actresscache.Candidate{
				Key:       key,
				Source:    s.Name(),
				SourceID:  id,
				SourceURL: profileURL,
			}
			body, _, err := options.Fetcher.Get(ctx, profileURL, "text/html,application/xhtml+xml,*/*", maxProfileBytes)
			if err != nil {
				failure := fmt.Errorf("fetch %s: %w", profileURL, err)
				if options.RecordFailure == nil {
					setError(failure)
					return
				}
				if recordErr := options.RecordFailure(candidate, failure); recordErr != nil {
					setError(recordErr)
					return
				}
				if ctx.Err() != nil {
					return
				}
				continue
			}
			profile, err := ParseProfile(body, profileURL)
			if err != nil {
				failure := fmt.Errorf("parse %s: %w", profileURL, err)
				if options.RecordFailure == nil {
					setError(failure)
					return
				}
				if recordErr := options.RecordFailure(candidate, failure); recordErr != nil {
					setError(recordErr)
					return
				}
				if ctx.Err() != nil {
					return
				}
				continue
			}
			candidate.DMMID = profile.DMMID
			candidate.FirstName = profile.FirstName
			candidate.LastName = profile.LastName
			candidate.JapaneseName = profile.JapaneseName
			candidate.Aliases = profile.Aliases
			candidate.ThumbURL = profile.ThumbURL
			if err := emit(candidate); err != nil {
				setError(err)
				return
			}
		}
	}
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go worker()
	}
enqueue:
	for _, profileURL := range profileURLs {
		select {
		case jobs <- profileURL:
		case <-ctx.Done():
			break enqueue
		}
	}
	close(jobs)
	wg.Wait()
	if sourceErr == nil && ctx.Err() != nil {
		// A cancelled crawl produced a partial listing; reporting completion
		// would let the builder prune unvisited records and publish a
		// truncated cache.
		sourceErr = ctx.Err()
	}
	if sourceErr == nil && !truncated && options.MarkComplete != nil {
		// A limit-truncated enumeration is not complete; pruning must stay off.
		options.MarkComplete()
	}
	return sourceErr
}

func discoverProfileURLs(ctx context.Context, fetcher *actresscache.Fetcher, sitemapURL string) ([]string, error) {
	body, _, err := fetcher.Get(ctx, sitemapURL, "application/xml,text/xml,*/*", maxSitemapBytes)
	if err != nil {
		return nil, err
	}
	var index sitemapIndex
	if err := xml.Unmarshal(body, &index); err != nil {
		return nil, fmt.Errorf("parse sitemap index: %w", err)
	}
	sitemaps := make([]string, 0)
	for _, item := range index.Sitemaps {
		u, err := url.Parse(strings.TrimSpace(item.Location))
		if err != nil || !isMinnanoURL(u) {
			continue
		}
		name := strings.ToLower(path.Base(u.Path))
		if strings.HasPrefix(name, "sitemap_actress_") && strings.HasSuffix(name, ".xml") && !strings.Contains(name, "list_index") {
			sitemaps = append(sitemaps, u.String())
		}
	}
	sort.Strings(sitemaps)
	if len(sitemaps) == 0 {
		return nil, fmt.Errorf("MinnanoAV sitemap contains no actress URL sets")
	}
	seen := make(map[string]struct{})
	for _, sitemap := range sitemaps {
		body, _, err := fetcher.Get(ctx, sitemap, "application/xml,text/xml,*/*", maxSitemapBytes)
		if err != nil {
			return nil, fmt.Errorf("fetch %s: %w", sitemap, err)
		}
		var set sitemapURLSet
		if err := xml.Unmarshal(body, &set); err != nil {
			return nil, fmt.Errorf("parse %s: %w", sitemap, err)
		}
		for _, item := range set.URLs {
			if profileURL, ok := normalizeProfileURL(item.Location); ok {
				seen[profileURL] = struct{}{}
			}
		}
	}
	urls := make([]string, 0, len(seen))
	for profileURL := range seen {
		urls = append(urls, profileURL)
	}
	sort.Strings(urls)
	return urls, nil
}

func normalizeProfileURL(raw string) (string, bool) {
	u, err := url.Parse(strings.TrimSpace(raw))
	if err != nil || !isMinnanoURL(u) {
		return "", false
	}
	match := profilePathPattern.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return "", false
	}
	return "https://www.minnano-av.com/actress" + match[1] + ".html", true
}

func profileID(raw string) (string, bool) {
	u, err := url.Parse(raw)
	if err != nil {
		return "", false
	}
	match := profilePathPattern.FindStringSubmatch(u.Path)
	if len(match) != 2 {
		return "", false
	}
	return match[1], true
}

func isMinnanoURL(u *url.URL) bool {
	if u == nil || u.Scheme != "https" {
		return false
	}
	host := strings.ToLower(strings.TrimSuffix(u.Hostname(), "."))
	return host == "minnano-av.com" || host == "www.minnano-av.com" || strings.HasSuffix(host, ".minnano-av.com")
}
