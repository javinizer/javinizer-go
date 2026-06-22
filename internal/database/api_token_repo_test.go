package database

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func setupApiTokenTestDB(t *testing.T) *DB {
	t.Helper()
	return setupBaseRepoTestDB(t)
}

func newTestApiToken(db *DB) *models.ApiToken {
	return &models.ApiToken{
		ID:          "test-id-001",
		Name:        "test-token",
		TokenHash:   "abc123hash456def789ghi012jkl345mno678pqr901stu234vwx567yz890ab",
		TokenPrefix: "abcd1234",
	}
}

func TestApiTokenRepo_Create(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	testCases := []struct {
		name    string
		token   *models.ApiToken
		wantErr bool
	}{
		{
			name:    "success",
			token:   newTestApiToken(db),
			wantErr: false,
		},
		{
			name: "duplicate id fails",
			token: &models.ApiToken{
				ID:          "test-id-001",
				Name:        "duplicate",
				TokenHash:   "different-hash-value-different-hash-value-different-hash-va",
				TokenPrefix: "zzzz9999",
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := repo.Create(context.TODO(), tc.token)
			if tc.wantErr {
				assert.Error(t, err)
				return
			}
			require.NoError(t, err)

			found, err := repo.FindByID(context.TODO(), tc.token.ID)
			require.NoError(t, err)
			assert.Equal(t, tc.token.ID, found.ID)
			assert.Equal(t, tc.token.Name, found.Name)
			assert.Equal(t, tc.token.TokenHash, found.TokenHash)
			assert.Equal(t, tc.token.TokenPrefix, found.TokenPrefix)
			assert.Nil(t, found.RevokedAt)
		})
	}
}

func TestApiTokenRepo_FindByTokenHash(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	token := newTestApiToken(db)
	require.NoError(t, repo.Create(context.TODO(), token))

	t.Run("found by hash", func(t *testing.T) {
		found, err := repo.FindByTokenHash(context.TODO(), token.TokenHash)
		require.NoError(t, err)
		assert.Equal(t, token.ID, found.ID)
		assert.Equal(t, token.TokenHash, found.TokenHash)
	})

	t.Run("not found returns error", func(t *testing.T) {
		_, err := repo.FindByTokenHash(context.TODO(), "nonexistent-hash-nonexistent-hash-nonexistent-hash-nonexi")
		assert.Error(t, err)
	})

	t.Run("excludes revoked tokens", func(t *testing.T) {
		require.NoError(t, repo.Revoke(context.TODO(), token.ID))
		_, err := repo.FindByTokenHash(context.TODO(), token.TokenHash)
		assert.Error(t, err)
	})
}

func TestApiTokenRepo_FindByPrefix(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	token := newTestApiToken(db)
	require.NoError(t, repo.Create(context.TODO(), token))

	t.Run("found by prefix", func(t *testing.T) {
		found, err := repo.FindByPrefix(context.TODO(), token.TokenPrefix)
		require.NoError(t, err)
		assert.Equal(t, token.ID, found.ID)
	})

	t.Run("not found returns error", func(t *testing.T) {
		_, err := repo.FindByPrefix(context.TODO(), "notexist")
		assert.Error(t, err)
	})

	t.Run("excludes revoked tokens", func(t *testing.T) {
		require.NoError(t, repo.Revoke(context.TODO(), token.ID))
		_, err := repo.FindByPrefix(context.TODO(), token.TokenPrefix)
		assert.Error(t, err)
	})
}

func TestApiTokenRepo_UpdateLastUsed(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	token := newTestApiToken(db)
	require.NoError(t, repo.Create(context.TODO(), token))

	before, err := repo.FindByID(context.TODO(), token.ID)
	require.NoError(t, err)
	assert.Nil(t, before.LastUsedAt)

	err = repo.UpdateLastUsed(context.TODO(), token.ID)
	require.NoError(t, err)

	after, err := repo.FindByID(context.TODO(), token.ID)
	require.NoError(t, err)
	assert.NotNil(t, after.LastUsedAt)
	assert.WithinDuration(t, time.Now().UTC(), *after.LastUsedAt, 5*time.Second)
}

