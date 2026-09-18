package database

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
)

// CollisionService implements the credit identity lifecycle contract.
type CollisionService struct {
	Collisions *CreditCollisionRepository
	Credits    *MovieCreditRepository
	Actresses  *ActressRepository
	Aliases    *ActressAliasRepository
	db         *DB
}

// NewCollisionService constructs a repository backed by the given database handle.
func NewCollisionService(db *DB) *CollisionService {
	return &CollisionService{
		Collisions: NewCreditCollisionRepository(db),
		Credits:    NewMovieCreditRepository(db),
		Actresses:  NewActressRepository(db),
		Aliases:    NewActressAliasRepository(db),
		db:         db,
	}
}

// AllowedCollisionResolutions returns the actions accepted by Resolve for the
// collision's current field, status, and linked identity state.
func AllowedCollisionResolutions(collision *models.CreditCollision, credit *models.MovieCredit) []string {
	allowed := make([]string, 0, 4)
	if collision == nil || credit == nil || collision.Status != models.CollisionStatusOpen {
		return allowed
	}
	switch collision.Field {
	case models.CreditFieldCreditedName:
		allowed = append(allowed, models.CollisionResolutionKeepIdentity, models.CollisionResolutionAdoptCanonical, models.CollisionResolutionAdoptAlias, models.CollisionResolutionReassign)
	case models.CreditFieldReportedThumb:
		allowed = append(allowed, models.CollisionResolutionKeepIdentity, models.CollisionResolutionAdoptCanonical, models.CollisionResolutionReassign)
	case models.CreditFieldIdentityLink:
		if credit.Actress != nil && credit.Actress.Verified {
			allowed = append(allowed, models.CollisionResolutionKeepIdentity)
		}
		allowed = append(allowed, models.CollisionResolutionAdoptCanonical, models.CollisionResolutionReassign)
	}
	return allowed
}

func validateCollisionResolution(collision *models.CreditCollision, credit *models.MovieCredit, resolution string, targetActressID uint) error {
	allowed := AllowedCollisionResolutions(collision, credit)
	for _, candidate := range allowed {
		if candidate == resolution {
			if resolution == models.CollisionResolutionReassign {
				if targetActressID == 0 {
					return fmt.Errorf("resolve collision: target_actress_id is required for reassign")
				}
				if targetActressID == credit.ActressID {
					return fmt.Errorf("resolve collision: credit already linked to target identity")
				}
			}
			return nil
		}
	}
	if collision != nil && collision.Field == models.CreditFieldIdentityLink && resolution == models.CollisionResolutionKeepIdentity {
		return fmt.Errorf("resolve collision: keep_identity requires a verified identity")
	}
	if resolution == models.CollisionResolutionAdoptAlias {
		return fmt.Errorf("resolve collision: adopt_alias requires a credited_name collision")
	}
	return fmt.Errorf("resolve collision: resolution %q is not allowed for this collision", resolution)
}

// CollisionActionContext is the server-derived action and current-identity state for a collision.
type CollisionActionContext struct {
	AllowedResolutions []string
	CurrentActressID   uint
}

// ActionContexts derives action contracts using linked identity state loaded from the server.
func (s *CollisionService) ActionContexts(ctx context.Context, collisions []models.CreditCollision) (map[uint]CollisionActionContext, error) {
	result := make(map[uint]CollisionActionContext, len(collisions))
	if len(collisions) == 0 {
		return result, nil
	}
	creditIDs := make([]uint, 0, len(collisions))
	for i := range collisions {
		creditIDs = append(creditIDs, collisions[i].CreditID)
	}
	var credits []models.MovieCredit
	if err := s.db.WithContext(ctx).Preload("Actress").Where("id IN ?", creditIDs).Find(&credits).Error; err != nil {
		return nil, wrapDBErr("list", "collision credits", err)
	}
	byID := make(map[uint]*models.MovieCredit, len(credits))
	for i := range credits {
		byID[credits[i].ID] = &credits[i]
	}
	for i := range collisions {
		credit := byID[collisions[i].CreditID]
		if credit == nil {
			return nil, fmt.Errorf("load collision credit %d: %w", collisions[i].CreditID, ErrNotFound)
		}
		result[collisions[i].ID] = CollisionActionContext{
			AllowedResolutions: AllowedCollisionResolutions(&collisions[i], credit),
			CurrentActressID:   credit.ActressID,
		}
	}
	return result, nil
}

