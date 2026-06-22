package token

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

type TokenService struct {
	repo database.ApiTokenRepositoryInterface
}

func NewTokenService(repo database.ApiTokenRepositoryInterface) *TokenService {
	return &TokenService{repo: repo}
}

func (s *TokenService) Create(ctx context.Context, name string) (*models.ApiToken, string, error) {
	fullToken, prefix, err := GenerateToken()
	if err != nil {
		return nil, "", err
	}

	hash := HashToken(fullToken)

	id, err := generateUUID()
	if err != nil {
		return nil, "", err
	}

	token := &models.ApiToken{
		ID:          id,
		Name:        name,
		TokenHash:   hash,
		TokenPrefix: prefix,
	}

	if err := s.repo.Create(ctx, token); err != nil {
		return nil, "", err
	}

	return token, fullToken, nil
}

func (s *TokenService) Revoke(ctx context.Context, id string) error {
	return s.repo.Revoke(ctx, id)
}

func (s *TokenService) List(ctx context.Context) ([]models.ApiToken, error) {
	return s.repo.ListActive(ctx)
}

func (s *TokenService) Regenerate(ctx context.Context, id string) (*models.ApiToken, string, error) {
	fullToken, prefix, err := GenerateToken()
	if err != nil {
		return nil, "", err
	}

	newHash := HashToken(fullToken)

	token, err := s.repo.Regenerate(ctx, id, newHash, prefix)
	if err != nil {
		return nil, "", err
	}

	return token, fullToken, nil
}

func generateUUID() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", err
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return hex.EncodeToString(bytes[0:4]) + "-" + hex.EncodeToString(bytes[4:6]) + "-" + hex.EncodeToString(bytes[6:8]) + "-" + hex.EncodeToString(bytes[8:10]) + "-" + hex.EncodeToString(bytes[10:]), nil
}
