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

	switch resolution {
	case models.CollisionResolutionKeepIdentity:
		if collision.Field != models.CreditFieldIdentityLink {
			if err := tx.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).
				Update("display_force_canonical", true).Error; err != nil {
				return 0, wrapDBErr("force canonical", fmt.Sprintf("credit %d", credit.ID), err)
			}
		}
	case models.CollisionResolutionAdoptCanonical:
		if collision.Field == models.CreditFieldIdentityLink {
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
		} else if collision.Field == models.CreditFieldReportedThumb {
			if err := tx.Model(&models.Actress{}).Where("id = ?", credit.ActressID).
				Update("thumb_url", collision.ReportedValue).Error; err != nil {
				return 0, wrapDBErr("adopt canonical", fmt.Sprintf("actress %d", credit.ActressID), err)
			}
		} else if collision.Field == models.CreditFieldCreditedName {
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
		if err := tx.Exec(
			"UPDATE movies SET render_dirty = 1, render_generation = render_generation + 1, updated_at = CURRENT_TIMESTAMP WHERE content_id IN (SELECT movie_content_id FROM movie_credits WHERE actress_id = ?)",
			credit.ActressID,
		).Error; err != nil {
			return 0, wrapDBErr("mark dirty", fmt.Sprintf("movies for actress %d", credit.ActressID), err)
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
					colResolution: models.CollisionResolutionByRemoval,
					colUpdatedAt:  time.Now().UTC(),
				}).Error; err != nil {
				return err
			}
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

func reassignCreditTx(tx *gorm.DB, credit *models.MovieCredit, targetActressID uint) error {
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
		return tx.Where("id = ?", credit.ID).Delete(&models.MovieCredit{}).Error
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return err
	}
	return tx.Model(&models.MovieCredit{}).Where("id = ?", credit.ID).Update("actress_id", targetActressID).Error
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