// AllowedResolutions preserves the policy-only service contract.
func (s *CollisionService) AllowedResolutions(ctx context.Context, collisions []models.CreditCollision) (map[uint][]string, error) {
	contexts, err := s.ActionContexts(ctx, collisions)
	if err != nil {
		return nil, err
	}
	result := make(map[uint][]string, len(contexts))
	for id, actionContext := range contexts {
		result[id] = actionContext.AllowedResolutions
	}
	return result, nil
}

// resolveTx applies the resolution outcome atomically and recomputes the
// open-collision count.
func (s *CollisionService) resolveTx(tx *gorm.DB, collisionID uint, resolution string, targetActressID uint) (remaining int, err error) {
	var collision models.CreditCollision
	if err := tx.First(&collision, collisionID).Error; err != nil {
		return 0, wrapDBErr("load", fmt.Sprintf("collision %d", collisionID), err)
	}
	if collision.Status != models.CollisionStatusOpen {
		return 0, ErrCollisionNotOpen
	}
	var credit models.MovieCredit
	if err := tx.Preload("Actress").First(&credit, collision.CreditID).Error; err != nil {
		return 0, wrapDBErr("load", fmt.Sprintf("credit %d", collision.CreditID), err)
	}
	if err := validateCollisionResolution(&collision, &credit, resolution, targetActressID); err != nil {
		return 0, err
	}
	res := tx.Model(&models.CreditCollision{}).
		Where("id = ? AND status = ?", collisionID, models.CollisionStatusOpen).
		Updates(map[string]interface{}{
			colStatus:     models.CollisionStatusResolved,
			colResolution: resolution,
			colUpdatedAt:  time.Now().UTC(),
		})
	if res.Error != nil {
		return 0, wrapDBErr("resolve", fmt.Sprintf("collision %d", collisionID), res.Error)
	}
	if res.RowsAffected == 0 {
		return 0, ErrCollisionNotOpen
	}

	creditPtr := &credit
	var previousIdentity *models.Actress
	if credit.Actress != nil {
		copy := *credit.Actress
		previousIdentity = &copy
	}
	switch resolution {
	case models.CollisionResolutionKeepIdentity:
		if err := applyCollisionFieldEffectTx(tx, credit.ID, collision.Field, resolution); err != nil {
			return 0, err
		}
	case models.CollisionResolutionAdoptCanonical:
		switch collision.Field {
		case models.CreditFieldIdentityLink:
			updates := map[string]interface{}{
				colVerified:             true,
				colOrigin:               ActressOriginUser,
				colAmbiguityQuarantined: false,
				colUpdatedAt:            time.Now().UTC(),
			}
			if strings.TrimSpace(credit.CreditedJapaneseName) != "" {
				updates[colJapaneseName] = credit.CreditedJapaneseName
			} else if strings.TrimSpace(credit.CreditedName) != "" {
				first, last := splitReportedName(credit.CreditedName)
				updates[colFirstName] = first
				updates[colLastName] = last
			}
			if strings.TrimSpace(credit.ReportedThumbURL) != "" {
				updates[colThumbURL] = credit.ReportedThumbURL
			}
			if err := tx.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Updates(updates).Error; err != nil {
				return 0, wrapDBErr("adopt canonical", fmt.Sprintf("actress %d", credit.ActressID), err)
			}
		case models.CreditFieldReportedThumb:
			if err := tx.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Updates(map[string]interface{}{
				colThumbURL:  collision.ReportedValue,
				colOrigin:    ActressOriginUser,
				colUpdatedAt: time.Now().UTC(),
			}).Error; err != nil {
				return 0, wrapDBErr("adopt canonical", fmt.Sprintf("actress %d", credit.ActressID), err)
			}
		case models.CreditFieldCreditedName:
			if isCJK(collision.ReportedValue) {
				if err := tx.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Updates(map[string]interface{}{
					colJapaneseName: collision.ReportedValue,
					colOrigin:       ActressOriginUser,
					colVerified:     true,
					colUpdatedAt:    time.Now().UTC(),
				}).Error; err != nil {
					return 0, wrapDBErr("adopt canonical", fmt.Sprintf("actress %d", credit.ActressID), err)
				}
			} else {
				first, last := splitReportedName(collision.ReportedValue)
				if err := tx.Model(&models.Actress{}).Where("id = ?", credit.ActressID).Updates(map[string]interface{}{
					colFirstName: first,
					colLastName:  last,
					colOrigin:    ActressOriginUser,
					colVerified:  true,
					colUpdatedAt: time.Now().UTC(),
				}).Error; err != nil {
					return 0, wrapDBErr("adopt canonical", fmt.Sprintf("actress %d", credit.ActressID), err)
				}
			}
		}
	case models.CollisionResolutionAdoptAlias:
		if err := applyCollisionFieldEffectTx(tx, credit.ID, collision.Field, resolution); err != nil {
			return 0, err
		}
		canonical := collision.CanonicalValue
		if canonical == "" && credit.Actress != nil {
			canonical = credit.Actress.FullName()
		}
		alias := &models.ActressAlias{AliasName: collision.ReportedValue, CanonicalName: canonical}
		if err := upsertAliasTx(tx, alias); err != nil {
			return 0, err
		}
	case models.CollisionResolutionReassign:
		if err := reassignCreditTx(tx, creditPtr, targetActressID); err != nil {
			return 0, err
		}
	}

	if resolution == models.CollisionResolutionAdoptCanonical {
		if err := transitionActressCanonicalNamesTx(tx, credit.ActressID, previousIdentity); err != nil {
			return 0, err
		}
		if err := reconcileActressCollisionsTx(tx, credit.ActressID); err != nil {
			return 0, err
		}
		if err := restoreActressProjectionTx(tx, credit.ActressID); err != nil {
			return 0, err
		}
	}

	var remainingCount int64
	if err := tx.Model(&models.CreditCollision{}).
		Where("movie_content_id = ? AND status = ?", collision.MovieContentID, models.CollisionStatusOpen).
		Count(&remainingCount).Error; err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("open collisions for movie %s", collision.MovieContentID), err)
	}
	return int(remainingCount), nil
}

