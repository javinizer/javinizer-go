package worker

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scraperutil"
)

// maxActressSyncMovies ...
const maxActressSyncMovies = 5

// ActressSyncOptions ...
type ActressSyncOptions struct {
	Revalidate                     bool
	ValidateThumbnail              func(context.Context, string) error
	LookupCache                    func(int, string, string, string) (models.ActressInfo, bool)
	MergeActresses                 func(uint, uint) (*database.ActressMergeResult, error)
	MergeActressesWithSource       func(uint, uint, models.Actress) (*database.ActressMergeResult, error)
	MergeActressesWithTargetSource func(uint, uint, models.Actress, models.Actress) (*database.ActressMergeResult, error)
	MergeCachedIdentity            func(uint, uint, int) (*database.ActressMergeResult, error)
	MergeCachedIdentityWithSource  func(uint, uint, int, models.Actress) (*database.ActressMergeResult, error)
	AssignDMMID                    func(uint, int) (bool, error)
	AssignDMMIDWithSource          func(uint, int, models.Actress) (bool, error)
	FillMetadata                   func(uint, int, models.ActressInfo) ([]string, error)
	ReplaceThumbnail               func(uint, int, string, string) (bool, error)
	PriorUpdatedFields             []string
	ScrapeActress                  *bool
	// ScrapersPriority orders scrapers for sync decisions (user-configured
	// global scraper priority); empty keeps registry order.
	ScrapersPriority []string
	// ActressFieldPriority ranks sources for actress metadata and thumbnail
	// picks (metadata priority "actress" list); empty falls back to
	// ScrapersPriority, then to the legacy source-quality order.
	ActressFieldPriority []string
}

// ActressSyncResult ...
type ActressSyncResult struct {
	UpdatedFields []string
	Messages      []string
	Warning       string
	Conflict      bool
	Verified      bool
}

