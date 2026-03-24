package auth

import (
	"crypto/subtle"
	"encoding/base64"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/crypto/argon2"
)

// Setup initializes single-user credentials.
func (m *AuthManager) Setup(username, password string) error {
	normalizedUsername := strings.TrimSpace(username)
	if normalizedUsername == "" {
		return fmt.Errorf("%w: username is required", ErrInvalidUsername)
	}
	if len(password) < minPasswordLength {
		return fmt.Errorf("%w: password must be at least %d characters", ErrWeakPassword, minPasswordLength)
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.credentials != nil {
		return ErrAuthAlreadySet
	}

	params := argon2Params{
		Memory:  defaultArgon2Memory,
		Time:    defaultArgon2Time,
		Threads: defaultArgon2Threads,
		KeyLen:  defaultArgon2KeyLen,
	}

	salt, err := m.randomBytes(saltLength)
	if err != nil {
		return fmt.Errorf("failed to generate password salt: %w", err)
	}

	hash := argon2.IDKey([]byte(password), salt, params.Time, params.Memory, params.Threads, params.KeyLen)
	credentials := &storedCredentials{
		Username: normalizedUsername,
		Salt:     salt,
		Hash:     hash,
		Params:   params,
	}

	if err := m.writeCredentialsToDisk(credentials); err != nil {
		return err
	}

	m.credentials = credentials
	m.sessions = make(map[string]sessionRecord)
	m.resetFailedLoginStateLocked()

	return nil
}

// Login validates credentials and returns a new session ID.
func (m *AuthManager) Login(username, password string) (string, error) {
	normalizedUsername := strings.TrimSpace(username)

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.credentials == nil {
		return "", ErrAuthNotInitialized
	}

	now := m.nowFn()
	m.pruneExpiredSessionsLocked(now)
	if now.Before(m.loginBlockedUntil) {
		return "", ErrLoginRateLimited
	}

	if !m.verifyCredentialsLocked(normalizedUsername, password) {
		m.recordFailedLoginLocked(now)
		return "", ErrInvalidCredentials
	}
	m.resetFailedLoginStateLocked()

	sessionID, err := m.newSessionID()
	if err != nil {
		return "", fmt.Errorf("failed to create session: %w", err)
	}

	m.enforceSessionLimitLocked()

	m.sessions[sessionID] = sessionRecord{
		Username:  m.credentials.Username,
		ExpiresAt: now.Add(m.sessionTTL),
	}

	return sessionID, nil
}

// AuthenticateSession validates a session and returns its username.
func (m *AuthManager) AuthenticateSession(sessionID string) (string, error) {
	if strings.TrimSpace(sessionID) == "" {
		return "", ErrInvalidSession
	}

	m.mu.Lock()
	defer m.mu.Unlock()

	if m.credentials == nil {
		return "", ErrAuthNotInitialized
	}

	session, ok := m.sessions[sessionID]
	if !ok {
		return "", ErrInvalidSession
	}

	now := m.nowFn()
	if now.After(session.ExpiresAt) {
		delete(m.sessions, sessionID)
		return "", ErrInvalidSession
	}

	if session.Username != m.credentials.Username {
		delete(m.sessions, sessionID)
		return "", ErrInvalidSession
	}

	return session.Username, nil
}

// Logout invalidates a session.
func (m *AuthManager) Logout(sessionID string) {
	if strings.TrimSpace(sessionID) == "" {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.sessions, sessionID)
}

func (m *AuthManager) recordFailedLoginLocked(now time.Time) {
	if m.failedLoginWindowStart.IsZero() || now.Sub(m.failedLoginWindowStart) > failedLoginWindow {
		m.failedLoginWindowStart = now
		m.failedLoginCount = 0
	}

	m.failedLoginCount++
	if m.failedLoginCount >= maxFailedLoginAttempts {
		m.loginBlockedUntil = now.Add(loginLockoutDuration)
		m.failedLoginCount = 0
		m.failedLoginWindowStart = time.Time{}
	}
}

func (m *AuthManager) resetFailedLoginStateLocked() {
	m.failedLoginCount = 0
	m.failedLoginWindowStart = time.Time{}
	m.loginBlockedUntil = time.Time{}
}

func (m *AuthManager) verifyCredentialsLocked(username, password string) bool {
	if m.credentials == nil {
		return false
	}

	passwordHash := argon2.IDKey(
		[]byte(password),
		m.credentials.Salt,
		m.credentials.Params.Time,
		m.credentials.Params.Memory,
		m.credentials.Params.Threads,
		m.credentials.Params.KeyLen,
	)

	usernameMatch := subtle.ConstantTimeCompare([]byte(username), []byte(m.credentials.Username)) == 1
	passwordMatch := subtle.ConstantTimeCompare(passwordHash, m.credentials.Hash) == 1

	return usernameMatch && passwordMatch
}

func (m *AuthManager) pruneExpiredSessionsLocked(now time.Time) {
	for sessionID, session := range m.sessions {
		if now.After(session.ExpiresAt) {
			delete(m.sessions, sessionID)
		}
	}
}

func (m *AuthManager) enforceSessionLimitLocked() {
	for len(m.sessions) >= maxActiveSessions {
		var (
			oldestSessionID string
			oldestExpiry    time.Time
		)

		for sessionID, session := range m.sessions {
			if oldestSessionID == "" || session.ExpiresAt.Before(oldestExpiry) {
				oldestSessionID = sessionID
				oldestExpiry = session.ExpiresAt
			}
		}

		if oldestSessionID == "" {
			return
		}
		delete(m.sessions, oldestSessionID)
	}
}

func (m *AuthManager) newSessionID() (string, error) {
	raw, err := m.randomBytes(sessionIDBytes)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(raw), nil
}

func (m *AuthManager) randomBytes(size int) ([]byte, error) {
	buf := make([]byte, size)
	if _, err := io.ReadFull(m.randReader, buf); err != nil {
		return nil, err
	}
	return buf, nil
}