// Resolve applies one of the four resolution outcomes atomically and
// returns the remaining open-collision count for the movie.
func (s *CollisionService) Resolve(ctx context.Context, collisionID uint, resolution string, targetActressID uint) (remaining int, err error) {
	switch resolution {
	case models.CollisionResolutionKeepIdentity, models.CollisionResolutionAdoptCanonical,
		models.CollisionResolutionAdoptAlias, models.CollisionResolutionReassign:
	default:
		return 0, fmt.Errorf("resolve collision: resolution must be one of keep_identity, adopt_canonical, adopt_alias, reassign")
	}
	var remainingOut int
	err = s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		var collision models.CreditCollision
		if err := tx.First(&collision, collisionID).Error; err != nil {
			return err
		}
		var credit models.MovieCredit
		if err := tx.First(&credit, collision.CreditID).Error; err != nil {
			return err
		}
		contentIDs, err := movieContentIDsForActressesTx(tx, credit.ActressID)
		if err != nil {
			return err
		}
		contentIDs = uniqueContentIDs(append(contentIDs, collision.MovieContentID))
		return mutateMovieRenderInputsTx(tx, contentIDs, func() error {
			r, e := s.resolveTx(tx, collisionID, resolution, targetActressID)
			remainingOut = r
			return e
		})
	})
	if err != nil {
		return 0, err
	}
	return remainingOut, nil
}

// UpdateCreditOverride sets a per-movie display override on a credit and
// dirties the crediting movie.
func (s *CollisionService) UpdateCreditOverride(ctx context.Context, creditID uint, overrideName string, userOverride bool) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		contentID, err := movieContentIDForCreditTx(tx, creditID)
		if err != nil {
			return err
		}
		return mutateMovieRenderInputsTx(tx, []string{contentID}, func() error {
			res := tx.Model(&models.MovieCredit{}).Where("id = ?", creditID).Updates(map[string]interface{}{
				colOverrideName: overrideName, colUserOverride: userOverride,
				colOrigin: string(models.CreditOriginUser), colUpdatedAt: time.Now().UTC(),
			})
			if res.Error != nil {
				return wrapDBErr("update override", fmt.Sprintf("movie credit %d", creditID), res.Error)
			}
			return nil
		})
	})
}

