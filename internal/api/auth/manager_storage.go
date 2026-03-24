package auth

import (
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func (m *AuthManager) loadCredentialsFromDisk() error {
	if _, err := os.Lstat(m.credentialPath); err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return fmt.Errorf("failed to stat auth credential file: %w", err)
	}

	if err := enforceCredentialFilePermissions(m.credentialPath); err != nil {
		return fmt.Errorf("failed to enforce auth credential permissions: %w", err)
	}

	data, err := os.ReadFile(m.credentialPath)
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
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return fmt.Errorf("failed to create auth credential directory: %w", err)
	}

	tmpFile, err := os.CreateTemp(dir, credentialFilename+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp auth credential file: %w", err)
	}
	tmpPath := tmpFile.Name()
	defer func() { _ = os.Remove(tmpPath) }()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp auth credential file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp auth credential file: %w", err)
	}

	if err := enforceCredentialFilePermissions(tmpPath); err != nil {
		return fmt.Errorf("failed to enforce temp auth credential permissions: %w", err)
	}

	if err := os.Rename(tmpPath, m.credentialPath); err != nil {
		return fmt.Errorf("failed to persist auth credential file: %w", err)
	}

	if err := enforceCredentialFilePermissions(m.credentialPath); err != nil {
		return fmt.Errorf("failed to enforce auth credential permissions: %w", err)
	}

	return nil
}
