package worker

import (
	"crypto/rand"
	"encoding/hex"
	"path/filepath"
	"sync"

	"github.com/spf13/afero"
)

// FSCaseCache caches filesystem case-sensitivity probe results.
// Ownership is per-BatchJob so the cache lifecycle is visible and testable,
// replacing the previous package-level mutable state.
type FSCaseCache struct {
	mu        sync.RWMutex
	cache     map[string]bool
	probeLock sync.Map
	fs        afero.Fs // Filesystem for case-sensitivity probes (nil = OS fs)
}

// NewFSCaseCache creates an empty FSCaseCache.
// If fs is nil, the OS filesystem is used.
func NewFSCaseCache(fs afero.Fs) *FSCaseCache {
	return &FSCaseCache{
		cache: make(map[string]bool),
		fs:    fs,
	}
}

// Reset clears all cached probe results.
// Probe locks are not deleted — in-flight goroutines may still hold them,
// and deleting entries from sync.Map while held would allow new goroutines
// to create a second mutex for the same path, defeating serialization.
func (c *FSCaseCache) Reset() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cache = make(map[string]bool)
}

// acquireProbeLock returns a path-specific mutex that serializes concurrent
// filesystem probes for the same directory, preventing temp-file interference.
// Uses sync.Map to avoid eviction races — entries are never deleted because
// in-flight goroutines may still hold them; deleting would allow a second
// mutex for the same path, defeating serialization.
// The initial Load avoids allocating a new mutex on every call; LoadOrStore
// handles the rare concurrent race where two goroutines both miss the cache.
func (c *FSCaseCache) acquireProbeLock(path string) *sync.Mutex {
	if v, ok := c.probeLock.Load(path); ok {
		return v.(*sync.Mutex)
	}
	mu := &sync.Mutex{}
	actual, _ := c.probeLock.LoadOrStore(path, mu)
	return actual.(*sync.Mutex)
}

// IsCaseInsensitive returns whether the filesystem at the given path
// is case-insensitive, caching the result to avoid repeated probes.
func (c *FSCaseCache) IsCaseInsensitive(path string) bool {
	c.mu.RLock()
	if result, ok := c.cache[path]; ok {
		c.mu.RUnlock()
		return result
	}
	c.mu.RUnlock()

	probeMu := c.acquireProbeLock(path)
	probeMu.Lock()
	defer probeMu.Unlock()

	c.mu.RLock()
	if result, ok := c.cache[path]; ok {
		c.mu.RUnlock()
		return result
	}
	c.mu.RUnlock()

	result := c.isCaseInsensitiveFS(path)

	c.mu.Lock()
	c.cache[path] = result
	c.mu.Unlock()

	return result
}

// isCaseInsensitiveFS probes the filesystem at path to determine case sensitivity.
// It creates two temp files that differ only in case and checks if they overwrite.
// Uses the injected afero.Fs when available, falling back to the OS filesystem.
func (c *FSCaseCache) isCaseInsensitiveFS(path string) bool {
	fs := c.fs
	if fs == nil {
		fs = afero.NewOsFs()
	}

	// Randomized probe names avoid truncating pre-existing files that happen to
	// share the old fixed names (.javinizer_case_test_1 / .JAVINIZER_CASE_TEST_1).
	token := randomProbeToken()
	testFile1 := filepath.Join(path, ".javinizer_case_test_"+token)
	testFile2 := filepath.Join(path, ".JAVINIZER_CASE_TEST_"+token)

	defer func() { _ = fs.Remove(testFile1) }()
	defer func() { _ = fs.Remove(testFile2) }()

	if err := afero.WriteFile(fs, testFile1, []byte("test"), 0644); err != nil {
		return false
	}

	if err := afero.WriteFile(fs, testFile2, []byte("test2"), 0644); err != nil {
		return false
	}

	content, err := afero.ReadFile(fs, testFile1)
	if err != nil {
		return false
	}

	return string(content) == "test2"
}

// randomProbeToken returns a short hex token for unique probe filenames.
// Uses crypto/rand so concurrent probes from different BatchJobs don't collide.
func randomProbeToken() string {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		// Fallback: extremely unlikely, but never panic on probe naming.
		return "fallback"
	}
	return hex.EncodeToString(b)
}
