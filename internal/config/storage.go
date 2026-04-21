package config

import (
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"gopkg.in/yaml.v3"
)

const (
	configLockWaitInterval = 50 * time.Millisecond
	configLockTimeout      = 10 * time.Second
	configLockStaleAge     = 2 * time.Minute
)

// Load reads configuration from a YAML file.
func Load(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			// If file doesn't exist, return default config.
			return DefaultConfig(), nil
		}
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	cfg, err := decodeConfig(data)
	if err != nil {
		return nil, err
	}
	return cfg, nil
}

func decodeConfig(data []byte) (*Config, error) {
	cfg := DefaultConfig()
	// Treat existing files without config_version as legacy schema (v0) so
	// LoadOrCreate can apply migrations and persist newly introduced fields.
	cfg.ConfigVersion = 0

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config file: %w", err)
	}

	// CONF-02: Populate Overrides and flatConfigs maps from flat per-scraper structs.
	// This enables generic iteration in Validate() without scraper-name branching.
	cfg.Scrapers.NormalizeScraperConfigs()

	return cfg, nil
}

func cloneYAMLNode(node *yaml.Node) *yaml.Node {
	if node == nil {
		return nil
	}

	cloned := *node
	cloned.Content = make([]*yaml.Node, len(node.Content))
	for i, child := range node.Content {
		cloned.Content[i] = cloneYAMLNode(child)
	}
	return &cloned
}

func applyNodeMetadataPreservingComments(dst, src *yaml.Node) {
	if src.HeadComment == "" {
		src.HeadComment = dst.HeadComment
	}
	if src.LineComment == "" {
		src.LineComment = dst.LineComment
	}
	if src.FootComment == "" {
		src.FootComment = dst.FootComment
	}
	if src.Style == 0 {
		src.Style = dst.Style
	}
}

func findMappingValueIndex(node *yaml.Node, key string) int {
	if node == nil || node.Kind != yaml.MappingNode {
		return -1
	}
	for i := 0; i < len(node.Content)-1; i += 2 {
		if node.Content[i].Value == key {
			return i + 1
		}
	}
	return -1
}

func mergeYAMLNode(dst, src *yaml.Node) {
	if dst == nil || src == nil {
		return
	}

	if dst.Kind == yaml.MappingNode && src.Kind == yaml.MappingNode {
		for i := 0; i < len(src.Content)-1; i += 2 {
			srcKey := src.Content[i]
			srcValue := src.Content[i+1]

			dstValueIdx := findMappingValueIndex(dst, srcKey.Value)
			if dstValueIdx == -1 {
				dst.Content = append(dst.Content, cloneYAMLNode(srcKey), cloneYAMLNode(srcValue))
				continue
			}

			mergeYAMLNode(dst.Content[dstValueIdx], srcValue)
		}
		return
	}

	if dst.Kind == yaml.DocumentNode && src.Kind == yaml.DocumentNode {
		if len(dst.Content) == 0 {
			dst.Content = append(dst.Content, cloneYAMLNode(src.Content[0]))
			return
		}
		if len(src.Content) == 0 {
			return
		}
		mergeYAMLNode(dst.Content[0], src.Content[0])
		return
	}

	replacement := cloneYAMLNode(src)
	applyNodeMetadataPreservingComments(dst, replacement)
	*dst = *replacement
}

func configToYAMLDocument(cfg *Config) (*yaml.Node, error) {
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to marshal config: %w", err)
	}

	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse marshaled config: %w", err)
	}

	if doc.Kind != yaml.DocumentNode {
		return nil, fmt.Errorf("invalid marshaled YAML document")
	}

	return &doc, nil
}

func parseYAMLDocument(data []byte) (*yaml.Node, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("failed to parse YAML document: %w", err)
	}
	if doc.Kind != yaml.DocumentNode {
		return nil, fmt.Errorf("invalid YAML document")
	}
	return &doc, nil
}

func encodeYAMLDocument(doc *yaml.Node) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(4)
	if err := enc.Encode(doc); err != nil {
		_ = enc.Close()
		return nil, fmt.Errorf("failed to encode YAML document: %w", err)
	}
	if err := enc.Close(); err != nil {
		return nil, fmt.Errorf("failed to finalize YAML encoding: %w", err)
	}
	return buf.Bytes(), nil
}

func makeConfigLockToken() string {
	return fmt.Sprintf("pid=%d,time=%d", os.Getpid(), time.Now().UnixNano())
}

func releaseConfigFileLock(lockPath string, token string) {
	currentToken, err := os.ReadFile(lockPath)
	if err != nil {
		return
	}
	if string(currentToken) != token {
		return
	}
	_ = os.Remove(lockPath)
}

