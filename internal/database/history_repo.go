package database

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

type HistoryRepository struct {
	db *DB
}

func NewHistoryRepository(db *DB) *HistoryRepository {
	return &HistoryRepository{db: db}
}

func (r *HistoryRepository) Create(history *models.History) error {
	if err := r.db.Create(history).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("history %d", history.ID), err)
	}
	return nil
}

func (r *HistoryRepository) FindByID(id uint) (*models.History, error) {
	var history models.History
	err := r.db.First(&history, id).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history %d", id), err)
	}
	return &history, nil
}

func (r *HistoryRepository) FindByMovieID(movieID string) ([]models.History, error) {
	var history []models.History
	err := r.db.Where("movie_id = ?", movieID).Order("created_at DESC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByOperation(operation string, limit int) ([]models.History, error) {
	var history []models.History
	query := r.db.Where("operation = ?", operation).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by operation %s", operation), err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByStatus(status string, limit int) ([]models.History, error) {
	var history []models.History
	query := r.db.Where("status = ?", status).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by status %s", status), err)
	}
	return history, nil
}

func (r *HistoryRepository) FindRecent(limit int) ([]models.History, error) {
	var history []models.History
	err := r.db.Order("created_at DESC").Limit(limit).Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "recent history", err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByDateRange(start, end time.Time) ([]models.History, error) {
	var history []models.History
	err := r.db.Where("datetime(created_at) BETWEEN datetime(?) AND datetime(?)", start.Format(SqliteTimeFormat), end.Format(SqliteTimeFormat)).Order("created_at DESC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "history by date range", err)
	}
	return history, nil
}

func (r *HistoryRepository) Count() (int64, error) {
	var count int64
	err := r.db.Model(&models.History{}).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "history", err)
	}
	return count, nil
}

func (r *HistoryRepository) CountByStatus(status string) (int64, error) {
	var count int64
	err := r.db.Model(&models.History{}).Where("status = ?", status).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("history by status %s", status), err)
	}
	return count, nil
}

func (r *HistoryRepository) CountByOperation(operation string) (int64, error) {
	var count int64
	err := r.db.Model(&models.History{}).Where("operation = ?", operation).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("history by operation %s", operation), err)
	}
	return count, nil
}

func (r *HistoryRepository) Delete(id uint) error {
	if err := r.db.Delete(&models.History{}, id).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("history %d", id), err)
	}
	return nil
}

func (r *HistoryRepository) DeleteByMovieID(movieID string) error {
	if err := r.db.Where("movie_id = ?", movieID).Delete(&models.History{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return nil
}

func (r *HistoryRepository) DeleteOlderThan(date time.Time) error {
	if err := r.db.Where("datetime(created_at) < datetime(?)", date.Format(SqliteTimeFormat)).Delete(&models.History{}).Error; err != nil {
		return wrapDBErr("delete", "history older than date", err)
	}
	return nil
}

func (r *HistoryRepository) List(limit, offset int) ([]models.History, error) {
	var history []models.History
	err := r.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "history", err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByBatchJobID(batchJobID string) ([]models.History, error) {
	var history []models.History
	err := r.db.Where("batch_job_id = ?", batchJobID).Order("created_at ASC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history for batch job %s", batchJobID), err)
	}
	return history, nil
}
