package scrape

import (
	"context"
	"strings"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
)

var lookupBuiltinActress = actresscache.Lookup

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
	return nil
}

func actressNeedsMetadata(a models.Actress) bool {
	return actressThumbNeedsResolution(a.ThumbURL) ||
		strings.TrimSpace(a.JapaneseName) == "" ||
		(strings.TrimSpace(a.FirstName) == "" && strings.TrimSpace(a.LastName) == "")
}

func actressThumbNeedsResolution(thumbURL string) bool {
	return strings.TrimSpace(thumbURL) == "" || models.IsKnownInvalidDMMActressThumbnail(thumbURL)
}

func enrichActressesFromResolvers(ctx context.Context, scraped *models.Movie, registry ScraperInstanceResolver, cfg *Config, priorityOverride ...[]string) int {
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
	resolvers := collectMetadataResolvers(registry, priority, cfg)
	if len(resolvers) == 0 {
		return 0
	}
	enriched := 0
	for i := range scraped.Actresses {
		actress := &scraped.Actresses[i]
		if !actressNeedsMetadata(*actress) {
			continue
		}
		actressEnriched := false
		for _, resolver := range resolvers {
			if !actressNeedsMetadata(*actress) {
				break
			}
			metadata := resolver.ResolveActressMetadata(ctx, models.ActressInfo{
				DMMID:        actress.DMMID,
				FirstName:    actress.FirstName,
				LastName:     actress.LastName,
				JapaneseName: actress.JapaneseName,
				ThumbURL:     actress.ThumbURL,
			})
			if metadata.DMMID != actress.DMMID && actress.DMMID > 0 {
				continue
			}
			resolverFilled := false
			thumbnail := strings.TrimSpace(metadata.ThumbURL)
			if actressThumbNeedsResolution(actress.ThumbURL) && thumbnail != "" && !models.IsKnownInvalidDMMActressThumbnail(thumbnail) {
				if err := validateResolverActressThumbnail(ctx, resolver, cfg.ValidateActressThumbnail, thumbnail); err != nil {
					logging.Debugf("Rejected resolver actress thumbnail %s: %v", thumbnail, err)
				} else {
					actress.ThumbURL = thumbnail
					resolverFilled = true
				}
			}
			if actress.FirstName == "" && metadata.FirstName != "" {
				actress.FirstName = metadata.FirstName
				resolverFilled = true
			}
			if actress.LastName == "" && metadata.LastName != "" {
				actress.LastName = metadata.LastName
				resolverFilled = true
			}
			if actress.JapaneseName == "" && metadata.JapaneseName != "" {
				actress.JapaneseName = metadata.JapaneseName
				resolverFilled = true
			}
			if resolverFilled {
				actressEnriched = true
				logging.Debugf("Enriched actress %s from resolver %s", actress.FullName(), resolverName(resolver))
			}
		}
		if actressEnriched {
			enriched++
		}
	}
	return enriched
}

func collectMetadataResolvers(registry ScraperInstanceResolver, priority []string, cfg *Config) []models.ActressMetadataResolver {
	instances := registry.GetInstancesByPriorityForInput(priority, "")
	seen := make(map[string]struct{}, len(instances))
	for _, instance := range instances {
		if instance != nil {
			seen[instance.Name()] = struct{}{}
		}
	}
	for _, instance := range registry.GetAllInstances() {
		if instance == nil {
			continue
		}
		if _, ok := seen[instance.Name()]; ok {
			continue
		}
		instances = append(instances, instance)
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

func resolverName(r models.ActressMetadataResolver) string {
	type named interface{ Name() string }
	if n, ok := r.(named); ok {
		return n.Name()
	}
	return "resolver"
}
