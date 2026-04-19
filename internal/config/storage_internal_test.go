package config

import (
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

func mustParseYAMLDoc(t *testing.T, data string) *yaml.Node {
	t.Helper()
	doc, err := parseYAMLDocument([]byte(data))
	require.NoError(t, err)
	return doc
}

func TestDecodeConfigAndYAMLHelpers(t *testing.T) {
	t.Run("decodeConfig rejects malformed YAML", func(t *testing.T) {
		_, err := decodeConfig([]byte("server: ["))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse config file")
	})

	t.Run("cloneYAMLNode performs deep clone", func(t *testing.T) {
		src := mustParseYAMLDoc(t, "root:\n  child: value\n")
		clone := cloneYAMLNode(src)
		require.NotNil(t, clone)
		require.NotSame(t, src, clone)
		require.NotSame(t, src.Content[0], clone.Content[0])

		clone.Content[0].Content[1].Content[1].Value = "mutated"
		assert.Equal(t, "value", src.Content[0].Content[1].Content[1].Value)
	})

	t.Run("applyNodeMetadataPreservingComments copies missing metadata", func(t *testing.T) {
		dst := &yaml.Node{HeadComment: "head", LineComment: "line", FootComment: "foot", Style: yaml.DoubleQuotedStyle}
		src := &yaml.Node{}

		applyNodeMetadataPreservingComments(dst, src)
		assert.Equal(t, "head", src.HeadComment)
		assert.Equal(t, "line", src.LineComment)
		assert.Equal(t, "foot", src.FootComment)
		assert.Equal(t, yaml.DoubleQuotedStyle, src.Style)
	})

	t.Run("findMappingValueIndex handles map and non-map nodes", func(t *testing.T) {
		mapping := mustParseYAMLDoc(t, "a: 1\nb: 2\n")
		idx := findMappingValueIndex(mapping.Content[0], "b")
		assert.GreaterOrEqual(t, idx, 0)
		assert.Equal(t, "2", mapping.Content[0].Content[idx].Value)
		assert.Equal(t, -1, findMappingValueIndex(mapping.Content[0], "missing"))
		assert.Equal(t, -1, findMappingValueIndex(nil, "a"))
		nonMap := &yaml.Node{Kind: yaml.ScalarNode, Value: "x"}
		assert.Equal(t, -1, findMappingValueIndex(nonMap, "a"))
	})

	t.Run("mergeYAMLNode merges mapping keys and keeps unknown keys", func(t *testing.T) {
		dst := mustParseYAMLDoc(t, "a: 1\nnested:\n  b: 2\nunknown: keep\n")
		src := mustParseYAMLDoc(t, "nested:\n  b: 3\n  c: 4\nnew: value\n")

		mergeYAMLNode(dst, src)
		encoded, err := encodeYAMLDocument(dst)
		require.NoError(t, err)
		text := string(encoded)
		assert.Contains(t, text, "a: 1")
		assert.Contains(t, text, "unknown: keep")
		assert.Contains(t, text, "b: 3")
		assert.Contains(t, text, "c: 4")
		assert.Contains(t, text, "new: value")
	})

	t.Run("mergeYAMLNode replaces scalar and preserves destination comments", func(t *testing.T) {
		dst := &yaml.Node{Kind: yaml.ScalarNode, Value: "old", HeadComment: "head", LineComment: "line", FootComment: "foot", Style: yaml.DoubleQuotedStyle}
		src := &yaml.Node{Kind: yaml.ScalarNode, Value: "new"}

		mergeYAMLNode(dst, src)
		assert.Equal(t, "new", dst.Value)
		assert.Equal(t, "head", dst.HeadComment)
		assert.Equal(t, "line", dst.LineComment)
		assert.Equal(t, "foot", dst.FootComment)
		assert.Equal(t, yaml.DoubleQuotedStyle, dst.Style)
	})

	t.Run("configToYAMLDocument and parse/encode helpers", func(t *testing.T) {
		cfg := DefaultConfig()
		cfg.Server.Port = 3210

		doc, err := configToYAMLDocument(cfg)
		require.NoError(t, err)
		require.Equal(t, yaml.DocumentNode, doc.Kind)

		encoded, err := encodeYAMLDocument(doc)
		require.NoError(t, err)
		assert.Contains(t, string(encoded), "port: 3210")

		parsed, err := parseYAMLDocument(encoded)
		require.NoError(t, err)
		require.Equal(t, yaml.DocumentNode, parsed.Kind)

		_, err = parseYAMLDocument([]byte("server: ["))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to parse YAML document")
	})
}

func TestConfigLockParsingAndReaping(t *testing.T) {
	now := time.Now()

	pid, ts, ok := parseConfigLockMetadata("pid=123,time=456")
	require.True(t, ok)
	assert.Equal(t, 123, pid)
	assert.Equal(t, int64(456), ts)

	_, _, ok = parseConfigLockMetadata("pid=oops,time=456")
	assert.False(t, ok)
	_, _, ok = parseConfigLockMetadata("pid=123,time=oops")
	assert.False(t, ok)
	_, _, ok = parseConfigLockMetadata("pid=123")
	assert.False(t, ok)

	assert.False(t, isProcessAlive(0))
	assert.False(t, isProcessAlive(-10))

	staleModTime := now.Add(-configLockStaleAge - time.Second)
	freshModTime := now.Add(-configLockStaleAge / 2)

	// Corrupt content: reclaim only if mtime is stale.
	assert.True(t, shouldReapConfigLock([]byte("bad metadata"), staleModTime, now))
	assert.False(t, shouldReapConfigLock([]byte("bad metadata"), freshModTime, now))

	freshToken := "pid=1,time=" + strconvI64(now.UnixNano())
	assert.False(t, shouldReapConfigLock([]byte(freshToken), staleModTime, now), "fresh token time should not be reaped")

	staleCurrentPID := "pid=" + strconvI(os.Getpid()) + ",time=" + strconvI64(now.Add(-configLockStaleAge-time.Second).UnixNano())
	if runtime.GOOS == "windows" {
		assert.False(t, shouldReapConfigLock([]byte(staleCurrentPID), staleModTime, now))
	} else {
		assert.False(t, shouldReapConfigLock([]byte(staleCurrentPID), staleModTime, now), "stale lock from current live process should not be reaped")
	}

	staleDeadPID := "pid=999999,time=" + strconvI64(now.Add(-configLockStaleAge-time.Second).UnixNano())
	assert.True(t, shouldReapConfigLock([]byte(staleDeadPID), staleModTime, now), "stale lock from dead PID should be reaped")
}

func TestConfigLockAcquireRelease(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.yaml")
	lockPath := path + ".lock"

	unlock, err := acquireConfigFileLock(path)
	require.NoError(t, err)
	require.FileExists(t, lockPath)

	// Ensure once.Do path in unlock is exercised.
	unlock()
	unlock()
	_, err = os.Stat(lockPath)
	assert.True(t, os.IsNotExist(err))

	// Recreate lock and verify token-gated release behavior.
	require.NoError(t, os.WriteFile(lockPath, []byte("token-a"), 0o600))
	releaseConfigFileLock(lockPath, "token-b")
	require.FileExists(t, lockPath)
	releaseConfigFileLock(lockPath, "token-a")
	_, err = os.Stat(lockPath)
	assert.True(t, os.IsNotExist(err))

	// Corrupt stale lock should be reaped by acquire path.
	require.NoError(t, os.WriteFile(lockPath, []byte("corrupt"), 0o600))
	stale := time.Now().Add(-configLockStaleAge - time.Second)
	require.NoError(t, os.Chtimes(lockPath, stale, stale))

	unlock, err = acquireConfigFileLock(path)
	require.NoError(t, err)
	require.FileExists(t, lockPath)
	unlock()
}

func TestSyncAndAtomicReplaceFile(t *testing.T) {
	t.Run("syncDir handles existing and missing directories", func(t *testing.T) {
		require.NoError(t, syncDir(t.TempDir()))
		err := syncDir(filepath.Join(t.TempDir(), "missing"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to open directory for sync")
	})

	t.Run("atomicReplaceFile writes data and reports temp file create errors", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "config.yaml")
		require.NoError(t, atomicReplaceFile(path, []byte("hello"), 0o640))

		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "hello", string(got))

		if runtime.GOOS != "windows" {
			info, err := os.Stat(path)
			require.NoError(t, err)
			assert.Equal(t, os.FileMode(0o640), info.Mode().Perm())
		}

		err = atomicReplaceFile(filepath.Join(dir, "missing", "config.yaml"), []byte("x"), 0o600)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to create temp config file")
	})
}

