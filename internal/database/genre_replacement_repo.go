package database

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

type GenreReplacementRepository struct {
	*BaseRepository[models.GenreReplacement, uint]
}

func NewGenreReplacementRepository(db *DB) *GenreReplacementRepository {
	return &GenreReplacementRepository{
		BaseRepository: NewBaseRepository[models.GenreReplacement, uint](
			db, "genre replacement",
			func(g models.GenreReplacement) string { return g.Original },
			WithNewEntity[models.GenreReplacement, uint](func() models.GenreReplacement { return models.GenreReplacement{} }),
		),
	}
}

func (r *GenreReplacementRepository) Create(ctx context.Context, replacement *models.GenreReplacement) error {
	return WrapDuplicateKey(r.BaseRepository.Create(ctx, replacement))
}

func (r *GenreReplacementRepository) Upsert(ctx context.Context, replacement *models.GenreReplacement) error {
	existing, err := r.FindByOriginal(ctx, replacement.Original)
	if err != nil {
		if !IsNotFound(err) {
			return err
		}
		return r.Create(ctx, replacement)
	}

	replacement.ID = existing.ID
	replacement.CreatedAt = existing.CreatedAt
	if err := r.GetDB().WithContext(ctx).Save(replacement).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("genre replacement %s", replacement.Original), err)
	}
	return nil
}

func (r *GenreReplacementRepository) FindByOriginal(ctx context.Context, original string) (*models.GenreReplacement, error) {
	var replacement models.GenreReplacement
	err := r.GetDB().WithContext(ctx).First(&replacement, "original = ?", original).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("genre replacement %s", original), err)
	}
	return &replacement, nil
}

func (r *GenreReplacementRepository) List(ctx context.Context) ([]models.GenreReplacement, error) {
	return r.ListAll(ctx)
}

func (r *GenreReplacementRepository) FindByID(ctx context.Context, id uint) (*models.GenreReplacement, error) {
	return r.BaseRepository.FindByID(ctx, id)
}

func (r *GenreReplacementRepository) DeleteByID(ctx context.Context, id uint) error {
	return r.BaseRepository.Delete(ctx, id)
}

func (r *GenreReplacementRepository) Delete(ctx context.Context, original string) error {
	if err := r.GetDB().WithContext(ctx).Delete(&models.GenreReplacement{}, "original = ?", original).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("genre replacement %s", original), err)
	}
	return nil
}

func (r *GenreReplacementRepository) GetReplacementMap(ctx context.Context) (map[string]string, error) {
	replacements, err := r.List(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, r := range replacements {
		result[r.Original] = r.Replacement
	}
	return result, nil
}
