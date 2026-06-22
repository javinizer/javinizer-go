package token

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type mockApiTokenRepo struct {
	tokens    map[string]*models.ApiToken
	hashIdx   map[string]string
	prefixIdx map[string]string
	nextID    int
	createErr error
}

func newMockRepo() *mockApiTokenRepo {
	return &mockApiTokenRepo{
		tokens:    make(map[string]*models.ApiToken),
		hashIdx:   make(map[string]string),
		prefixIdx: make(map[string]string),
	}
}

func (m *mockApiTokenRepo) Create(ctx context.Context, token *models.ApiToken) error {
	if m.createErr != nil {
		return m.createErr
	}
	if _, exists := m.tokens[token.ID]; exists {
		return errors.New("duplicate")
	}
	cp := *token
	m.tokens[token.ID] = &cp
	m.hashIdx[token.TokenHash] = token.ID
	m.prefixIdx[token.TokenPrefix] = token.ID
	return nil
}

func (m *mockApiTokenRepo) FindByID(ctx context.Context, id string) (*models.ApiToken, error) {
	t, ok := m.tokens[id]
	if !ok {
		return nil, database.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *mockApiTokenRepo) FindByTokenHash(ctx context.Context, hash string) (*models.ApiToken, error) {
	id, ok := m.hashIdx[hash]
	if !ok {
		return nil, database.ErrNotFound
	}
	t := m.tokens[id]
	if t.RevokedAt != nil {
		return nil, database.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *mockApiTokenRepo) FindByPrefix(ctx context.Context, prefix string) (*models.ApiToken, error) {
	id, ok := m.prefixIdx[prefix]
	if !ok {
		return nil, database.ErrNotFound
	}
	t := m.tokens[id]
	if t.RevokedAt != nil {
		return nil, database.ErrNotFound
	}
	cp := *t
	return &cp, nil
}

func (m *mockApiTokenRepo) ListActive(ctx context.Context) ([]models.ApiToken, error) {
	var result []models.ApiToken
	for _, t := range m.tokens {
		if t.RevokedAt == nil {
			result = append(result, *t)
		}
	}
	return result, nil
}

func (m *mockApiTokenRepo) Revoke(ctx context.Context, id string) error {
	t, ok := m.tokens[id]
	if !ok {
		return database.ErrNotFound
	}
	now := time.Now().UTC()
	t.RevokedAt = &now
	return nil
}

func (m *mockApiTokenRepo) UpdateLastUsed(ctx context.Context, id string) error {
	t, ok := m.tokens[id]
	if !ok {
		return database.ErrNotFound
	}
	now := time.Now().UTC()
	t.LastUsedAt = &now
	return nil
}

func (m *mockApiTokenRepo) Regenerate(ctx context.Context, id string, newHash string, newPrefix string) (*models.ApiToken, error) {
	t, ok := m.tokens[id]
	if !ok {
		return nil, database.ErrNotFound
	}
	if t.RevokedAt != nil {
		return nil, database.ErrNotFound
	}
	delete(m.hashIdx, t.TokenHash)
	delete(m.prefixIdx, t.TokenPrefix)
	t.TokenHash = newHash
	t.TokenPrefix = newPrefix
	m.hashIdx[newHash] = id
	m.prefixIdx[newPrefix] = id
	cp := *t
	return &cp, nil
}

func TestTokenService_Create(t *testing.T) {
	repo := newMockRepo()
	svc := NewTokenService(repo)

	t.Run("returns full token and metadata", func(t *testing.T) {
		apiToken, fullToken, err := svc.Create(context.Background(), "my-token")
		require.NoError(t, err)
		assert.NotEmpty(t, apiToken.ID)
		assert.Equal(t, "my-token", apiToken.Name)
		assert.True(t, strings.HasPrefix(fullToken, TokenPrefix))
		assert.Len(t, fullToken, len(TokenPrefix)+32)
	})

	t.Run("hash stored correctly", func(t *testing.T) {
		apiToken, fullToken, err := svc.Create(context.Background(), "hash-check")
		require.NoError(t, err)
		expectedHash := HashToken(fullToken)
		assert.Equal(t, expectedHash, apiToken.TokenHash)
		found, err := repo.FindByTokenHash(context.Background(), expectedHash)
		require.NoError(t, err)
		assert.Equal(t, apiToken.ID, found.ID)
	})

	t.Run("create error from repo", func(t *testing.T) {
		errRepo := newMockRepo()
		errRepo.createErr = errors.New("db error")
		errSvc := NewTokenService(errRepo)
		_, _, err := errSvc.Create(context.Background(), "fail-token")
		assert.Error(t, err)
	})

	t.Run("regenerate error from repo", func(t *testing.T) {
		repo := newMockRepo()
		svc := NewTokenService(repo)
		_, _, err := svc.Regenerate(context.Background(), "nonexistent-id")
		assert.Error(t, err)
	})
}

func TestTokenService_Revoke(t *testing.T) {
	repo := newMockRepo()
	svc := NewTokenService(repo)

	apiToken, _, err := svc.Create(context.Background(), "to-revoke")
	require.NoError(t, err)

	t.Run("marks revoked", func(t *testing.T) {
		err := svc.Revoke(context.Background(), apiToken.ID)
		require.NoError(t, err)
		found, err := repo.FindByID(context.Background(), apiToken.ID)
		require.NoError(t, err)
		assert.NotNil(t, found.RevokedAt)
	})
}

func TestTokenService_List(t *testing.T) {
	repo := newMockRepo()
	svc := NewTokenService(repo)

	_, _, err := svc.Create(context.Background(), "token-a")
	require.NoError(t, err)
	_, _, err = svc.Create(context.Background(), "token-b")
	require.NoError(t, err)
	tokenC, _, err := svc.Create(context.Background(), "token-c")
	require.NoError(t, err)
	require.NoError(t, svc.Revoke(context.Background(), tokenC.ID))

	t.Run("returns active tokens excluding revoked", func(t *testing.T) {
		tokens, err := svc.List(context.Background())
		require.NoError(t, err)
		assert.Len(t, tokens, 2)
		names := make(map[string]bool)
		for _, t := range tokens {
			names[t.Name] = true
		}
		assert.True(t, names["token-a"])
		assert.True(t, names["token-b"])
		assert.False(t, names["token-c"])
	})
}

func TestTokenService_Regenerate(t *testing.T) {
	repo := newMockRepo()
	svc := NewTokenService(repo)

	t.Run("new token value and old token invalid", func(t *testing.T) {
		apiToken, oldFullToken, err := svc.Create(context.Background(), "regen-test")
		require.NoError(t, err)

		regenToken, newFullToken, err := svc.Regenerate(context.Background(), apiToken.ID)
		require.NoError(t, err)
		assert.Equal(t, apiToken.ID, regenToken.ID)
		assert.NotEqual(t, oldFullToken, newFullToken)
		assert.True(t, strings.HasPrefix(newFullToken, TokenPrefix))

		// Old token hash should no longer match
		_, err = repo.FindByTokenHash(context.Background(), HashToken(oldFullToken))
		assert.Error(t, err, "old token should be invalid after regenerate")

		// New token hash should match
		found, err := repo.FindByTokenHash(context.Background(), HashToken(newFullToken))
		require.NoError(t, err)
		assert.Equal(t, apiToken.ID, found.ID)
	})
}

func TestTokenService_Create_EmptyName(t *testing.T) {
	repo := newMockRepo()
	svc := NewTokenService(repo)

	apiToken, fullToken, err := svc.Create(context.Background(), "")
	require.NoError(t, err)
	assert.Equal(t, "", apiToken.Name)
	assert.True(t, strings.HasPrefix(fullToken, TokenPrefix))
}

func TestTokenService_Regenerate_RevokedToken(t *testing.T) {
	repo := newMockRepo()
	svc := NewTokenService(repo)

	apiToken, _, err := svc.Create(context.Background(), "regen-revoked")
	require.NoError(t, err)
	require.NoError(t, svc.Revoke(context.Background(), apiToken.ID))

	_, _, err = svc.Regenerate(context.Background(), apiToken.ID)
	assert.Error(t, err)
}