func TestApiTokenRepo_SoftDelete(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	token := newTestApiToken(db)
	require.NoError(t, repo.Create(context.TODO(), token))

	err := repo.Revoke(context.TODO(), token.ID)
	require.NoError(t, err)

	found, err := repo.FindByID(context.TODO(), token.ID)
	require.NoError(t, err)
	assert.NotNil(t, found.RevokedAt)
	assert.WithinDuration(t, time.Now().UTC(), *found.RevokedAt, 5*time.Second)

	_, err = repo.FindByTokenHash(context.TODO(), token.TokenHash)
	assert.True(t, errors.Is(err, ErrNotFound) || IsNotFound(err),
		"FindByTokenHash should exclude revoked tokens")
}

func TestApiTokenRepo_Revoke_NotFound(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	err := repo.Revoke(context.TODO(), "nonexistent-id")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrNotFound), "expected ErrNotFound, got %v", err)
}

func TestApiTokenRepo_ListActive(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	token1 := &models.ApiToken{
		ID:          "active-001",
		Name:        "active-token-1",
		TokenHash:   "hash1-hash1-hash1-hash1-hash1-hash1-hash1-hash1-hash1-hash1-ha",
		TokenPrefix: "pref0001",
	}
	token2 := &models.ApiToken{
		ID:          "active-002",
		Name:        "active-token-2",
		TokenHash:   "hash2-hash2-hash2-hash2-hash2-hash2-hash2-hash2-hash2-hash2-ha",
		TokenPrefix: "pref0002",
	}
	token3 := &models.ApiToken{
		ID:          "revoked-001",
		Name:        "revoked-token",
		TokenHash:   "hash3-hash3-hash3-hash3-hash3-hash3-hash3-hash3-hash3-hash3-ha",
		TokenPrefix: "pref0003",
	}

	require.NoError(t, repo.Create(context.TODO(), token1))
	require.NoError(t, repo.Create(context.TODO(), token2))
	require.NoError(t, repo.Create(context.TODO(), token3))
	require.NoError(t, repo.Revoke(context.TODO(), token3.ID))

	tokens, err := repo.ListActive(context.TODO())
	require.NoError(t, err)
	assert.Len(t, tokens, 2)

	ids := make(map[string]bool)
	for _, t := range tokens {
		ids[t.ID] = true
	}
	assert.True(t, ids["active-001"])
	assert.True(t, ids["active-002"])
	assert.False(t, ids["revoked-001"])
}

func TestApiTokenRepo_Regenerate(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	token := newTestApiToken(db)
	require.NoError(t, repo.Create(context.TODO(), token))

	newHash := "newhash-newhash-newhash-newhash-newhash-newhash-newhash-newhash-n"
	newPrefix := "newp0001"

	regenerated, err := repo.Regenerate(context.TODO(), token.ID, newHash, newPrefix)
	require.NoError(t, err)
	assert.Equal(t, newHash, regenerated.TokenHash)
	assert.Equal(t, newPrefix, regenerated.TokenPrefix)
	assert.Equal(t, token.ID, regenerated.ID)
	assert.Equal(t, token.Name, regenerated.Name)
}

func TestApiTokenRepo_Regenerate_RevokedFails(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	token := newTestApiToken(db)
	require.NoError(t, repo.Create(context.TODO(), token))
	require.NoError(t, repo.Revoke(context.TODO(), token.ID))

	_, err := repo.Regenerate(context.TODO(), token.ID, "newhash", "newpref1")
	assert.Error(t, err)
}

func TestApiTokenRepo_Regenerate_NotFound(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	_, err := repo.Regenerate(context.TODO(), "nonexistent-id", "newhash", "newpref1")
	assert.Error(t, err)
}

func TestApiTokenRepo_FindByID_NotFound(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	_, err := repo.FindByID(context.TODO(), "nonexistent-id")
	assert.Error(t, err)
}

func TestApiTokenRepo_ListActive_EmptyDB(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	tokens, err := repo.ListActive(context.TODO())
	require.NoError(t, err)
	assert.NotNil(t, tokens)
	assert.Empty(t, tokens)
}

func TestApiTokenRepo_UpdateLastUsed_NotFound(t *testing.T) {
	db := setupApiTokenTestDB(t)
	repo := NewApiTokenRepository(db)

	err := repo.UpdateLastUsed(context.TODO(), "nonexistent-id")
	assert.NoError(t, err)
}
