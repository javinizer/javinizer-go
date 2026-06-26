package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/spf13/afero"
	"gopkg.in/yaml.v3"
)

// ConfigStorage provides file-system–backed config I/O with injectable\n// dependencies. Production code uses the package-level functions (Load,\n// Save, LoadOrCreate) which delegate to defaultStorage(). Tests can\n// construct ConfigStorage directly with afero.NewMemMapFs() for\n// filesystem-free operation.
type ConfigStorage struct {
	fs          afero.Fs
	lockFactory func(string) (func(), error) // nil = use real flock
}

// NewConfigStorage creates a ConfigStorage with the given filesystem.\n// If fs is nil, afero.NewOsFs() is used.\n// If lockFactory is nil, the default file-based lock is used.
func NewConfigStorage(fs afero.Fs, lockFactory func(string) (func(), error)) *ConfigStorage {
	if fs == nil {
		fs = afero.NewOsFs()
	}
	return &ConfigStorage{fs: fs, lockFactory: lockFactory}
}

// defaultStorage returns a ConfigStorage backed by the real OS filesystem.
func defaultStorage() *ConfigStorage {
	return &ConfigStorage{fs: afero.NewOsFs()}
}

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	return defaultStorage().Load(path)
}

// Load reads configuration from a YAML file using the configured filesystem.
func (cs *ConfigStorage) Load(path string) (*Config, error) {
	data, err := afero.ReadFile(cs.fs, path)
	if err != nil {
		if isNotExist(err) {
			return DefaultConfig(nil, nil), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg, err := decodeConfig(data)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

// Save writes the configuration to a YAML file.
func Save(cfg *Config, path string) error {
	return defaultStorage().Save(cfg, path)
}

// Save writes the configuration to a YAML file using the configured filesystem.
func (cs *ConfigStorage) Save(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := cs.fs.MkdirAll(dir, DirPerm); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	unlock, err := cs.acquireLock(path)
	if err != nil {
		return err
	}
	defer unlock()

	targetDoc, err := configToYAMLDocument(cfg)
	if err != nil {
		return err
	}

	var data []byte
	existingData, readErr := afero.ReadFile(cs.fs, path)
	if readErr == nil {
		existingDoc, parseErr := parseYAMLDocument(existingData)
		if parseErr == nil {
			mergedDoc := cloneYAMLNode(existingDoc)
			mergeYAMLNode(mergedDoc, targetDoc)

			data, err = encodeYAMLDocument(mergedDoc)
			if err != nil {
				return err
			}
		} else {
			data, err = encodeYAMLDocument(targetDoc)
			if err != nil {
				return err
			}
		}
	} else if isNotExist(readErr) {
		data, err = encodeYAMLDocument(targetDoc)
		if err != nil {
			return err
		}
	} else {
		data, err = encodeYAMLDocument(targetDoc)
		if err != nil {
			return err
		}
	}

	if readErr == nil && bytes.Equal(existingData, data) {
		return nil
	}

	if err := cs.atomicReplace(path, data, FilePerm); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// LoadOrCreate loads config from file or creates it with defaults.
func LoadOrCreate(path string) (*Config, error) {
	return defaultStorage().LoadOrCreate(path)
}

// LoadOrCreate loads config from file or creates it with defaults using the configured filesystem.
func (cs *ConfigStorage) LoadOrCreate(path string) (*Config, error) {
	_, statErr := cs.fs.Stat(path)
	fileMissing := isNotExist(statErr)
	if statErr != nil && !fileMissing {
		return nil, fmt.Errorf("failed to stat config file: %w", statErr)
	}

	if fileMissing {
		return cs.createFromEmbedded(path)
	}

	cfg, err := cs.Load(path)
	if err != nil {
		return nil, err
	}

	if cfg.ConfigVersion < CurrentConfigVersion {
		SetMigrationContext(MigrationContext{
			ConfigPath: path,
			DryRun:     false,
		})

		if cfg.ConfigVersion <= 2 {
			fmt.Fprintf(os.Stderr, "\n⚠️  WARNING: Your config (version %d) is outdated.\n", cfg.ConfigVersion)
			fmt.Fprintf(os.Stderr, "   A backup will be created at: %s.bak-<timestamp>\n", path)
			fmt.Fprintf(os.Stderr, "   A fresh configuration will be generated from defaults.\n")
			fmt.Fprintf(os.Stderr, "   Your previous settings will NOT be preserved.\n\n")
			fmt.Fprintf(os.Stderr, "[Proceeding with migration...]\n")
		}

		if err := MigrateToCurrent(cfg); err != nil {
			return nil, fmt.Errorf("migration failed: %w", err)
		}

		ctx := GetMigrationContext()
		if ctx.BackupPath != "" {
			fmt.Fprintf(os.Stderr, "✓ Backup created: %s\n", ctx.BackupPath)
		}
		fmt.Fprintf(os.Stderr, "✓ Config migrated to version %d\n\n", cfg.ConfigVersion)

		if err := cs.Save(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to save migrated config: %w", err)
		}

		return cfg, nil
	}

	ApplyEnvironmentOverrides(cfg)
	changed, err := Prepare(cfg)
	if err != nil {
		return nil, err
	}

	if changed {
		if err := cs.Save(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to save migrated config: %w", err)
		}
	}

	return cfg, nil
}

// createFromEmbedded creates a new config file from the embedded example.
func (cs *ConfigStorage) createFromEmbedded(path string) (*Config, error) {
	dir := filepath.Dir(path)
	if err := cs.fs.MkdirAll(dir, DirPerm); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	embeddedData := embeddedConfigBytes()

	if err := cs.atomicReplace(path, embeddedData, FilePerm); err != nil {
		return nil, fmt.Errorf("failed to save default config: %w", err)
	}

	cfg, err := cs.Load(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load newly created config: %w", err)
	}

	if applyInitDefaultsFromEnv(cfg) {
		if err := cs.Save(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to save config with environment overrides: %w", err)
		}
	}

	return cfg, nil
}

// acquireLock acquires a config file lock using the configured lockFactory,
// falling back to the real flock-based implementation when lockFactory is nil.
func (cs *ConfigStorage) acquireLock(path string) (func(), error) {
	if cs.lockFactory != nil {
		return cs.lockFactory(path)
	}
	return acquireConfigFileLock(path)
}

// atomicReplace writes data to path atomically using the configured filesystem.
func (cs *ConfigStorage) atomicReplace(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmpPath := filepath.Join(dir, fmt.Sprintf(".%s.tmp-%d-%d", filepath.Base(path), time.Now().UnixNano(), os.Getpid()))
	tmpFile, err := cs.fs.OpenFile(tmpPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, perm)
	if err != nil {
		return fmt.Errorf("failed to create temp config file: %w", err)
	}
	cleanup := true
	defer func() {
		if cleanup {
			_ = cs.fs.Remove(tmpPath)
		}
	}()

	if _, err := tmpFile.Write(data); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := tmpFile.Sync(); err != nil {
		_ = tmpFile.Close()
		return fmt.Errorf("failed to sync temp config file: %w", err)
	}
	if err := tmpFile.Close(); err != nil {
		return fmt.Errorf("failed to close temp config file: %w", err)
	}

	if err := cs.fs.Rename(tmpPath, path); err != nil {
		if runtime.GOOS != "windows" {
			return fmt.Errorf("failed to atomically replace config file: %w", err)
		}
		if err := cs.replaceFileOnWindows(path, tmpPath); err != nil {
			return err
		}
	}

	// fsync the directory after the atomic rename so the rename itself is
	// durable. The interface{ Sync(name string) error } assertion never matched
	// afero.NewOsFs() (Sync is a File method, not an Fs method), so this fsync
	// was silently skipped for normal writes. syncDir performs the real OS
	// directory sync; ignore the error since a failed dir sync does not
	// invalidate the already-durable file rename.
	_ = syncDir(dir)

	cleanup = false
	return nil
}

// replaceFileOnWindows handles Windows atomic replace with backup.
func (cs *ConfigStorage) replaceFileOnWindows(path string, tmpPath string) error {
	backupPath := fmt.Sprintf("%s.bak-%d", path, time.Now().UnixNano())
	backupCreated := false

	if _, statErr := cs.fs.Stat(path); statErr == nil {
		if err := cs.fs.Rename(path, backupPath); err != nil {
			return fmt.Errorf("failed to atomically replace config file: failed to create backup: %w", err)
		}
		backupCreated = true
	} else if !isNotExist(statErr) {
		return fmt.Errorf("failed to atomically replace config file: failed to stat destination: %w", statErr)
	}

	if err := cs.fs.Rename(tmpPath, path); err != nil {
		if backupCreated {
			if restoreErr := cs.fs.Rename(backupPath, path); restoreErr != nil {
				return fmt.Errorf(
					"failed to atomically replace config file: %w (rollback failed: %v)",
					err,
					restoreErr,
				)
			}
		}
		return fmt.Errorf("failed to atomically replace config file: %w", err)
	}

	if backupCreated {
		_ = cs.fs.Remove(backupPath)
	}
	return nil
}

// isNotExist checks if an error indicates a missing file, handling both
// os and afero error types.
func isNotExist(err error) bool {
	if err == nil {
		return false
	}
	if os.IsNotExist(err) {
		return true
	}
	// afero MemMapFs may wrap errors differently
	return strings.Contains(err.Error(), "file does not exist")
}

// Update atomically reads, mutates, and writes the config under an exclusive
// file lock. Use this instead of Load+Save when persisting a single setting
// to avoid a TOCTOU race where a concurrent writer's changes (e.g. from
// `javinizer api`) between the read and write are silently reverted.
func Update(path string, mutate func(*Config)) error {
	if mutate == nil {
		return fmt.Errorf("config.Update: mutate callback must not be nil")
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirPerm); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	unlock, err := acquireConfigFileLock(path)
	if err != nil {
		return err
	}
	defer unlock()

	cfg, err := loadLocked(path)
	if err != nil {
		return err
	}
	mutate(cfg)
	return writeLocked(path, cfg)
}

// loadLocked reads and decodes the config from path. Caller must hold the file lock.
func loadLocked(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return DefaultConfig(nil, nil), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}
	return decodeConfig(data)
}

// writeLocked merges cfg into the existing on-disk config (preserving comments)
// and atomically writes it. Caller must hold the file lock.
func writeLocked(path string, cfg *Config) error {
	targetDoc, err := configToYAMLDocument(cfg)
	if err != nil {
		return err
	}

	var data []byte
	existingData, readErr := os.ReadFile(path)
	if readErr == nil {
		existingDoc, parseErr := parseYAMLDocument(existingData)
		if parseErr == nil {
			mergedDoc := cloneYAMLNode(existingDoc)
			mergeYAMLNode(mergedDoc, targetDoc)

			data, err = encodeYAMLDocument(mergedDoc)
			if err != nil {
				return err
			}
		} else {
			// Fallback: write canonical YAML from struct if existing YAML is malformed.
			data, err = encodeYAMLDocument(targetDoc)
			if err != nil {
				return err
			}
		}
	} else if os.IsNotExist(readErr) {
		data, err = encodeYAMLDocument(targetDoc)
		if err != nil {
			return err
		}
	} else {
		// If existing file can't be read (e.g., permissions), fall back to
		// canonical YAML output and let the write path return the final error.
		data, err = encodeYAMLDocument(targetDoc)
		if err != nil {
			return err
		}
	}

	if readErr == nil && bytes.Equal(existingData, data) {
		return nil
	}

	if err := atomicReplaceFile(path, data, FilePerm); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

func decodeConfig(data []byte) (*Config, error) {
	cfg := DefaultConfig(nil, nil)
	// Deprecated: Legacy config schema v0 detection. Remove after v1.0 migration period.
	// Treat existing files without config_version as legacy schema (v0) so
	// LoadOrCreate can apply migrations and persist newly introduced fields.
	cfg.ConfigVersion = 0

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// CONF-02: Populate Overrides and validateFns maps from scraper registry.
	// No Finalize call here — resolver is not available during Load.
	// The caller (NewDependencies) calls Finalize after Load.

	return cfg, nil
}

func applyInitDefaultsFromEnv(cfg *Config, envLookup ...func(key string) string) bool {
	lookup := os.Getenv
	if len(envLookup) > 0 && envLookup[0] != nil {
		lookup = envLookup[0]
	}

	if cfg == nil {
		return false
	}

	changed := false

	if initHost := strings.TrimSpace(lookup("JAVINIZER_INIT_SERVER_HOST")); initHost != "" {
		cfg.Server.Host = initHost
		changed = true
	}

	if rawDirs := strings.TrimSpace(lookup("JAVINIZER_INIT_ALLOWED_DIRECTORIES")); rawDirs != "" {
		parts := strings.Split(rawDirs, ",")
		dirs := make([]string, 0, len(parts))
		for _, part := range parts {
			dir := strings.TrimSpace(part)
			if dir != "" {
				dirs = append(dirs, dir)
			}
		}
		if len(dirs) > 0 {
			cfg.API.Security.AllowedDirectories = dirs
			changed = true
		}
	}

	if rawOrigins := strings.TrimSpace(lookup("JAVINIZER_INIT_ALLOWED_ORIGINS")); rawOrigins != "" {
		parts := strings.Split(rawOrigins, ",")
		origins := make([]string, 0, len(parts))
		for _, part := range parts {
			origin := strings.TrimSpace(part)
			if origin != "" {
				origins = append(origins, origin)
			}
		}
		if len(origins) > 0 {
			cfg.API.Security.AllowedOrigins = origins
			changed = true
		}
	}

	return changed
}

// createConfigFromEmbedded creates a new config file from the embedded example.
// Package-level convenience for backward compatibility.
//
//nolint:unused // used by same-package tests
func createConfigFromEmbedded(path string) (*Config, error) {
	return defaultStorage().createFromEmbedded(path)
}

// syncDir syncs a directory to disk for durability.
//
//nolint:unused // used by same-package tests
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("failed to open directory for sync: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := f.Sync(); err != nil {
		if runtime.GOOS == "windows" {
			return nil
		}
		return fmt.Errorf("failed to sync directory: %w", err)
	}

	return nil
}

// atomicReplaceFile writes data to path atomically using the real OS filesystem.
//
//nolint:unused // used by same-package tests
func atomicReplaceFile(path string, data []byte, perm os.FileMode) error {
	return defaultStorage().atomicReplace(path, data, perm)
}

// replaceFileOnWindows handles Windows atomic replace with backup.
//
//nolint:unused // used by same-package tests
func replaceFileOnWindows(path string, tmpPath string) error {
	return defaultStorage().replaceFileOnWindows(path, tmpPath)
}