// SetCreditSuppressed toggles the user-removal tombstone on a credit and
// closes its open collisions when suppressed.
func (s *CollisionService) SetCreditSuppressed(ctx context.Context, creditID uint, suppressed bool) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		contentID, err := movieContentIDForCreditTx(tx, creditID)
		if err != nil {
			return err
		}
		return mutateMovieRenderInputsTx(tx, []string{contentID}, func() error { return setCreditSuppressedTx(tx, creditID, suppressed) })
	})
}

func setCreditSuppressedTx(tx *gorm.DB, creditID uint, suppressed bool) error {
	var credit models.MovieCredit
	if err := tx.Preload("Actress").Where("id = ?", creditID).First(&credit).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("update suppressed: movie credit %d: %w", creditID, ErrNotFound)
		}
		return wrapDBErr("find", fmt.Sprintf("movie credit %d", creditID), err)
	}
	updates := map[string]interface{}{
		colSuppressed: suppressed,
		colOrigin:     string(models.CreditOriginUser),
		colUpdatedAt:  time.Now().UTC(),
	}
	res := tx.Model(&models.MovieCredit{}).Where("id = ?", creditID).Updates(updates)
	if res.Error != nil {
		return wrapDBErr("update suppressed", fmt.Sprintf("movie credit %d", creditID), res.Error)
	}
	if res.RowsAffected == 0 {
		return fmt.Errorf("update suppressed: movie credit %d: %w", creditID, ErrNotFound)
	}
	if suppressed {
		if err := tx.Model(&models.CreditCollision{}).
			Where("credit_id = ? AND status = ?", creditID, models.CollisionStatusOpen).
			Updates(map[string]interface{}{
				colStatus:     models.CollisionStatusResolved,
				colResolution: models.CollisionResolutionBySuppression,
				colUpdatedAt:  time.Now().UTC(),
			}).Error; err != nil {
			return err
		}
		if err := tx.Exec(
			"DELETE FROM movie_actresses WHERE movie_content_id = ? AND actress_id = ?",
			credit.MovieContentID, credit.ActressID,
		).Error; err != nil {
			return err
		}
	} else if credit.Suppressed {
		if err := restoreSuppressedCreditCollisionsTx(tx, &credit); err != nil {
			return err
		}
		if err := tx.Exec(`
		INSERT OR IGNORE INTO movie_actresses (movie_content_id, actress_id)
		SELECT ?, ?
		WHERE EXISTS (
			SELECT 1 FROM actresses WHERE id = ? AND verified = ?
		)
	`, credit.MovieContentID, credit.ActressID, credit.ActressID, true).Error; err != nil {
			return err
		}
	} else if err := tx.Exec(`
		INSERT OR IGNORE INTO movie_actresses (movie_content_id, actress_id)
		SELECT ?, ?
		WHERE EXISTS (
			SELECT 1 FROM actresses WHERE id = ? AND verified = ?
		)
	`, credit.MovieContentID, credit.ActressID, credit.ActressID, true).Error; err != nil {
		return err
	}
	return nil
}