func TestReplaceFileOnWindowsHelper(t *testing.T) {
	t.Run("replaces when destination does not exist", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "dest.yaml")
		tmpPath := filepath.Join(dir, "tmp.yaml")
		require.NoError(t, os.WriteFile(tmpPath, []byte("new"), 0o644))

		require.NoError(t, replaceFileOnWindows(path, tmpPath))
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "new", string(got))
	})

	t.Run("restores backup on replace failure", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "dest.yaml")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))

		err := replaceFileOnWindows(path, filepath.Join(dir, "missing-tmp.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to atomically replace config file")

		got, readErr := os.ReadFile(path)
		require.NoError(t, readErr)
		assert.Equal(t, "old", string(got))
	})

	t.Run("replaces existing destination and removes backup", func(t *testing.T) {
		dir := t.TempDir()
		path := filepath.Join(dir, "dest.yaml")
		tmpPath := filepath.Join(dir, "tmp.yaml")
		require.NoError(t, os.WriteFile(path, []byte("old"), 0o644))
		require.NoError(t, os.WriteFile(tmpPath, []byte("new"), 0o644))

		require.NoError(t, replaceFileOnWindows(path, tmpPath))
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, "new", string(got))

		backups, err := filepath.Glob(path + ".bak-*")
		require.NoError(t, err)
		assert.Len(t, backups, 0)
	})
}