// SyncActressMetadata ...
func SyncActressMetadata(ctx context.Context, actressID uint, actressRepo *database.ActressRepository, movieRepo *database.MovieRepository, registry scraperutil.ScraperInstancesInterface, options ...ActressSyncOptions) (*ActressSyncResult, error) {
	actress, err := actressRepo.FindByID(ctx, actressID)
	if err != nil {
		return nil, err
	}
	result := &ActressSyncResult{UpdatedFields: []string{}, Messages: []string{}}
	if len(options) > 0 {
		result.UpdatedFields = append(result.UpdatedFields, options[0].PriorUpdatedFields...)
	}

	mergeActressesWithSource := func(targetID, sourceID uint, expectedSource models.Actress) (*database.ActressMergeResult, error) {
		if len(options) > 0 && options[0].MergeActressesWithSource != nil {
			return options[0].MergeActressesWithSource(targetID, sourceID, expectedSource)
		}
		if len(options) > 0 && options[0].MergeActresses != nil {
			return options[0].MergeActresses(targetID, sourceID)
		}
		return actressRepo.MergeWithSource(ctx, targetID, sourceID, nil, expectedSource)
	}
	mergeActressesWithTargetSource := func(targetID, sourceID uint, expectedTarget, expectedSource models.Actress) (*database.ActressMergeResult, error) {
		if len(options) > 0 && options[0].MergeActressesWithTargetSource != nil {
			return options[0].MergeActressesWithTargetSource(targetID, sourceID, expectedTarget, expectedSource)
		}
		return mergeActressesWithSource(targetID, sourceID, expectedSource)
	}
	mergeCachedIdentity := func(targetID, sourceID uint, expectedDMMID int, expectedSource models.Actress) (*database.ActressMergeResult, error) {
		if len(options) > 0 && options[0].MergeCachedIdentityWithSource != nil {
			return options[0].MergeCachedIdentityWithSource(targetID, sourceID, expectedDMMID, expectedSource)
		}
		if len(options) > 0 && options[0].MergeCachedIdentity != nil {
			return options[0].MergeCachedIdentity(targetID, sourceID, expectedDMMID)
		}
		if len(options) > 0 && options[0].MergeActresses != nil {
			return options[0].MergeActresses(targetID, sourceID)
		}
		return actressRepo.MergeCachedIdentityWithSource(ctx, targetID, sourceID, expectedDMMID, expectedSource)
	}

	assignDMMIDWithSource := func(id uint, dmmID int, expectedSource models.Actress) (bool, error) {
		if len(options) > 0 && options[0].AssignDMMIDWithSource != nil {
			return options[0].AssignDMMIDWithSource(id, dmmID, expectedSource)
		}
		if len(options) > 0 && options[0].AssignDMMID != nil {
			return options[0].AssignDMMID(id, dmmID)
		}
		return actressRepo.AssignDMMIDIfMissingWithSource(ctx, id, dmmID, expectedSource)
	}
	fillMetadata := func(id uint, dmmID int, info models.ActressInfo) ([]string, error) {
		if len(options) > 0 && options[0].FillMetadata != nil {
			return options[0].FillMetadata(id, dmmID, info)
		}
		return actressRepo.FillBlankMetadata(ctx, id, dmmID, info)
	}
	replaceThumbnail := func(id uint, dmmID int, expected, replacement string) (bool, error) {
		if len(options) > 0 && options[0].ReplaceThumbnail != nil {
			return options[0].ReplaceThumbnail(id, dmmID, expected, replacement)
		}
		return actressRepo.ReplaceThumbnail(ctx, id, dmmID, expected, replacement)
	}
	revalidate := len(options) > 0 && options[0].Revalidate
	// validateThumbnail ...
	var validateThumbnail func(context.Context, string) error
	// lookupCache ...
	var lookupCache func(int, string, string, string) (models.ActressInfo, bool)
	if len(options) > 0 {
		validateThumbnail = options[0].ValidateThumbnail
		lookupCache = options[0].LookupCache
	}
	matches := make([]rankedActressMatch, 0)
	scrapeActress := true
	var scrapersPriority, actressFieldPriority []string
	if len(options) > 0 && options[0].ScrapeActress != nil {
		scrapeActress = *options[0].ScrapeActress
	}
	if len(options) > 0 {
		scrapersPriority = append(scrapersPriority, options[0].ScrapersPriority...)
		actressFieldPriority = append(actressFieldPriority, options[0].ActressFieldPriority...)
	}
	scrapers := authoritativeActressScrapers(registry, scrapeActress, scrapersPriority)
	metadataScrapers := actressMetadataScrapers(registry, scrapeActress, scrapersPriority)
	thumbnailRank := actressSyncThumbnailRank(actressFieldPriority, scrapersPriority)
	// A non-empty actress field override means "consult these exclusively";
	// the skip sentinel suppresses resolver-driven metadata resolution.
	if actressSyncSkipSentinel(actressFieldPriority) {
		metadataScrapers = nil
		scrapers = nil
	} else if len(actressFieldPriority) > 0 {
		metadataScrapers = restrictScrapersByPriorityNames(metadataScrapers, actressFieldPriority)
		scrapers = restrictScrapersByPriorityNames(scrapers, actressFieldPriority)
	}
	// With any configured priority, name/thumbnail picks resolve
	// deterministically to the best-ranked source instead of conflicting.
	deterministic := len(actressFieldPriority) > 0 || len(scrapersPriority) > 0
	appendMatch := func(info models.ActressInfo, source string) {
		matches = append(matches, rankedActressMatch{info: info, rank: thumbnailRank(source)})
	}
	// Cache-originated matches ride DMM's rank in field selection while being
	// excluded from revalidation evidence (see rankedActressMatch.fromCache).
	appendCacheMatch := func(info models.ActressInfo) {
		matches = append(matches, rankedActressMatch{info: info, rank: thumbnailRank(resolverNameDMM), fromCache: true})
	}
	cachedSource := *actress
	cacheMatch, cacheHit := lookupActressCache(actress, lookupCache)
	// Hoisted: identity recovery below (assigning a DMM ID or merging into the
	// cached identity) is a stronger write than field fill, so it rides the
	// same admission gate — __skip__ or an exclusive priority omitting dmm
	// must suppress cache-derived identity too (codex).
	cacheAllowed := cacheAllowedForPriority(actressSyncSkipSentinel(actressFieldPriority), actressFieldPriority)
	mergeCachedDuplicate := func(existing *models.Actress) (bool, error) {
		if existing.ID == actress.ID {
			actress = existing
			return true, nil
		}
		if !cacheMatchesCanonical(cacheMatch, existing) {
			cacheHit = false
			return false, nil
		}
		merged, mergeErr := mergeCachedIdentity(existing.ID, actress.ID, cacheMatch.DMMID, cachedSource)
		if mergeErr != nil {
			return false, mergeErr
		}
		actress = &merged.MergedActress
		result.UpdatedFields = append(result.UpdatedFields, "merged_duplicate")
		return true, nil
	}
	applyCachedIdentity := func() (bool, error) {
		if actress.DMMID > 0 || !cacheAllowed || !cacheHit || cacheMatch.DMMID <= 0 {
			return false, nil
		}
		existing, findErr := actressRepo.FindByDMMID(ctx, cacheMatch.DMMID)
		switch {
		case findErr == nil:
			return mergeCachedDuplicate(existing)
		case findErr != nil && !database.IsNotFound(findErr):
			return false, findErr
		default:
			assigned, assignErr := assignDMMIDWithSource(actress.ID, cacheMatch.DMMID, cachedSource)
			if assignErr != nil {
				if !database.IsUniqueConstraint(assignErr) {
					return false, assignErr
				}
				canonical, reloadErr := actressRepo.FindByDMMID(ctx, cacheMatch.DMMID)
				if reloadErr != nil {
					return false, fmt.Errorf("reload canonical actress after DMM ID assignment race: %w", reloadErr)
				}
				return mergeCachedDuplicate(canonical)
			}
			if !assigned {
				cacheHit = false
				return false, nil
			}
			result.UpdatedFields = append(result.UpdatedFields, "dmm_id")
			actress, err = actressRepo.FindByID(ctx, actress.ID)
			if err != nil {
				return false, err
			}
			return true, nil
		}
	}
	if !revalidate {
		if _, cacheErr := applyCachedIdentity(); cacheErr != nil {
			return nil, cacheErr
		}
	}
	if actress.DMMID <= 0 {
		recovered, recoveredMatches, recoveredFields, recoverErr := recoverMissingDMMIdentity(ctx, actress, actressRepo, movieRepo, scrapers, nil, nil, linkedIdentityRecoveryOptions{expectedSource: cachedSource, mergeActressesWithSource: mergeActressesWithSource, mergeActressesWithTargetSource: mergeActressesWithTargetSource, assignDMMIDWithSource: assignDMMIDWithSource})
		if recoverErr != nil {
			return nil, recoverErr
		}
		if recovered == nil && revalidate {
			applied, cacheErr := applyCachedIdentity()
			if cacheErr != nil {
				return nil, cacheErr
			}
			if applied {
				recovered = actress
			}
		}
		if recovered == nil {
			result.Messages = append(result.Messages, "missing_dmm_id")
			return result, nil
		}
		actress = recovered
		for _, recoveredMatch := range recoveredMatches {
			// Screen recovered thumbnails with the same validator (and the
			// originating scraper's session when it carries one) — recovery
			// isn't exempt from SSRF/image policy (codex round 9), and the
			// recovered match keeps its source rank (fix: lost source string).
			if strings.TrimSpace(recoveredMatch.info.ThumbURL) != "" {
				if v := validateThumbnail; v != nil {
					if validateErr := validateActressThumbnail(ctx, recoveredMatch.scraper, v, recoveredMatch.info.ThumbURL); validateErr != nil {
						logging.Debugf("Actress sync: recovery %s rejected thumbnail %q: %v", recoveredMatch.source, recoveredMatch.info.ThumbURL, validateErr)
						recoveredMatch.info.ThumbURL = ""
					}
				}
			}
			appendMatch(recoveredMatch.info, recoveredMatch.source)
		}
		result.UpdatedFields = append(result.UpdatedFields, recoveredFields...)
	}
	if !revalidate && cacheHit && cacheMatch.DMMID == actress.DMMID && cacheAllowed {
		if actressCacheOutranksAll(actressFieldPriority, scrapersPriority) {
			// DMM ranks first (default order, or the configured priority leads
			// with dmm), so the DMM-sourced cache snapshot wins every field it
			// carries — direct blank-fill matches what ranked resolution
			// would produce and still short-circuits the scrape.
			fields, fillErr := fillMetadata(actress.ID, actress.DMMID, cacheMatch)
			if fillErr != nil {
				return nil, fillErr
			}
			result.UpdatedFields = append(result.UpdatedFields, fields...)
			if len(fields) > 0 {
				actress, err = actressRepo.FindByID(ctx, actress.ID)
				if err != nil {
					return nil, err
				}
			}
			if !actressNeedsMetadata(actress) {
				if len(result.UpdatedFields) == 0 {
					result.Messages = append(result.Messages, "already_complete")
				}
				return result, nil
			}
		} else {
			// A higher-ranked source (e.g. javdb ahead of dmm) must still run
			// and win its fields: direct blank-fill would pin cache values the
			// preferred source is meant to replace (codex). Admit the cache as
			// a dmm-ranked match so resolveActressInfo ranks it — it still
			// backstops every field higher-ranked sources leave blank.
			if len(actressInfoFields(cacheMatch)) > 0 {
				appendCacheMatch(cacheMatch)
			}
		}
	}
	if !actressNeedsMetadata(actress) && !revalidate {
		if len(result.UpdatedFields) == 0 {
			result.Messages = append(result.Messages, "already_complete")
		}
		return result, nil
	}
	resolverInput := models.ActressInfo{
		DMMID:        actress.DMMID,
		FirstName:    actress.FirstName,
		LastName:     actress.LastName,
		JapaneseName: actress.JapaneseName,
		ThumbURL:     actress.ThumbURL,
	}
	preferredThumbnail := ""
	preferredThumbnailSource := ""
	preferredThumbnailPriority := int(^uint(0) >> 1)
	// Each resolver sees values discovered by earlier sources, not just the
	// stored actress: DMM may surface the Japanese name a name-keyed source
	// (MinnanoAV/JavDB) needs to contribute its remaining fields.
	known := resolverInput
	var resolverFailures []string
	var resolverErrors []error
	var revisit []models.Scraper
	initialJapaneseName := strings.TrimSpace(actress.JapaneseName)
	if revalidate || actressNeedsMetadata(actress) {
		for _, scraper := range metadataScrapers {
			name := strings.ToLower(strings.TrimSpace(scraper.Name()))
			if resolver, ok := scraper.(models.ActressMetadataResolver); ok {
				logging.Debugf("Actress sync: resolving DMM ID %d with %s", actress.DMMID, name)
				sourceInput := known
				if name != resolverNameJavDB {
					sourceInput.ThumbURL = ""
				}
				metadata, resolverErr := resolver.ResolveActressMetadata(ctx, sourceInput)
				if resolverErr != nil {
					// A transient resolver failure must surface in the report —
					// pretending the lookup verified nothing mislabels it as skipped.
					logging.Warnf("Actress sync: %s failed for DMM ID %d: %v", name, actress.DMMID, resolverErr)
					resolverFailures = append(resolverFailures, name)
					resolverErrors = append(resolverErrors, resolverErr)
					continue
				}
				if metadata.DMMID == actress.DMMID {
					metadata = filterActressResolverFields(scraper, metadata)
					if strings.TrimSpace(known.JapaneseName) == "" {
						known.JapaneseName = strings.TrimSpace(metadata.JapaneseName)
					}
					if strings.TrimSpace(known.FirstName) == "" {
						known.FirstName = strings.TrimSpace(metadata.FirstName)
					}
					if strings.TrimSpace(known.LastName) == "" {
						known.LastName = strings.TrimSpace(metadata.LastName)
					}
					if strings.TrimSpace(known.ThumbURL) == "" {
						known.ThumbURL = strings.TrimSpace(metadata.ThumbURL)
					}
				}
				if !revalidate && !actressThumbnailNeedsResolution(actress.ThumbURL) {
					metadata.ThumbURL = ""
				}
				if strings.TrimSpace(metadata.ThumbURL) != "" && validateThumbnail != nil {
					if validateErr := validateActressThumbnail(ctx, scraper, validateThumbnail, metadata.ThumbURL); validateErr != nil {
						logging.Debugf("Actress sync: %s rejected thumbnail for DMM ID %d: %v", name, actress.DMMID, validateErr)
						metadata.ThumbURL = ""
					}
				}
				fields := actressInfoFields(metadata)
				if len(fields) == 0 {
					// Name-keyed sources (javdb, minnanoav) can only resolve when a
					// Japanese name is known. If we ran them with none, queue a single
					// revisit once a later resolver teaches the identity (codex P2).
					if nameIsKeyed(name) && strings.TrimSpace(sourceInput.JapaneseName) == "" {
						revisit = append(revisit, scraper)
					}
					logging.Debugf("Actress sync: %s returned no metadata for DMM ID %d", name, actress.DMMID)
				} else {
					logging.Debugf("Actress sync: %s returned fields for DMM ID %d: %s", name, actress.DMMID, strings.Join(fields, ", "))
				}
				if metadata.DMMID == actress.DMMID {
					appendMatch(metadata, name)
					if strings.TrimSpace(metadata.ThumbURL) != "" && !models.IsKnownInvalidDMMActressThumbnail(metadata.ThumbURL) && models.ResolverSupportsActressField(scraper, "actress_url") {
						priority := thumbnailRank(name)
						if priority < preferredThumbnailPriority {
							preferredThumbnail = strings.TrimSpace(metadata.ThumbURL)
							preferredThumbnailSource = name
							preferredThumbnailPriority = priority
						}
					}
				}
				continue
			}
			if !actressThumbnailNeedsResolution(actress.ThumbURL) {
				continue
			}
			if resolver, ok := scraper.(models.ActressThumbnailResolver); ok {
				logging.Debugf("Actress sync: resolving thumbnail for DMM ID %d with %s", actress.DMMID, name)
				thumbnail := strings.TrimSpace(resolver.ResolveActressThumbnail(ctx, resolverInput))
				if thumbnail != "" && validateThumbnail != nil {
					if validateErr := validateActressThumbnail(ctx, scraper, validateThumbnail, thumbnail); validateErr != nil {
						logging.Debugf("Actress sync: %s rejected thumbnail for DMM ID %d: %v", name, actress.DMMID, validateErr)
						thumbnail = ""
					}
				}
				if thumbnail != "" && !models.IsKnownInvalidDMMActressThumbnail(thumbnail) {
					appendMatch(models.ActressInfo{DMMID: actress.DMMID, ThumbURL: thumbnail}, name)
					priority := thumbnailRank(name)
					if priority < preferredThumbnailPriority {
						preferredThumbnail = thumbnail
						preferredThumbnailSource = name
						preferredThumbnailPriority = priority
					}
				}
			}
		}
	}
	// One revisit of name-keyed resolvers: a DMM-style source may have taught
	// us the Japanese name after they ran with an empty one.
	if len(revisit) > 0 && initialJapaneseName == "" && strings.TrimSpace(known.JapaneseName) != "" {
		for _, scraper := range revisit {
			name := strings.ToLower(strings.TrimSpace(scraper.Name()))
			resolver, ok := scraper.(models.ActressMetadataResolver)
			if !ok {
				continue
			}
			logging.Debugf("Actress sync: revisiting %s with learned Japanese name for DMM ID %d", name, actress.DMMID)
			metadata, resolverErr := resolver.ResolveActressMetadata(ctx, known)
			if resolverErr != nil {
				// Surface the revisit failure exactly like a first-pass failure,
				// so the task report doesn't claim success after a transient lookup.
				logging.Warnf("Actress sync: revisit %s failed for DMM ID %d: %v", name, actress.DMMID, resolverErr)
				resolverFailures = append(resolverFailures, name)
				resolverErrors = append(resolverErrors, resolverErr)
				continue
			}
			if metadata.DMMID != actress.DMMID {
				continue
			}
			metadata = filterActressResolverFields(scraper, metadata)
			if fields := actressInfoFields(metadata); len(fields) > 0 {
				logging.Debugf("Actress sync: revisit %s contributed fields for DMM ID %d: %s", name, actress.DMMID, strings.Join(fields, ", "))
			}
			// Mirror the initial pass: strip thumbnail when it can't win.
			if !revalidate && !actressThumbnailNeedsResolution(actress.ThumbURL) {
				metadata.ThumbURL = ""
			}
			// Apply the same thumbnail screening the initial pass applies: a
			// rejected URL must not persist or win the preferred slot.
			if strings.TrimSpace(metadata.ThumbURL) != "" && validateThumbnail != nil {
				if validateErr := validateActressThumbnail(ctx, scraper, validateThumbnail, metadata.ThumbURL); validateErr != nil {
					logging.Debugf("Actress sync: revisit %s rejected thumbnail for DMM ID %d: %v", name, actress.DMMID, validateErr)
					metadata.ThumbURL = ""
				}
			}
			appendMatch(metadata, name)
			if strings.TrimSpace(metadata.ThumbURL) != "" && !models.IsKnownInvalidDMMActressThumbnail(metadata.ThumbURL) && models.ResolverSupportsActressField(scraper, "actress_url") {
				priority := thumbnailRank(name)
				if priority < preferredThumbnailPriority {
					preferredThumbnail = strings.TrimSpace(metadata.ThumbURL)
					preferredThumbnailSource = name
					preferredThumbnailPriority = priority
				}
			}
		}
	}
	if revalidate && cacheAllowed && cacheHit && cacheMatch.DMMID == actress.DMMID {
		// Codex latest head: the built-in cache is a DMM snapshot admitted as a
		// DMM-ranked source of fields; never suppress it — register its fields
		// like any dmm-named source so resolveActressInfoByRank sees them.
		if len(actressInfoFields(cacheMatch)) > 0 {
			appendCacheMatch(cacheMatch)
		}
	}
	if needsLinkedActressFallback(actress, matches, deterministic) {
		linkedMatches, linkedErr := linkedActressMatches(ctx, movieRepo, actress.ID, actress.DMMID, scrapers)
		if linkedErr != nil {
			// Identity lookups failed transiently (e.g. scraper timeout);
			// task must fail for retry instead of terminal missing_dmm_id.
			return nil, linkedErr
		}
		for _, linkedMatch := range linkedMatches {
			// Codex P2 (round 7 + round 9): linked-match thumbnails are
			// screened through the synonymous validator the resolver loop
			// uses (the scraper's session-aware one when it has one), so a
			// proxy/UA-restricted scraper doesn't lose usable shots to the
			// global fallback.
			if strings.TrimSpace(linkedMatch.info.ThumbURL) != "" {
				if v := validateThumbnail; v != nil {
					if validateErr := validateActressThumbnail(ctx, linkedMatch.scraper, v, linkedMatch.info.ThumbURL); validateErr != nil {
						logging.Debugf("Actress sync: linked fallback rejected thumbnail %q for DMM ID %d: %v", linkedMatch.info.ThumbURL, actress.DMMID, validateErr)
						linkedMatch.info.ThumbURL = ""
					}
				}
			}
			appendMatch(linkedMatch.info, linkedMatch.source)
		}
	}

	if preferredThumbnail != "" {
		logging.Debugf("Actress sync: selected %s thumbnail for DMM ID %d", preferredThumbnailSource, actress.DMMID)
	}
	candidate, conflict := resolveActressInfo(actress, matches, deterministic)
	// Codex late head: when the stored thumb is missing, resolveActressInfo
	// already picked the best-ranked candidate across linked + direct sources
	// — overriding with the direct-only "preferred" pick would demote a
	// higher-ranked linked thumbnail. Refresh-only override keeps the
	// high-priority pick while preserving refresh through lower-ranked sources.
	if preferredThumbnail != "" && revalidate && scraperThumbnailCanRefresh(actress.ThumbURL) && !actressThumbnailNeedsResolution(actress.ThumbURL) {
		candidate.ThumbURL = preferredThumbnail
	}
	if conflict {
		result.Conflict = true
		result.Messages = append(result.Messages, "conflicting_metadata")
		return result, nil
	}
	fields, err := fillMetadata(actress.ID, actress.DMMID, candidate)
	if err != nil {
		return nil, err
	}
	if preferredThumbnail != "" && strings.TrimSpace(actress.ThumbURL) != "" && preferredThumbnail != strings.TrimSpace(actress.ThumbURL) && (actressThumbnailNeedsResolution(actress.ThumbURL) || (revalidate && scraperThumbnailCanRefresh(actress.ThumbURL))) {
		replaced, replaceErr := replaceThumbnail(actress.ID, actress.DMMID, actress.ThumbURL, preferredThumbnail)
		if replaceErr != nil {
			return nil, replaceErr
		}
		if replaced {
			fields = append(fields, "thumb_url")
		}
	}
	result.UpdatedFields = append(result.UpdatedFields, fields...)
	if len(resolverFailures) > 0 {
		result.Messages = append(result.Messages, "resolver_error:"+strings.Join(resolverFailures, ","))
		if result.Warning == "" {
			result.Warning = "resolver_error: " + strings.Join(resolverFailures, ",")
		}
	}
	if len(fields) == 0 {
		if len(result.UpdatedFields) > 0 {
			return result, nil
		}
		if revalidate && actressMetadataVerified(actress, matches) {
			result.Verified = true
			result.Messages = append(result.Messages, "verified_no_changes")
			return result, nil
		}
		if len(resolverFailures) > 0 && len(metadataScrapers) > 0 && len(resolverFailures) == len(metadataScrapers) {
			return result, errors.Join(resolverErrors...)
		}
		result.Messages = append(result.Messages, "no_verified_metadata")
		return result, nil
	}
	updated, err := actressRepo.FindByID(ctx, actress.ID)
	if err != nil {
		return nil, err
	}
	if actressNeedsMetadata(updated) {
		result.Warning = "partial_metadata"
	}
	return result, nil
}

