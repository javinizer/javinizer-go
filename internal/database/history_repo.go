package database

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

type HistoryRepository struct {
	*BaseRepository[models.History, uint]
}

func NewHistoryRepository(db *DB) *HistoryRepository {
	return &HistoryRepository{
		BaseRepository: NewBaseRepository[models.History, uint](
			db, "history",
			func(h models.History) string { return fmt.Sprintf("%d", h.ID) },
			withDefaultOrder[models.History, uint]("created_at DESC"),
			WithNewEntity[models.History, uint](func() models.History { return models.History{} }),
		),
	}
}

func (r *HistoryRepository) Create(ctx context.Context, history *models.History) error {
	return r.BaseRepository.Create(ctx, history)
}

func (r *HistoryRepository) FindByID(ctx context.Context, id uint) (*models.History, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

func (r *HistoryRepository) FindByMovieID(ctx context.Context, movieID string) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().WithContext(ctx).Where("movie_id = ?", movieID).Order("created_at DESC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByOperation(ctx context.Context, operation models.HistoryOperation, limit int) ([]models.History, error) {
	var history []models.History
	query := r.GetDB().WithContext(ctx).Where("operation = ?", operation).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by operation %s", operation), err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByStatus(ctx context.Context, status models.HistoryStatus, limit int) ([]models.History, error) {
	var history []models.History
	query := r.GetDB().WithContext(ctx).Where("status = ?", status).Order("created_at DESC")
	if limit > 0 {
		query = query.Limit(limit)
	}
	err := query.Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history by status %s", status), err)
	}
	return history, nil
}

func (r *HistoryRepository) FindRecent(ctx context.Context, limit int) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().WithContext(ctx).Order("created_at DESC").Limit(limit).Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "recent history", err)
	}
	return history, nil
}

func (r *HistoryRepository) FindByDateRange(ctx context.Context, start, end time.Time) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().WithContext(ctx).Where("datetime(created_at) BETWEEN datetime(?) AND datetime(?)", start.Format(sqliteTimeFormat), end.Format(sqliteTimeFormat)).Order("created_at DESC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", "history by date range", err)
	}
	return history, nil
}

func (r *HistoryRepository) Count(ctx context.Context) (int64, error) {
	return r.BaseRepository.Count(ctx)
}

func (r *HistoryRepository) CountByStatus(ctx context.Context, status models.HistoryStatus) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.History{}).Where("status = ?", status).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("history by status %s", status), err)
	}
	return count, nil
}

func (r *HistoryRepository) CountByOperation(ctx context.Context, operation models.HistoryOperation) (int64, error) {
	var count int64
	err := r.GetDB().WithContext(ctx).Model(&models.History{}).Where("operation = ?", operation).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("history by operation %s", operation), err)
	}
	return count, nil
}

func (r *HistoryRepository) Delete(ctx context.Context, id uint) error {
	return r.BaseRepository.Delete(ctx, id)
}

func (r *HistoryRepository) DeleteByMovieID(ctx context.Context, movieID string) error {
	if err := r.GetDB().WithContext(ctx).Where("movie_id = ?", movieID).Delete(&models.History{}).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("history for movie %s", movieID), err)
	}
	return nil
}

func (r *HistoryRepository) DeleteOlderThan(ctx context.Context, date time.Time) error {
	if err := r.GetDB().WithContext(ctx).Where("datetime(created_at) < datetime(?)", date.UTC().Format(sqliteTimeFormat)).Delete(&models.History{}).Error; err != nil {
		return wrapDBErr("delete", "history older than date", err)
	}
	return nil
}

func (r *HistoryRepository) List(ctx context.Context, limit, offset int) ([]models.History, error) {
	return r.BaseRepository.List(ctx, limit, offset)
}

func (r *HistoryRepository) FindByBatchJobID(ctx context.Context, batchJobID string) ([]models.History, error) {
	var history []models.History
	err := r.GetDB().WithContext(ctx).Where("batch_job_id = ?", batchJobID).Order("created_at ASC").Find(&history).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("history for batch job %s", batchJobID), err)
	}
	return history, nil
}
