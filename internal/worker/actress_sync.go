package worker

import (
	"context"
	"fmt"
	"net/url"
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
	matches := make([]models.ActressInfo, 0)
	scrapers := authoritativeActressScrapers(registry)
	metadataScrapers := actressMetadataScrapers(registry)
	cachedSource := *actress
	cacheMatch, cacheHit := lookupActressCache(actress, lookupCache)
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
		if actress.DMMID > 0 || !cacheHit || cacheMatch.DMMID <= 0 {
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
		matches = append(matches, recoveredMatches...)
		result.UpdatedFields = append(result.UpdatedFields, recoveredFields...)
	}
	if !revalidate && cacheHit && cacheMatch.DMMID == actress.DMMID {
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
	if revalidate || actressNeedsMetadata(actress) {
		for _, scraper := range metadataScrapers {
			name := strings.ToLower(strings.TrimSpace(scraper.Name()))
			if resolver, ok := scraper.(models.ActressMetadataResolver); ok {
				logging.Debugf("Actress sync: resolving DMM ID %d with %s", actress.DMMID, name)
				sourceInput := resolverInput
				if name != "javdb" {
					sourceInput.ThumbURL = ""
				}
				metadata := resolver.ResolveActressMetadata(ctx, sourceInput)
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
					logging.Debugf("Actress sync: %s returned no metadata for DMM ID %d", name, actress.DMMID)
				} else {
					logging.Debugf("Actress sync: %s returned fields for DMM ID %d: %s", name, actress.DMMID, strings.Join(fields, ", "))
				}
				if metadata.DMMID == actress.DMMID {
					matches = append(matches, metadata)
					if strings.TrimSpace(metadata.ThumbURL) != "" && !models.IsKnownInvalidDMMActressThumbnail(metadata.ThumbURL) {
						priority := actressThumbnailSourcePriority(name)
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
					matches = append(matches, models.ActressInfo{DMMID: actress.DMMID, ThumbURL: thumbnail})
					priority := actressThumbnailSourcePriority(name)
					if priority < preferredThumbnailPriority {
						preferredThumbnail = thumbnail
						preferredThumbnailSource = name
						preferredThumbnailPriority = priority
					}
				}
			}
		}
	}
	if revalidate && cacheHit && cacheMatch.DMMID == actress.DMMID {
		if fallback := cacheFallbackMatch(actress, matches, cacheMatch); len(actressInfoFields(fallback)) > 0 {
			matches = append(matches, fallback)
		}
	}
	if needsLinkedActressFallback(actress, matches) {
		linkedMatches, linkedErr := linkedActressMatches(ctx, movieRepo, actress.ID, actress.DMMID, scrapers)
		if linkedErr != nil {
			return nil, linkedErr
		}
		matches = append(matches, linkedMatches...)
	}

	if preferredThumbnail != "" {
		logging.Debugf("Actress sync: selected %s thumbnail for DMM ID %d", preferredThumbnailSource, actress.DMMID)
	}
	candidate, conflict := resolveActressInfo(actress, matches)
	if preferredThumbnail != "" && (actressThumbnailNeedsResolution(actress.ThumbURL) || (revalidate && scraperThumbnailCanRefresh(actress.ThumbURL))) {
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
	if len(fields) == 0 {
		if len(result.UpdatedFields) > 0 {
			return result, nil
		}
		if revalidate && actressMetadataVerified(actress, matches) {
			result.Verified = true
			result.Messages = append(result.Messages, "verified_no_changes")
			return result, nil
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
func authoritativeActressScrapers(registry scraperutil.ScraperInstancesInterface) []models.Scraper {
	if registry == nil {
		return nil
	}
	result := make([]models.Scraper, 0, 2)
	for _, scraper := range registry.GetEnabledInstances() {
		if scraper == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(scraper.Name())) {
		case "dmm", "r18dev", "r18.dev":
			result = append(result, scraper)
		}
	}
	return result
}

type linkedIdentityRecoveryOptions struct {
	expectedSource                 models.Actress
	mergeActressesWithSource       func(uint, uint, models.Actress) (*database.ActressMergeResult, error)
	mergeActressesWithTargetSource func(uint, uint, models.Actress, models.Actress) (*database.ActressMergeResult, error)
	assignDMMIDWithSource          func(uint, int, models.Actress) (bool, error)
}

// recoverMissingDMMIdentity ...
func recoverMissingDMMIdentity(ctx context.Context, actress *models.Actress, actressRepo *database.ActressRepository, movieRepo *database.MovieRepository, scrapers []models.Scraper, mergeActresses func(uint, uint) (*database.ActressMergeResult, error), assignDMMID func(uint, int) (bool, error), sourceOptions ...linkedIdentityRecoveryOptions) (*models.Actress, []models.ActressInfo, []string, error) {
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
		return nil, nil, nil, err
	}
	matchesByDMM := make(map[int]models.ActressInfo)
	for _, candidate := range candidates {
		if candidate.DMMID > 0 && identityNameMatches(names, candidate.JapaneseName) {
			matchesByDMM[candidate.DMMID] = candidate
		}
	}
	if len(matchesByDMM) != 1 {
		return nil, nil, nil, nil
	}
	var dmmID int
	var match models.ActressInfo
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
		return &merged.MergedActress, []models.ActressInfo{match}, []string{"merged_duplicate"}, nil
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
			return canonical, []models.ActressInfo{match}, []string{"dmm_id"}, nil
		}
		if !canMergeMissingDMMActress(actress, canonical) {
			return nil, nil, nil, nil
		}
		merged, mergeErr := mergeWithTarget(canonical, actress.ID)
		if mergeErr != nil {
			return nil, nil, nil, mergeErr
		}
		return &merged.MergedActress, []models.ActressInfo{match}, []string{"merged_duplicate"}, nil
	}
	if !assigned {
		return nil, nil, nil, nil
	}
	updated, loadErr := actressRepo.FindByID(ctx, actress.ID)
	if loadErr != nil {
		return nil, nil, nil, loadErr
	}
	return updated, []models.ActressInfo{match}, []string{"dmm_id"}, nil
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

// cacheFallbackMatch ...
func cacheFallbackMatch(actress *models.Actress, matches []models.ActressInfo, cached models.ActressInfo) models.ActressInfo {
	fallback := models.ActressInfo{DMMID: cached.DMMID}
	hasValue := func(value func(models.ActressInfo) string) bool {
		for _, match := range matches {
			if match.DMMID == cached.DMMID && strings.TrimSpace(value(match)) != "" {
				return true
			}
		}
		return false
	}
	if strings.TrimSpace(actress.FirstName) == "" && !hasValue(func(info models.ActressInfo) string { return info.FirstName }) {
		fallback.FirstName = cached.FirstName
	}
	if strings.TrimSpace(actress.LastName) == "" && !hasValue(func(info models.ActressInfo) string { return info.LastName }) {
		fallback.LastName = cached.LastName
	}
	if strings.TrimSpace(actress.JapaneseName) == "" && !hasValue(func(info models.ActressInfo) string { return info.JapaneseName }) {
		fallback.JapaneseName = cached.JapaneseName
	}
	if actressThumbnailNeedsResolution(actress.ThumbURL) && !hasValue(func(info models.ActressInfo) string { return info.ThumbURL }) {
		fallback.ThumbURL = cached.ThumbURL
	}
	return fallback
}

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
	return names
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

// linkedActressCandidates ...
func linkedActressCandidates(ctx context.Context, movieRepo *database.MovieRepository, actressID uint, scrapers []models.Scraper) ([]models.ActressInfo, error) {
	movies, err := linkedActressMovies(ctx, movieRepo, actressID)
	if err != nil {
		return nil, err
	}
	candidates := make([]models.ActressInfo, 0)
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
			if err == nil && scraped != nil {
				candidates = append(candidates, scraped.Actresses...)
			}
		}
	}
	return candidates, nil
}