// actressThumbnailSessionValidator ...
type actressThumbnailSessionValidator interface {
	ValidateActressThumbnail(context.Context, string) error
}

// validateActressThumbnail ...
func validateActressThumbnail(ctx context.Context, scraper models.Scraper, fallback func(context.Context, string) error, rawURL string) error {
	if validator, ok := scraper.(actressThumbnailSessionValidator); ok {
		return validator.ValidateActressThumbnail(ctx, rawURL)
	}
	if fallback != nil {
		return fallback(ctx, rawURL)
	}
	return nil
}

// authoritativeActressScrapers ...
func authoritativeActressScrapers(registry scraperutil.ScraperInstancesInterface, scrapeActress bool, priority []string) []models.Scraper {
	if registry == nil {
		return nil
	}
	result := make([]models.Scraper, 0, 2)
	for _, scraper := range registry.GetEnabledInstances() {
		if scraper == nil || !scraper.Config().ShouldScrapeActress(scrapeActress) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(scraper.Name())) {
		case resolverNameDMM, "r18dev", "r18.dev":
			result = append(result, scraper)
		}
	}
	return orderScrapersByConfiguredPriority(result, priority)
}

// orderScrapersByConfiguredPriority stably sorts named scrapers to the front
// in configured priority order; unlisted scrapers keep registry order after.
func orderScrapersByConfiguredPriority(scrapers []models.Scraper, priority []string) []models.Scraper {
	if len(priority) == 0 || len(scrapers) < 2 {
		return scrapers
	}
	rank := make(map[string]int, len(priority))
	for i, name := range priority {
		key := strings.ToLower(strings.TrimSpace(name))
		if key == "" {
			continue
		}
		if _, ok := rank[key]; !ok {
			rank[key] = i
		}
	}
	ordered := append([]models.Scraper(nil), scrapers...)
	sort.SliceStable(ordered, func(i, j int) bool {
		iv, iok := rank[strings.ToLower(strings.TrimSpace(ordered[i].Name()))]
		jv, jok := rank[strings.ToLower(strings.TrimSpace(ordered[j].Name()))]
		switch {
		case iok && jok:
			return iv < jv
		case iok:
			return true
		default:
			return false
		}
	})
	return ordered
}

