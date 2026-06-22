package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"gopkg.in/yaml.v3"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestLoadV5_NonexistentFile tests Load with nonexistent file
func TestLoadV5_NonexistentFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "nonexistent.yaml")
	cfg, err := Load(configPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	// Should return default config
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
}

// TestLoadV5_InvalidYAML tests Load with invalid YAML
func TestLoadV5_InvalidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	err := os.WriteFile(cfgPath, []byte(":\n  invalid: [yaml: content"), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	assert.Error(t, err)
	assert.Nil(t, cfg)
}

// TestLoadV5_ValidYAML tests Load with valid YAML
func TestLoadV5_ValidYAML(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")
	yamlContent := `config_version: 3
server:
  host: "0.0.0.0"
  port: 8080
`
	err := os.WriteFile(cfgPath, []byte(yamlContent), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)
	assert.Equal(t, 3, cfg.ConfigVersion)
	assert.Equal(t, "0.0.0.0", cfg.Server.Host)
	assert.Equal(t, 8080, cfg.Server.Port)
}

// TestSaveV5_NewFile tests Save creating a new file
func TestSaveV5_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	err := Save(cfg, cfgPath)
	require.NoError(t, err)

	// Verify file was created
	_, statErr := os.Stat(cfgPath)
	assert.NoError(t, statErr)
}

// TestSaveV5_RoundTrip tests Save then Load round trip
func TestSaveV5_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	cfg.Server.Host = "127.0.0.1"
	cfg.Server.Port = 9090

	err := Save(cfg, cfgPath)
	require.NoError(t, err)

	loaded, err := Load(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, "127.0.0.1", loaded.Server.Host)
	assert.Equal(t, 9090, loaded.Server.Port)
}

// TestSaveV5_PreservesComments tests Save preserves existing file comments
func TestSaveV5_PreservesComments(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yamlWithComments := `# This is a comment
config_version: 3
server:
  # Host comment
  host: "0.0.0.0"
  port: 8080
`
	err := os.WriteFile(cfgPath, []byte(yamlWithComments), 0644)
	require.NoError(t, err)

	cfg, err := Load(cfgPath)
	require.NoError(t, err)

	cfg.Server.Port = 9090
	err = Save(cfg, cfgPath)
	require.NoError(t, err)

	data, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "This is a comment")
}

// TestSaveV5_Idempotent tests that Save without changes doesn't modify the file
func TestSaveV5_Idempotent(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	err := Save(cfg, cfgPath)
	require.NoError(t, err)

	data1, err := os.ReadFile(cfgPath)
	require.NoError(t, err)

	// Save again without changes
	err = Save(cfg, cfgPath)
	require.NoError(t, err)

	data2, err := os.ReadFile(cfgPath)
	require.NoError(t, err)
	assert.Equal(t, string(data1), string(data2))
}

// TestParseConfigLockMetadataV5 tests lock metadata parsing
func TestParseConfigLockMetadataV5(t *testing.T) {
	tests := []struct {
		name      string
		content   string
		expectOK  bool
		expectPID int
	}{
		{"valid", "pid=1234,time=1609459200000000000", true, 1234},
		{"with whitespace", "pid=1234, time=1609459200000000000", true, 1234},
		{"missing pid", "time=1609459200000000000", false, 0},
		{"missing time", "pid=1234", false, 0},
		{"invalid pid", "pid=abc,time=1609459200000000000", false, 0},
		{"empty", "", false, 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pid, _, ok := parseConfigLockMetadata(tt.content)
			assert.Equal(t, tt.expectOK, ok)
			if ok {
				assert.Equal(t, tt.expectPID, pid)
			}
		})
	}
}

// TestMakeConfigLockTokenV5 tests lock token generation
func TestMakeConfigLockTokenV5(t *testing.T) {
	token := makeConfigLockToken()
	assert.Contains(t, token, "pid=")
	assert.Contains(t, token, "time=")

	// Verify the pid portion is correct
	assert.Contains(t, token, fmt.Sprintf("pid=%d", os.Getpid()))

	// Verify the time portion is a valid nanosecond timestamp
	parts := strings.SplitN(token, ",", 2)
	require.Len(t, parts, 2)
	timeStr := strings.TrimPrefix(parts[1], "time=")
	_, err := strconv.ParseInt(timeStr, 10, 64)
	assert.NoError(t, err, "time= should be a valid integer")
}

