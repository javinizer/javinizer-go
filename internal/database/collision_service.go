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

// resolveTx applies the resolution outcome atomically and recomputes the
// open-collision count.
func (s *CollisionService) resolveTx(tx *gorm.DB, collisionID uint, resolution string, targetActressID uint) (remaining int, err error) {
	var collision models.CreditCollision
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
	if err := tx.First(&collision, collisionID).Error; err != nil {
		return 0, wrapDBErr("load", fmt.Sprintf("collision %d", collisionID), err)
	}

	var credit models.MovieCredit
	if err := tx.Preload("Actress").First(&credit, collision.CreditID).Error; err != nil {
		return 0, wrapDBErr("load", fmt.Sprintf("credit %d", collision.CreditID), err)
	}
	creditPtr := &credit
	oldCanonicalName := ""
	if credit.Actress != nil {
		oldCanonicalName = credit.Actress.FullName()
	}

	switch resolution {
	case models.CollisionResolutionKeepIdentity:
		if collision.Field != models.CreditFieldIdentityLink {
			if err := tx.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).
				Update("display_force_canonical", true).Error; err != nil {
				return 0, wrapDBErr("force canonical", fmt.Sprintf("credit %d", credit.ID), err)
			}
		}
	case models.CollisionResolutionAdoptCanonical:
		switch collision.Field {
		case models.CreditFieldIdentityLink:
			updates := map[string]interface{}{
				colVerified:  true,
				colOrigin:    ActressOriginUser,
				colUpdatedAt: time.Now().UTC(),
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
			if err := tx.Model(&models.Actress{}).Where("id = ?", credit.ActressID).
				Update("thumb_url", collision.ReportedValue).Error; err != nil {
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
		if collision.Field != models.CreditFieldCreditedName {
			return 0, fmt.Errorf("resolve collision: adopt_alias requires a credited_name collision")
		}
		if err := tx.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).
			Update("display_force_canonical", true).Error; err != nil {
			return 0, wrapDBErr("force canonical", fmt.Sprintf("credit %d", credit.ID), err)
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
		if targetActressID == 0 {
			return 0, fmt.Errorf("resolve collision: target_actress_id is required for reassign")
		}
		if targetActressID == credit.ActressID {
			return 0, fmt.Errorf("resolve collision: credit already linked to target identity")
		}
		if err := reassignCreditTx(tx, creditPtr, targetActressID); err != nil {
			return 0, err
		}
	}

	if resolution == models.CollisionResolutionAdoptCanonical {
		if err := retargetActressAliasesTx(tx, credit.ActressID, oldCanonicalName); err != nil {
			return 0, err
		}
		if err := reconcileActressCollisionsTx(tx, credit.ActressID); err != nil {
			return 0, err
		}
		if err := restoreActressProjectionTx(tx, credit.ActressID); err != nil {
			return 0, err
		}
	} else if err := tx.Exec(
		"UPDATE movies SET render_dirty = 1, render_generation = render_generation + 1, updated_at = CURRENT_TIMESTAMP WHERE content_id = ?",
		collision.MovieContentID,
	).Error; err != nil {
		return 0, wrapDBErr("mark dirty", fmt.Sprintf("movie %s", collision.MovieContentID), err)
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
		r, e := s.resolveTx(tx, collisionID, resolution, targetActressID)
		remainingOut = r
		return e
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
		updates := map[string]interface{}{
			colOverrideName: overrideName,
			colUserOverride: userOverride,
			colOrigin:       string(models.CreditOriginUser),
			colUpdatedAt:    time.Now().UTC(),
		}
		res := tx.Model(&models.MovieCredit{}).Where("id = ?", creditID).Updates(updates)
		if res.Error != nil {
			return wrapDBErr("update override", fmt.Sprintf("movie credit %d", creditID), res.Error)
		}
		if res.RowsAffected == 0 {
			return fmt.Errorf("update override: movie credit %d: %w", creditID, ErrNotFound)
		}
		var contentID string
		if err := tx.Model(&models.MovieCredit{}).Where("id = ?", creditID).Pluck("movie_content_id", &contentID).Error; err != nil {
			return err
		}
		return tx.Exec(
			"UPDATE movies SET render_dirty = 1, render_generation = render_generation + 1, updated_at = CURRENT_TIMESTAMP WHERE content_id = ?",
			contentID,
		).Error
	})
}

// SetCreditSuppressed toggles the user-removal tombstone on a credit and
// closes its open collisions when suppressed.
func (s *CollisionService) SetCreditSuppressed(ctx context.Context, creditID uint, suppressed bool) error {
	return s.db.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		return setCreditSuppressedTx(tx, creditID, suppressed)
	})
}

