package config

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// --- configToYAMLDocument: 66.7% ---

func TestMiss3_ConfigToYAMLDocument_HappyPath(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	doc, err := configToYAMLDocument(cfg)
	require.NoError(t, err)
	assert.Equal(t, yaml.DocumentNode, doc.Kind)
}

// --- parseYAMLDocument: 83.3% ---

func TestMiss3_ParseYAMLDocument_ValidYAML(t *testing.T) {
	doc, err := parseYAMLDocument([]byte("key: value"))
	require.NoError(t, err)
	assert.Equal(t, yaml.DocumentNode, doc.Kind)
}

// --- encodeYAMLDocument: 88.9% ---

func TestMiss3_EncodeYAMLDocument_ValidDoc(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	doc, err := configToYAMLDocument(cfg)
	require.NoError(t, err)
	data, err := encodeYAMLDocument(doc)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

// --- isProcessAlive: 88.9% ---

func TestMiss3_IsProcessAlive_OwnPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	assert.True(t, isProcessAlive(os.Getpid()))
}

func TestMiss3_IsProcessAlive_ZeroPID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	assert.False(t, isProcessAlive(0))
}

func TestMiss3_IsProcessAlive_NegativePID(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("isProcessAlive uses syscall.Signal(0) which is not supported on Windows")
	}
	assert.False(t, isProcessAlive(-1))
}

// --- shouldReapConfigLock: 88.9% ---

func TestMiss3_ShouldReapConfigLock_StaleOwnPID(t *testing.T) {
	now := time.Now()
	staleNano := now.Add(-configLockStaleAge - time.Minute).UnixNano()
	content := []byte(fmt.Sprintf("pid=%d,time=%d", os.Getpid(), staleNano))

	result := shouldReapConfigLock(content, now.Add(-configLockStaleAge-time.Minute), now)
	if runtime.GOOS == "windows" {
		assert.False(t, result)
	} else {
		assert.False(t, result)
	}
}

func TestMiss3_ShouldReapConfigLock_StaleDeadPID(t *testing.T) {
	now := time.Now()
	staleNano := now.Add(-configLockStaleAge - time.Minute).UnixNano()
	content := []byte(fmt.Sprintf("pid=99999,time=%d", staleNano))

	result := shouldReapConfigLock(content, now.Add(-configLockStaleAge-time.Minute), now)
	assert.True(t, result)
}

func TestMiss3_ShouldReapConfigLock_FreshLiveProcess(t *testing.T) {
	now := time.Now()
	freshNano := now.Add(-10 * time.Second).UnixNano()
	content := []byte(fmt.Sprintf("pid=%d,time=%d", os.Getpid(), freshNano))

	result := shouldReapConfigLock(content, now.Add(-10*time.Second), now)
	assert.False(t, result)
}

// --- acquireConfigFileLock: 71.8% ---

func TestMiss3_AcquireConfigFileLock_ReadonlyDir(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't enforce Unix-style directory permissions")
	}

	dir := t.TempDir()
	subdir := filepath.Join(dir, "readonly")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	require.NoError(t, os.Chmod(subdir, 0o500))
	defer os.Chmod(subdir, 0o755)

	_, err := acquireConfigFileLock(filepath.Join(subdir, "config.yaml"))
	require.Error(t, err)
}

func TestMiss3_AcquireConfigFileLock_StaleLockReaped(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	lockPath := configPath + ".lock"

	staleTime := time.Now().Add(-configLockStaleAge - time.Second)
	token := fmt.Sprintf("pid=99999,time=%d", staleTime.UnixNano())
	require.NoError(t, os.WriteFile(lockPath, []byte(token), 0o600))
	require.NoError(t, os.Chtimes(lockPath, staleTime, staleTime))

	unlock, err := acquireConfigFileLock(configPath)
	require.NoError(t, err)
	unlock()
}

func TestMiss3_AcquireConfigFileLock_ConcurrentSecondRelease(t *testing.T) {
	dir := t.TempDir()
	configPath := filepath.Join(dir, "config.yaml")
	unlock, err := acquireConfigFileLock(configPath)
	require.NoError(t, err)
	unlock()
	// Second release should be no-op (sync.Once)
	unlock()
}

// --- syncDir: 70.0% ---

func TestMiss3_SyncDir_NonExistentDir(t *testing.T) {
	err := syncDir(filepath.Join(t.TempDir(), "no-such-dir"))
	require.Error(t, err)
}

