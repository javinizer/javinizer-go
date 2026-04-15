package database

import (
	"fmt"

	"github.com/javinizer/javinizer-go/internal/models"
)

// ActressAliasRepository provides database operations for actress aliases
type ActressAliasRepository struct {
	db *DB
}

// NewActressAliasRepository creates a new actress alias repository
func NewActressAliasRepository(db *DB) *ActressAliasRepository {
	return &ActressAliasRepository{db: db}
}

// Create adds a new actress alias
func (r *ActressAliasRepository) Create(alias *models.ActressAlias) error {
	if err := r.db.Create(alias).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("actress alias %s", alias.AliasName), err)
	}
	return nil
}

// Upsert creates or updates an actress alias
func (r *ActressAliasRepository) Upsert(alias *models.ActressAlias) error {
	existing, err := r.FindByAliasName(alias.AliasName)
	if err != nil {
		if !isRecordNotFound(err) {
			return err
		}
		// Doesn't exist, create it
		return r.Create(alias)
	}

	// Exists, update it
	alias.ID = existing.ID
	alias.CreatedAt = existing.CreatedAt
	if err := r.db.Save(alias).Error; err != nil {
		return wrapDBErr("update", fmt.Sprintf("actress alias %s", alias.AliasName), err)
	}
	return nil
}

// FindByAliasName finds a canonical name by alias
func (r *ActressAliasRepository) FindByAliasName(aliasName string) (*models.ActressAlias, error) {
	var alias models.ActressAlias
	err := r.db.First(&alias, "alias_name = ?", aliasName).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress alias %s", aliasName), err)
	}
	return &alias, nil
}

// FindByCanonicalName finds all aliases for a canonical name
func (r *ActressAliasRepository) FindByCanonicalName(canonicalName string) ([]models.ActressAlias, error) {
	var aliases []models.ActressAlias
	err := r.db.Where("canonical_name = ?", canonicalName).Find(&aliases).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("actress aliases for %s", canonicalName), err)
	}
	return aliases, nil
}

// List returns all actress aliases
func (r *ActressAliasRepository) List() ([]models.ActressAlias, error) {
	var aliases []models.ActressAlias
	err := r.db.Find(&aliases).Error
	if err != nil {
		return nil, wrapDBErr("find", "actress aliases", err)
	}
	return aliases, nil
}

// Delete removes an actress alias
func (r *ActressAliasRepository) Delete(aliasName string) error {
	if err := r.db.Delete(&models.ActressAlias{}, "alias_name = ?", aliasName).Error; err != nil {
		return wrapDBErr("delete", fmt.Sprintf("actress alias %s", aliasName), err)
	}
	return nil
}

// GetAliasMap returns all aliases as a map[aliasName]canonicalName
func (r *ActressAliasRepository) GetAliasMap() (map[string]string, error) {
	aliases, err := r.List()
	if err != nil {
		return nil, err
	}

	result := make(map[string]string)
	for _, a := range aliases {
		result[a.AliasName] = a.CanonicalName
	}
	return result, nil
}
