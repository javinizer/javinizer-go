package aggregator

// newAliasResolverWithCache creates an aliasResolver with a pre-populated cache
// for testing. The cache map is defensively copied so callers can safely
// mutate the original after passing it in.
func newAliasResolverWithCache(cfg *MetadataConfig, repo aliasLookup, cache map[string]string) *aliasResolver {
	if cfg == nil {
		return nil
	}
	copied := make(map[string]string, len(cache))
	for k, v := range cache {
		copied[k] = v
	}
	return &aliasResolver{
		cfg:   cfg,
		repo:  repo,
		cache: copied,
	}
}

// newGenreProcessorWithCache creates a genreProcessor with a pre-populated cache
// for testing. The cache map is defensively copied so callers can safely
// mutate the original after passing it in.
func newGenreProcessorWithCache(cfg *MetadataConfig, repo genreLookup, cache map[string]string) *genreProcessor {
	if cfg == nil {
		return nil
	}
	copied := make(map[string]string, len(cache))
	for k, v := range cache {
		copied[k] = v
	}
	gp := &genreProcessor{
		cfg:   cfg,
		repo:  repo,
		cache: copied,
	}
	gp.mu.Lock()
	gp.compileRegexes()
	gp.mu.Unlock()
	return gp
}

// newWordProcessorWithCache creates a wordProcessor with a pre-populated cache
// for testing. The cache map is defensively copied so callers can safely
// mutate the original after passing it in.
func newWordProcessorWithCache(cfg *MetadataConfig, repo wordLookup, cache map[string]string) *wordProcessor {
	if cfg == nil {
		return nil
	}
	copied := make(map[string]string, len(cache))
	for k, v := range cache {
		copied[k] = v
	}
	return &wordProcessor{
		cfg:    cfg,
		repo:   repo,
		cache:  copied,
		sorted: buildWordReplacementSorted(copied),
	}
}
