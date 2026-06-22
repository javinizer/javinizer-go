package auth

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/spf13/afero"
)

const (
	credentialFilename = "auth.credentials.json"
	sessionFilename    = "auth.sessions.json"
	minPasswordLength  = 8
	sessionIDBytes     = 32
	saltLength         = 16
	maxActiveSessions  = 256

	defaultArgon2Memory  uint32 = 64 * 1024 // 64 MB
	defaultArgon2Time    uint32 = 1
	defaultArgon2Threads uint8  = 4
	defaultArgon2KeyLen  uint32 = 32
)

const (
	maxFailedLoginAttempts = 5
	failedLoginWindow      = 5 * time.Minute
	loginLockoutDuration   = 5 * time.Minute
)

// DefaultSessionTTL is the default authenticated session lifetime.
const DefaultSessionTTL = 24 * time.Hour

var (
	ErrAuthNotInitialized = errors.New("authentication is not initialized")
	ErrAuthAlreadySet     = errors.New("authentication is already initialized")
	ErrInvalidCredentials = errors.New("invalid username or password")
	ErrInvalidSession     = errors.New("invalid or expired session")
	ErrInvalidUsername    = errors.New("invalid username")
	ErrWeakPassword       = errors.New("weak password")
	ErrLoginRateLimited   = errors.New("too many login attempts")
)

type argon2Params struct {
	Memory  uint32
	Time    uint32
	Threads uint8
	KeyLen  uint32
}

type storedCredentials struct {
	Username string
	Salt     []byte
	Hash     []byte
	Params   argon2Params
}

type sessionRecord struct {
	Username   string
	ExpiresAt  time.Time
	Persistent bool
}

type credentialFile struct {
	Version   int    `json:"version"`
	Username  string `json:"username"`
	Salt      string `json:"salt"`
	Hash      string `json:"hash"`
	Memory    uint32 `json:"memory"`
	Time      uint32 `json:"time"`
	Threads   uint8  `json:"threads"`
	KeyLen    uint32 `json:"key_len"`
	CreatedAt string `json:"created_at,omitempty"`
}

type sessionFile struct {
	Version  int               `json:"version"`
	Sessions []sessionFileItem `json:"sessions"`
}

type sessionFileItem struct {
	ID        string `json:"id"`
	Username  string `json:"username"`
	ExpiresAt string `json:"expires_at"`
}

// AuthManager manages single-user credentials and in-memory sessions.
type AuthManager struct {
	mu             sync.RWMutex
	credentialPath string
	sessionPath    string
	credentials    *storedCredentials
	sessions       map[string]sessionRecord
	sessionTTL     time.Duration
	nowFn          func() time.Time
	randReader     io.Reader
	apiTokenRepo   database.ApiTokenRepositoryInterface
	fs             afero.Fs
	envLookup      func(key string) (string, bool)

	failedLoginCount       int
	failedLoginWindowStart time.Time
	loginBlockedUntil      time.Time
	disableRateLimit       bool
}

func (m *AuthManager) SetApiTokenRepo(repo database.ApiTokenRepositoryInterface) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.apiTokenRepo = repo
}

func (m *AuthManager) ValidateToken(ctx context.Context, tokenHash string) (string, error) {
	m.mu.RLock()
	repo := m.apiTokenRepo
	m.mu.RUnlock()

	if repo == nil {
		return "", fmt.Errorf("api token repository not configured")
	}

	apiToken, err := repo.FindByTokenHash(ctx, tokenHash)
	if err != nil {
		return "", err
	}

	if apiToken.RevokedAt != nil {
		return "", fmt.Errorf("token revoked")
	}

	return apiToken.ID, nil
}

func (m *AuthManager) UpdateTokenLastUsed(ctx context.Context, tokenID string) error {
	m.mu.RLock()
	repo := m.apiTokenRepo
	m.mu.RUnlock()

	if repo == nil {
		return fmt.Errorf("api token repository not configured")
	}

	return repo.UpdateLastUsed(ctx, tokenID)
}

// GetEnv returns the value of the environment variable identified by key,
// using the injectable envLookup function. Falls back to os.Getenv when
// no custom lookup was provided at construction time.
func (m *AuthManager) GetEnv(key string) string {
	if m.envLookup != nil {
		v, _ := m.envLookup(key)
		return v
	}
	return os.Getenv(key)
}

// SetDisableRateLimit enables or disables rate limiting on login attempts.
// Used for e2e testing where rate limiting would interfere with automated logins.
func (m *AuthManager) SetDisableRateLimit(disabled bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.disableRateLimit = disabled
}

// credentialPathForConfig returns the auth credential file path next to config.
func credentialPathForConfig(configFile string) string {
	return filepath.Join(filepath.Dir(configFile), credentialFilename)
}

func sessionPathForConfig(configFile string) string {
	return filepath.Join(filepath.Dir(configFile), sessionFilename)
}

// NewAuthManager creates an auth manager and loads credentials from disk if present.
// envLookup defaults to os.LookupEnv when nil, enabling test injection.
func NewAuthManager(configFile string, sessionTTL time.Duration, envLookup ...func(key string) (string, bool)) (*AuthManager, error) {
	lookup := os.LookupEnv
	if len(envLookup) > 0 && envLookup[0] != nil {
		lookup = envLookup[0]
	}

	if sessionTTL <= 0 {
		sessionTTL = DefaultSessionTTL
	}

	manager := &AuthManager{
		credentialPath: credentialPathForConfig(configFile),
		sessionPath:    sessionPathForConfig(configFile),
		sessions:       make(map[string]sessionRecord),
		sessionTTL:     sessionTTL,
		nowFn:          time.Now,
		randReader:     rand.Reader,
		fs:             afero.NewOsFs(),
		envLookup:      lookup,
	}

	if err := manager.loadCredentialsFromDisk(); err != nil {
		return nil, err
	}

	manager.loadSessionsFromDisk()

	e2eAuth, e2eEnabled := lookup("JAVINIZER_E2E_AUTH")
	if e2eEnabled && e2eAuth == "true" && manager.credentials == nil {
		username, _ := lookup("JAVINIZER_E2E_USERNAME")
		if username == "" {
			username = "admin"
		}
		password, _ := lookup("JAVINIZER_E2E_PASSWORD")
		if password == "" {
			password = "adminpassword123"
		}
		if err := manager.Setup(username, password); err != nil {
			return nil, fmt.Errorf("e2e auth auto-setup failed: %w", err)
		}
	}

	return manager, nil
}

// SessionTTL returns the configured session lifetime.
func (m *AuthManager) SessionTTL() time.Duration {
	return m.sessionTTL
}

// IsInitialized reports whether credentials exist.
func (m *AuthManager) IsInitialized() bool {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.credentials != nil
}

// Username returns the configured username when initialized.
func (m *AuthManager) Username() (string, bool) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	if m.credentials == nil {
		return "", false
	}
	return m.credentials.Username, true
}
