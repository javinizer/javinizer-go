package database

import (
	"fmt"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	MergeResolutionTarget = "target"
	MergeResolutionSource = "source"
)

// mergeFieldDecision validates and normalizes a merge field decision.
// Returns "target" or "source" based on the decision string.
// Empty/whitespace or "target" returns "target", "source" returns "source".
func mergeFieldDecision(decision string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(decision)) {
	case "", "target":
		return "target", nil
	case "source":
		return "source", nil
	default:
		return "", fmt.Errorf("%w: %s", ErrActressMergeInvalidDecision, decision)
	}
}

// normalizeMergeResolutions normalizes merge resolution map by validating
// field names and decision values. Returns normalized map or error.
func normalizeMergeResolutions(resolutions map[string]string) (map[string]string, error) {
	normalized := make(map[string]string)
	allowed := map[string]bool{
		"dmm_id":        true,
		"first_name":    true,
		"last_name":     true,
		"japanese_name": true,
		"thumb_url":     true,
	}

	for field, decision := range resolutions {
		field = strings.ToLower(strings.TrimSpace(field))
		if !allowed[field] {
			return nil, fmt.Errorf("%w: %s", ErrActressMergeInvalidField, field)
		}
		normalizedDecision, err := mergeFieldDecision(decision)
		if err != nil {
			return nil, err
		}
		normalized[field] = normalizedDecision
	}

	return normalized, nil
}

// nonEmptyString returns true if the string has non-whitespace content.
func nonEmptyString(v string) bool {
	return strings.TrimSpace(v) != ""
}

// appendConflict adds a conflict to the list with the given field and values.
func appendConflict(conflicts []ActressMergeConflict, field string, targetValue, sourceValue interface{}) []ActressMergeConflict {
	conflicts = append(conflicts, ActressMergeConflict{
		Field:             field,
		TargetValue:       targetValue,
		SourceValue:       sourceValue,
		DefaultResolution: "target",
	})
	return conflicts
}

// buildActressMergeConflicts compares target and source actresses and returns
// a list of conflicting fields (where both have values that differ).
func buildActressMergeConflicts(target, source *models.Actress) []ActressMergeConflict {
	conflicts := make([]ActressMergeConflict, 0)

	if target.DMMID > 0 && source.DMMID > 0 && target.DMMID != source.DMMID {
		conflicts = appendConflict(conflicts, "dmm_id", target.DMMID, source.DMMID)
	}
	if nonEmptyString(target.FirstName) && nonEmptyString(source.FirstName) && target.FirstName != source.FirstName {
		conflicts = appendConflict(conflicts, "first_name", target.FirstName, source.FirstName)
	}
	if nonEmptyString(target.LastName) && nonEmptyString(source.LastName) && target.LastName != source.LastName {
		conflicts = appendConflict(conflicts, "last_name", target.LastName, source.LastName)
	}
	if nonEmptyString(target.JapaneseName) && nonEmptyString(source.JapaneseName) && target.JapaneseName != source.JapaneseName {
		conflicts = appendConflict(conflicts, "japanese_name", target.JapaneseName, source.JapaneseName)
	}
	if nonEmptyString(target.ThumbURL) && nonEmptyString(source.ThumbURL) && target.ThumbURL != source.ThumbURL {
		conflicts = appendConflict(conflicts, "thumb_url", target.ThumbURL, source.ThumbURL)
	}

	return conflicts
}

// defaultResolutionsFromConflicts creates a resolution map where all conflicts
// default to "target" (keep target value).
func defaultResolutionsFromConflicts(conflicts []ActressMergeConflict) map[string]string {
	resolutions := make(map[string]string, len(conflicts))
	for _, conflict := range conflicts {
		resolutions[conflict.Field] = "target"
	}
	return resolutions
}

// canonicalActressName returns the canonical name for an actress.
// Priority: JapaneseName > FullName() > FirstName > LastName.
func canonicalActressName(actress *models.Actress) string {
	if nonEmptyString(actress.JapaneseName) {
		return strings.TrimSpace(actress.JapaneseName)
	}
	fullName := strings.TrimSpace(actress.FullName())
	if fullName != "" {
		return fullName
	}
	if nonEmptyString(actress.FirstName) {
		return strings.TrimSpace(actress.FirstName)
	}
	return strings.TrimSpace(actress.LastName)
}

