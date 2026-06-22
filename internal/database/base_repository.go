package database

import (
	"context"
	"errors"
	"fmt"

	"gorm.io/gorm"
)

type BaseRepository[T any, ID string | uint] struct {
	db           *DB
	entityName   string
	labelFunc    func(t T) string
	defaultOrder string
	newEntity    func() T
}

type BaseRepoOption[T any, ID string | uint] func(*BaseRepository[T, ID])

func withDefaultOrder[T any, ID string | uint](order string) BaseRepoOption[T, ID] {
	return func(br *BaseRepository[T, ID]) { br.defaultOrder = order }
}

func WithNewEntity[T any, ID string | uint](fn func() T) BaseRepoOption[T, ID] {
	return func(br *BaseRepository[T, ID]) { br.newEntity = fn }
}

func NewBaseRepository[T any, ID string | uint](
	db *DB,
	entityName string,
	labelFunc func(t T) string,
	opts ...BaseRepoOption[T, ID],
) *BaseRepository[T, ID] {
	br := &BaseRepository[T, ID]{
		db:         db,
		entityName: entityName,
		labelFunc:  labelFunc,
		newEntity:  func() T { var zero T; return zero },
	}
	for _, opt := range opts {
		opt(br)
	}
	return br
}

func (r *BaseRepository[T, ID]) Create(ctx context.Context, entity *T) error {
	if entity == nil {
		return fmt.Errorf("create %s: entity must not be nil", r.entityName)
	}
	if err := r.db.WithContext(ctx).Create(entity).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("%s %s", r.entityName, r.labelFunc(*entity)), err)
	}
	return nil
}

func (r *BaseRepository[T, ID]) FindByID(ctx context.Context, id ID) (*T, error) {
	var entity T
	var err error
	switch any(id).(type) {
	case string:
		err = r.db.WithContext(ctx).First(&entity, "id = ?", id).Error
	default:
		err = r.db.WithContext(ctx).First(&entity, id).Error
	}
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, fmt.Errorf("find %s %v: %w", r.entityName, id, ErrNotFound)
		}
		return nil, wrapDBErr("find", fmt.Sprintf("%s %v", r.entityName, id), err)
	}
	return &entity, nil
}

func (r *BaseRepository[T, ID]) Delete(ctx context.Context, id ID) error {
	var err error
	switch any(id).(type) {
	case string:
		err = r.db.WithContext(ctx).Delete(r.newEntity(), "id = ?", id).Error
	default:
		err = r.db.WithContext(ctx).Delete(r.newEntity(), id).Error
	}
	if err != nil {
		return wrapDBErr("delete", fmt.Sprintf("%s %v", r.entityName, id), err)
	}
	return nil
}

func (r *BaseRepository[T, ID]) List(ctx context.Context, limit, offset int) ([]T, error) {
	var entities []T
	query := r.db.WithContext(ctx).Session(&gorm.Session{})
	if r.defaultOrder != "" {
		query = query.Order(r.defaultOrder)
	}
	if limit > 0 {
		query = query.Limit(limit).Offset(offset)
	}
	err := query.Find(&entities).Error
	if err != nil {
		return nil, wrapDBErr("find", r.entityName+"s", err)
	}
	return entities, nil
}

func (r *BaseRepository[T, ID]) ListAll(ctx context.Context) ([]T, error) {
	var entities []T
	query := r.db.WithContext(ctx).Session(&gorm.Session{})
	if r.defaultOrder != "" {
		query = query.Order(r.defaultOrder)
	}
	err := query.Find(&entities).Error
	if err != nil {
		return nil, wrapDBErr("find", r.entityName+"s", err)
	}
	return entities, nil
}

func (r *BaseRepository[T, ID]) Count(ctx context.Context) (int64, error) {
	var count int64
	zero := r.newEntity()
	err := r.db.WithContext(ctx).Model(&zero).Count(&count).Error
	if err != nil {
		return 0, wrapDBErr("count", r.entityName+"s", err)
	}
	return count, nil
}

func (r *BaseRepository[T, ID]) GetDB() *DB {
	return r.db
}
