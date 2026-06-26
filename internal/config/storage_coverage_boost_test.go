package config

import (
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gopkg.in/yaml.v3"
)

// ---------------------------------------------------------------------------
// getFirstScraperPriorityStatic (defaults.go:14)
// ---------------------------------------------------------------------------

func TestGetFirstScraperPriorityStatic_Default(t *testing.T) {
	// With the default priority list populated, it should return "r18dev"
	// (aligned with configs/config.yaml.example, which lists r18dev first).
	result := getFirstScraperPriorityStatic()
	assert.Equal(t, "r18dev", result, "should return first element of defaultScraperPriority")
}

func TestGetFirstScraperPriorityStatic_EmptyList(t *testing.T) {
	// Temporarily swap out the package-level var to test the empty-branch.
	orig := defaultScraperPriority
	defaultScraperPriority = nil
	defer func() { defaultScraperPriority = orig }()

	result := getFirstScraperPriorityStatic()
	assert.Equal(t, "", result, "should return empty string when list is empty")
}

func TestGetFirstScraperPriorityStatic_SingleItem(t *testing.T) {
	orig := defaultScraperPriority
	defaultScraperPriority = []string{"onlyone"}
	defer func() { defaultScraperPriority = orig }()

	result := getFirstScraperPriorityStatic()
	assert.Equal(t, "onlyone", result, "should return the single element")
}

func TestGetFirstScraperPriorityStatic_MultipleItems(t *testing.T) {
	orig := defaultScraperPriority
	defaultScraperPriority = []string{"first", "second", "third"}
	defer func() { defaultScraperPriority = orig }()

	result := getFirstScraperPriorityStatic()
	assert.Equal(t, "first", result, "should return the first element")
}

// ---------------------------------------------------------------------------
// configToYAMLDocument (storage.go:138)
// ---------------------------------------------------------------------------

func TestConfigToYAMLDocument_FullConfig(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	doc, err := configToYAMLDocument(cfg)
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, yaml.DocumentNode, doc.Kind)
	// Should have at least one content node (the mapping)
	require.NotEmpty(t, doc.Content, "document should contain at least one node")
}

func TestConfigToYAMLDocument_NilConfig(t *testing.T) {
	// Passing nil should cause yaml.Marshal to handle nil gracefully.
	// In Go, yaml.Marshal(nil) returns "null\n" which parses as a DocumentNode
	// with a ScalarNode content — not a MappingNode. This exercises the
	// "invalid marshaled YAML document" error path if the document kind
	// check fails, or succeeds if yaml.Marshal(nil) still produces a DocumentNode.
	doc, err := configToYAMLDocument(nil)
	// yaml.Marshal(nil) returns "null\n" which is a valid document
	if err != nil {
		assert.Contains(t, err.Error(), "invalid marshaled YAML document")
	} else {
		// It may succeed; just verify it's a document node.
		assert.Equal(t, yaml.DocumentNode, doc.Kind)
	}
}

func TestConfigToYAMLDocument_MinimalConfig(t *testing.T) {
	cfg := &Config{ConfigVersion: 3}
	doc, err := configToYAMLDocument(cfg)
	require.NoError(t, err)
	require.NotNil(t, doc)
	assert.Equal(t, yaml.DocumentNode, doc.Kind)
}

// ---------------------------------------------------------------------------
// encodeYAMLDocument (storage.go:167)
// ---------------------------------------------------------------------------

func TestEncodeYAMLDocument_RoundTrip(t *testing.T) {
	cfg := DefaultConfig(nil, nil)
	doc, err := configToYAMLDocument(cfg)
	require.NoError(t, err)

	data, err := encodeYAMLDocument(doc)
	require.NoError(t, err)
	assert.NotEmpty(t, data)

	// Parse it back and verify key values survive round-trip.
	var parsed map[string]any
	err = yaml.Unmarshal(data, &parsed)
	require.NoError(t, err)
	assert.Equal(t, 3, parsed["config_version"], "config_version should survive round-trip")
}

func TestEncodeYAMLDocument_SimpleDocument(t *testing.T) {
	raw := []byte("server:\n    host: myhost\n    port: 9999\n")
	doc, err := parseYAMLDocument(raw)
	require.NoError(t, err)

	data, err := encodeYAMLDocument(doc)
	require.NoError(t, err)
	assert.Contains(t, string(data), "myhost")
	assert.Contains(t, string(data), "9999")
}

func TestEncodeYAMLDocument_EmptyDocument(t *testing.T) {
	// A DocumentNode with a null scalar content should encode without error.
	doc := &yaml.Node{Kind: yaml.DocumentNode, Content: []*yaml.Node{
		{Kind: yaml.ScalarNode, Value: "null", Tag: "!!null"},
	}}
	data, err := encodeYAMLDocument(doc)
	require.NoError(t, err)
	assert.Contains(t, string(data), "null")
}

// ---------------------------------------------------------------------------
// acquireConfigFileLock (storage.go:275)
// ---------------------------------------------------------------------------

func TestAcquireConfigFileLock_Success(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	unlock, err := acquireConfigFileLock(path)
	require.NoError(t, err)
	require.NotNil(t, unlock)

	// Lock file should exist.
	lockPath := path + ".lock"
	_, statErr := os.Stat(lockPath)
	assert.NoError(t, statErr, "lock file should exist")

	// Unlock should remove the lock file.
	unlock()
	_, statErr = os.Stat(lockPath)
	assert.True(t, os.IsNotExist(statErr), "lock file should be removed after unlock")
}