// splitAliasList splits a pipe-separated alias string into individual aliases.
// Empty strings are filtered out.
func splitAliasList(raw string) []string {
	parts := strings.Split(raw, "|")
	out := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		out = append(out, part)
	}
	return out
}

// collectActressAliasCandidates collects all possible alias candidates from an actress.
// Includes explicit aliases, JapaneseName, and name variations.
func collectActressAliasCandidates(actress *models.Actress) []string {
	candidates := make([]string, 0, 8)
	candidates = append(candidates, splitAliasList(actress.Aliases)...)

	if nonEmptyString(actress.JapaneseName) {
		candidates = append(candidates, strings.TrimSpace(actress.JapaneseName))
	}
	if nonEmptyString(actress.FirstName) && nonEmptyString(actress.LastName) {
		candidates = append(candidates, strings.TrimSpace(actress.LastName+" "+actress.FirstName))
		candidates = append(candidates, strings.TrimSpace(actress.FirstName+" "+actress.LastName))
	} else {
		if nonEmptyString(actress.FirstName) {
			candidates = append(candidates, strings.TrimSpace(actress.FirstName))
		}
		if nonEmptyString(actress.LastName) {
			candidates = append(candidates, strings.TrimSpace(actress.LastName))
		}
	}

	return candidates
}

// mergeAliasValues merges source alias candidates into target aliases.
// Returns merged alias string, count of added aliases, and list of added aliases.
func mergeAliasValues(targetAliases string, sourceCandidates []string, canonicalName string) (string, int, []string) {
	seen := make(map[string]bool)
	merged := make([]string, 0)
	addedFromSource := make([]string, 0)

	for _, alias := range splitAliasList(targetAliases) {
		key := strings.ToLower(strings.TrimSpace(alias))
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, strings.TrimSpace(alias))
	}

	addedCount := 0
	canonicalKey := strings.ToLower(strings.TrimSpace(canonicalName))
	for _, alias := range sourceCandidates {
		alias = strings.TrimSpace(alias)
		key := strings.ToLower(alias)
		if key == "" || key == canonicalKey || seen[key] {
			continue
		}
		seen[key] = true
		merged = append(merged, alias)
		addedFromSource = append(addedFromSource, alias)
		addedCount++
	}

	return strings.Join(merged, "|"), addedCount, addedFromSource
}

// sourceAliasesForUpsert filters source candidates to return only aliases that
// should be upserted (excluding canonical name and duplicates).
func sourceAliasesForUpsert(sourceCandidates []string, canonicalName string) []string {
	canonicalKey := strings.ToLower(strings.TrimSpace(canonicalName))
	seen := make(map[string]bool)
	upserts := make([]string, 0, len(sourceCandidates))

	for _, alias := range sourceCandidates {
		alias = strings.TrimSpace(alias)
		key := strings.ToLower(alias)
		if key == "" || key == canonicalKey || seen[key] {
			continue
		}
		seen[key] = true
		upserts = append(upserts, alias)
	}

	return upserts
}