func TestSaveAndLoadOrCreateBranches(t *testing.T) {
	t.Run("Save handles malformed existing YAML and no-op writes", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.yaml")
		require.NoError(t, os.WriteFile(path, []byte("server: ["), 0o644))

		cfg := DefaultConfig()
		cfg.Server.Port = 18080
		require.NoError(t, Save(cfg, path))

		loaded, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, 18080, loaded.Server.Port)

		before, err := os.ReadFile(path)
		require.NoError(t, err)
		require.NoError(t, Save(cfg, path))
		after, err := os.ReadFile(path)
		require.NoError(t, err)
		assert.Equal(t, string(before), string(after))
	})

	t.Run("LoadOrCreate wraps save default errors", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not enforce Unix-style directory permissions")
		}
		dir := filepath.Join(t.TempDir(), "readonly")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		_, err := LoadOrCreate(filepath.Join(dir, "config.yaml"))
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to save default config")
	})

	t.Run("LoadOrCreate wraps migration errors", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not enforce Unix-style directory permissions")
		}
		dir := filepath.Join(t.TempDir(), "readonly")
		require.NoError(t, os.MkdirAll(dir, 0o755))
		path := filepath.Join(dir, "config.yaml")
		legacy := "server:\n  port: 8088\n"
		require.NoError(t, os.WriteFile(path, []byte(legacy), 0o644))
		require.NoError(t, os.Chmod(dir, 0o500))
		t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })

		_, err := LoadOrCreate(path)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "migration failed")
	})

	t.Run("LoadOrCreate applies init defaults env only on first create", func(t *testing.T) {
		t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
		t.Setenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES", "/media")
		t.Setenv("JAVINIZER_INIT_ALLOWED_ORIGINS", "http://example.com,https://app.example.com")

		path := filepath.Join(t.TempDir(), "config.yaml")
		cfg, err := LoadOrCreate(path)
		require.NoError(t, err)
		assert.Equal(t, "0.0.0.0", cfg.Server.Host)
		assert.Equal(t, []string{"/media"}, cfg.API.Security.AllowedDirectories)
		assert.Equal(t, []string{"http://example.com", "https://app.example.com"}, cfg.API.Security.AllowedOrigins)

		reloaded, err := Load(path)
		require.NoError(t, err)
		assert.Equal(t, "0.0.0.0", reloaded.Server.Host)
		assert.Equal(t, []string{"/media"}, reloaded.API.Security.AllowedDirectories)
		assert.Equal(t, []string{"http://example.com", "https://app.example.com"}, reloaded.API.Security.AllowedOrigins)
	})

	t.Run("LoadOrCreate does not override existing config with init defaults env", func(t *testing.T) {
		t.Setenv("JAVINIZER_INIT_SERVER_HOST", "0.0.0.0")
		t.Setenv("JAVINIZER_INIT_ALLOWED_DIRECTORIES", "/media")

		path := filepath.Join(t.TempDir(), "config.yaml")
		existing := DefaultConfig()
		existing.Server.Host = "127.0.0.1"
		existing.API.Security.AllowedDirectories = []string{"/existing"}
		require.NoError(t, Save(existing, path))

		cfg, err := LoadOrCreate(path)
		require.NoError(t, err)
		assert.Equal(t, "127.0.0.1", cfg.Server.Host)
		assert.Equal(t, []string{"/existing"}, cfg.API.Security.AllowedDirectories)
	})
}

func strconvI(v int) string {
	return strconv.Itoa(v)
}

func strconvI64(v int64) string {
	return strconv.FormatInt(v, 10)
}
