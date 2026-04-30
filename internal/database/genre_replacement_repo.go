package database

import (
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

func (r *GenreReplacementRepository) Create(replacement *models.GenreReplacement) error {
	return r.BaseRepository.Create(replacement)
}

func (r *GenreReplacementRepository) Upsert(replacement *models.GenreReplacement) error {
	existing, err := r.FindByOriginal(replacement.Original)
	if err != nil {
		if !isRecordNotFound(err) {
			return err
		}
		return r.Create(replacement)
	}

	replacement.ID = existing.ID
	replacement.CreatedAt = existing.CreatedAt
	if err := r.GetDB().Save(replacement).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("genre replacement %s", replacement.Original), err)
	}
	return nil
}

func (r *GenreReplacementRepository) FindByOriginal(original string) (*models.GenreReplacement, error) {
	var replacement models.GenreReplacement
	err := r.GetDB().First(&replacement, "original = ?", original).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("genre replacement %s", original), err)
	}
	return &replacement, nil
}

func (r *GenreReplacementRepository) List() ([]models.GenreReplacement, error) {
	return r.ListAll()
}

func (r *GenreReplacementRepository) FindByID(id uint) (*models.GenreReplacement, error) {
	return r.BaseRepository.FindByID(id)
}

func (r *GenreReplacementRepository) DeleteByID(id uint) error {
	return r.BaseRepository.Delete(id)
}

func (r *GenreReplacementRepository) Delete(original string) error {
	if err := r.GetDB().Delete(&models.GenreReplacement{}, "original = ?", original).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("genre replacement %s", original), err)
	}
	return nil
}

func (r *GenreReplacementRepository) GetReplacementMap() (map[string]string, error) {
	replacements, err := r.List()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, r := range replacements {
		result[r.Original] = r.Replacement
	}
	return result, nil
}
