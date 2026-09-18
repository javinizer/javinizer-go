package database

import (
	"errors"
	"fmt"
	"strings"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ResolutionOutcome reports how a scraped actress resolved against the
// identity catalog.
type ResolutionOutcome int

// Resolution outcomes for a scraped actress against the identity catalog.
const (
	ResolutionMatched ResolutionOutcome = iota
	ResolutionCandidateLinked
	ResolutionAmbiguous
)

func actressNameKey(a *models.Actress) string {
	if a == nil {
		return ""
	}
	if ja := models.NormalizeActressNameKey(a.JapaneseName); ja != "" {
		return ja
	}
	if a.LastName != "" || a.FirstName != "" {
		return models.NormalizeActressNameKey(a.LastName + " " + a.FirstName)
	}
	return ""
}

func findVerifiedByDMMIDTx(tx *gorm.DB, dmmID int) (*models.Actress, error) {
	if dmmID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var found models.Actress
	err := tx.First(&found, "dmm_id = ?", dmmID).Error
	if err != nil {
		return nil, err
	}
	return &found, nil
}

func findVerifiedByAliasTx(tx *gorm.DB, japaneseName, firstName, lastName string) ([]models.Actress, error) {
	lookups := make([]string, 0, 3)
	if ja := models.NormalizeActressNameKey(japaneseName); ja != "" {
		lookups = append(lookups, ja)
	}
	if firstName != "" && lastName != "" {
		lookups = append(lookups,
			models.NormalizeActressNameKey(firstName+" "+lastName),
			models.NormalizeActressNameKey(lastName+" "+firstName),
		)
	} else if first := models.NormalizeActressNameKey(firstName); first != "" {
		lookups = append(lookups, first)
	} else if last := models.NormalizeActressNameKey(lastName); last != "" {
		lookups = append(lookups, last)
	}
	if len(lookups) == 0 {
		return nil, nil
	}
	var aliases []models.ActressAlias
	if err := tx.Where("alias_name_key IN ?", lookups).Find(&aliases).Error; err != nil {
		return nil, err
	}
	if len(aliases) == 0 {
		return nil, nil
	}
	if err := validateNormalizedAliasRows(aliases); err != nil {
		return nil, err
	}
	matched := make([]models.Actress, 0, len(aliases))
	seenIDs := make(map[uint]struct{}, len(aliases))
	var verifiedAll []models.Actress
	if err := tx.Where("verified = ?", true).Find(&verifiedAll).Error; err != nil {
		return nil, err
	}
	for _, alias := range aliases {
		key := models.NormalizeActressNameKey(alias.CanonicalName)
		if key == "" {
			continue
		}
		for _, a := range verifiedAll {
			if _, dup := seenIDs[a.ID]; dup {
				continue
			}
			ja := models.NormalizeActressNameKey(a.JapaneseName)
			lf := models.NormalizeActressNameKey(a.LastName + " " + a.FirstName)
			fl := models.NormalizeActressNameKey(a.FirstName + " " + a.LastName)
			if key == ja || key == lf || key == fl {
				matched = append(matched, a)
				seenIDs[a.ID] = struct{}{}
			}
		}
	}
	return matched, nil
}

func findVerifiedByNameTx(tx *gorm.DB, japaneseName, firstName, lastName string) ([]models.Actress, error) {
	hasJP := strings.TrimSpace(japaneseName) != ""
	hasFirst := strings.TrimSpace(firstName) != ""
	hasLast := strings.TrimSpace(lastName) != ""
	hasBoth := hasFirst && hasLast
	if !hasJP && !hasFirst && !hasLast {
		return nil, nil
	}
	var verifiedAll []models.Actress
	if err := tx.Where("verified = ?", true).Find(&verifiedAll).Error; err != nil {
		return nil, err
	}
	targetJP := models.NormalizeActressNameKey(japaneseName)
	targetLF := models.NormalizeActressNameKey(lastName + " " + firstName)
	targetFL := models.NormalizeActressNameKey(firstName + " " + lastName)
	matched := make([]models.Actress, 0, 2)
	for _, a := range verifiedAll {
		if hasJP && targetJP != "" && models.NormalizeActressNameKey(a.JapaneseName) == targetJP {
			matched = append(matched, a)
			continue
		}
		if hasBoth && targetLF != "" && targetLF == models.NormalizeActressNameKey(a.LastName+" "+a.FirstName) {
			matched = append(matched, a)
			continue
		}
		if hasBoth && targetFL != "" && targetFL == models.NormalizeActressNameKey(a.FirstName+" "+a.LastName) {
			matched = append(matched, a)
			continue
		}
		if hasFirst && !hasLast && strings.TrimSpace(a.LastName) == "" && models.NormalizeActressNameKey(firstName) == models.NormalizeActressNameKey(a.FirstName) {
			matched = append(matched, a)
			continue
		}
		if hasLast && !hasFirst && strings.TrimSpace(a.FirstName) == "" && models.NormalizeActressNameKey(lastName) == models.NormalizeActressNameKey(a.LastName) {
			matched = append(matched, a)
		}
	}
	return matched, nil
}

func filterDMMlessActresses(actresses []models.Actress) []models.Actress {
	filtered := make([]models.Actress, 0, len(actresses))
	for i := range actresses {
		if actresses[i].DMMID <= 0 {
			filtered = append(filtered, actresses[i])
		}
	}
	return filtered
}

func candidateEvidenceKeys(candidate *models.Actress) map[string]struct{} {
	keys := canonicalActressRepresentationKeys(candidate)
	if candidate != nil {
		if key := models.NormalizeActressNameKey(candidate.NameKey); key != "" {
			keys[key] = struct{}{}
		}
	}
	return keys
}

func findCandidateByNameEvidenceTx(tx *gorm.DB, incoming *models.Actress) (*models.Actress, error) {
	incomingKeys := candidateEvidenceKeys(incoming)
	if len(incomingKeys) == 0 {
		return nil, gorm.ErrRecordNotFound
	}

	// Load the complete candidate set. A candidate may intentionally be keyless
	// (positive DMM evidence), or its indexed primary key may differ while an
	// alternate JP/LF/FL representation matches the incoming evidence.
	var candidates []models.Actress
	if err := tx.Where("verified = ?", false).Order("id").Find(&candidates).Error; err != nil {
		return nil, err
	}
	matches := make(map[uint]models.Actress, 2)
	for i := range candidates {
		candidate := &candidates[i]
		if incoming.DMMID > 0 && candidate.DMMID > 0 && candidate.DMMID != incoming.DMMID {
			continue
		}
		candidateKeys := candidateEvidenceKeys(candidate)
		matched := false
		for key := range incomingKeys {
			if _, ok := candidateKeys[key]; ok {
				matched = true
				break
			}
		}
		if !matched {
			continue
		}
		// IDs are the identity boundary. Map assignment keeps the union
		// deduplicated even if its query shape later gains overlapping arms.
		matches[candidate.ID] = *candidate
	}
	if len(matches) > 1 {
		return nil, fmt.Errorf("candidate name evidence matches %d preserved identities: %w", len(matches), ErrActressCandidateAmbiguous)
	}
	if len(matches) == 1 {
		for _, match := range matches {
			if match.AmbiguityQuarantined {
				return nil, fmt.Errorf("candidate name evidence matches a durably quarantined identity: %w", ErrActressCandidateAmbiguous)
			}
			return &match, nil
		}
	}
	return nil, gorm.ErrRecordNotFound
}

func findCandidateByDMMIDTx(tx *gorm.DB, dmmID int) (*models.Actress, error) {
	if dmmID <= 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var found models.Actress
	err := tx.Where("verified = ? AND dmm_id = ?", false, dmmID).First(&found).Error
	if err != nil {
		return nil, err
	}
	return &found, nil
}

func resolveAmbiguousCandidateTx(tx *gorm.DB, scraped *models.Actress, nameKey string) (*models.Actress, error) {
	if scraped.DMMID > 0 {
		candidate, err := findCandidateByDMMIDTx(tx, scraped.DMMID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, wrapDBErr("resolve candidate", fmt.Sprintf("dmm %d", scraped.DMMID), err)
		}
		if candidate != nil {
			if !candidate.AmbiguityQuarantined {
				if err := tx.Model(candidate).Update(colAmbiguityQuarantined, true).Error; err != nil {
					return nil, wrapDBErr("quarantine candidate", fmt.Sprintf("dmm %d", scraped.DMMID), err)
				}
				candidate.AmbiguityQuarantined = true
			}
			return candidate, nil
		}
		candidate, err = createCandidateTx(tx, scraped, "")
		if err != nil {
			return nil, err
		}
		if err := tx.Model(candidate).Update(colAmbiguityQuarantined, true).Error; err != nil {
			return nil, wrapDBErr("quarantine candidate", fmt.Sprintf("dmm %d", scraped.DMMID), err)
		}
		candidate.AmbiguityQuarantined = true
		return candidate, nil
	}
	if nameKey == "" {
		return nil, fmt.Errorf("resolve actress identity: ambiguous match with empty name key")
	}
	candidate, err := findCandidateByNameEvidenceTx(tx, scraped)
	if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, wrapDBErr("resolve candidate", nameKey, err)
	}
	if candidate != nil {
		return candidate, nil
	}
	return createCandidateTx(tx, scraped, nameKey)
}