func restoreSuppressedCreditCollisionsTx(tx *gorm.DB, credit *models.MovieCredit) error {
	if credit == nil || credit.Actress == nil {
		return fmt.Errorf("restore suppressed collisions: credit identity is missing")
	}
	type evidence struct {
		reported  string
		canonical string
	}
	active := make(map[string]evidence, 3)
	actress := credit.Actress
	if !credit.LegacyInferred {
		if !actress.Verified {
			reported := strings.TrimSpace(credit.CreditedName)
			if reported == "" {
				reported = strings.TrimSpace(credit.CreditedJapaneseName)
			}
			if reported != "" {
				active[models.CreditFieldIdentityLink] = evidence{reported: reported, canonical: canonicalActressName(actress)}
			}
		} else {
			reportedName := strings.TrimSpace(credit.CreditedName)
			if reportedName == "" {
				reportedName = strings.TrimSpace(credit.CreditedJapaneseName)
			}
			canonicalName := canonicalActressName(actress)
			if reportedName != "" && canonicalName != "" {
				nameMatches := actressNameMatchesCanonicalRepresentations(reportedName, actress)
				if !nameMatches {
					aliasMatches, err := aliasMatchesCanonicalTx(tx, reportedName, actress)
					if err != nil {
						return err
					}
					nameMatches = aliasMatches
				}
				if !nameMatches {
					active[models.CreditFieldCreditedName] = evidence{reported: reportedName, canonical: canonicalName}
				}
			}
			reportedThumb := strings.TrimSpace(credit.ReportedThumbURL)
			if reportedThumb != "" && strings.TrimSpace(actress.ThumbURL) != "" && reportedThumb != actress.ThumbURL {
				active[models.CreditFieldReportedThumb] = evidence{reported: reportedThumb, canonical: actress.ThumbURL}
			}
		}
	}

	var previous []models.CreditCollision
	if err := tx.Where("credit_id = ? AND status = ? AND resolution = ?", credit.ID, models.CollisionStatusResolved, models.CollisionResolutionBySuppression).Find(&previous).Error; err != nil {
		return wrapDBErr("list", fmt.Sprintf("suppressed collisions for credit %d", credit.ID), err)
	}
	if !credit.LegacyInferred && !actress.Verified {
		hasIdentityCollision := false
		for i := range previous {
			if previous[i].Field == models.CreditFieldIdentityLink {
				hasIdentityCollision = true
				break
			}
		}
		if !hasIdentityCollision {
			delete(active, models.CreditFieldIdentityLink)
		}
	}
	restored := make(map[string]bool, len(previous))
	for i := range previous {
		collision := &previous[i]
		current, ok := active[collision.Field]
		updates := map[string]interface{}{
			"canonical_value": collision.CanonicalValue,
			colUpdatedAt:      time.Now().UTC(),
		}
		if ok && collision.ReportedValue == current.reported {
			updates["canonical_value"] = current.canonical
			updates[colStatus] = models.CollisionStatusOpen
			updates[colResolution] = ""
			restored[collision.Field+"\x00"+collision.ReportedValue] = true
		} else {
			updates[colStatus] = models.CollisionStatusResolved
			updates[colResolution] = models.CollisionResolutionByRemoval
		}
		if err := tx.Model(&models.CreditCollision{}).Where("id = ?", collision.ID).Updates(updates).Error; err != nil {
			return wrapDBErr("restore", fmt.Sprintf("collision %d", collision.ID), err)
		}
	}
	var collisionRepo CreditCollisionRepository
	for field, current := range active {
		key := field + "\x00" + current.reported
		if restored[key] {
			continue
		}
		collision := &models.CreditCollision{
			CreditID:       credit.ID,
			MovieContentID: credit.MovieContentID,
			Field:          field,
			ReportedValue:  current.reported,
			CanonicalValue: current.canonical,
		}
		if err := collisionRepo.RecordTx(tx, collision, credit.Source); err != nil {
			return err
		}
	}
	return nil
}

