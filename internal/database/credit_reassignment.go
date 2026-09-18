package database

import (
	"time"

	"gorm.io/gorm"

	"github.com/javinizer/javinizer-go/internal/models"
)

func loadCreditReassignmentsTx(tx *gorm.DB, movieContentID string) (map[uint]uint, error) {
	var rows []models.MovieCreditReassignment
	if err := tx.Where("movie_content_id = ?", movieContentID).Find(&rows).Error; err != nil {
		return nil, err
	}
	result := make(map[uint]uint, len(rows))
	for _, row := range rows {
		result[row.SourceActressID] = row.TargetActressID
	}
	return result, nil
}

func recordCreditReassignmentTx(tx *gorm.DB, movieContentID string, sourceActressID, targetActressID uint) error {
	now := time.Now().UTC()
	if err := tx.Model(&models.MovieCreditReassignment{}).
		Where("movie_content_id = ? AND target_actress_id = ?", movieContentID, sourceActressID).
		Updates(map[string]interface{}{"target_actress_id": targetActressID, "updated_at": now}).Error; err != nil {
		return err
	}
	return tx.Exec(`
		INSERT INTO movie_credit_reassignments (movie_content_id, source_actress_id, target_actress_id, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?)
		ON CONFLICT(movie_content_id, source_actress_id) DO UPDATE SET
			target_actress_id = excluded.target_actress_id,
			updated_at = excluded.updated_at`,
		movieContentID, sourceActressID, targetActressID, now, now).Error
}

func moveCreditReassignmentsTx(tx *gorm.DB, sourceActressID, targetActressID uint) error {
	var rows []models.MovieCreditReassignment
	if err := tx.Where(
		"source_actress_id = ? OR target_actress_id = ? OR source_actress_id = ?",
		sourceActressID, sourceActressID, targetActressID,
	).Order("updated_at ASC, id ASC").Find(&rows).Error; err != nil {
		return err
	}
	if len(rows) == 0 {
		return nil
	}

	type reassignmentKey struct {
		movieContentID  string
		sourceActressID uint
	}
	ids := make([]uint, 0, len(rows))
	byKey := make(map[reassignmentKey]models.MovieCreditReassignment, len(rows))
	for _, row := range rows {
		ids = append(ids, row.ID)
		if row.SourceActressID == sourceActressID {
			row.SourceActressID = targetActressID
		}
		if row.TargetActressID == sourceActressID {
			row.TargetActressID = targetActressID
		}
		if row.SourceActressID == row.TargetActressID {
			continue
		}
		key := reassignmentKey{movieContentID: row.MovieContentID, sourceActressID: row.SourceActressID}
		byKey[key] = row
	}

	if err := tx.Where("id IN ?", ids).Delete(&models.MovieCreditReassignment{}).Error; err != nil {
		return err
	}
	if len(byKey) == 0 {
		return nil
	}
	now := time.Now().UTC()
	normalized := make([]models.MovieCreditReassignment, 0, len(byKey))
	for _, row := range byKey {
		row.ID = 0
		row.UpdatedAt = now
		normalized = append(normalized, row)
	}
	return tx.Create(&normalized).Error
}