func createCandidateTx(tx *gorm.DB, scraped *models.Actress, nameKey string) (*models.Actress, error) {
	candidate := models.Actress{
		DMMID:        scraped.DMMID,
		FirstName:    scraped.FirstName,
		LastName:     scraped.LastName,
		JapaneseName: scraped.JapaneseName,
		ThumbURL:     scraped.ThumbURL,
		Verified:     false,
		Origin:       "scrape",
		NameKey:      nameKey,
	}
	err := raceRetryCreate(tx, &candidate, func(tx *gorm.DB) error {
		var found models.Actress
		if candidate.DMMID > 0 {
			if ferr := tx.First(&found, "dmm_id = ?", candidate.DMMID).Error; ferr == nil {
				candidate = found
				return nil
			}
		}
		if candidate.NameKey != "" {
			if matched, ferr := findCandidateByNameEvidenceTx(tx, &candidate); ferr == nil {
				candidate = *matched
				return nil
			}
		}
		return gorm.ErrRecordNotFound
	})
	if err != nil {
		return nil, wrapDBErr("create candidate", fmt.Sprintf("candidate %s", actressNameKey(scraped)), err)
	}
	return &candidate, nil
}

func quarantinePositiveDMMNameMatchTx(tx *gorm.DB, scraped *models.Actress) (*models.Actress, ResolutionOutcome, error) {
	candidate, err := resolveAmbiguousCandidateTx(tx, scraped, actressNameKey(scraped))
	if err != nil {
		return nil, ResolutionAmbiguous, err
	}
	return candidate, ResolutionAmbiguous, nil
}