func reconcileActressCollisionsTx(tx *gorm.DB, actressID uint) error {
	var actress models.Actress
	if err := tx.First(&actress, actressID).Error; err != nil {
		return wrapDBErr("load", fmt.Sprintf("actress %d", actressID), err)
	}

	var collisions []models.CreditCollision
	if err := tx.Model(&models.CreditCollision{}).
		Joins("JOIN movie_credits ON movie_credits.id = credit_collisions.credit_id").
		Where("movie_credits.actress_id = ? AND credit_collisions.field IN ?", actressID, []string{
			models.CreditFieldCreditedName,
			models.CreditFieldReportedThumb,
			models.CreditFieldIdentityLink,
		}).
		Find(&collisions).Error; err != nil {
		return wrapDBErr("list", fmt.Sprintf("open collisions for actress %d", actressID), err)
	}

	canonicalName := canonicalActressName(&actress)
	for i := range collisions {
		collision := &collisions[i]
		canonicalValue := canonicalName
		matches := false
		switch collision.Field {
		case models.CreditFieldCreditedName, models.CreditFieldIdentityLink:
			matches = actressNameMatchesCanonicalRepresentations(collision.ReportedValue, &actress)
		case models.CreditFieldReportedThumb:
			canonicalValue = actress.ThumbURL
			matches = strings.TrimSpace(collision.ReportedValue) != "" && strings.TrimSpace(canonicalValue) != "" && collision.ReportedValue == canonicalValue
		}
		updates := map[string]interface{}{
			"canonical_value": canonicalValue,
			colUpdatedAt:      time.Now().UTC(),
		}
		if collision.Field == models.CreditFieldIdentityLink && actress.AmbiguityQuarantined {
			updates[colStatus] = models.CollisionStatusOpen
			updates[colResolution] = ""
		} else if collision.Status == models.CollisionStatusOpen && !collision.UserPinned && matches {
			updates[colStatus] = models.CollisionStatusResolved
			updates[colResolution] = models.CollisionResolutionAdoptCanonical
		}
		if err := tx.Model(&models.CreditCollision{}).Where("id = ?", collision.ID).Updates(updates).Error; err != nil {
			return wrapDBErr("reconcile", fmt.Sprintf("collision %d", collision.ID), err)
		}
	}
	return nil
}

func splitReportedName(reported string) (first, last string) {
	reported = strings.TrimSpace(reported)
	parts := strings.SplitN(reported, " ", 2)
	if len(parts) == 2 {
		return strings.TrimSpace(parts[1]), strings.TrimSpace(parts[0])
	}
	return reported, ""
}

func isCJK(s string) bool {
	for _, r := range s {
		if (r >= 0x4E00 && r <= 0x9FFF) || (r >= 0x3040 && r <= 0x30FF) {
			return true
		}
	}
	return false
}

func canonicalActressRepresentations(actress *models.Actress) []string {
	if actress == nil {
		return nil
	}
	canonical := *actress
	canonical.Aliases = ""
	return collectActressAliasCandidates(&canonical)
}

func actressNameMatchesCanonicalRepresentations(reported string, actress *models.Actress) bool {
	reportedKey := models.NormalizeActressNameKey(reported)
	if reportedKey == "" {
		return false
	}
	for _, canonical := range canonicalActressRepresentations(actress) {
		canonicalKey := models.NormalizeActressNameKey(canonical)
		if canonicalKey != "" && reportedKey == canonicalKey {
			return true
		}
	}
	return false
}

func transitionActressCanonicalNamesTx(tx *gorm.DB, actressID uint, previous *models.Actress) error {
	return transitionActressCanonicalNamesForMergeTx(tx, actressID, actressID, previous)
}

