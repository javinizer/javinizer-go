package database

import (
	"context"
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

type ActressAliasRepository struct {
	*BaseRepository[models.ActressAlias, uint]
}

func NewActressAliasRepository(db *DB) *ActressAliasRepository {
	return &ActressAliasRepository{
		BaseRepository: NewBaseRepository[models.ActressAlias, uint](
			db, "actress alias",
			func(a models.ActressAlias) string { return a.AliasName },
			WithNewEntity[models.ActressAlias, uint](func() models.ActressAlias { return models.ActressAlias{} }),
		),
	}
}

func (r *ActressAliasRepository) Create(ctx context.Context, alias *models.ActressAlias) error {
	return r.BaseRepository.Create(ctx, alias)
}

func (r *ActressAliasRepository) Upsert(ctx context.Context, alias *models.ActressAlias) error {
	existing, err := r.FindByAliasName(ctx, alias.AliasName)
	if err != nil {
		if !IsNotFound(err) {
			return err
		}
		return r.Create(ctx, alias)
	}

	alias.ID = existing.ID
	alias.CreatedAt = existing.CreatedAt
	if err := r.GetDB().WithContext(ctx).Save(alias).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("actress alias %s", alias.AliasName), err)
	}
	return nil
}

func (r *ActressAliasRepository) FindByAliasName(ctx context.Context, aliasName string) (*models.ActressAlias, error) {
	var alias models.ActressAlias
	err := r.GetDB().WithContext(ctx).First(&alias, "alias_name = ?", aliasName).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress alias %s", aliasName), err)
	}
	return &alias, nil
}

func (r *ActressAliasRepository) FindByCanonicalName(ctx context.Context, canonicalName string) ([]models.ActressAlias, error) {
	var aliases []models.ActressAlias
	err := r.GetDB().WithContext(ctx).Where("canonical_name = ?", canonicalName).Find(&aliases).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress aliases for %s", canonicalName), err)
	}
	return aliases, nil
}

func (r *ActressAliasRepository) List(ctx context.Context) ([]models.ActressAlias, error) {
	return r.ListAll(ctx)
}

func (r *ActressAliasRepository) Delete(ctx context.Context, aliasName string) error {
	if err := r.GetDB().WithContext(ctx).Delete(&models.ActressAlias{}, "alias_name = ?", aliasName).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("actress alias %s", aliasName), err)
	}
	return nil
}

func (r *ActressAliasRepository) GetAliasMap(ctx context.Context) (map[string]string, error) {
	aliases, err := r.List(ctx)
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, a := range aliases {
		result[a.AliasName] = a.CanonicalName
	}
	return result, nil
}