// mergeActressValues merges source actress into target based on resolutions.
// Returns merged actress or error if resolution is invalid.
func mergeActressValues(target, source *models.Actress, resolutions map[string]string) (models.Actress, error) {
	merged := *target

	conflicts := buildActressMergeConflicts(target, source)
	conflictSet := make(map[string]bool, len(conflicts))
	for _, conflict := range conflicts {
		conflictSet[conflict.Field] = true
	}

	getDecision := func(field string) (string, error) {
		if !conflictSet[field] {
			return "target", nil
		}
		decision, err := mergeFieldDecision(resolutions[field])
		if err != nil {
			return "", err
		}
		return decision, nil
	}

	decision, err := getDecision("dmm_id")
	if err != nil {
		return models.Actress{}, err
	}
	switch {
	case target.DMMID == 0 && source.DMMID > 0:
		merged.DMMID = source.DMMID
	case target.DMMID > 0 && source.DMMID > 0 && target.DMMID != source.DMMID:
		if decision == "source" {
			merged.DMMID = source.DMMID
		}
	}

	decision, err = getDecision("first_name")
	if err != nil {
		return models.Actress{}, err
	}
	switch {
	case !nonEmptyString(target.FirstName) && nonEmptyString(source.FirstName):
		merged.FirstName = strings.TrimSpace(source.FirstName)
	case nonEmptyString(target.FirstName) && nonEmptyString(source.FirstName) && target.FirstName != source.FirstName:
		if decision == "source" {
			merged.FirstName = strings.TrimSpace(source.FirstName)
		}
	}

	decision, err = getDecision("last_name")
	if err != nil {
		return models.Actress{}, err
	}
	switch {
	case !nonEmptyString(target.LastName) && nonEmptyString(source.LastName):
		merged.LastName = strings.TrimSpace(source.LastName)
	case nonEmptyString(target.LastName) && nonEmptyString(source.LastName) && target.LastName != source.LastName:
		if decision == "source" {
			merged.LastName = strings.TrimSpace(source.LastName)
		}
	}

	decision, err = getDecision("japanese_name")
	if err != nil {
		return models.Actress{}, err
	}
	switch {
	case !nonEmptyString(target.JapaneseName) && nonEmptyString(source.JapaneseName):
		merged.JapaneseName = strings.TrimSpace(source.JapaneseName)
	case nonEmptyString(target.JapaneseName) && nonEmptyString(source.JapaneseName) && target.JapaneseName != source.JapaneseName:
		if decision == "source" {
			merged.JapaneseName = strings.TrimSpace(source.JapaneseName)
		}
	}

	decision, err = getDecision("thumb_url")
	if err != nil {
		return models.Actress{}, err
	}
	switch {
	case !nonEmptyString(target.ThumbURL) && nonEmptyString(source.ThumbURL):
		merged.ThumbURL = strings.TrimSpace(source.ThumbURL)
	case nonEmptyString(target.ThumbURL) && nonEmptyString(source.ThumbURL) && target.ThumbURL != source.ThumbURL:
		if decision == "source" {
			merged.ThumbURL = strings.TrimSpace(source.ThumbURL)
		}
	}

	return merged, nil
}

// moveMovieAssociations moves movie associations from source actress to target actress.
// Returns count of updated movies. Uses the provided transaction.
func moveMovieAssociations(tx *gorm.DB, sourceID, targetID uint) (int, error) {
	var movies []models.Movie
	if err := tx.Preload("Actresses").Find(&movies).Error; err != nil {
		return 0, err
	}

	updatedMovies := 0
	for _, movie := range movies {
		hasSource := false
		hasTarget := false
		nextActresses := make([]models.Actress, 0, len(movie.Actresses)+1)

		for _, actress := range movie.Actresses {
			switch actress.ID {
			case sourceID:
				hasSource = true
				if !hasTarget {
					nextActresses = append(nextActresses, models.Actress{ID: targetID})
					hasTarget = true
				}
			case targetID:
				if !hasTarget {
					nextActresses = append(nextActresses, actress)
					hasTarget = true
				}
			default:
				nextActresses = append(nextActresses, actress)
			}
		}

		if !hasSource {
			continue
		}
		if !hasTarget {
			nextActresses = append(nextActresses, models.Actress{ID: targetID})
		}

		stub := models.Movie{ContentID: movie.ContentID}
		if err := tx.Model(&stub).Association("Actresses").Replace(nextActresses); err != nil {
			return updatedMovies, err
		}
		updatedMovies++
	}

	return updatedMovies, nil
}

// upsertActressAliases creates or updates actress alias records.
// Uses ON CONFLICT to handle duplicates. Uses the provided transaction.
func upsertActressAliases(tx *gorm.DB, aliases []string, canonicalName string) error {
	canonicalName = strings.TrimSpace(canonicalName)
	canonicalKey := strings.ToLower(canonicalName)
	if canonicalName == "" {
		return nil
	}

	seen := make(map[string]bool)
	for _, alias := range aliases {
		alias = strings.TrimSpace(alias)
		key := strings.ToLower(alias)
		if alias == "" || key == canonicalKey || seen[key] {
			continue
		}
		seen[key] = true

		entry := models.ActressAlias{
			AliasName:     alias,
			CanonicalName: canonicalName,
		}
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "alias_name"}},
			DoUpdates: clause.AssignmentColumns([]string{"canonical_name", "updated_at"}),
		}).Create(&entry).Error; err != nil {
			return err
		}
	}

	return nil
}