// transitionActressCanonicalNamesForMergeTx binds canonical ownership proof to
// the one source and one target selected by the merge. Rename callers pass the
// same ID for both arguments.
func transitionActressCanonicalNamesForMergeTx(tx *gorm.DB, targetActressID, sourceActressID uint, previous *models.Actress) error {
	actressID := targetActressID
	if previous == nil {
		return nil
	}
	var current models.Actress
	if err := tx.First(&current, actressID).Error; err != nil {
		return wrapDBErr("load", fmt.Sprintf("actress %d", actressID), err)
	}
	newCanonical := canonicalActressName(&current)
	if strings.TrimSpace(newCanonical) == "" {
		return nil
	}
	currentKeys := make(map[string]struct{})
	for _, name := range canonicalActressRepresentations(&current) {
		if key := models.NormalizeActressNameKey(name); key != "" {
			currentKeys[key] = struct{}{}
		}
	}
	previousNames := canonicalActressRepresentations(previous)
	previousKeys := make(map[string]struct{}, len(previousNames))
	for _, name := range previousNames {
		if key := models.NormalizeActressNameKey(name); key != "" {
			previousKeys[key] = struct{}{}
		}
	}
	provenOwnerID := previous.ID
	if provenOwnerID == 0 && sourceActressID == targetActressID {
		provenOwnerID = actressID
	}
	if provenOwnerID != sourceActressID && provenOwnerID != targetActressID {
		return fmt.Errorf("canonical transition snapshot actress %d is not merge source %d or target %d: %w", provenOwnerID, sourceActressID, targetActressID, ErrActressAliasOwnershipConflict)
	}
	for _, oldName := range previousNames {
		oldName = strings.TrimSpace(oldName)
		key := models.NormalizeActressNameKey(oldName)
		if _, unchanged := currentKeys[key]; unchanged {
			continue
		}
		if err := retargetProvenCanonicalAliasesTx(tx, sourceActressID, targetActressID, oldName, newCanonical, previousKeys); err != nil {
			return wrapDBErr("retarget", fmt.Sprintf("actress aliases for %d", actressID), err)
		}
		existing, err := normalizedActressAliasesTx(tx, oldName)
		switch {
		case errors.Is(err, gorm.ErrRecordNotFound):
			if err := tx.Create(&models.ActressAlias{AliasName: oldName, CanonicalName: newCanonical}).Error; err != nil {
				return wrapDBErr("create", fmt.Sprintf("actress alias %s", oldName), err)
			}
		case err != nil:
			return wrapDBErr("find", fmt.Sprintf("actress alias %s", oldName), err)
		default:
			ownerKey := models.NormalizeActressNameKey(existing[0].CanonicalName)
			if ownerKey == models.NormalizeActressNameKey(newCanonical) {
				continue
			}
			if _, owned := previousKeys[ownerKey]; !owned {
				// This spelling belongs to an unrelated identity. A merge may
				// retarget only aliases whose old owner is represented by the
				// source snapshot loaded in this transaction.
				continue
			}
			ids := make([]uint, len(existing))
			for i := range existing {
				ids[i] = existing[i].ID
			}
			if err := tx.Model(&models.ActressAlias{}).Where("id IN ?", ids).Updates(map[string]interface{}{
				colCanonicalName: newCanonical, "canonical_name_key": models.NormalizeActressNameKey(newCanonical), colUpdatedAt: time.Now().UTC(),
			}).Error; err != nil {
				return wrapDBErr("retarget", fmt.Sprintf("actress alias %s", oldName), err)
			}
		}
	}
	return nil
}

func upsertAliasTx(tx *gorm.DB, alias *models.ActressAlias) error {
	return claimNormalizedActressAliasTx(tx, alias)
}

func reassignLegacyActressTx(tx *gorm.DB, movieContentID string, sourceActressID, targetActressID uint, suppressed bool) error {
	if suppressed {
		return tx.Exec(
			"DELETE FROM movie_actresses WHERE movie_content_id = ? AND actress_id IN (?, ?)",
			movieContentID, sourceActressID, targetActressID,
		).Error
	}
	if err := tx.Exec(
		"INSERT OR IGNORE INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)",
		movieContentID, targetActressID,
	).Error; err != nil {
		return err
	}
	return tx.Exec(
		"DELETE FROM movie_actresses WHERE movie_content_id = ? AND actress_id = ?",
		movieContentID, sourceActressID,
	).Error
}