func setCreditSuppressedTx(tx *gorm.DB, creditID uint, suppressed bool) error {
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
	collisionUpdates := map[string]interface{}{
		colStatus:     models.CollisionStatusResolved,
		colResolution: models.CollisionResolutionBySuppression,
		colUpdatedAt:  time.Now().UTC(),
	}
	collisionQuery := tx.Model(&models.CreditCollision{}).
		Where("credit_id = ? AND status = ?", creditID, models.CollisionStatusOpen)
	if !suppressed {
		collisionUpdates = map[string]interface{}{
			colStatus:     models.CollisionStatusOpen,
			colResolution: "",
			"user_pinned": true,
			colUpdatedAt:  time.Now().UTC(),
		}
		collisionQuery = tx.Model(&models.CreditCollision{}).
			Where("credit_id = ? AND status = ? AND resolution = ?", creditID, models.CollisionStatusResolved, models.CollisionResolutionBySuppression)
	}
	if err := collisionQuery.Updates(collisionUpdates).Error; err != nil {
		return err
	}
	var credit models.MovieCredit
	if err := tx.Model(&models.MovieCredit{}).Select("movie_content_id", "actress_id").Where("id = ?", creditID).First(&credit).Error; err != nil {
		return err
	}
	if suppressed {
		if err := tx.Exec(
			"DELETE FROM movie_actresses WHERE movie_content_id = ? AND actress_id = ?",
			credit.MovieContentID, credit.ActressID,
		).Error; err != nil {
			return err
		}
	} else if err := tx.Exec(
		"INSERT OR IGNORE INTO movie_actresses (movie_content_id, actress_id) VALUES (?, ?)",
		credit.MovieContentID, credit.ActressID,
	).Error; err != nil {
		return err
	}
	return tx.Exec(
		"UPDATE movies SET render_dirty = 1, render_generation = render_generation + 1, updated_at = CURRENT_TIMESTAMP WHERE content_id = ?",
		credit.MovieContentID,
	).Error
}

func reconcileActressCollisionsTx(tx *gorm.DB, actressID uint) error {
	var actress models.Actress
	if err := tx.First(&actress, actressID).Error; err != nil {
		return wrapDBErr("load", fmt.Sprintf("actress %d", actressID), err)
	}

	var collisions []models.CreditCollision
	if err := tx.Model(&models.CreditCollision{}).
		Joins("JOIN movie_credits ON movie_credits.id = credit_collisions.credit_id").
		Where("movie_credits.actress_id = ? AND credit_collisions.status = ? AND credit_collisions.field IN ?", actressID, models.CollisionStatusOpen, []string{
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
			matches = strings.TrimSpace(collision.ReportedValue) != "" && strings.TrimSpace(canonicalValue) != "" &&
				models.NormalizeActressNameKey(collision.ReportedValue) == models.NormalizeActressNameKey(canonicalValue)
		case models.CreditFieldReportedThumb:
			canonicalValue = actress.ThumbURL
			matches = strings.TrimSpace(collision.ReportedValue) != "" && strings.TrimSpace(canonicalValue) != "" && collision.ReportedValue == canonicalValue
		}
		updates := map[string]interface{}{
			"canonical_value": canonicalValue,
			colUpdatedAt:      time.Now().UTC(),
		}
		if !collision.UserPinned && matches {
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

func retargetActressAliasesTx(tx *gorm.DB, actressID uint, oldCanonicalName string) error {
	if strings.TrimSpace(oldCanonicalName) == "" {
		return nil
	}
	var actress models.Actress
	if err := tx.First(&actress, actressID).Error; err != nil {
		return wrapDBErr("load", fmt.Sprintf("actress %d", actressID), err)
	}
	newCanonicalName := canonicalActressName(&actress)
	if strings.TrimSpace(newCanonicalName) == "" || oldCanonicalName == newCanonicalName {
		return nil
	}
	if err := tx.Model(&models.ActressAlias{}).Where("canonical_name = ?", oldCanonicalName).Updates(map[string]interface{}{
		colCanonicalName: newCanonicalName,
		colUpdatedAt:     time.Now().UTC(),
	}).Error; err != nil {
		return wrapDBErr("retarget", fmt.Sprintf("actress aliases for %d", actressID), err)
	}
	return nil
}

func upsertAliasTx(tx *gorm.DB, alias *models.ActressAlias) error {
	var existing models.ActressAlias
	err := tx.First(&existing, "alias_name = ?", alias.AliasName).Error
	if err == nil {
		alias.ID = existing.ID
		alias.CreatedAt = existing.CreatedAt
		if err := tx.Model(&existing).Updates(map[string]interface{}{
			colCanonicalName: alias.CanonicalName,
			colUpdatedAt:     time.Now().UTC(),
		}).Error; err != nil {
			return wrapDBErr("update", fmt.Sprintf("actress alias %s", alias.AliasName), err)
		}
		alias.ID = existing.ID
		return nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return wrapDBErr("find", fmt.Sprintf("actress alias %s", alias.AliasName), err)
	}
	if err := tx.Create(alias).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("actress alias %s", alias.AliasName), err)
	}
	return nil
}

func reassignLegacyActressTx(tx *gorm.DB, movieContentID string, sourceActressID, targetActressID uint) error {
	if err := tx.Exec(`
		INSERT OR IGNORE INTO movie_actresses (movie_content_id, actress_id)
		SELECT ?, ?
		WHERE EXISTS (
			SELECT 1 FROM movie_actresses WHERE movie_content_id = ? AND actress_id = ?
		)`, movieContentID, targetActressID, movieContentID, sourceActressID).Error; err != nil {
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
		return reassignLegacyActressTx(tx, credit.MovieContentID, credit.ActressID, targetActressID)
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	if err := tx.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).Update("actress_id", targetActressID).Error; err != nil {
		return err
	}
	return reassignLegacyActressTx(tx, credit.MovieContentID, credit.ActressID, targetActressID)
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
