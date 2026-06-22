package database

import (
	"context"
	"fmt"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
)

type ApiTokenRepository struct {
	db *DB
}

func NewApiTokenRepository(db *DB) *ApiTokenRepository {
	return &ApiTokenRepository{db: db}
}

var _ ApiTokenRepositoryInterface = (*ApiTokenRepository)(nil)

func (r *ApiTokenRepository) Create(ctx context.Context, token *models.ApiToken) error {
	if err := r.db.WithContext(ctx).Create(token).Error; err != nil {
		return wrapDBErr("create", fmt.Sprintf("api token %s", token.ID), err)
	}
	return nil
}

func (r *ApiTokenRepository) FindByID(ctx context.Context, id string) (*models.ApiToken, error) {
	var token models.ApiToken
	err := r.db.WithContext(ctx).First(&token, "id = ?", id).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("api token %s", id), err)
	}
	return &token, nil
}

func (r *ApiTokenRepository) FindByTokenHash(ctx context.Context, hash string) (*models.ApiToken, error) {
	var token models.ApiToken
	err := r.db.WithContext(ctx).Where("token_hash = ? AND revoked_at IS NULL", hash).First(&token).Error
	if err != nil {
		return nil, wrapDBErr("find", "api token by hash", err)
	}
	return &token, nil
}

func (r *ApiTokenRepository) FindByPrefix(ctx context.Context, prefix string) (*models.ApiToken, error) {
	var token models.ApiToken
	err := r.db.WithContext(ctx).Where("token_prefix = ? AND revoked_at IS NULL", prefix).First(&token).Error
	if err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("api token by prefix %s", prefix), err)
	}
	return &token, nil
}

func (r *ApiTokenRepository) ListActive(ctx context.Context) ([]models.ApiToken, error) {
	var tokens []models.ApiToken
	err := r.db.WithContext(ctx).Where("revoked_at IS NULL").Order("created_at DESC").Find(&tokens).Error
	if err != nil {
		return nil, wrapDBErr("list", "active api tokens", err)
	}
	return tokens, nil
}

func (r *ApiTokenRepository) Revoke(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Model(&models.ApiToken{}).Where("id = ?", id).Update("revoked_at", time.Now().UTC())
	if result.Error != nil {
		return wrapDBErr("revoke", fmt.Sprintf("api token %s", id), result.Error)
	}
	if result.RowsAffected == 0 {
		return fmt.Errorf("%w: api token %s", ErrNotFound, id)
	}
	return nil
}

func (r *ApiTokenRepository) UpdateLastUsed(ctx context.Context, id string) error {
	result := r.db.WithContext(ctx).Model(&models.ApiToken{}).Where("id = ?", id).Update("last_used_at", time.Now().UTC())
	if result.Error != nil {
		return wrapDBErr("update_last_used", fmt.Sprintf("api token %s", id), result.Error)
	}
	return nil
}

func (r *ApiTokenRepository) Regenerate(ctx context.Context, id string, newHash string, newPrefix string) (*models.ApiToken, error) {
	var token models.ApiToken
	if err := r.db.WithContext(ctx).First(&token, "id = ?", id).Error; err != nil {
		return nil, wrapDBErr("find", fmt.Sprintf("api token %s", id), err)
	}

	if token.RevokedAt != nil {
		return nil, fmt.Errorf("cannot regenerate revoked api token %s: %w", id, ErrNotFound)
	}

	if err := r.db.WithContext(ctx).Model(&token).Updates(map[string]any{
		"token_hash":   newHash,
		"token_prefix": newPrefix,
	}).Error; err != nil {
		return nil, wrapDBErr("regenerate", fmt.Sprintf("api token %s", id), err)
	}

	token.TokenHash = newHash
	token.TokenPrefix = newPrefix
	return &token, nil
}