// TestDecodeConfigV5 tests YAML decoding
func TestDecodeConfigV5(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{"valid", `config_version: 3`, false},
		{"invalid yaml", `: {invalid}`, true},
		{"empty", ``, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := decodeConfig([]byte(tt.yaml))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, cfg)
			}
		})
	}
}

// TestAtomicReplaceFileV5 tests atomic file replacement
func TestAtomicReplaceFileV5(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")

	// Create initial file
	err := os.WriteFile(filePath, []byte("original"), 0644)
	require.NoError(t, err)

	// Replace it
	err = atomicReplaceFile(filePath, []byte("replaced"), 0644)
	require.NoError(t, err)

	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "replaced", string(data))
}

// TestAtomicReplaceFileV5_NewFile tests atomic replacement when file doesn't exist
func TestAtomicReplaceFileV5_NewFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "new.txt")

	err := atomicReplaceFile(filePath, []byte("new content"), 0644)
	require.NoError(t, err)

	data, err := os.ReadFile(filePath)
	require.NoError(t, err)
	assert.Equal(t, "new content", string(data))
}

// TestConfigToYAMLDocumentV5 tests config to YAML document conversion
func TestConfigToYAMLDocumentV5(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	doc, err := configToYAMLDocument(cfg)
	require.NoError(t, err)
	require.NotNil(t, doc)
}

// TestParseYAMLDocumentV5 tests YAML document parsing
func TestParseYAMLDocumentV5(t *testing.T) {
	tests := []struct {
		name    string
		yaml    string
		wantErr bool
	}{
		{"valid", `key: value`, false},
		{"invalid", `: {invalid}`, true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			doc, err := parseYAMLDocument([]byte(tt.yaml))
			if tt.wantErr {
				assert.Error(t, err)
			} else {
				assert.NoError(t, err)
				assert.NotNil(t, doc)
			}
		})
	}
}

// TestEncodeYAMLDocumentV5 tests YAML document encoding
func TestEncodeYAMLDocumentV5(t *testing.T) {
	doc, err := parseYAMLDocument([]byte(`key: value`))
	require.NoError(t, err)

	data, err := encodeYAMLDocument(doc)
	require.NoError(t, err)
	assert.Contains(t, string(data), "key")
}

// TestCloneYAMLNodeV5 tests YAML node cloning
func TestCloneYAMLNodeV5(t *testing.T) {
	doc, err := parseYAMLDocument([]byte(`key: value`))
	require.NoError(t, err)

	cloned := cloneYAMLNode(doc)
	require.NotNil(t, cloned)
	assert.NotSame(t, doc, cloned)
}

// TestMergeYAMLNodeV5 tests YAML node merging
func TestMergeYAMLNodeV5(t *testing.T) {
	dst, err := parseYAMLDocument([]byte(`key1: value1`))
	require.NoError(t, err)

	src, err := parseYAMLDocument([]byte(`key2: value2`))
	require.NoError(t, err)

	mergeYAMLNode(dst, src)

	data, err := encodeYAMLDocument(dst)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "key1")
	assert.Contains(t, content, "key2")
}

// TestMergeYAMLNodeV5_Overwrite tests YAML node merging with overwrite
func TestMergeYAMLNodeV5_Overwrite(t *testing.T) {
	dst, err := parseYAMLDocument([]byte(`key: old_value`))
	require.NoError(t, err)

	src, err := parseYAMLDocument([]byte(`key: new_value`))
	require.NoError(t, err)

	mergeYAMLNode(dst, src)

	data, err := encodeYAMLDocument(dst)
	require.NoError(t, err)
	content := string(data)
	assert.Contains(t, content, "new_value")
}