func TestMiss3_SyncDir_ExistingDir(t *testing.T) {
	require.NoError(t, syncDir(t.TempDir()))
}

// --- atomicReplaceFile: 57.7% ---

func TestMiss3_AtomicReplaceFile_MissingDir(t *testing.T) {
	err := atomicReplaceFile(filepath.Join(t.TempDir(), "missing", "config.yaml"), []byte("x"), 0o644)
	require.Error(t, err)
}

func TestMiss3_AtomicReplaceFile_Valid(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "valid.yaml")
	require.NoError(t, atomicReplaceFile(path, []byte("content"), 0o644))
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "content", string(data))
}

// --- replaceFileOnWindows: 87.5% ---

func TestMiss3_ReplaceFileOnWindows_StatPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific permission test")
	}

	dir := t.TempDir()
	subdir := filepath.Join(dir, "blocked")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	tmpPath := filepath.Join(dir, "tmp.yaml")
	require.NoError(t, os.WriteFile(tmpPath, []byte("data"), 0o644))

	require.NoError(t, os.Chmod(subdir, 0o000))
	defer os.Chmod(subdir, 0o755)

	err := replaceFileOnWindows(filepath.Join(subdir, "dest.yaml"), tmpPath)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to stat destination")
}

func TestMiss3_ReplaceFileOnWindows_RollbackAfterFailedRename(t *testing.T) {
	dir := t.TempDir()
	destPath := filepath.Join(dir, "dest.yaml")
	require.NoError(t, os.WriteFile(destPath, []byte("original"), 0o644))

	err := replaceFileOnWindows(destPath, filepath.Join(dir, "nonexistent-tmp.yaml"))
	require.Error(t, err)

	data, err := os.ReadFile(destPath)
	require.NoError(t, err)
	assert.Equal(t, "original", string(data))
}

// --- Save: 82.9% ---

func TestMiss3_Save_MalformedYAMLFallback(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("server: [\n  broken"), 0o644))

	cfg := DefaultConfig(nil, nil)
	cfg.Server.Port = 7777
	require.NoError(t, Save(cfg, path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "port: 7777")
}

func TestMiss3_Save_ReadErrorFallback(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't enforce Unix-style file permissions")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("key: value\n"), 0o000))
	defer os.Chmod(path, 0o644)

	cfg := DefaultConfig(nil, nil)
	_ = Save(cfg, path)
}

func TestMiss3_Save_DataUnchangedNoOp(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	cfg := DefaultConfig(nil, nil)
	require.NoError(t, Save(cfg, path))
	info1, err := os.Stat(path)
	require.NoError(t, err)

	time.Sleep(50 * time.Millisecond)
	require.NoError(t, Save(cfg, path))

	info2, err := os.Stat(path)
	require.NoError(t, err)
	assert.Equal(t, info1.ModTime(), info2.ModTime())
}

func TestMiss3_Save_NewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "new-cfg.yaml")

	cfg := DefaultConfig(nil, nil)
	cfg.Server.Port = 9090
	require.NoError(t, Save(cfg, path))

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Contains(t, string(data), "port: 9090")
}

func TestMiss3_Save_MkdirAllFailure(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't enforce Unix-style directory permissions")
	}

	dir := t.TempDir()
	blockingPath := filepath.Join(dir, "blocked")
	require.NoError(t, os.WriteFile(blockingPath, []byte("x"), 0o644))

	path := filepath.Join(blockingPath, "subdir", "config.yaml")
	cfg := DefaultConfig(nil, nil)
	err := Save(cfg, path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create config directory")
}

// --- LoadOrCreate: 91.2% ---

func TestMiss3_LoadOrCreate_StatPermissionDenied(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't enforce Unix-style directory permissions")
	}

	dir := t.TempDir()
	subdir := filepath.Join(dir, "noperm")
	require.NoError(t, os.MkdirAll(subdir, 0o755))
	require.NoError(t, os.Chmod(subdir, 0o000))
	defer os.Chmod(subdir, 0o755)

	_, err := LoadOrCreate(filepath.Join(subdir, "config.yaml"))
	require.Error(t, err)
}

func TestMiss3_LoadOrCreate_LegacyMigration(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	require.NoError(t, os.WriteFile(path, []byte("config_version: 1\nserver:\n  port: 8088\n"), 0o644))

	cfg, err := LoadOrCreate(path)
	require.NoError(t, err)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
}

