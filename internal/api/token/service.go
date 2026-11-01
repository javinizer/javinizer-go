package token

import (
	"context"
	"crypto/rand"
	"encoding/hex"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
)

// TokenService issues, revokes, lists, and regenerates API tokens using the
// backing token repository.
type TokenService struct {
	repo database.ApiTokenRepositoryInterface
}

// NewTokenService constructs a TokenService backed by the given repository.
func NewTokenService(repo database.ApiTokenRepositoryInterface) *TokenService {
	return &TokenService{repo: repo}
}

// Create issues a new API token for the given name, persists its hash and
// prefix, and returns the token record plus the plaintext value.
func (s *TokenService) Create(ctx context.Context, name string) (*models.ApiToken, string, error) {
	fullToken, prefix, err := GenerateToken()
	if err != nil {
		return nil, "", err
	}

	hash := HashToken(fullToken)

	id := generateUUID()

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

// Revoke marks the token with the given id as revoked.
func (s *TokenService) Revoke(ctx context.Context, id string) error {
	return s.repo.Revoke(ctx, id)
}

// List returns all active API tokens.
func (s *TokenService) List(ctx context.Context) ([]models.ApiToken, error) {
	return s.repo.ListActive(ctx)
}

// Regenerate issues a new plaintext value for the token with the given id and
// returns the updated record.
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

func generateUUID() string {
	bytes := make([]byte, 16)
	// On Go 1.27+, crypto/rand.Read fills the buffer or irrecoverably terminates.
	rand.Read(bytes)
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return hex.EncodeToString(bytes[0:4]) + "-" + hex.EncodeToString(bytes[4:6]) + "-" + hex.EncodeToString(bytes[6:8]) + "-" + hex.EncodeToString(bytes[8:10]) + "-" + hex.EncodeToString(bytes[10:])
}