func parseConfigLockMetadata(content string) (pid int, createdUnixNano int64, ok bool) {
	pidSet := false
	timeSet := false

	parts := strings.FieldsFunc(content, func(r rune) bool {
		return r == ',' || r == '\n' || r == '\r' || r == '\t' || r == ' '
	})

	for _, part := range parts {
		if part == "" {
			continue
		}
		kv := strings.SplitN(part, "=", 2)
		if len(kv) != 2 {
			continue
		}
		key := strings.TrimSpace(kv[0])
		value := strings.TrimSpace(kv[1])
		switch key {
		case "pid":
			parsedPID, err := strconv.Atoi(value)
			if err != nil {
				return 0, 0, false
			}
			pid = parsedPID
			pidSet = true
		case "time":
			parsedTime, err := strconv.ParseInt(value, 10, 64)
			if err != nil {
				return 0, 0, false
			}
			createdUnixNano = parsedTime
			timeSet = true
		}
	}

	return pid, createdUnixNano, pidSet && timeSet
}

func isProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}

	process, err := os.FindProcess(pid)
	if err != nil {
		return false
	}

	// Signal(0) probes process existence without sending a signal.
	err = process.Signal(syscall.Signal(0))
	if err == nil {
		return true
	}
	return err == syscall.EPERM
}

func shouldReapConfigLock(content []byte, modTime time.Time, now time.Time) bool {
	pid, createdUnixNano, ok := parseConfigLockMetadata(string(content))
	if !ok {
		// Fallback for corrupt/partial lock files (e.g., crash between create and write):
		// reclaim only when the lock file itself is stale by mtime.
		return now.Sub(modTime) > configLockStaleAge
	}

	createdAt := time.Unix(0, createdUnixNano)
	if now.Sub(createdAt) <= configLockStaleAge {
		return false
	}

	if runtime.GOOS == "windows" {
		// PID liveness probing via Signal(0) is unreliable on Windows.
		// Reclaim parseable stale locks by age, but avoid stealing our own lock.
		return pid != os.Getpid()
	}

	return !isProcessAlive(pid)
}

func acquireConfigFileLock(path string) (func(), error) {
	lockPath := path + ".lock"
	deadline := time.Now().Add(configLockTimeout)
	token := makeConfigLockToken()

	for {
		lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err == nil {
			if _, writeErr := lockFile.WriteString(token); writeErr != nil {
				_ = lockFile.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("failed to write config lock: %w", writeErr)
			}
			if syncErr := lockFile.Sync(); syncErr != nil {
				_ = lockFile.Close()
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("failed to sync config lock: %w", syncErr)
			}
			if closeErr := lockFile.Close(); closeErr != nil {
				_ = os.Remove(lockPath)
				return nil, fmt.Errorf("failed to close config lock: %w", closeErr)
			}

			var once sync.Once
			return func() {
				once.Do(func() {
					releaseConfigFileLock(lockPath, token)
				})
			}, nil
		}

		if !os.IsExist(err) {
			return nil, fmt.Errorf("failed to acquire config lock: %w", err)
		}

		lockContent, readErr := os.ReadFile(lockPath)
		if readErr == nil {
			now := time.Now()
			lockInfo, statErr := os.Stat(lockPath)
			if os.IsNotExist(statErr) {
				continue
			}
			if statErr == nil && shouldReapConfigLock(lockContent, lockInfo.ModTime(), now) {
				if removeErr := os.Remove(lockPath); removeErr == nil || os.IsNotExist(removeErr) {
					continue
				}
			}
		} else if os.IsNotExist(readErr) {
			continue
		}

		if _, statErr := os.Stat(lockPath); os.IsNotExist(statErr) {
			continue
		}

		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for config lock: %s", lockPath)
		}

		time.Sleep(configLockWaitInterval)
	}
}

func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return fmt.Errorf("failed to open directory for sync: %w", err)
	}
	defer func() { _ = f.Close() }()

	if err := f.Sync(); err != nil {
		// Directory sync is best-effort on platforms that do not support it.
		if runtime.GOOS == "windows" {
			return nil
		}
		return fmt.Errorf("failed to sync directory: %w", err)
	}

	return nil
}

func atomicReplaceFile(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, "."+filepath.Base(path)+".tmp-*")
	if err != nil {
		return fmt.Errorf("failed to create temp config file: %w", err)
	}
	tmpPath := tmp.Name()
	cleanup := true
	defer func() {
		if cleanup {
			_ = os.Remove(tmpPath)
		}
	}()

	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to write temp config file: %w", err)
	}
	if err := tmp.Chmod(perm); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to set temp config permissions: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("failed to sync temp config file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("failed to close temp config file: %w", err)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if runtime.GOOS != "windows" {
			return fmt.Errorf("failed to atomically replace config file: %w", err)
		}
		if err := replaceFileOnWindows(path, tmpPath); err != nil {
			return err
		}
	}

	if err := syncDir(dir); err != nil {
		return err
	}

	cleanup = false
	return nil
}