type linkedIdentityRecoveryOptions struct {
	expectedSource                 models.Actress
	mergeActressesWithSource       func(uint, uint, models.Actress) (*database.ActressMergeResult, error)
	mergeActressesWithTargetSource func(uint, uint, models.Actress, models.Actress) (*database.ActressMergeResult, error)
	assignDMMIDWithSource          func(uint, int, models.Actress) (bool, error)
}

// recoverMissingDMMIdentity ...
func recoverMissingDMMIdentity(ctx context.Context, actress *models.Actress, actressRepo *database.ActressRepository, movieRepo *database.MovieRepository, scrapers []models.Scraper, mergeActresses func(uint, uint) (*database.ActressMergeResult, error), assignDMMID func(uint, int) (bool, error), sourceOptions ...linkedIdentityRecoveryOptions) (*models.Actress, []sourcedActressMatch, []string, error) {
	expectedSource := models.Actress{}
	var mergeActressesWithSource func(uint, uint, models.Actress) (*database.ActressMergeResult, error)
	var mergeActressesWithTargetSource func(uint, uint, models.Actress, models.Actress) (*database.ActressMergeResult, error)
	var assignDMMIDWithSource func(uint, int, models.Actress) (bool, error)
	if len(sourceOptions) > 0 {
		expectedSource = sourceOptions[0].expectedSource
		mergeActressesWithSource = sourceOptions[0].mergeActressesWithSource
		mergeActressesWithTargetSource = sourceOptions[0].mergeActressesWithTargetSource
		assignDMMIDWithSource = sourceOptions[0].assignDMMIDWithSource
	}
	merge := func(targetID, sourceID uint) (*database.ActressMergeResult, error) {
		if mergeActressesWithSource != nil {
			return mergeActressesWithSource(targetID, sourceID, expectedSource)
		}
		return mergeActresses(targetID, sourceID)
	}
	mergeWithTarget := func(target *models.Actress, sourceID uint) (*database.ActressMergeResult, error) {
		if mergeActressesWithTargetSource != nil {
			return mergeActressesWithTargetSource(target.ID, sourceID, *target, expectedSource)
		}
		return merge(target.ID, sourceID)
	}
	assign := func(id uint, dmmID int) (bool, error) {
		if assignDMMIDWithSource != nil {
			return assignDMMIDWithSource(id, dmmID, expectedSource)
		}
		return assignDMMID(id, dmmID)
	}
	names := actressIdentityNames(actress)
	canonicalByID := make(map[uint]*models.Actress)
	for _, name := range names {
		candidates, err := actressRepo.FindAllByJapaneseName(ctx, name)
		if err != nil {
			return nil, nil, nil, err
		}
		for i := range candidates {
			candidate := &candidates[i]
			if candidate.ID != actress.ID && candidate.DMMID > 0 && canMergeMissingDMMActress(actress, candidate) {
				canonicalByID[candidate.ID] = candidate
			}
		}
	}
	if len(canonicalByID) > 1 {
		return nil, nil, nil, nil
	}
	if len(canonicalByID) == 1 {
		for _, canonical := range canonicalByID {
			merged, err := mergeWithTarget(canonical, actress.ID)
			if err != nil {
				return nil, nil, nil, err
			}
			return &merged.MergedActress, nil, []string{"merged_duplicate"}, nil
		}
	}
	candidates, err := linkedActressCandidates(ctx, movieRepo, actress.ID, scrapers)
	if err != nil {
		// Tolerate degraded sets that still carry data (codex round 11): the
		// partial marker means some scrapers failed but the usable ones are
		// kept; discarding them wastes a recoverable identity.
		var partial partialCandidatesError
		if !errors.As(err, &partial) {
			return nil, nil, nil, err
		}
		// fall through with the usable candidates
	}
	matchesByDMM := make(map[int]sourcedActressMatch)
	for _, candidate := range candidates {
		if candidate.info.DMMID > 0 && identityCandidateMatches(names, candidate.info) {
			matchesByDMM[candidate.info.DMMID] = candidate
		}
	}
	if len(matchesByDMM) != 1 {
		// An empty narrowing after a partial failure is NOT "no identity":
		// the surviving candidates pointed elsewhere and the transient source
		// carried the real one. Retry instead of terminal-skip.
		if err != nil {
			return nil, nil, nil, err
		}
		return nil, nil, nil, nil
	}
	var dmmID int
	var match sourcedActressMatch
	for dmmID, match = range matchesByDMM {
		break
	}
	existing, findErr := actressRepo.FindByDMMID(ctx, dmmID)
	if findErr == nil && existing.ID != actress.ID {
		if !canMergeMissingDMMActress(actress, existing) {
			return nil, nil, nil, nil
		}
		merged, mergeErr := mergeWithTarget(existing, actress.ID)
		if mergeErr != nil {
			return nil, nil, nil, mergeErr
		}
		return &merged.MergedActress, []sourcedActressMatch{match}, []string{"merged_duplicate"}, nil
	}
	if findErr != nil && !database.IsNotFound(findErr) {
		return nil, nil, nil, findErr
	}
	assigned, assignErr := assign(actress.ID, dmmID)
	if assignErr != nil {
		if !database.IsUniqueConstraint(assignErr) {
			return nil, nil, nil, assignErr
		}
		canonical, reloadErr := actressRepo.FindByDMMID(ctx, dmmID)
		if reloadErr != nil {
			return nil, nil, nil, fmt.Errorf("reload canonical actress after DMM ID assignment race: %w", reloadErr)
		}
		if canonical.ID == actress.ID {
			return canonical, []sourcedActressMatch{match}, []string{"dmm_id"}, nil
		}
		if !canMergeMissingDMMActress(actress, canonical) {
			return nil, nil, nil, nil
		}
		merged, mergeErr := mergeWithTarget(canonical, actress.ID)
		if mergeErr != nil {
			return nil, nil, nil, mergeErr
		}
		return &merged.MergedActress, []sourcedActressMatch{match}, []string{"merged_duplicate"}, nil
	}
	if !assigned {
		return nil, nil, nil, nil
	}
	updated, loadErr := actressRepo.FindByID(ctx, actress.ID)
	if loadErr != nil {
		return nil, nil, nil, loadErr
	}
	return updated, []sourcedActressMatch{match}, []string{"dmm_id"}, nil
}

