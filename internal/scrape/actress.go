package scrape

import (
	"context"
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

var lookupBuiltinActress = actresscache.Lookup

func builtinCacheAllowedForPriority(cfg *Config, effectivePriority []string) bool {
	if cfg == nil {
		return true
	}
	var priority []string
	if len(cfg.ActressFieldPriority) > 0 {
		priority = cfg.ActressFieldPriority
	} else if len(effectivePriority) > 0 {
		priority = effectivePriority
	} else {
		priority = cfg.ScrapersPriority
	}
	if len(priority) == 0 {
		return true
	}
	if len(priority) == 1 && strings.EqualFold(strings.TrimSpace(priority[0]), "__skip__") {
		return false
	}
	for _, name := range priority {
		if strings.EqualFold(strings.TrimSpace(name), "dmm") {
			return true
		}
	}
	return false
}

func enrichActressesFromBuiltinCache(scraped *models.Movie) int {
	if scraped == nil {
		return 0
	}
	enriched := 0
	for i := range scraped.Actresses {
		actress := &scraped.Actresses[i]
		record, ok := lookupBuiltinActress(actress.DMMID, actress.JapaneseName, actress.FirstName, actress.LastName)
		if !ok {
			continue
		}
		if actress.DMMID > 0 && record.DMMID > 0 && actress.DMMID != record.DMMID {
			continue
		}
		changed := false
		if actress.DMMID == 0 && record.DMMID > 0 {
			actress.DMMID = record.DMMID
			changed = true
		}
		if actress.ThumbURL == "" || models.IsKnownInvalidDMMActressThumbnail(actress.ThumbURL) {
			if record.ThumbURL != "" {
				actress.ThumbURL = record.ThumbURL
				changed = true
			}
		}
		if actress.FirstName == "" && record.FirstName != "" {
			actress.FirstName = record.FirstName
			changed = true
		}
		if actress.LastName == "" && record.LastName != "" {
			actress.LastName = record.LastName
			changed = true
		}
		if actress.JapaneseName == "" && record.JapaneseName != "" {
			actress.JapaneseName = record.JapaneseName
			changed = true
		}
		if strings.TrimSpace(actress.Aliases) == "" && len(record.Aliases) > 0 {
			actress.Aliases = strings.Join(record.Aliases, "|")
			changed = true
		}
		if changed {
			enriched++
		}
	}
	return enriched
}

func enrichActressesFromDB(ctx context.Context, scraped *models.Movie, actressRepo database.ActressRepositoryInterface, cfg *Config) int {
	if cfg == nil || !cfg.ActressDBEnabled {
		return 0
	}
	if actressRepo == nil || scraped == nil {
		return 0
	}

	enriched := 0
	for i := range scraped.Actresses {
		actress := &scraped.Actresses[i]
		dbActress, err := lookupActress(ctx, actressRepo, actress)
		if err != nil {
			continue
		}
		if enrichActressFields(actress, dbActress) {
			enriched++
		}
	}
	return enriched
}

func lookupActress(ctx context.Context, actressRepo database.ActressRepositoryInterface, actress *models.Actress) (*models.Actress, error) {
	if actress.DMMID > 0 {
		found, err := actressRepo.FindByDMMID(ctx, actress.DMMID)
		if err == nil {
			return found, nil
		}
		if !database.IsNotFound(err) {
			logging.Debugf("Actress DB lookup by DMMID %d failed: %v", actress.DMMID, err)
		}
	}
	if actress.JapaneseName != "" {
		found, err := actressRepo.FindByJapaneseName(ctx, actress.JapaneseName)
		if err == nil {
			return found, nil
		}
		if !database.IsNotFound(err) {
			logging.Debugf("Actress DB lookup by JapaneseName %s failed: %v", actress.JapaneseName, err)
		}
	}
	if actress.FirstName != "" && actress.LastName != "" {
		found, err := actressRepo.FindByFirstNameLastName(ctx, actress.FirstName, actress.LastName)
		if err == nil {
			return found, nil
		}
		if !database.IsNotFound(err) {
			logging.Debugf("Actress DB lookup by name %s %s failed: %v", actress.LastName, actress.FirstName, err)
		}
	}
	return nil, database.ErrNotFound
}

func enrichActressFields(actress *models.Actress, dbActress *models.Actress) bool {
	changed := false
	if actress.ThumbURL == "" && dbActress.ThumbURL != "" {
		actress.ThumbURL = dbActress.ThumbURL
		changed = true
	}
	if actress.FirstName == "" && dbActress.FirstName != "" {
		actress.FirstName = dbActress.FirstName
		changed = true
	}
	if actress.LastName == "" && dbActress.LastName != "" {
		actress.LastName = dbActress.LastName
		changed = true
	}
	if actress.JapaneseName == "" && dbActress.JapaneseName != "" {
		actress.JapaneseName = dbActress.JapaneseName
		changed = true
	}
	if changed {
		logging.Debugf("Enriched actress %s from database (ThumbURL=%s)", actress.FullName(), actress.ThumbURL)
	}
	return changed
}

func validateActressThumbnails(scraped *models.Movie, cfg *Config) int {
	if scraped == nil || cfg == nil {
		return 0
	}
	invalid := 0
	for i := range scraped.Actresses {
		thumbnail := strings.TrimSpace(scraped.Actresses[i].ThumbURL)
		if thumbnail == "" {
			continue
		}
		if models.IsKnownInvalidDMMActressThumbnail(thumbnail) {
			scraped.Actresses[i].ThumbURL = ""
			invalid++
			continue
		}
	}
	return invalid
}

type actressThumbnailSessionValidator interface {
	ValidateActressThumbnail(context.Context, string) error
}

func validateResolverActressThumbnail(ctx context.Context, resolver models.ActressMetadataResolver, fallback func(context.Context, string) error, rawURL string) error {
	if validator, ok := resolver.(actressThumbnailSessionValidator); ok {
		return validator.ValidateActressThumbnail(ctx, rawURL)
	}
	if fallback != nil {
		return fallback(ctx, rawURL)
	}
	return fmt.Errorf("no thumbnail validator configured: cannot verify %s", rawURL)
}

func actressNeedsMetadata(a models.Actress) bool {
	return actressThumbNeedsResolution(a.ThumbURL) ||
		strings.TrimSpace(a.JapaneseName) == "" ||
		(strings.TrimSpace(a.FirstName) == "" || strings.TrimSpace(a.LastName) == "")
}

func actressThumbNeedsResolution(thumbURL string) bool {
	return strings.TrimSpace(thumbURL) == "" || models.IsKnownInvalidDMMActressThumbnail(thumbURL)
}

func enrichActressesFromResolvers(ctx context.Context, scraped *models.Movie, registry ScraperInstanceResolver, cfg *Config, warnings *[]string, breaker *scraperCircuitBreaker, priorityOverride ...[]string) int {
	// cfg.ScrapeActress is only the global default here: collectMetadataResolvers
	// applies per-scraper overrides, so a global false with a scraper-specific
	// true still enriches (documented three-state behavior).
	if cfg == nil || scraped == nil || registry == nil {
		return 0
	}
	priority := cfg.ScrapersPriority
	if len(priorityOverride) > 0 && len(priorityOverride[0]) > 0 {
		priority = priorityOverride[0]
	}
	resolvers := collectMetadataResolvers(registry, priority, cfg, len(priorityOverride) > 0 && len(priorityOverride[0]) > 0)
	if len(resolvers) == 0 {
		return 0
	}
	// A non-empty actress field priority means "consult these, exclusively";
	// the skip sentinel suppresses actress enrichment outright.
	if len(cfg.ActressFieldPriority) == 1 && strings.EqualFold(strings.TrimSpace(cfg.ActressFieldPriority[0]), "__skip__") {
		return 0
	}
	if len(cfg.ActressFieldPriority) > 0 {
		allowed := make(map[string]struct{}, len(cfg.ActressFieldPriority))
		for _, name := range cfg.ActressFieldPriority {
			allowed[strings.ToLower(strings.TrimSpace(name))] = struct{}{}
		}
		filtered := resolvers[:0]
		for _, resolver := range resolvers {
			if _, ok := allowed[strings.ToLower(resolverName(resolver))]; ok {
				filtered = append(filtered, resolver)
			}
		}
		resolvers = filtered
		if len(resolvers) == 0 {
			return 0
		}
	}
	// Resolution blends from every resolver in the chain, so per-field picks
	// must follow the configured actress priority rather than iteration order:
	// resolvers differ in capability (e.g. JavDB only carries Japanese names),
	// and positional order could otherwise promote a lower-ranked source.
	actressRank := actressFieldRanker(cfg.ActressFieldPriority, priority)
	enriched := 0
	// Best remaining rank for each loop position, letting the blend break as
	// soon as no later resolver can improve on the picks already made.
	suffixBestRank := make([]int, len(resolvers)+1)
	suffixBestRank[len(resolvers)] = int(^uint(0) >> 1)
	for i := len(resolvers) - 1; i >= 0; i-- {
		rank := actressRank(resolverName(resolvers[i]))
		if suffixBestRank[i+1] < rank {
			suffixBestRank[i] = suffixBestRank[i+1]
		} else {
			suffixBestRank[i] = rank
		}
	}
	for i := range scraped.Actresses {
		actress := &scraped.Actresses[i]
		if !actressNeedsMetadata(*actress) {
			continue
		}
		needed := make([]string, 0, 4)
		picks := map[string]*actressFieldPick{
			"actress_first_name":    {},
			"actress_last_name":     {},
			"actress_japanese_name": {},
			"actress_url":           {},
		}
		needsThumb := actressThumbNeedsResolution(actress.ThumbURL)
		if needsThumb {
			needed = append(needed, "actress_url")
		}
		if strings.TrimSpace(actress.FirstName) == "" {
			needed = append(needed, "actress_first_name")
		}
		if strings.TrimSpace(actress.LastName) == "" {
			needed = append(needed, "actress_last_name")
		}
		if strings.TrimSpace(actress.JapaneseName) == "" {
			needed = append(needed, "actress_japanese_name")
		}
		allSet := func() (bool, int) {
			worst := -1
			for _, field := range needed {
				p := picks[field]
				if !p.set {
					return false, 0
				}
				if p.rank > worst {
					worst = p.rank
				}
			}
			return true, worst
		}
		for idx, resolver := range resolvers {
			if breaker != nil {
				if skip := breaker.skipFailure(resolverName(resolver)); skip != nil {
					logging.Debugf("Actress resolver %s skipped by circuit breaker for %s", resolverName(resolver), actress.FullName())
					*warnings = append(*warnings, fmt.Sprintf("%s: circuit breaker open", resolverName(resolver)))
					continue
				}
			}
			// Each resolver sees the best-known values so far, not the raw
			// actress: an earlier source may have discovered the Japanese name
			// a name-keyed source (MinnanoAV/JavDB) needs to contribute.
			metadata, resolverErr := resolver.ResolveActressMetadata(ctx, models.ActressInfo{
				DMMID:        actress.DMMID,
				FirstName:    firstNonBlank(actress.FirstName, picks["actress_first_name"].value),
				LastName:     firstNonBlank(actress.LastName, picks["actress_last_name"].value),
				JapaneseName: firstNonBlank(actress.JapaneseName, picks["actress_japanese_name"].value),
				ThumbURL:     firstNonBlank(actress.ThumbURL, picks["actress_url"].value),
			})
			if resolverErr != nil {
				logging.Warnf("Actress resolver %s failed for %s: %v", resolverName(resolver), actress.FullName(), resolverErr)
				*warnings = append(*warnings, fmt.Sprintf("%s: %v", resolverName(resolver), resolverErr))
				if breaker != nil && ctx.Err() == nil {
					breaker.recordOutcome(resolverName(resolver), classifyScraperError(resolverName(resolver), resolverErr, ""))
				}
				continue
			}
			if breaker != nil {
				breaker.recordOutcome(resolverName(resolver), nil)
			}
			if metadata.DMMID != actress.DMMID && actress.DMMID > 0 {
				continue
			}
			name := resolverName(resolver)
			rank := actressRank(name)
			pick := func(field, value string) {
				value = strings.TrimSpace(value)
				if value == "" || !models.ResolverSupportsActressField(resolver, field) {
					return
				}
				p := picks[field]
				if !p.set || rank < p.rank {
					p.value, p.rank, p.set = value, rank, true
				}
			}
			thumbnail := strings.TrimSpace(metadata.ThumbURL)
			if needsThumb && thumbnail != "" && !models.IsKnownInvalidDMMActressThumbnail(thumbnail) && models.ResolverSupportsActressField(resolver, "actress_url") {
				if err := validateResolverActressThumbnail(ctx, resolver, cfg.ValidateActressThumbnail, thumbnail); err != nil {
					logging.Debugf("Rejected resolver actress thumbnail %s: %v", thumbnail, err)
				} else {
					p := picks["actress_url"]
					if !p.set || rank < p.rank {
						p.value, p.rank, p.set = thumbnail, rank, true
					}
				}
			}
			pick("actress_first_name", metadata.FirstName)
			pick("actress_last_name", metadata.LastName)
			pick("actress_japanese_name", metadata.JapaneseName)
			if done, worst := allSet(); done && suffixBestRank[idx+1] >= worst {
				break // no remaining resolver can improve the picks
			}
		}
		actressEnriched := false
		if needsThumb && picks["actress_url"].set {
			actress.ThumbURL = picks["actress_url"].value
			actressEnriched = true
		}
		if strings.TrimSpace(actress.FirstName) == "" && picks["actress_first_name"].set {
			actress.FirstName = picks["actress_first_name"].value
			actressEnriched = true
		}
		if strings.TrimSpace(actress.LastName) == "" && picks["actress_last_name"].set {
			actress.LastName = picks["actress_last_name"].value
			actressEnriched = true
		}
		if strings.TrimSpace(actress.JapaneseName) == "" && picks["actress_japanese_name"].set {
			actress.JapaneseName = picks["actress_japanese_name"].value
			actressEnriched = true
		}
		if actressEnriched {
			enriched++
			logging.Debugf("Enriched actress %s from resolvers", actress.FullName())
		}
	}
	return enriched
}

// collectMetadataResolvers returns enabled actress-capable resolvers in
// priority order. exclusive=true (an explicit scraper selection) consults
// only the named instances instead of appending every registered resolver.
func collectMetadataResolvers(registry ScraperInstanceResolver, priority []string, cfg *Config, exclusive bool) []models.ActressMetadataResolver {
	instances := registry.GetInstancesByPriorityForInput(priority, "")
	seen := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if instance != nil {
			seen[instance.Name()] = struct{}{}
		}
	}
	if !exclusive {
		for _, instance := range registry.GetAllInstances() {
			if instance == nil {
				continue
			}
			if _, ok := seen[instance.Name()]; ok {
				continue
			}
			instances = append(instances, instance)
		}
	}
	resolvers := make([]models.ActressMetadataResolver, 0, len(instances))
	for _, s := range instances {
		if s == nil || !s.IsEnabled() || !s.Config().ShouldScrapeActress(cfg.ScrapeActress) {
			continue
		}
		if r, ok := s.(models.ActressMetadataResolver); ok {
			resolvers = append(resolvers, r)
		}
	}
	return resolvers
}

// firstNonBlank returns the first value that is not just whitespace.
func firstNonBlank(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return value
		}
	}
	return ""
}

// actressFieldPick records the best-ranked value offered for one field.
type actressFieldPick struct {
	value string
	rank  int
	set   bool
}

// actressFieldRanker ranks scrapers by the configured actress field priority,
// falling back to the global scraper priority; unknown names rank last.
func actressFieldRanker(fieldPriority, global []string) func(string) int {
	index := make(map[string]int, len(fieldPriority)+len(global))
	seed := func(list []string, offset int) int {
		added := 0
		for _, name := range list {
			key := strings.ToLower(strings.TrimSpace(name))
			if key == "" {
				continue
			}
			if _, ok := index[key]; !ok {
				index[key] = offset + added
				added++
			}
		}
		return added
	}
	offset := seed(fieldPriority, 0)
	seed(global, offset)
	fallback := offset + len(global)
	return func(name string) int {
		if rank, ok := index[strings.ToLower(strings.TrimSpace(name))]; ok {
			return rank
		}
		return fallback
	}
}

func resolverName(r models.ActressMetadataResolver) string {
	type named interface{ Name() string }
	if n, ok := r.(named); ok {
		return n.Name()
	}
	return "resolver"
}
