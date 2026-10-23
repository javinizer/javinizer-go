//go:build !windows

package auth

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

var errStorageFault = errors.New("injected storage failure")

type storageFaultFS struct {
	afero.Fs
	fault     string
	finalPath string
}

func (f *storageFaultFS) Open(name string) (afero.File, error) {
	if f.fault == "read" && name == f.finalPath {
		return nil, errStorageFault
	}
	return f.Fs.Open(name)
}
func (f *storageFaultFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if strings.Contains(name, ".tmp-") {
		return &storageFaultFile{File: file, fault: f.fault}, nil
	}
	return file, nil
}
func (f *storageFaultFS) Stat(name string) (os.FileInfo, error) {
	if (f.fault == "temp permissions" && strings.Contains(name, ".tmp-")) || (f.fault == "final permissions" && name == f.finalPath) {
		return nil, errStorageFault
	}
	return f.Fs.Stat(name)
}

type storageFaultFile struct {
	afero.File
	fault string
}

func (f *storageFaultFile) Write(b []byte) (int, error) {
	if f.fault == "write" {
		return 0, errStorageFault
	}
	return f.File.Write(b)
}
func (f *storageFaultFile) Close() error {
	err := f.File.Close()
	if f.fault == "close" {
		return errStorageFault
	}
	return err
}

func TestAuthStorageWriteFailuresCleanTemporaryFiles(t *testing.T) {
	for _, kind := range []string{"credential", "session"} {
		for _, fault := range []string{"write", "close", "temp permissions", "final permissions"} {
			t.Run(kind+"/"+fault, func(t *testing.T) {
				base := afero.NewMemMapFs()
				now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				path := filepath.Join("/auth", credentialFilename)
				if kind == "session" {
					path = filepath.Join("/auth", sessionFilename)
				}
				fs := &storageFaultFS{Fs: base, fault: fault, finalPath: path}
				m := &AuthManager{fs: fs, credentialPath: path, sessionPath: path, nowFn: func() time.Time { return now }, sessions: map[string]sessionRecord{"session-one": {Username: "admin", Persistent: true, ExpiresAt: now.Add(time.Hour)}}}
				var err error
				if kind == "credential" {
					err = m.writeCredentialsToDisk(&storedCredentials{Username: "admin", Salt: []byte("salt"), Hash: []byte("hash"), Params: argon2Params{Memory: 8, Time: 1, Threads: 1, KeyLen: 4}})
				} else {
					err = m.writePersistentSessionsLocked()
				}
				require.ErrorIs(t, err, errStorageFault)
				expected := "failed to " + fault + " temp auth " + kind + " file"
				switch fault {
				case "write":
					expected = "failed to write temp auth " + kind + " file"
				case "close":
					expected = "failed to close temp auth " + kind + " file"
				case "temp permissions":
					expected = "failed to enforce temp auth " + kind + " permissions"
				case "final permissions":
					expected = "failed to enforce auth " + kind + " permissions"
				}
				require.ErrorContains(t, err, expected)
				entries, err := afero.ReadDir(base, "/auth")
				require.NoError(t, err)
				for _, entry := range entries {
					require.NotContains(t, entry.Name(), ".tmp-", "temporary files must be removed after every failure")
				}
				exists, err := afero.Exists(base, path)
				require.NoError(t, err)
				require.Equal(t, fault == "final permissions", exists, "only the post-rename permission failure can leave the destination")
			})
		}
	}
}
func TestAuthStorageReadFailuresPreserveState(t *testing.T) {
	for _, kind := range []string{"credential", "session"} {
		t.Run(kind, func(t *testing.T) {
			base := afero.NewMemMapFs()
			path := "/auth.json"
			require.NoError(t, afero.WriteFile(base, path, []byte(`{}`), 0600))
			creds := &storedCredentials{Username: "existing"}
			m := &AuthManager{fs: &storageFaultFS{Fs: base, fault: "read", finalPath: path}, credentialPath: path, sessionPath: path, credentials: creds, sessions: map[string]sessionRecord{"existing": {Username: "existing"}}}
			if kind == "credential" {
				require.ErrorIs(t, m.loadCredentialsFromDisk(), errStorageFault)
			} else {
				m.loadSessionsFromDisk()
			}
			require.Same(t, creds, m.credentials)
			require.Contains(t, m.sessions, "existing")
			exists, err := afero.Exists(base, path)
			require.NoError(t, err)
			require.True(t, exists, "transient read errors must not delete persisted state")
		})
	}
}

func TestAuthStoragePreRenameFailuresPreserveExistingFile(t *testing.T) {
	for _, kind := range []string{"credential", "session"} {
		for _, fault := range []string{"write", "close", "temp permissions"} {
			t.Run(kind+"/"+fault, func(t *testing.T) {
				base := afero.NewMemMapFs()
				path := filepath.Join("/auth", credentialFilename)
				if kind == "session" {
					path = filepath.Join("/auth", sessionFilename)
				}
				original := []byte(`{"existing":"persisted state"}`)
				require.NoError(t, base.MkdirAll(filepath.Dir(path), 0700))
				require.NoError(t, afero.WriteFile(base, path, original, 0600))
				now := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
				credentials := &storedCredentials{Username: "existing-user", Salt: []byte("old salt"), Hash: []byte("old hash")}
				sessions := map[string]sessionRecord{"existing-session": {Username: "existing-user", ExpiresAt: now.Add(time.Hour), Persistent: true}}
				m := &AuthManager{fs: &storageFaultFS{Fs: base, fault: fault, finalPath: path}, credentialPath: path, sessionPath: path, nowFn: func() time.Time { return now }, credentials: credentials, sessions: sessions}
				var err error
				if kind == "credential" {
					err = m.writeCredentialsToDisk(&storedCredentials{Username: "replacement-user", Salt: []byte("new salt"), Hash: []byte("new hash"), Params: argon2Params{Memory: 8, Time: 1, Threads: 1, KeyLen: 8}})
				} else {
					err = m.writePersistentSessionsLocked()
				}
				require.ErrorIs(t, err, errStorageFault)
				got, err := afero.ReadFile(base, path)
				require.NoError(t, err)
				require.Equal(t, original, got, "failed replacement must preserve the existing file byte-for-byte")
				require.Same(t, credentials, m.credentials)
				require.Equal(t, &storedCredentials{Username: "existing-user", Salt: []byte("old salt"), Hash: []byte("old hash")}, m.credentials)
				require.Equal(t, map[string]sessionRecord{"existing-session": {Username: "existing-user", ExpiresAt: now.Add(time.Hour), Persistent: true}}, m.sessions)
				entries, err := afero.ReadDir(base, filepath.Dir(path))
				require.NoError(t, err)
				require.Len(t, entries, 1)
				require.Equal(t, filepath.Base(path), entries[0].Name(), "failed replacement must remove its temporary file")
			})
		}
	}
}