// lookupActressCache ...
func lookupActressCache(actress *models.Actress, lookup func(int, string, string, string) (models.ActressInfo, bool)) (models.ActressInfo, bool) {
	if actress == nil || lookup == nil {
		return models.ActressInfo{}, false
	}
	match, ok := lookup(actress.DMMID, actress.JapaneseName, actress.FirstName, actress.LastName)
	if !ok {
		return models.ActressInfo{}, false
	}
	if actress.DMMID > 0 {
		if match.DMMID > 0 && match.DMMID != actress.DMMID {
			return models.ActressInfo{}, false
		}
		match.DMMID = actress.DMMID
		return match, true
	}
	if match.DMMID <= 0 {
		return models.ActressInfo{}, false
	}
	return match, true
}

// cacheMatchesCanonical ...
func cacheMatchesCanonical(cached models.ActressInfo, existing *models.Actress) bool {
	if existing == nil || cached.DMMID > 0 && existing.DMMID != cached.DMMID {
		return false
	}
	existingJapanese := strings.TrimSpace(existing.JapaneseName)
	cachedJapanese := strings.TrimSpace(cached.JapaneseName)
	if existingJapanese == "" || cachedJapanese == "" {
		return false
	}
	if strings.EqualFold(existingJapanese, cachedJapanese) {
		return true
	}
	for _, alias := range cached.Aliases {
		if strings.EqualFold(strings.TrimSpace(alias), existingJapanese) {
			return true
		}
	}
	for _, alias := range strings.Split(existing.Aliases, "|") {
		alias = strings.TrimSpace(alias)
		if alias == "" {
			continue
		}
		if strings.EqualFold(alias, cachedJapanese) {
			return true
		}
	}
	return !hasJapaneseText(existingJapanese) && hasJapaneseText(cachedJapanese)
}

