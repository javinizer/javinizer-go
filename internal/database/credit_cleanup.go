package database

import (
	"fmt"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
)

func deleteCreditRecordsTx(tx *gorm.DB, collisionWhere, creditWhere string, value any, label string) error {
	if err := tx.Where(collisionWhere, value).Delete(&models.CreditCollision{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("credit collisions for %s", label), err)
	}
	if err := tx.Where(creditWhere, value).Delete(&models.MovieCredit{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("movie credits for %s", label), err)
	}
	return nil
}

func deleteCreditReassignmentsTx(tx *gorm.DB, where, label string, args ...any) error {
	if err := tx.Where(where, args...).Delete(&models.MovieCreditReassignment{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("credit reassignments for %s", label), err)
	}
	return nil
}