func reassignCreditTx(tx *gorm.DB, credit *models.MovieCredit, targetActressID uint) error {
	var target models.Actress
	if err := tx.Where("id = ? AND verified = ?", targetActressID, true).First(&target).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return fmt.Errorf("reassign credit: target actress %d: %w", targetActressID, ErrNotFound)
		}
		return wrapDBErr("find", fmt.Sprintf("target actress %d", targetActressID), err)
	}
	if err := recordCreditReassignmentTx(tx, credit.MovieContentID, credit.ActressID, targetActressID); err != nil {
		return err
	}
	var targetCredit models.MovieCredit
	err := tx.Model(&models.MovieCredit{}).
		Where("movie_content_id = ? AND actress_id = ?", credit.MovieContentID, targetActressID).
		First(&targetCredit).Error
	if err == nil {
		survivingSuppressed := targetCredit.Suppressed || credit.Suppressed
		updates := map[string]interface{}{colUpdatedAt: time.Now().UTC()}
		if credit.UserOverride && !targetCredit.UserOverride {
			updates["override_name"] = credit.OverrideName
			updates["user_override"] = true
		}
		if credit.Suppressed && !targetCredit.Suppressed {
			updates["suppressed"] = true
		}
		if credit.Origin == string(models.CreditOriginUser) && targetCredit.Origin != string(models.CreditOriginUser) {
			updates["origin"] = string(models.CreditOriginUser)
		}
		if credit.DisplayForceCanonical && !targetCredit.DisplayForceCanonical {
			updates["display_force_canonical"] = true
		}
		if credit.OrderPinned && !targetCredit.OrderPinned {
			updates["order_index"] = credit.OrderIndex
			updates["order_pinned"] = true
		}
		if len(updates) > 1 {
			if err := tx.Model(&models.MovieCredit{}).Where("id = ?", targetCredit.ID).Updates(updates).Error; err != nil {
				return err
			}
		}
		if err := transferCollisionsTx(tx, credit.ID, targetCredit.ID); err != nil {
			return err
		}
		if err := tx.Where("id = ?", credit.ID).Delete(&models.MovieCredit{}).Error; err != nil {
			return err
		}
		if err := reassignLegacyActressTx(tx, credit.MovieContentID, credit.ActressID, targetActressID, survivingSuppressed); err != nil {
			return err
		}
	} else {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			return err
		}
		if err := tx.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).Update("actress_id", targetActressID).Error; err != nil {
			return err
		}
		if err := reassignLegacyActressTx(tx, credit.MovieContentID, credit.ActressID, targetActressID, credit.Suppressed); err != nil {
			return err
		}
	}
	if err := reconcileActressCollisionsTx(tx, targetActressID); err != nil {
		return err
	}
	return nil
}

func transferCollisionsTx(tx *gorm.DB, fromCreditID, toCreditID uint) error {
	var sourceCollisions []models.CreditCollision
	if err := tx.Where("credit_id = ?", fromCreditID).Find(&sourceCollisions).Error; err != nil {
		return err
	}
	for i := range sourceCollisions {
		sc := &sourceCollisions[i]
		var targetCollision models.CreditCollision
		err := tx.Where(
			"credit_id = ? AND field = ? AND reported_value = ?",
			toCreditID, sc.Field, sc.ReportedValue).First(&targetCollision).Error
		if err == nil {
			merged := map[string]interface{}{
				"occurrences": targetCollision.Occurrences + sc.Occurrences,
				colUpdatedAt:  time.Now().UTC(),
			}
			mergedSources := mergeSourceLists(targetCollision.SourcesSeen, sc.SourcesSeen)
			if mergedSources != "" {
				merged["sources_seen"] = mergedSources
			}
			if sc.UserPinned || targetCollision.UserPinned {
				merged["user_pinned"] = true
			}
			if sc.Status == models.CollisionStatusOpen || targetCollision.Status == models.CollisionStatusOpen {
				merged["status"] = models.CollisionStatusOpen
				merged["resolution"] = ""
			}
			if err := tx.Model(&models.CreditCollision{}).Where("id = ?", targetCollision.ID).Updates(merged).Error; err != nil {
				return err
			}
			if err := tx.Delete(sc).Error; err != nil {
				return err
			}
		} else if errors.Is(err, gorm.ErrRecordNotFound) {
			if err := tx.Model(&models.CreditCollision{}).Where("id = ?", sc.ID).
				Updates(map[string]interface{}{"credit_id": toCreditID, colUpdatedAt: time.Now().UTC()}).Error; err != nil {
				return err
			}
		} else {
			return err
		}
	}
	return nil
}

func mergeSourceLists(a, b string) string {
	a, b = strings.TrimSpace(a), strings.TrimSpace(b)
	if a == "" {
		return b
	}
	if b == "" {
		return a
	}
	seen := make(map[string]bool)
	out := make([]string, 0, 4)
	for _, part := range strings.Split(a+","+b, ",") {
		part = strings.TrimSpace(part)
		if part == "" || seen[part] {
			continue
		}
		seen[part] = true
		out = append(out, part)
	}
	return strings.Join(out, ",")
}
