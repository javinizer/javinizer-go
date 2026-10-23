package auth

import (
	"bytes"
	"os"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
)

type sessionFailureFS struct {
	afero.Fs
	denyRemove bool
}

func (f sessionFailureFS) Remove(name string) error {
	if f.denyRemove && name == "/sessions.json" {
		return os.ErrPermission
	}
	return f.Fs.Remove(name)
}

func TestLoginPersistenceFailureRollsBackNewSession(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	salt := []byte("fixed test salt")
	params := argon2Params{Memory: 8, Time: 1, Threads: 1, KeyLen: 16}
	m := &AuthManager{fs: afero.NewReadOnlyFs(afero.NewMemMapFs()), sessionPath: "/sessions.json", nowFn: func() time.Time { return now }, randReader: bytes.NewReader(make([]byte, 64)), sessionTTL: time.Hour, persistentSessionTTL: 24 * time.Hour, sessions: map[string]sessionRecord{}, credentials: &storedCredentials{Username: "admin", Salt: salt, Params: params, Hash: argon2.IDKey([]byte("test password"), salt, params.Time, params.Memory, params.Threads, params.KeyLen)}}
	id, err := m.Login("admin", "test password", true)
	require.Empty(t, id)
	require.Error(t, err)
	require.Empty(t, m.sessions, "failed persistent login must not leave an authenticated in-memory session")
}

func TestAuthenticateMismatchedPersistentSessionRemovesDiskState(t *testing.T) {
	now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/sessions.json", []byte("stale session data"), 0600))
	m := &AuthManager{fs: fs, sessionPath: "/sessions.json", nowFn: func() time.Time { return now }, credentials: &storedCredentials{Username: "new-user"}, sessions: map[string]sessionRecord{"stale": {Username: "old-user", ExpiresAt: now.Add(time.Hour), Persistent: true}}}
	username, err := m.AuthenticateSession("stale")
	require.ErrorIs(t, err, ErrInvalidSession)
	require.Empty(t, username)
	require.Empty(t, m.sessions)
	exists, err := afero.Exists(fs, "/sessions.json")
	require.NoError(t, err)
	require.False(t, exists)
}

func TestSetupStorageFailureDoesNotPublishCredentials(t *testing.T) {
	m := &AuthManager{fs: afero.NewReadOnlyFs(afero.NewMemMapFs()), credentialPath: "/auth.json", nowFn: time.Now, randReader: bytes.NewReader(make([]byte, 64))}
	require.Error(t, m.Setup("admin", "test password"))
	require.Nil(t, m.credentials)
}
func TestSetupSessionCleanupFailureIsReturned(t *testing.T) {
	m := &AuthManager{fs: sessionFailureFS{Fs: afero.NewMemMapFs(), denyRemove: true}, credentialPath: "/auth.json", sessionPath: "/sessions.json", nowFn: time.Now, randReader: bytes.NewReader(make([]byte, 64))}
	require.ErrorContains(t, m.Setup("admin", "test password"), "failed to remove auth session file")
	require.NotNil(t, m.credentials, "credentials were persisted before session cleanup failed")
}
func TestVerifyCredentialsWithoutInitialization(t *testing.T) {
	require.False(t, (&AuthManager{}).verifyCredentialsLocked("admin", "test password"))
}

func TestAuthManagerLongSessionTTLRemainsPersistentMinimum(t *testing.T) {
	ttl := DefaultPersistentSessionTTL + time.Hour
	manager, err := NewAuthManager(t.TempDir()+"/config.yaml", ttl, func(string) (string, bool) { return "", false })
	require.NoError(t, err)
	require.Equal(t, ttl, manager.SessionTTL())
	require.Equal(t, ttl, manager.PersistentSessionTTL(), "remember-me sessions must not expire before ordinary sessions")
}

func TestAuthManagerE2ESetupRejectsInvalidCredentials(t *testing.T) {
	manager, err := NewAuthManager(t.TempDir()+"/config.yaml", time.Hour, func(key string) (string, bool) {
		switch key {
		case "JAVINIZER_E2E_AUTH":
			return "true", true
		case "JAVINIZER_E2E_USERNAME":
			return "   ", true
		case "JAVINIZER_E2E_PASSWORD":
			return "test password", true
		default:
			return "", false
		}
	})
	require.Nil(t, manager)
	require.ErrorIs(t, err, ErrInvalidUsername)
	require.ErrorContains(t, err, "e2e auth auto-setup failed")
}