// needsLinkedActressFallback ...
func needsLinkedActressFallback(actress *models.Actress, matches []models.ActressInfo) bool {
	if actress == nil || actress.DMMID <= 0 {
		return false
	}
	candidate, conflict := resolveActressInfo(actress, matches)
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
func linkedActressMatches(ctx context.Context, movieRepo *database.MovieRepository, actressID uint, dmmID int, scrapers []models.Scraper) ([]models.ActressInfo, error) {
	candidates, err := linkedActressCandidates(ctx, movieRepo, actressID, scrapers)
	if err != nil {
		return nil, err
	}
	matches := make([]models.ActressInfo, 0)
	for _, candidate := range candidates {
		if candidate.DMMID == dmmID {
			matches = append(matches, candidate)
		}
	}
	return matches, nil
}

// actressMetadataScrapers ...
func actressMetadataScrapers(registry scraperutil.ScraperInstancesInterface) []models.Scraper {
	if registry == nil {
		return nil
	}
	result := make([]models.Scraper, 0, 3)
	for _, scraper := range registry.GetEnabledInstances() {
		if scraper == nil {
			continue
		}
		switch strings.ToLower(strings.TrimSpace(scraper.Name())) {
		case "dmm", "r18dev", "r18.dev", "javdb", "minnanoav":
			result = append(result, scraper)
		}
	}
	return result
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

// resolveActressInfo ...
func resolveActressInfo(actress *models.Actress, matches []models.ActressInfo) (models.ActressInfo, bool) {
	if actress == nil {
		return models.ActressInfo{}, false
	}
	candidate := models.ActressInfo{DMMID: actress.DMMID}
	japaneseNames := map[string]struct{}{}
	firstNames := map[string]struct{}{}
	lastNames := map[string]struct{}{}
	conflict := false
	for _, match := range matches {
		if match.DMMID != actress.DMMID {
			continue
		}
		if actressThumbnailNeedsResolution(actress.ThumbURL) && candidate.ThumbURL == "" && !models.IsKnownInvalidDMMActressThumbnail(match.ThumbURL) {
			candidate.ThumbURL = strings.TrimSpace(match.ThumbURL)
		}
		if strings.TrimSpace(actress.JapaneseName) == "" {
			conflict = mergeActressValue(&candidate.JapaneseName, match.JapaneseName, japaneseNames) || conflict
		}
		if strings.TrimSpace(actress.FirstName) == "" {
			conflict = mergeActressValue(&candidate.FirstName, match.FirstName, firstNames) || conflict
		}
		if strings.TrimSpace(actress.LastName) == "" {
			conflict = mergeActressValue(&candidate.LastName, match.LastName, lastNames) || conflict
		}
	}
	return candidate, conflict
}

// actressThumbnailSourcePriority ...
func actressThumbnailSourcePriority(name string) int {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "dmm":
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
func actressMetadataVerified(actress *models.Actress, matches []models.ActressInfo) bool {
	if actress == nil {
		return false
	}
	for _, match := range matches {
		if match.DMMID != actress.DMMID {
			continue
		}
		if strings.TrimSpace(match.JapaneseName) != "" && strings.TrimSpace(match.JapaneseName) == strings.TrimSpace(actress.JapaneseName) {
			return true
		}
		if strings.TrimSpace(match.FirstName) != "" && strings.TrimSpace(match.LastName) != "" &&
			strings.EqualFold(strings.TrimSpace(match.FirstName), strings.TrimSpace(actress.FirstName)) &&
			strings.EqualFold(strings.TrimSpace(match.LastName), strings.TrimSpace(actress.LastName)) {
			return true
		}
		if strings.TrimSpace(match.ThumbURL) != "" && strings.TrimSpace(match.ThumbURL) == strings.TrimSpace(actress.ThumbURL) {
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
