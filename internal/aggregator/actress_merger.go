package aggregator

import (
	"github.com/javinizer/javinizer-go/internal/models"
)

// actressSource represents actress data from a single scraper source.
// The Aggregator maps ScraperResult → actressSource at the call boundary,
// decoupling actressMerger from the full scraper result shape.
type actressSource struct {
	Source    string
	Actresses []models.ActressInfo
}

// actressMergeOptions carries the narrow configuration needed for a single
// Merge call. The Aggregator populates these from its *Config and injected
// aliasResolver — actressMerger never accesses *config.Config directly.
type actressMergeOptions struct {
	Priority      []string
	SkipUnknown   bool
	UnknownText   string
	AliasResolver aliasResolverInterface
}

// actressMergerInterface defines the contract for actress merge operations.
// Allows the Aggregator to accept either a real actressMerger or a test mock.
type actressMergerInterface interface {
	Merge(sources []actressSource, opts actressMergeOptions) []models.Actress
}

// actressMerger owns the actress priority-based merge logic.
// Stateless — all configuration is passed per-call via actressMergeOptions.
type actressMerger struct{}

// newActressMerger creates a new actressMerger.
func newActressMerger() *actressMerger {
	return &actressMerger{}
}

// Merge combines actresses from multiple sources according to priority,
// deduplicates by DMMID then by name key, fills empty fields from lower
// priority sources, applies alias resolution, and adds unknown actress
// fallback if configured.
//
// The method has 3 phases:
//  1. Collect + Dedup: iterate sources in priority order, deduplicate actresses
//     by DMMID then by name key, merge empty fields from lower priority sources.
//  2. Resolve: apply alias resolution to each actress if a resolver is provided.
//  3. Fallback: if no actresses were found and fallback mode is enabled, add
//     unknown actress text as a placeholder.
func (m *actressMerger) Merge(sources []actressSource, opts actressMergeOptions) []models.Actress {
	// Phase 1: Collect + Dedup
	actressByDMMID := make(map[int]*models.Actress)
	actressByName := make(map[string]*models.Actress)

	// Pre-process unknownText for case-insensitive comparison.
	// The original text is preserved for the fallback actress placeholder.
	unknownTextLower := ""
	if opts.UnknownText != "" {
		unknownTextLower = models.NormalizeActressNameKey(opts.UnknownText)
	}

	hadAnyActressFromScrapers := false

	for _, src := range sources {
		if len(src.Actresses) == 0 {
			continue
		}

		for _, info := range src.Actresses {
			hadAnyActressFromScrapers = true

			nameKey := resolveCanonicalNameKey(opts.AliasResolver, info.JapaneseName, info.FirstName, info.LastName)

			if opts.SkipUnknown && unknownTextLower != "" && isUnknownActress(info, nameKey, unknownTextLower) {
				continue
			}

			var existing *models.Actress
			var foundInDMMIDMap bool

			if info.DMMID != 0 {
				existing, foundInDMMIDMap = actressByDMMID[info.DMMID]
			}

			if existing == nil && nameKey != "" {
				for _, actress := range actressByDMMID {
					actressNameKey := resolveCanonicalNameKey(opts.AliasResolver, actress.JapaneseName, actress.FirstName, actress.LastName)
					if actressNameKey == nameKey {
						existing = actress
						foundInDMMIDMap = true
						break
					}
				}

				if existing == nil {
					existing = actressByName[nameKey]
				}
			}

			// If actress exists, merge fields
			if existing != nil {
				if existing.DMMID <= 0 && info.DMMID != 0 {
					oldDMMID := existing.DMMID
					existing.DMMID = info.DMMID
					// Move from placeholder/non-DMMID entries to real DMMID key.
					if foundInDMMIDMap && oldDMMID != info.DMMID {
						delete(actressByDMMID, oldDMMID)
					}
					if !foundInDMMIDMap && nameKey != "" {
						delete(actressByName, nameKey)
					}
					actressByDMMID[info.DMMID] = existing
				}
				// When two sources disagree on a non-zero DMMID for the same
				// actress (matched by name), the first DMMID wins. Re-indexing the
				// same pointer under the second DMMID here caused Phase 2 to emit
				// the actress twice (duplicate <actor> in NFO / DB rows).
				if existing.FirstName == "" && info.FirstName != "" {
					existing.FirstName = info.FirstName
				}
				if existing.LastName == "" && info.LastName != "" {
					existing.LastName = info.LastName
				}
				if existing.JapaneseName == "" && info.JapaneseName != "" {
					existing.JapaneseName = info.JapaneseName
				}
				if existing.ThumbURL == "" && info.ThumbURL != "" {
					existing.ThumbURL = info.ThumbURL
				}
			} else {
				// New actress - add to appropriate map
				actress := &models.Actress{
					DMMID:        info.DMMID,
					FirstName:    info.FirstName,
					LastName:     info.LastName,
					JapaneseName: info.JapaneseName,
					ThumbURL:     info.ThumbURL,
				}

				if info.DMMID != 0 {
					actressByDMMID[info.DMMID] = actress
				} else if nameKey != "" {
					actressByName[nameKey] = actress
				}
				// Skip actresses with no DMMID and no name
			}
		}
	}

	// Phase 2: Resolve aliases + collect results
	totalActresses := len(actressByDMMID) + len(actressByName)
	if totalActresses > 0 {
		actresses := make([]models.Actress, 0, totalActresses)

		// Add actresses with DMMID first (primary source)
		for _, actress := range actressByDMMID {
			// Apply alias conversion if resolver is available
			if opts.AliasResolver != nil {
				opts.AliasResolver.Resolve(actress)
			}
			actresses = append(actresses, *actress)
		}

		// Add actresses without DMMID (fallback)
		for _, actress := range actressByName {
			// Apply alias conversion if resolver is available
			if opts.AliasResolver != nil {
				opts.AliasResolver.Resolve(actress)
			}
			actresses = append(actresses, *actress)
		}

		return actresses
	}

	// Phase 3: Fallback
	if !hadAnyActressFromScrapers && !opts.SkipUnknown && opts.UnknownText != "" {
		return []models.Actress{
			{
				FirstName:    opts.UnknownText,
				JapaneseName: opts.UnknownText,
			},
		}
	}

	return []models.Actress{}
}

// Compile-time interface check
var _ actressMergerInterface = (*actressMerger)(nil)