// TestMergeYAMLNodeV5_NilNodes tests YAML node merging with nil
func TestMergeYAMLNodeV5_NilNodes(t *testing.T) {
	// Should not panic
	mergeYAMLNode(nil, nil)
	mergeYAMLNode(nil, &yaml.Node{})
	mergeYAMLNode(&yaml.Node{}, nil)
}

// TestApplyInitDefaultsFromEnvV5 tests environment variable defaults
func TestApplyInitDefaultsFromEnvV5(t *testing.T) {
	cfg := DefaultConfig(nil, nil)

	// Set environment variables
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "192.168.1.1")
	t.Setenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES", "/dir1,/dir2")
	t.Setenv("JAVINIZER_INIT_ALLOWED_ORIGINS", "http://localhost:3000,http://example.com")

	changed := applyInitDefaultsFromEnv(cfg)
	assert.True(t, changed)
	assert.Equal(t, "192.168.1.1", cfg.Server.Host)
	assert.Len(t, cfg.API.Security.AllowedDirectories, 2)
	assert.Len(t, cfg.API.Security.AllowedOrigins, 2)
}

// TestApplyInitDefaultsFromEnvV5_NoEnv tests no environment variables
func TestApplyInitDefaultsFromEnvV5_NoEnv(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	changed := applyInitDefaultsFromEnv(cfg)
	assert.False(t, changed)
}

// TestApplyInitDefaultsFromEnvV5_NilConfig tests nil config
func TestApplyInitDefaultsFromEnvV5_NilConfig(t *testing.T) {
	changed := applyInitDefaultsFromEnv(nil)
	assert.False(t, changed)
}

// TestShouldReapConfigLockV5 tests stale lock detection
func TestShouldReapConfigLockV5(t *testing.T) {
	now := time.Now()

	t.Run("corrupt lock stale by mtime", func(t *testing.T) {
		content := []byte("corrupt-data")
		oldModTime := now.Add(-3 * time.Minute) // older than stale age
		assert.True(t, shouldReapConfigLock(content, oldModTime, now))
	})

	t.Run("corrupt lock recent by mtime", func(t *testing.T) {
		content := []byte("corrupt-data")
		recentModTime := now.Add(-30 * time.Second)
		assert.False(t, shouldReapConfigLock(content, recentModTime, now))
	})
}

// TestSyncDirV5 tests directory sync
func TestSyncDirV5(t *testing.T) {
	tmpDir := t.TempDir()
	err := syncDir(tmpDir)
	assert.NoError(t, err)
}

// TestSyncDirV5_NonexistentDir tests sync with nonexistent directory
func TestSyncDirV5_NonexistentDir(t *testing.T) {
	err := syncDir("/nonexistent/directory")
	assert.Error(t, err)
}

// TestFindMappingValueIndexV5 tests YAML mapping value index lookup
func TestFindMappingValueIndexV5(t *testing.T) {
	doc, err := parseYAMLDocument([]byte(`key1: value1
key2: value2`))
	require.NoError(t, err)

	idx := findMappingValueIndex(doc.Content[0], "key1")
	assert.GreaterOrEqual(t, idx, 0)

	idx = findMappingValueIndex(doc.Content[0], "nonexistent")
	assert.Equal(t, -1, idx)

	// nil node
	idx = findMappingValueIndex(nil, "key")
	assert.Equal(t, -1, idx)
}

// TestLoadOrCreateV5_CreateNew tests LoadOrCreate creating new file
func TestLoadOrCreateV5_CreateNew(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "new_config.yaml")

	cfg, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)
	require.NotNil(t, cfg)

	// File should exist now
	_, statErr := os.Stat(cfgPath)
	assert.NoError(t, statErr)
}

// TestLoadOrCreateV5_LoadExisting tests LoadOrCreate loading existing file
func TestLoadOrCreateV5_LoadExisting(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "existing_config.yaml")

	// Create initial config
	cfg := DefaultConfig(nil, nil)
	err := Save(cfg, cfgPath)
	require.NoError(t, err)

	// Load it back
	loaded, err := LoadOrCreate(cfgPath)
	require.NoError(t, err)
	require.NotNil(t, loaded)
}
