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
	if ja := strings.TrimSpace(japaneseName); ja != "" {
		lookups = append(lookups, ja)
	}
	if firstName != "" && lastName != "" {
		lookups = append(lookups,
			strings.TrimSpace(firstName+" "+lastName),
			strings.TrimSpace(lastName+" "+firstName),
		)
	}
	if len(lookups) == 0 {
		return nil, nil
	}
	var aliases []models.ActressAlias
	if err := tx.Where("alias_name IN ?", lookups).Find(&aliases).Error; err != nil {
		return nil, err
	}
	if len(aliases) == 0 {
		return nil, nil
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
	hasBoth := firstName != "" && lastName != ""
	if !hasJP && !hasBoth {
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
		}
	}
	return matched, nil
}

func findCandidateByDMMIDTx(tx *gorm.DB, dmmID int) (*models.Actress, error) {
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

func findCandidateByNameKeyTx(tx *gorm.DB, nameKey string) (*models.Actress, error) {
	if nameKey == "" {
		return nil, gorm.ErrRecordNotFound
	}
	var found models.Actress
	err := tx.Where("verified = ? AND name_key = ?", false, nameKey).First(&found).Error
	if err != nil {
		return nil, err
	}
	return &found, nil
}

func resolveAmbiguousCandidateTx(tx *gorm.DB, scraped *models.Actress, nameKey string) (*models.Actress, error) {
	if nameKey == "" {
		return nil, fmt.Errorf("resolve actress identity: ambiguous match with empty name key")
	}
	candidate, err := findCandidateByNameKeyTx(tx, nameKey)
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
			if ferr := tx.Where("verified = ? AND name_key = ?", false, candidate.NameKey).First(&found).Error; ferr == nil {
				candidate = found
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
			if !found.Verified && found.NameKey != "" {
				return found, ResolutionAmbiguous, nil
			}
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
	if len(aliasMatches) == 1 {
		return &aliasMatches[0], ResolutionMatched, nil
	}
	if len(aliasMatches) > 1 {
		nameKey := actressNameKey(scraped)
		candidate, cerr := resolveAmbiguousCandidateTx(tx, scraped, nameKey)
		if cerr != nil {
			return nil, ResolutionAmbiguous, cerr
		}
		return candidate, ResolutionAmbiguous, nil
	}

	verifiedMatches, err := findVerifiedByNameTx(tx, scraped.JapaneseName, scraped.FirstName, scraped.LastName)
	if err != nil {
		return nil, ResolutionMatched, wrapDBErr("resolve name", actressNameKey(scraped), err)
	}
	if len(verifiedMatches) == 1 {
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
		candidate, err := findCandidateByNameKeyTx(tx, nameKey)
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