// ResolveActressIdentityTx resolves a scraped actress against existing
// identities, linking to a verified match or a quarantined candidate.
func ResolveActressIdentityTx(tx *gorm.DB, scraped *models.Actress) (*models.Actress, ResolutionOutcome, error) {
	if scraped == nil {
		return nil, ResolutionMatched, fmt.Errorf("resolve actress identity: scraped actress must not be nil")
	}

	if scraped.DMMID > 0 {
		found, err := findVerifiedByDMMIDTx(tx, scraped.DMMID)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ResolutionMatched, wrapDBErr("resolve dmm", fmt.Sprintf("dmm %d", scraped.DMMID), err)
		}
		if found != nil {
			// Exact positive-DMM evidence is the identity boundary. Name-based
			// quarantine only governs name-only resolution and must not downgrade
			// an exact candidate hit to ambiguity.
			if !found.Verified {
				return found, ResolutionCandidateLinked, nil
			}
			return found, ResolutionMatched, nil
		}
	}

	aliasMatches, err := findVerifiedByAliasTx(tx, scraped.JapaneseName, scraped.FirstName, scraped.LastName)
	if err != nil {
		return nil, ResolutionMatched, wrapDBErr("resolve alias", actressNameKey(scraped), err)
	}
	canonicalMatches, err := findVerifiedByNameTx(tx, scraped.JapaneseName, scraped.FirstName, scraped.LastName)
	if err != nil {
		return nil, ResolutionMatched, wrapDBErr("resolve name", actressNameKey(scraped), err)
	}
	if scraped.DMMID > 0 {
		aliasMatches = filterDMMlessActresses(aliasMatches)
		canonicalMatches = filterDMMlessActresses(canonicalMatches)
	}
	verifiedByID := make(map[uint]models.Actress, len(aliasMatches)+len(canonicalMatches))
	for _, match := range append(aliasMatches, canonicalMatches...) {
		verifiedByID[match.ID] = match
	}
	verifiedMatches := make([]models.Actress, 0, len(verifiedByID))
	for _, match := range verifiedByID {
		verifiedMatches = append(verifiedMatches, match)
	}
	if len(verifiedMatches) == 1 {
		if scraped.DMMID > 0 {
			return quarantinePositiveDMMNameMatchTx(tx, scraped)
		}
		return &verifiedMatches[0], ResolutionMatched, nil
	}

	if len(verifiedMatches) > 1 {
		nameKey := actressNameKey(scraped)
		candidate, cerr := resolveAmbiguousCandidateTx(tx, scraped, nameKey)
		if cerr != nil {
			return nil, ResolutionAmbiguous, cerr
		}
		return candidate, ResolutionAmbiguous, nil
	}

	nameKey := ""
	if scraped.DMMID == 0 {
		nameKey = actressNameKey(scraped)
	}

	if scraped.DMMID > 0 {
		candidate, err := createCandidateTx(tx, scraped, "")
		if err != nil {
			return nil, ResolutionCandidateLinked, err
		}
		return candidate, ResolutionCandidateLinked, nil
	}

	if nameKey != "" {
		candidate, err := findCandidateByNameEvidenceTx(tx, scraped)
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, ResolutionCandidateLinked, wrapDBErr("resolve candidate", nameKey, err)
		}
		if candidate != nil {
			return candidate, ResolutionCandidateLinked, nil
		}
	}

	candidate, err := createCandidateTx(tx, scraped, nameKey)
	if err != nil {
		return nil, ResolutionCandidateLinked, err
	}
	return candidate, ResolutionCandidateLinked, nil
}
