package database

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

type EventRepository struct {
	*BaseRepository[models.Event, uint]
}

func NewEventRepository(db *DB) *EventRepository {
	return &EventRepository{
		BaseRepository: NewBaseRepository[models.Event, uint](
			db, "event",
			func(e models.Event) string { return fmt.Sprintf("%d", e.ID) },
			withDefaultOrder[models.Event, uint]("created_at DESC"),
			WithNewEntity[models.Event, uint](func() models.Event { return models.Event{} }),
		),
	}
}

func (r *EventRepository) Create(ctx context.Context, event *models.Event) error {
	return r.BaseRepository.Create(ctx, event)
}

func (r *EventRepository) FindByID(ctx context.Context, id uint) (*models.Event, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

func (r *EventRepository) FindFiltered(ctx context.Context, filter EventFilter, limit, offset int) ([]models.Event, error) {
	query := r.GetDB().WithContext(ctx).Order("created_at DESC").Limit(limit).Offset(offset)
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.Start != nil {
		query = query.Where("datetime(created_at) >= datetime(?)", filter.Start.UTC().Format(sqliteTimeFormat))
	}
	if filter.End != nil {
		query = query.Where("datetime(created_at) < datetime(?)", filter.End.UTC().Format(sqliteTimeFormat))
	}
	var events []models.Event
	err := query.Find(&events).Error
	if err != nil {
		return nil, wrapDBErr("find", "filtered events", err)
	}
	return events, nil
}

func (r *EventRepository) CountFiltered(ctx context.Context, filter EventFilter) (int64, error) {
	query := r.GetDB().WithContext(ctx).Model(&models.Event{})
	if filter.EventType != "" {
		query = query.Where("event_type = ?", filter.EventType)
	}
	if filter.Severity != "" {
		query = query.Where("severity = ?", filter.Severity)
	}
	if filter.Source != "" {
		query = query.Where("source = ?", filter.Source)
	}
	if filter.Start != nil {
		query = query.Where("datetime(created_at) >= datetime(?)", filter.Start.UTC().Format(sqliteTimeFormat))
	}
	if filter.End != nil {
		query = query.Where("datetime(created_at) < datetime(?)", filter.End.UTC().Format(sqliteTimeFormat))
	}
	var count int64
	err := query.Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "filtered events", err)
	}
	return count, nil
}

func (r *EventRepository) List(ctx context.Context, limit, offset int) ([]models.Event, error) {
	return r.BaseRepository.List(ctx, limit, offset)
}

func (r *EventRepository) Count(ctx context.Context) (int64, error) {
	return r.BaseRepository.Count(ctx)
}

func (r *EventRepository) CountGroupBySource(ctx context.Context) (map[string]int64, error) {
	type result struct {
		Source string
		Count  int64
	}
	var results []result
	err := r.GetDB().WithContext(ctx).Model(&models.Event{}).Select("source, count(*) as count").Group("source").Find(&results).Error
	if err != nil {
		return nil, wrapDBErr("count", "events grouped by source", err)
	}
	bySource := make(map[string]int64, len(results))
	for _, r := range results {
		bySource[r.Source] = r.Count
	}
	return bySource, nil
}

func (r *EventRepository) DeleteOlderThan(ctx context.Context, date time.Time) (int64, error) {
	result := r.GetDB().WithContext(ctx).Where("datetime(created_at) < datetime(?)", date.UTC().Format(sqliteTimeFormat)).Delete(&models.Event{})
	if result.Error != nil {
		return 0, wrapDBErr("delete", "events older than date", result.Error)
	}
	return result.RowsAffected, nil
}