func replaceFileOnWindows(path string, tmpPath string) error {
	backupPath := fmt.Sprintf("%s.bak-%d", path, time.Now().UnixNano())
	backupCreated := false

	if _, statErr := os.Stat(path); statErr == nil {
		if err := os.Rename(path, backupPath); err != nil {
			return fmt.Errorf("failed to atomically replace config file: failed to create backup: %w", err)
		}
		backupCreated = true
	} else if !os.IsNotExist(statErr) {
		return fmt.Errorf("failed to atomically replace config file: failed to stat destination: %w", statErr)
	}

	if err := os.Rename(tmpPath, path); err != nil {
		if backupCreated {
			if restoreErr := os.Rename(backupPath, path); restoreErr != nil {
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
		_ = os.Remove(backupPath)
	}
	return nil
}

// Save writes the configuration to a YAML file.
func Save(cfg *Config, path string) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirPermConfig); err != nil {
		return fmt.Errorf("failed to create config directory: %w", err)
	}

	unlock, err := acquireConfigFileLock(path)
	if err != nil {
		return err
	}
	defer unlock()

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

	if err := atomicReplaceFile(path, data, FilePermConfig); err != nil {
		return fmt.Errorf("failed to write config file: %w", err)
	}

	return nil
}

// LoadOrCreate loads config from file or creates it with defaults.
// When creating a new file, it uses the embedded config.yaml.example to preserve
// all comments and documentation, ensuring Docker and non-Docker deployments
// generate identical commented configurations.
func LoadOrCreate(path string) (*Config, error) {
	// Check if file exists before attempting to load
	_, statErr := os.Stat(path)
	fileMissing := os.IsNotExist(statErr)
	if statErr != nil && !fileMissing {
		return nil, fmt.Errorf("failed to stat config file: %w", statErr)
	}

	// If file doesn't exist, create from embedded config
	if fileMissing {
		return createConfigFromEmbedded(path)
	}

	// File exists - load and process normally
	cfg, err := Load(path)
	if err != nil {
		return nil, err
	}

	// Check if migration is needed
	if cfg.ConfigVersion < CurrentConfigVersion {
		// Set migration context for backup creation
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

		// Run migration
		if err := MigrateToCurrent(cfg); err != nil {
			return nil, fmt.Errorf("migration failed: %w", err)
		}

		// Get backup path if created
		ctx := GetMigrationContext()
		if ctx.BackupPath != "" {
			fmt.Fprintf(os.Stderr, "✓ Backup created: %s\n", ctx.BackupPath)
		}
		fmt.Fprintf(os.Stderr, "✓ Config migrated to version %d\n\n", cfg.ConfigVersion)

		// Save migrated config
		if err := Save(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to save migrated config: %w", err)
		}

		return cfg, nil
	}

	// Normal prepare for current version configs
	ApplyEnvironmentOverrides(cfg)
	changed, err := Prepare(cfg)
	if err != nil {
		return nil, err
	}

	// Save if migrations changed the config
	if changed {
		if err := Save(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to save migrated config: %w", err)
		}
	}

	return cfg, nil
}

// createConfigFromEmbedded creates a new config file from the embedded example.
// This preserves all comments and documentation from config.yaml.example.
func createConfigFromEmbedded(path string) (*Config, error) {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, DirPermConfig); err != nil {
		return nil, fmt.Errorf("failed to create config directory: %w", err)
	}

	// Get embedded config content (raw bytes with all comments preserved)
	embeddedData := EmbeddedConfigBytes()

	// Write the raw embedded config first to preserve all comments
	if err := atomicReplaceFile(path, embeddedData, FilePermConfig); err != nil {
		return nil, fmt.Errorf("failed to save default config: %w", err)
	}

	// Load the config we just wrote
	cfg, err := Load(path)
	if err != nil {
		return nil, fmt.Errorf("failed to load newly created config: %w", err)
	}

	// Populate scraper overrides from registry
	cfg.Scrapers.NormalizeScraperConfigs()

	// Apply environment-based initialization overrides if any
	if applyInitDefaultsFromEnv(cfg) {
		// Save with overrides - mergeYAMLNode will preserve existing comments
		if err := Save(cfg, path); err != nil {
			return nil, fmt.Errorf("failed to save config with environment overrides: %w", err)
		}
	}

	return cfg, nil
}

func applyInitDefaultsFromEnv(cfg *Config) bool {
	if cfg == nil {
		return false
	}

	changed := false

	if initHost := strings.TrimSpace(os.Getenv("JAVINIZER_INIT_SERVER_HOST")); initHost != "" {
		cfg.Server.Host = initHost
		changed = true
	}

	if rawDirs := strings.TrimSpace(os.Getenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES")); rawDirs != "" {
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

	if rawOrigins := strings.TrimSpace(os.Getenv("JAVINIZER_INIT_ALLOWED_ORIGINS")); rawOrigins != "" {
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