// hasJapaneseText ...
func hasJapaneseText(s string) bool {
	for _, r := range s {
		if r >= 0x3040 && r <= 0x30FF || r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// rankForSource ties each cached-match field to its source's rank; the cache
// is a DMM-sourced snapshot, so it ranks exactly like the live dmm name.
// actressIdentityNames ...
func actressIdentityNames(actress *models.Actress) []string {
	if actress == nil {
		return nil
	}
	values := []string{actress.JapaneseName, actress.Aliases}
	seen := make(map[string]struct{})
	names := make([]string, 0, 4)
	for _, value := range values {
		value = strings.NewReplacer("（", "(", "）", ")", "，", ",").Replace(value)
		parts := strings.FieldsFunc(value, func(r rune) bool {
			return r == '(' || r == ')' || r == ',' || r == ';' || r == '|' || r == '\n'
		})
		for _, part := range parts {
			name := strings.TrimSpace(part)
			key := strings.ToLower(name)
			if name == "" {
				continue
			}
			if _, exists := seen[key]; exists {
				continue
			}
			seen[key] = struct{}{}
			names = append(names, name)
		}
	}
	// Romanized-only actresses (to whom ListSyncCandidates explicitly admits
	// no-DMM-ID rows with only first/last names) still need linked-movie
	// recovery to match: add both orders of the romanized full name.
	firstName := strings.TrimSpace(actress.FirstName)
	lastName := strings.TrimSpace(actress.LastName)
	switch {
	case firstName != "" && lastName != "":
		names = appendDedupLower(names, firstName+" "+lastName)
		names = appendDedupLower(names, lastName+" "+firstName)
	case firstName != "":
		names = appendDedupLower(names, firstName)
	case lastName != "":
		names = appendDedupLower(names, lastName)
	}
	return names
}

func appendDedupLower(names []string, value string) []string {
	value = strings.TrimSpace(value)
	for _, existing := range names {
		if strings.EqualFold(existing, value) {
			return names
		}
	}
	return append(names, value)
}

// identityCandidateMatches reports whether a linked-movie result matches any
// known actress identity, checking the Japanese name first, then both
// orderings of the romanized full name.
func identityCandidateMatches(names []string, candidate models.ActressInfo) bool {
	if identityNameMatches(names, candidate.JapaneseName) {
		return true
	}
	firstName := strings.TrimSpace(candidate.FirstName)
	lastName := strings.TrimSpace(candidate.LastName)
	if firstName != "" && lastName != "" {
		if identityNameMatches(names, firstName+" "+lastName) || identityNameMatches(names, lastName+" "+firstName) {
			return true
		}
	}
	// Singleton fallback: some catalogs store a one-token Latin name under
	// FirstName. The candidate predicate admits those singleton rows, and
	// without this they'd be silently discarded (codex phase 3, latest head).
	return identityNameMatches(names, firstName) || identityNameMatches(names, lastName)
}

// identityNameMatches ...
func identityNameMatches(names []string, candidate string) bool {
	candidate = strings.TrimSpace(candidate)
	for _, name := range names {
		if strings.EqualFold(name, candidate) {
			return true
		}
	}
	return false
}

// canMergeMissingDMMActress ...
func canMergeMissingDMMActress(target, canonical *models.Actress) bool {
	if target == nil || canonical == nil || target.DMMID > 0 || canonical.DMMID <= 0 {
		return false
	}
	compatible := func(left, right string) bool {
		left = strings.TrimSpace(left)
		right = strings.TrimSpace(right)
		return left == "" || right == "" || strings.EqualFold(left, right)
	}
	return compatible(target.FirstName, canonical.FirstName) &&
		compatible(target.LastName, canonical.LastName) &&
		compatible(target.ThumbURL, canonical.ThumbURL)
}

// sourcedActressMatch remembers which scraper produced a linked-movie
// candidate so configured priorities still apply to fallback resolution.
type sourcedActressMatch struct {
	info    models.ActressInfo
	source  string
	scraper models.Scraper // originating scraper — needed so thumbnail screening can reuse its session (codex round 9)
}

// linkedActressCandidates ...
func linkedActressCandidates(ctx context.Context, movieRepo *database.MovieRepository, actressID uint, scrapers []models.Scraper) ([]sourcedActressMatch, error) {
	movies, err := linkedActressMovies(ctx, movieRepo, actressID)
	if err != nil {
		return nil, err
	}
	candidates := make([]sourcedActressMatch, 0)
	var searchErrs []error
	for _, movie := range movies {
		for _, scraper := range scrapers {
			// scraped ...
			var scraped *models.ScraperResult
			if handler, ok := scraper.(models.URLHandler); ok && handler.CanHandleURL(strings.TrimSpace(movie.SourceURL)) {
				scraped, err = handler.ScrapeURL(ctx, strings.TrimSpace(movie.SourceURL))
			} else {
				query := strings.TrimSpace(movie.ID)
				if query == "" {
					query = strings.TrimSpace(movie.ContentID)
				}
				if query == "" {
					continue
				}
				scraped, err = scraper.Search(ctx, query)
			}
			if err != nil {
				// An ordinary catalog miss is data, not an outage: filtering it
				// out keeps only genuine transient failures for the retry path.
				if se, ok := models.AsScraperError(err); ok && se.Kind == models.ScraperErrorKindNotFound {
					continue
				}
				searchErrs = append(searchErrs, fmt.Errorf("%s: %w", strings.ToLower(strings.TrimSpace(scraper.Name())), err))
				continue
			}
			if scraped != nil {
				for _, result := range scraped.Actresses {
					candidates = append(candidates, sourcedActressMatch{info: result, source: strings.ToLower(strings.TrimSpace(scraper.Name())), scraper: scraper})
				}
			}
		}
	}
	if joined := errors.Join(searchErrs...); joined != nil {
		if len(candidates) == 0 {
			return nil, joined
		}
		// Partial failures with usable matches: degrade gracefully, but surface
		// the aggregate so a caller whose filter empties the set falls into the
		// retry path instead of a bogus skip (codex P2 round 9).
		logging.Debugf("Actress sync: linked-movie identity used %d candidates despite scraper failures: %v", len(candidates), joined)
		return candidates, partialCandidatesError{err: joined}
	}
	return candidates, nil
}

// partialCandidatesError marks a usable candidate set that rode past transient
// scraper failures; surfaced when the caller's filter empties the set.
type partialCandidatesError struct{ err error }

func (e partialCandidatesError) Error() string { return e.err.Error() }
func (e partialCandidatesError) Unwrap() error { return e.err }

// needsLinkedActressFallback ...
func needsLinkedActressFallback(actress *models.Actress, matches []rankedActressMatch, deterministic bool) bool {
	if actress == nil || actress.DMMID <= 0 {
		return false
	}
	candidate, conflict := resolveActressInfo(actress, matches, deterministic)
	if conflict {
		return false
	}
	resolved := *actress
	if strings.TrimSpace(resolved.FirstName) == "" {
		resolved.FirstName = candidate.FirstName
	}
	if strings.TrimSpace(resolved.LastName) == "" {
		resolved.LastName = candidate.LastName
	}
	if strings.TrimSpace(resolved.JapaneseName) == "" {
		resolved.JapaneseName = candidate.JapaneseName
	}
	if actressThumbnailNeedsResolution(resolved.ThumbURL) && strings.TrimSpace(candidate.ThumbURL) != "" {
		resolved.ThumbURL = candidate.ThumbURL
	}
	return actressNeedsMetadata(&resolved)
}

// linkedActressMatches ...
func linkedActressMatches(ctx context.Context, movieRepo *database.MovieRepository, actressID uint, dmmID int, scrapers []models.Scraper) ([]sourcedActressMatch, error) {
	candidates, err := linkedActressCandidates(ctx, movieRepo, actressID, scrapers)
	if err != nil {
		// A degraded-but-usable set rides a partial marker: filtering may still
		// discard every candidate as "other cast member", and that must land in
		// the retry path rather than a mislabeled skip.
		var partial partialCandidatesError
		if errors.As(err, &partial) && len(candidates) > 0 {
			matches := make([]sourcedActressMatch, 0)
			for _, candidate := range candidates {
				if candidate.info.DMMID == dmmID {
					matches = append(matches, candidate)
				}
			}
			if len(matches) == 0 {
				return nil, err
			}
			return matches, nil
		}
		return nil, err
	}
	matches := make([]sourcedActressMatch, 0)
	for _, candidate := range candidates {
		if candidate.info.DMMID == dmmID {
			matches = append(matches, candidate)
		}
	}
	return matches, nil
}

// actressMetadataScrapers ...
func actressMetadataScrapers(registry scraperutil.ScraperInstancesInterface, scrapeActress bool, priority []string) []models.Scraper {
	if registry == nil {
		return nil
	}
	result := make([]models.Scraper, 0, 3)
	for _, scraper := range registry.GetEnabledInstances() {
		if scraper == nil || !scraper.Config().ShouldScrapeActress(scrapeActress) {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(scraper.Name())) {
		case resolverNameDMM, "r18dev", "r18.dev", resolverNameJavDB, resolverNameMinnanoAV:
			result = append(result, scraper)
		}
	}
	return orderScrapersByConfiguredPriority(result, priority)
}

const (
	// Name-keyed resolvers: resolve only from a known Japanese name.
	resolverNameJavDB     = "javdb"
	resolverNameMinnanoAV = "minnanoav"
	// resolverNameDMM names the authoritative DMM source; the built-in
	// actress cache rides its rank/priority slot.
	resolverNameDMM = "dmm"
)

// priorityListContains reports whether a configured priority list names the
// given source (case-insensitive, trimmed). Used to admit the built-in cache
// when DMM is an eligible source without unpacking full ranking.
func priorityListContains(priority []string, name string) bool {
	for _, entry := range priority {
		if strings.EqualFold(strings.TrimSpace(entry), name) {
			return true
		}
	}
	return false
}

// actressCacheOutranksAll reports whether the built-in (DMM-sourced) cache
// is the highest-ranked eligible source. The actress field priority ranks
// first; when it is empty the global scrapers priority is the documented
// fallback (codex); with neither configured the legacy order leads with dmm.
// When false, the cache competes as a ranked match instead of filling
// blanks directly so higher-ranked sources can win their fields.
func actressCacheOutranksAll(fieldPriority, global []string) bool {
	first := func(list []string) string {
		for _, name := range list {
			if key := strings.ToLower(strings.TrimSpace(name)); key != "" {
				return key
			}
		}
		return ""
	}
	top := first(fieldPriority)
	if top == "" {
		top = first(global)
	}
	if top == "" {
		return true
	}
	return top == resolverNameDMM
}

// cacheAllowedForPriority reports whether the built-in (DMM-sourced) actress
// cache may write fields. Skipped under __skip__; under an explicit priority it
// is admitted iff DMM itself is (the cache is a snapshot of DMM-sourced data,
// so it rides DMM's admission). Under no explicit priority it is admitted.
func cacheAllowedForPriority(skipped bool, priority []string) bool {
	if skipped {
		return false
	}
	return len(priority) == 0 || priorityListContains(priority, resolverNameDMM)
}

// nameIsKeyed identifies resolvers whose lookup needs a Japanese name.
func nameIsKeyed(name string) bool {
	switch name {
	case resolverNameJavDB, resolverNameMinnanoAV:
		return true
	}
	return false
}

// filterActressResolverFields clears fields the resolver does not advertise
// via models.ActressFieldCapable, so an actress profile that (for instance)
// only carries a Japanese name and avatar cannot overwrite first/last names.
// Undeclared resolvers keep all their fields.
func filterActressResolverFields(scraper models.Scraper, info models.ActressInfo) models.ActressInfo {
	if !models.ResolverSupportsActressField(scraper, "actress_japanese_name") {
		info.JapaneseName = ""
	}
	if !models.ResolverSupportsActressField(scraper, "actress_first_name") {
		info.FirstName = ""
	}
	if !models.ResolverSupportsActressField(scraper, "actress_last_name") {
		info.LastName = ""
	}
	if !models.ResolverSupportsActressField(scraper, "actress_url") {
		info.ThumbURL = ""
	}
	return info
}

// linkedActressMovies ...
func linkedActressMovies(ctx context.Context, repo *database.MovieRepository, actressID uint) ([]models.Movie, error) {
	if repo == nil {
		return nil, nil
	}
	movies := make([]models.Movie, 0)
	err := repo.GetDB().WithContext(ctx).Model(&models.Movie{}).Joins("JOIN movie_actresses ON movie_actresses.movie_content_id = movies.content_id").Where("movie_actresses.actress_id = ?", actressID).Order("movies.created_at DESC").Limit(maxActressSyncMovies).Find(&movies).Error
	if err != nil {
		return nil, fmt.Errorf("list linked actress movies: %w", err)
	}
	return movies, nil
}

// rankedActressMatch attaches a source rank to a resolved candidate, so
// configured priorities can resolve picks deterministically.
type rankedActressMatch struct {
	info models.ActressInfo
	rank int
	// fromCache marks snapshot data from the built-in DMM actress cache: it
	// competes in ranked field selection but must never count as
	// verification evidence — a static snapshot is not fresh upstream
	// confirmation (codex).
	fromCache bool
}

// resolveActressInfo ...
func resolveActressInfo(actress *models.Actress, matches []rankedActressMatch, deterministic bool) (models.ActressInfo, bool) {
	if actress == nil {
		return models.ActressInfo{}, false
	}
	if deterministic {
		return resolveActressInfoByRank(actress, matches)
	}
	candidate := models.ActressInfo{DMMID: actress.DMMID}
	japaneseNames := map[string]struct{}{}
	firstNames := map[string]struct{}{}
	lastNames := map[string]struct{}{}
	conflict := false
	for _, match := range matches {
		if match.info.DMMID != actress.DMMID {
			continue
		}
		if actressThumbnailNeedsResolution(actress.ThumbURL) && candidate.ThumbURL == "" && !models.IsKnownInvalidDMMActressThumbnail(match.info.ThumbURL) {
			candidate.ThumbURL = strings.TrimSpace(match.info.ThumbURL)
		}
		if strings.TrimSpace(actress.JapaneseName) == "" {
			conflict = mergeActressValue(&candidate.JapaneseName, match.info.JapaneseName, japaneseNames) || conflict
		}
		if strings.TrimSpace(actress.FirstName) == "" {
			conflict = mergeActressValue(&candidate.FirstName, match.info.FirstName, firstNames) || conflict
		}
		if strings.TrimSpace(actress.LastName) == "" {
			conflict = mergeActressValue(&candidate.LastName, match.info.LastName, lastNames) || conflict
		}
	}
	return candidate, conflict
}

// resolveActressInfoByRank resolves each field to the best-ranked (most
// preferred) configured source. Differing values from lower-priority sources
// neither override nor conflict; only same-rank disagreements stay ambiguous.
func resolveActressInfoByRank(actress *models.Actress, matches []rankedActressMatch) (models.ActressInfo, bool) {
	candidate := models.ActressInfo{DMMID: actress.DMMID}
	conflict := false
	maxRank := int(^uint(0) >> 1)
	resolveField := func(dst *string, blank bool, get func(models.ActressInfo) string) {
		if !blank {
			return
		}
		best, bestRank := "", maxRank
		tie := false
		for _, match := range matches {
			value := strings.TrimSpace(get(match.info))
			if match.info.DMMID != actress.DMMID || value == "" || match.rank > bestRank {
				continue
			}
			if match.rank < bestRank {
				best, bestRank, tie = value, match.rank, false
			} else if value != best {
				tie = true
			}
		}
		if best != "" {
			*dst = best
			conflict = conflict || tie
		}
	}
	resolveField(&candidate.FirstName, strings.TrimSpace(actress.FirstName) == "", func(info models.ActressInfo) string { return info.FirstName })
	resolveField(&candidate.LastName, strings.TrimSpace(actress.LastName) == "", func(info models.ActressInfo) string { return info.LastName })
	resolveField(&candidate.JapaneseName, strings.TrimSpace(actress.JapaneseName) == "", func(info models.ActressInfo) string { return info.JapaneseName })
	if actressThumbnailNeedsResolution(actress.ThumbURL) {
		best, bestRank := "", maxRank
		for _, match := range matches {
			value := strings.TrimSpace(match.info.ThumbURL)
			if match.info.DMMID != actress.DMMID || value == "" || models.IsKnownInvalidDMMActressThumbnail(value) || match.rank >= bestRank {
				continue
			}
			best, bestRank = value, match.rank
		}
		candidate.ThumbURL = best
	}
	return candidate, conflict
}

// actressSyncSkipSentinel reports whether the priority list is the
// deliberate-suppression marker ("["__skip__"]" from the settings UI).
func actressSyncSkipSentinel(priority []string) bool {
	return len(priority) == 1 && strings.EqualFold(strings.TrimSpace(priority[0]), "__skip__")
}

// restrictScrapersByPriorityNames keeps only scrapers named in priority:
// a non-empty field override means "consult these, exclusively".
func restrictScrapersByPriorityNames(scrapers []models.Scraper, priority []string) []models.Scraper {
	allowed := make(map[string]struct{}, len(priority))
	for _, name := range priority {
		if key := strings.ToLower(strings.TrimSpace(name)); key != "" {
			allowed[key] = struct{}{}
		}
	}
	out := make([]models.Scraper, 0, len(scrapers))
	for _, scraper := range scrapers {
		if scraper == nil {
			continue
		}
		if _, ok := allowed[strings.ToLower(strings.TrimSpace(scraper.Name()))]; ok {
			out = append(out, scraper)
		}
	}
	return out
}

// actressSyncThumbnailRank positions thumbnail sources by the configured
// actress field priority, then the global scraper priority, and finally the
// legacy source-quality order for sources neither list mentions.
func actressSyncThumbnailRank(fieldPriority, global []string) func(string) int {
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
	base := offset + len(global)
	return func(name string) int {
		key := strings.ToLower(strings.TrimSpace(name))
		if rank, ok := index[key]; ok {
			return rank
		}
		return base + actressThumbnailSourcePriority(key)
	}
}

// actressThumbnailSourcePriority ...
func actressThumbnailSourcePriority(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case resolverNameDMM:
		return 0
	case "minnanoav":
		return 1
	case "javdb":
		return 2
	default:
		return 3
	}
}

// scraperThumbnailCanRefresh ...
func scraperThumbnailCanRefresh(raw string) bool {
	parsed, err := url.Parse(strings.TrimSpace(raw))
	if err != nil {
		return false
	}
	host := strings.ToLower(parsed.Hostname())
	return host == "jdbstatic.com" || strings.HasSuffix(host, ".jdbstatic.com") ||
		host == "pics.dmm.co.jp" || host == "awsimgsrc.dmm.com" || host == "awsimgsrc.dmm.co.jp" ||
		host == "minnano-av.com" || strings.HasSuffix(host, ".minnano-av.com")
}

// actressInfoFields ...
func actressInfoFields(info models.ActressInfo) []string {
	fields := make([]string, 0, 4)
	if strings.TrimSpace(info.FirstName) != "" {
		fields = append(fields, "first_name")
	}
	if strings.TrimSpace(info.LastName) != "" {
		fields = append(fields, "last_name")
	}
	if strings.TrimSpace(info.JapaneseName) != "" {
		fields = append(fields, "japanese_name")
	}
	if strings.TrimSpace(info.ThumbURL) != "" && !models.IsKnownInvalidDMMActressThumbnail(info.ThumbURL) {
		fields = append(fields, "thumb_url")
	}
	return fields
}

// actressMetadataVerified ...
func actressMetadataVerified(actress *models.Actress, matches []rankedActressMatch) bool {
	if actress == nil {
		return false
	}
	for _, match := range matches {
		if match.fromCache || match.info.DMMID != actress.DMMID {
			continue
		}
		if strings.TrimSpace(match.info.JapaneseName) != "" && strings.TrimSpace(match.info.JapaneseName) == strings.TrimSpace(actress.JapaneseName) {
			return true
		}
		if strings.TrimSpace(match.info.FirstName) != "" && strings.TrimSpace(match.info.LastName) != "" &&
			strings.EqualFold(strings.TrimSpace(match.info.FirstName), strings.TrimSpace(actress.FirstName)) &&
			strings.EqualFold(strings.TrimSpace(match.info.LastName), strings.TrimSpace(actress.LastName)) {
			return true
		}
		if strings.TrimSpace(match.info.ThumbURL) != "" && strings.TrimSpace(match.info.ThumbURL) == strings.TrimSpace(actress.ThumbURL) {
			return true
		}
	}
	return false
}

// mergeActressValue ...
func mergeActressValue(target *string, source string, values map[string]struct{}) bool {
	value := strings.TrimSpace(source)
	if value == "" {
		return false
	}
	values[value] = struct{}{}
	if strings.TrimSpace(*target) == "" {
		*target = value
	}
	return len(values) > 1
}

// actressNeedsMetadata ...
func actressNeedsMetadata(actress *models.Actress) bool {
	if actress == nil {
		return true
	}
	return actressThumbnailNeedsResolution(actress.ThumbURL) || strings.TrimSpace(actress.JapaneseName) == "" || (strings.TrimSpace(actress.FirstName) == "" && strings.TrimSpace(actress.LastName) == "")
}

// actressThumbnailNeedsResolution ...
func actressThumbnailNeedsResolution(thumbnail string) bool {
	return strings.TrimSpace(thumbnail) == "" || models.IsKnownInvalidDMMActressThumbnail(thumbnail)
}