func TestMiss3_LoadOrCreate_PrepareChangedSave(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	cfg := DefaultConfig(nil, nil)
	require.NoError(t, Save(cfg, path))

	loaded, err := LoadOrCreate(path)
	require.NoError(t, err)
	require.NotNil(t, loaded)
}

// --- createConfigFromEmbedded: 85.7% ---

func TestMiss3_CreateConfigFromEmbedded_EnvOverrides(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "10.0.0.1")
	t.Setenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES", "/media,/data")
	t.Setenv("JAVINIZER_INIT_ALLOWED_ORIGINS", "http://app.example.com")

	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := createConfigFromEmbedded(path)
	require.NoError(t, err)
	assert.Equal(t, "10.0.0.1", cfg.Server.Host)
	assert.Equal(t, []string{"/media", "/data"}, cfg.API.Security.AllowedDirectories)
	assert.Equal(t, []string{"http://app.example.com"}, cfg.API.Security.AllowedOrigins)
}

func TestMiss3_CreateConfigFromEmbedded_NoEnv(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	cfg, err := createConfigFromEmbedded(path)
	require.NoError(t, err)
	assert.Equal(t, CurrentConfigVersion, cfg.ConfigVersion)
}

// --- mergeYAMLNode: sequence node ---

func TestMiss3_MergeYAMLNode_SequenceReplaced(t *testing.T) {
	dst := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "old"},
	}, HeadComment: "hc"}
	src := &yaml.Node{Kind: yaml.SequenceNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "new"},
	}}

	mergeYAMLNode(dst, src)
	assert.Equal(t, "hc", dst.HeadComment)
	assert.Equal(t, "new", dst.Content[0].Value)
}

// --- Load: read error ---

func TestMiss3_Load_ReadError(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows doesn't enforce Unix-style file permissions")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "unreadable.yaml")
	require.NoError(t, os.WriteFile(path, []byte("data"), 0o000))
	defer os.Chmod(path, 0o644)

	_, err := Load(path)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to read config file")
}

func TestMiss3_Load_FileNotFound(t *testing.T) {
	cfg, err := Load(filepath.Join(t.TempDir(), "nonexistent.yaml"))
	require.NoError(t, err)
	require.NotNil(t, cfg)
}

// --- releaseConfigFileLock ---

func TestMiss3_ReleaseConfigFileLock_TokenMismatch(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, "config.yaml.lock")
	require.NoError(t, os.WriteFile(lockPath, []byte("different-token"), 0o600))

	releaseConfigFileLock(lockPath, "my-token")
	_, err := os.Stat(lockPath)
	require.NoError(t, err)
}

func TestMiss3_ReleaseConfigFileLock_AlreadyGone(t *testing.T) {
	releaseConfigFileLock(filepath.Join(t.TempDir(), "config.yaml.lock"), "any")
}

// --- applyInitDefaultsFromEnv ---

func TestMiss3_ApplyInitDefaultsFromEnv_Nil(t *testing.T) {
	assert.False(t, applyInitDefaultsFromEnv(nil))
}

func TestMiss3_ApplyInitDefaultsFromEnv_Whitespace(t *testing.T) {
	t.Setenv("JAVINIZER_INIT_SERVER_HOST", "  ")
	t.Setenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES", "  ,  ")

	cfg := DefaultConfig(nil, nil)
	assert.False(t, applyInitDefaultsFromEnv(cfg))
}

// --- decodeConfig ---

func TestMiss3_DecodeConfig_VersionZero(t *testing.T) {
	cfg, err := decodeConfig([]byte("server:\n  port: 8080\n"))
	require.NoError(t, err)
	assert.Equal(t, 0, cfg.ConfigVersion)
}

// --- cloneYAMLNode ---

func TestMiss3_CloneYAMLNode_Nil(t *testing.T) {
	assert.Nil(t, cloneYAMLNode(nil))
}

// --- findMappingValueIndex ---

func TestMiss3_FindMappingValueIndex_NilNode(t *testing.T) {
	assert.Equal(t, -1, findMappingValueIndex(nil, "key"))
}

func TestMiss3_FindMappingValueIndex_NonMapping(t *testing.T) {
	node := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
	assert.Equal(t, -1, findMappingValueIndex(node, "key"))
}
