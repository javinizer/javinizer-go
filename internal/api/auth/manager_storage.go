package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
)

func (m *AuthManager) loadCredentialsFromDisk() error {
	if _, err := m.fs.Stat(m.credentialPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat auth credential file: %w", err)
	}

	if err := enforceCredentialFilePermissions(m.fs, m.credentialPath); err != nil {
		return fmt.Errorf("failed to enforce auth credential permissions: %w", err)
	}

	data, err := afero.ReadFile(m.fs, m.credentialPath)
	if err != nil {
		return fmt.Errorf("failed to read auth credential file: %w", err)
	}

	var payload credentialFile
	if err := json.Unmarshal(data, &payload); err != nil {
		return fmt.Errorf("failed to parse auth credential file: %w", err)
	}

	username := strings.TrimSpace(payload.Username)
	if username == "" {
		return fmt.Errorf("invalid auth credential file: username is required")
	}
	if payload.Memory == 0 || payload.Time == 0 || payload.Threads == 0 || payload.KeyLen == 0 {
		return fmt.Errorf("invalid auth credential file: argon2 parameters are required")
	}

	salt, err := base64.RawStdEncoding.DecodeString(payload.Salt)
	if err != nil || len(salt) == 0 {
		return fmt.Errorf("invalid auth credential file: invalid salt")
	}
	hash, err := base64.RawStdEncoding.DecodeString(payload.Hash)
	if err != nil || len(hash) == 0 {
		return fmt.Errorf("invalid auth credential file: invalid hash")
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.credentials = &storedCredentials{
		Username: username,
		Salt:     salt,
		Hash:     hash,
		Params: argon2Params{
			Memory:  payload.Memory,
			Time:    payload.Time,
			Threads: payload.Threads,
			KeyLen:  payload.KeyLen,
		},
	}

	return nil
}

func (m *AuthManager) writeCredentialsToDisk(creds *storedCredentials) error {
	if creds == nil {
		return errors.New("credentials are required")
	}

	payload := credentialFile{
		Version:   1,
		Username:  creds.Username,
		Salt:      base64.RawStdEncoding.EncodeToString(creds.Salt),
		Hash:      base64.RawStdEncoding.EncodeToString(creds.Hash),
		Memory:    creds.Params.Memory,
		Time:      creds.Params.Time,
		Threads:   creds.Params.Threads,
		KeyLen:    creds.Params.KeyLen,
		CreatedAt: m.nowFn().UTC().Format(time.RFC3339),
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal auth credential file: %w", err)
	}

	dir := filepath.Dir(m.credentialPath)
	if err := m.fs.MkdirAll(dir, config.DirPermTemp); err != nil {
		return fmt.Errorf("failed to create auth credential directory: %w", err)
	}

	tmpFile, err := afero.TempFile(m.fs, dir, credentialFilename+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp auth credential file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = m.fs.Remove(tmpPath) }()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp auth credential file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp auth credential file: %w", err)
	}

	if err := enforceCredentialFilePermissions(m.fs, tmpPath); err != nil {
		return fmt.Errorf("failed to enforce temp auth credential permissions: %w", err)
	}

	if err := m.fs.Rename(tmpPath, m.credentialPath); err != nil {
		return fmt.Errorf("failed to persist auth credential file: %w", err)
	}

	if err := enforceCredentialFilePermissions(m.fs, m.credentialPath); err != nil {
		return fmt.Errorf("failed to enforce auth credential permissions: %w", err)
	}

	return nil
}

func (m *AuthManager) loadSessionsFromDisk() {
	if m.credentials == nil {
		return
	}

	if _, err := m.fs.Stat(m.sessionPath); err != nil {
		return
	}

	if err := enforceCredentialFilePermissions(m.fs, m.sessionPath); err != nil {
		return
	}

	data, err := afero.ReadFile(m.fs, m.sessionPath)
	if err != nil {
		return
	}

	var payload sessionFile
	if err := json.Unmarshal(data, &payload); err != nil {
		_ = m.fs.Remove(m.sessionPath)
		return
	}

	now := m.nowFn()
	sessions := make(map[string]sessionRecord, len(payload.Sessions))

	for _, item := range payload.Sessions {
		sessionID := strings.TrimSpace(item.ID)
		username := strings.TrimSpace(item.Username)
		if sessionID == "" || username == "" {
			continue
		}

		expiresAt, err := time.Parse(time.RFC3339, item.ExpiresAt)
		if err != nil || now.After(expiresAt) {
			continue
		}

		if username != m.credentials.Username {
			continue
		}

		sessions[sessionID] = sessionRecord{
			Username:   username,
			ExpiresAt:  expiresAt,
			Persistent: true,
		}
	}

	m.mu.Lock()
	defer m.mu.Unlock()
	m.sessions = sessions
}

func (m *AuthManager) writePersistentSessionsLocked() error {
	items := make([]sessionFileItem, 0, len(m.sessions))
	now := m.nowFn()

	for sessionID, session := range m.sessions {
		if !session.Persistent || now.After(session.ExpiresAt) {
			continue
		}

		items = append(items, sessionFileItem{
			ID:        sessionID,
			Username:  session.Username,
			ExpiresAt: session.ExpiresAt.UTC().Format(time.RFC3339),
		})
	}

	if len(items) == 0 {
		if err := m.fs.Remove(m.sessionPath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("failed to remove auth session file: %w", err)
		}
		return nil
	}

	sort.Slice(items, func(i, j int) bool {
		return items[i].ID < items[j].ID
	})

	payload := sessionFile{
		Version:  1,
		Sessions: items,
	}

	data, err := json.MarshalIndent(payload, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal auth session file: %w", err)
	}

	dir := filepath.Dir(m.sessionPath)
	if err := m.fs.MkdirAll(dir, config.DirPermTemp); err != nil {
		return fmt.Errorf("failed to create auth session directory: %w", err)
	}

	tmpFile, err := afero.TempFile(m.fs, dir, sessionFilename+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp auth session file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = m.fs.Remove(tmpPath) }()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp auth session file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp auth session file: %w", err)
	}

	if err := enforceCredentialFilePermissions(m.fs, tmpPath); err != nil {
		return fmt.Errorf("failed to enforce temp auth session permissions: %w", err)
	}

	if err := m.fs.Rename(tmpPath, m.sessionPath); err != nil {
		return fmt.Errorf("failed to persist auth session file: %w", err)
	}

	if err := enforceCredentialFilePermissions(m.fs, m.sessionPath); err != nil {
		return fmt.Errorf("failed to enforce auth session permissions: %w", err)
	}

	return nil
}