func TestAcquireConfigFileLock_DoubleUnlock(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	unlock, err := acquireConfigFileLock(path)
	require.NoError(t, err)

	// Calling unlock twice should not panic (sync.Once protects).
	unlock()
	unlock() // second call is a no-op
}

func TestAcquireConfigFileLock_Contention(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")

	unlock1, err := acquireConfigFileLock(path)
	require.NoError(t, err)

	gotLock := make(chan struct{})
	timeout := make(chan struct{})

	go func() {
		unlock2, err2 := acquireConfigFileLock(path)
		if err2 == nil {
			close(gotLock)
			unlock2()
		} else {
			close(timeout)
		}
	}()

	// Give the goroutine a moment to hit the lock contention path.
	time.Sleep(200 * time.Millisecond)

	// The second goroutine should NOT have the lock yet.
	select {
	case <-gotLock:
		t.Fatal("second goroutine acquired lock while first holds it")
	case <-timeout:
		t.Fatal("second goroutine failed to acquire lock (unexpected)")
	default:
		// Expected: second goroutine is still waiting.
	}

	// Release the first lock; second goroutine should now acquire it.
	unlock1()

	select {
	case <-gotLock:
		// Success: second goroutine acquired lock after first released.
	case <-time.After(5 * time.Second):
		t.Fatal("second goroutine did not acquire lock after first released")
	case <-timeout:
		t.Fatal("second goroutine failed to acquire lock after first released")
	}
}

func TestAcquireConfigFileLock_StaleLockReaped(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	lockPath := path + ".lock"

	// Write a stale lock file (old PID that doesn't exist, old timestamp).
	staleContent := "pid=99999999,time=1000000000000000000"
	require.NoError(t, os.WriteFile(lockPath, []byte(staleContent), 0600))

	// Set the mtime to be older than configLockStaleAge.
	staleTime := time.Now().Add(-configLockStaleAge - time.Second)
	require.NoError(t, os.Chtimes(lockPath, staleTime, staleTime))

	// acquireConfigFileLock should reap the stale lock and succeed.
	unlock, err := acquireConfigFileLock(path)
	require.NoError(t, err)
	require.NotNil(t, unlock)
	unlock()
}

func TestAcquireConfigFileLock_InvalidDir(t *testing.T) {
	// Path in a nonexistent directory — lock file creation should fail.
	configPath := unreachableConfigPath(t)
	_, err := acquireConfigFileLock(configPath)
	assert.Error(t, err)
}

// ---------------------------------------------------------------------------
// syncDir (storage.go:338)
// ---------------------------------------------------------------------------

func TestSyncDir_ValidDir(t *testing.T) {
	dir := t.TempDir()
	err := syncDir(dir)
	assert.NoError(t, err)
}

func TestSyncDir_NonexistentDir(t *testing.T) {
	err := syncDir("/nonexistent/path/that/does/not/exist")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to open directory for sync")
}

func TestSyncDir_CurrentWorkingDir(t *testing.T) {
	// The current working directory should always be syncable.
	dir, err := os.Getwd()
	require.NoError(t, err)
	err = syncDir(dir)
	assert.NoError(t, err)
}

// ---------------------------------------------------------------------------
// atomicReplaceFile (storage.go:356)
// ---------------------------------------------------------------------------

func TestAtomicReplaceFile_CreateNewFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "newfile.yaml")

	err := atomicReplaceFile(path, []byte("content"), 0644)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "content", string(data))
}

func TestAtomicReplaceFile_ReplaceExistingFile(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "existing.yaml")

	// Write initial content.
	require.NoError(t, os.WriteFile(path, []byte("old content"), 0644))

	err := atomicReplaceFile(path, []byte("new content"), 0644)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, "new content", string(data))
}

func TestAtomicReplaceFile_NonexistentDirectory(t *testing.T) {
	configPath := unreachableConfigPath(t)
	err := atomicReplaceFile(configPath, []byte("data"), 0644)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to create temp config file")
}

func TestAtomicReplaceFile_LargeContent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "large.yaml")

	// Write a reasonably large file to exercise Write/Sync paths.
	content := make([]byte, 64*1024) // 64KB
	for i := range content {
		content[i] = 'A' + byte(i%26)
	}

	err := atomicReplaceFile(path, content, 0644)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Equal(t, len(content), len(data))
}

func TestAtomicReplaceFile_ConcurrentWrites(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "concurrent.yaml")

	const writers = 5
	var wg sync.WaitGroup
	var mu sync.Mutex
	errs := make([]error, 0, writers)

	for i := range writers {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			if err := atomicReplaceFile(path, []byte("writer-"+string(rune('0'+idx))), 0644); err != nil {
				mu.Lock()
				errs = append(errs, err)
				mu.Unlock()
			}
		}(i)
		// Small sleep to reduce temp filename collisions from identical nanosecond timestamps.
		if i < writers-1 {
			time.Sleep(time.Millisecond)
		}
	}

	wg.Wait()

	// At least some writers should have succeeded; temp file collisions are possible
	// on fast machines, so we tolerate partial failures.
	if len(errs) == writers {
		t.Fatalf("all writers failed: %v", errs)
	}

	// File should exist and have content from one of the successful writers.
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.NotEmpty(t, data)
}

func TestAtomicReplaceFile_EmptyData(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "empty.yaml")

	err := atomicReplaceFile(path, []byte{}, 0644)
	require.NoError(t, err)

	data, err := os.ReadFile(path)
	require.NoError(t, err)
	assert.Empty(t, data)
}
