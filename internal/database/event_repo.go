package database

import (
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

type EventRepository struct {
	db *DB
}

func NewEventRepository(db *DB) *EventRepository {
	return &EventRepository{db: db}
}

func (r *EventRepository) Create(event *models.Event) error {
	if err := r.db.Create(event).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("event %d", event.ID), err)
	}
	return nil
}

func (r *EventRepository) FindByID(id uint) (*models.Event, error) {
	var event models.Event
	err := r.db.First(&event, id).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("event %d", id), err)
	}
	return &event, nil
}

func (r *EventRepository) FindByType(eventType string, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("event_type = ?", eventType).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("events by type %s", eventType), err)
	}
	return events, nil
}

func (r *EventRepository) FindBySeverity(severity string, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("severity = ?", severity).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("events by severity %s", severity), err)
	}
	return events, nil
}

func (r *EventRepository) FindByTypeAndSeverity(eventType, severity string, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("event_type = ? AND severity = ?", eventType, severity).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("events by type %s and severity %s", eventType, severity), err)
	}
	return events, nil
}

func (r *EventRepository) FindBySource(source string, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("source = ?", source).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("events by source %s", source), err)
	}
	return events, nil
}

func (r *EventRepository) FindByDateRange(start, end time.Time, limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Where("datetime(created_at) >= datetime(?) AND datetime(created_at) < datetime(?)", start.Format(SqliteTimeFormat), end.Format(SqliteTimeFormat)).Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	if err != nil {
		return nil, wrapDBErr("find", "events by date range", err)
	}
	return events, nil
}

func (r *EventRepository) FindFiltered(filter EventFilter, limit, offset int) ([]models.Event, error) {
	query := r.db.Order("created_at DESC").Limit(limit).Offset(offset)
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
		query = query.Where("datetime(created_at) >= datetime(?)", filter.Start.UTC().Format(SqliteTimeFormat))
	}
	if filter.End != nil {
		query = query.Where("datetime(created_at) < datetime(?)", filter.End.UTC().Format(SqliteTimeFormat))
	}
	var events []models.Event
	err := query.Find(&events).Error
	if err != nil {
		return nil, wrapDBErr("find", "filtered events", err)
	}
	return events, nil
}

func (r *EventRepository) CountFiltered(filter EventFilter) (int64, error) {
	query := r.db.Model(&models.Event{})
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
		query = query.Where("datetime(created_at) >= datetime(?)", filter.Start.UTC().Format(SqliteTimeFormat))
	}
	if filter.End != nil {
		query = query.Where("datetime(created_at) < datetime(?)", filter.End.UTC().Format(SqliteTimeFormat))
	}
	var count int64
	err := query.Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "filtered events", err)
	}
	return count, nil
}

func (r *EventRepository) List(limit, offset int) ([]models.Event, error) {
	var events []models.Event
	err := r.db.Order("created_at DESC").Limit(limit).Offset(offset).Find(&events).Error
	if err != nil {
		return nil, wrapDBErr("find", "events", err)
	}
	return events, nil
}

func (r *EventRepository) Count() (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "events", err)
	}
	return count, nil
}

func (r *EventRepository) CountByType(eventType string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("event_type = ?", eventType).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("events by type %s", eventType), err)
	}
	return count, nil
}

func (r *EventRepository) CountBySeverity(severity string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("severity = ?", severity).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("events by severity %s", severity), err)
	}
	return count, nil
}

func (r *EventRepository) CountByTypeAndSeverity(eventType, severity string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("event_type = ? AND severity = ?", eventType, severity).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("events by type %s and severity %s", eventType, severity), err)
	}
	return count, nil
}

func (r *EventRepository) CountBySource(source string) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("source = ?", source).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", fmt.Sprintf("events by source %s", source), err)
	}
	return count, nil
}

func (r *EventRepository) CountGroupBySource() (map[string]int64, error) {
	type result struct {
		Source string
		Count  int64
	}
	var results []result
	err := r.db.Model(&models.Event{}).Select("source, count(*) as count").Group("source").Find(&results).Error
	if err != nil {
		return nil, wrapDBErr("count", "events grouped by source", err)
	}
	bySource := make(map[string]int64, len(results))
	for _, r := range results {
		bySource[r.Source] = r.Count
	}
	return bySource, nil
}

func (r *EventRepository) CountByDateRange(start, end time.Time) (int64, error) {
	var count int64
	err := r.db.Model(&models.Event{}).Where("datetime(created_at) >= datetime(?) AND datetime(created_at) < datetime(?)", start.Format(SqliteTimeFormat), end.Format(SqliteTimeFormat)).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", "events by date range", err)
	}
	return count, nil
}

func (r *EventRepository) DeleteOlderThan(date time.Time) error {
	if err := r.db.Where("datetime(created_at) < datetime(?)", date.Format(SqliteTimeFormat)).Delete(&models.Event{}).Error; err != nil {
		return wrapDBErr("delete", "events older than date", err)
	}
	return nil
}
