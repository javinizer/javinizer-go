package database

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
)

type collisionFieldEffect struct {
	setForceCanonical bool
	forceCanonical    bool
}

func collisionEffect(field, resolution string) collisionFieldEffect {
	if field != models.CreditFieldCreditedName {
		return collisionFieldEffect{}
	}
	switch resolution {
	case models.CollisionResolutionKeepIdentity,
		models.CollisionResolutionAdoptAlias,
		models.CollisionResolutionAutoKeep,
		models.CollisionResolutionAutoAlias:
		return collisionFieldEffect{setForceCanonical: true, forceCanonical: true}
	default:
		return collisionFieldEffect{}
	}
}

func applyCollisionFieldEffectTx(tx *gorm.DB, creditID uint, field, resolution string) error {
	effect := collisionEffect(field, resolution)
	if !effect.setForceCanonical {
		return nil
	}
	if err := tx.Model(&models.MovieCredit{}).Where("id = ?", creditID).
		Update("display_force_canonical", effect.forceCanonical).Error; err != nil {
		return wrapDBErr("apply collision field effect", fmt.Sprintf("credit %d", creditID), err)
	}
	return nil
}
