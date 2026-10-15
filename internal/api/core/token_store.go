package core

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sync"
	"time"
)

// verificationToken represents a successful proxy test that can be used for save authorization
type verificationToken struct {
	Token      string    `json:"token"`
	Scope      string    `json:"scope"`       // "global", "flaresolverr", or "profile:{name}"
	ConfigHash string    `json:"config_hash"` // Hash of config at test time
	ExpiresAt  time.Time `json:"expires_at"`
	CreatedAt  time.Time `json:"created_at"`
}

const tokenValidityDuration = 5 * time.Minute

// TokenStoreInterface exposes the methods external packages need from the token store.
type TokenStoreInterface interface {
	Create(scope string, configHash string) (string, time.Time, error)
	Validate(token string, scope string, configHash string) bool
	CleanupExpired()
}

// tokenStore manages verification tokens in-memory
type tokenStore struct {
	mu       sync.RWMutex
	tokens   map[string]verificationToken
	stopOnce sync.Once
	stop     chan struct{}
	done     chan struct{}
}

// NewTokenStore creates a new token store with background cleanup.
// Its owner must call CloseTokenStore when done, or transfer ownership to an
// APIRuntime, whose Shutdown stops and joins the cleanup worker.
func NewTokenStore() TokenStoreInterface {
	ts := &tokenStore{
		tokens: make(map[string]verificationToken),
		stop:   make(chan struct{}),
		done:   make(chan struct{}),
	}

	// Start background cleanup every 10 minutes
	go ts.backgroundCleanup()
	return ts
}

// CloseTokenStore stops and joins the worker of a store created by NewTokenStore.
// It is safe to call concurrently or repeatedly. Nil and other implementations
// are left alone, so TokenStoreInterface implementations need no lifecycle method.
// Token creation, validation (including expiration), and manual cleanup remain
// usable after closing; only periodic reclamation stops.
func CloseTokenStore(store TokenStoreInterface) {
	if s, ok := store.(*tokenStore); ok && s != nil {
		s.stopOnce.Do(func() { close(s.stop) })
		<-s.done
	}
}

func (s *tokenStore) backgroundCleanup() {
	defer close(s.done)
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()

	for {
		select {
		case <-s.stop:
			return
		case <-ticker.C:
			s.CleanupExpired()
		}
	}
}

// Create generates a new verification token for the given scope and config hash
func (s *tokenStore) Create(scope string, configHash string) (string, time.Time, error) {
	token, err := generateToken()
	if err != nil {
		return "", time.Time{}, err
	}
	vt := verificationToken{
		Token:      token,
		Scope:      scope,
		ConfigHash: configHash,
		ExpiresAt:  time.Now().Add(tokenValidityDuration),
		CreatedAt:  time.Now(),
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	s.tokens[token] = vt
	return token, vt.ExpiresAt, nil
}

// Validate checks if a token is valid for the given scope and config hash
func (s *tokenStore) Validate(token string, scope string, configHash string) bool {
	s.mu.RLock()
	vt, ok := s.tokens[token]
	if !ok {
		s.mu.RUnlock()
		return false
	}

	// Check expiration - let CleanupExpired handle deletion
	if vt.ExpiresAt.Before(time.Now()) {
		s.mu.RUnlock()
		return false
	}

	// Check scope
	if vt.Scope != scope {
		s.mu.RUnlock()
		return false
	}

	// Check config hash
	if vt.ConfigHash != configHash {
		s.mu.RUnlock()
		return false
	}

	s.mu.RUnlock()
	return true
}

// CleanupExpired removes expired tokens from the store
func (s *tokenStore) CleanupExpired() {
	s.mu.Lock()
	defer s.mu.Unlock()

	now := time.Now()
	for token, vt := range s.tokens {
		if vt.ExpiresAt.Before(now) {
			delete(s.tokens, token)
		}
	}
}

func generateToken() (string, error) {
	bytes := make([]byte, 16)
	if _, err := rand.Read(bytes); err != nil {
		return "", fmt.Errorf("generate token: %w", err)
	}
	return hex.EncodeToString(bytes), nil
}

// HashProxyConfig creates a hash of proxy config for comparison using SHA-256
func HashProxyConfig(proxyConfig any) (string, error) {
	// Use canonical JSON for consistent hashing
	data, err := json.Marshal(proxyConfig)
	if err != nil {
		return "", fmt.Errorf("hash proxy config: %w", err)
	}
	hash := sha256.Sum256(data)
	return hex.EncodeToString(hash[:8]), nil // First 8 bytes (16 hex chars) is sufficient
}
