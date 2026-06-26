package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/spf13/afero"
)

// UpdateSource represents the source of an update check result.
type UpdateSource string

const (
	// UpdateSourceCached indicates the result came from a cached check.
	UpdateSourceCached UpdateSource = "cached"
	// UpdateSourceFresh indicates the result came from a fresh check.
	UpdateSourceFresh UpdateSource = "fresh"
	// UpdateSourceDisabled indicates update checking is disabled.
	UpdateSourceDisabled UpdateSource = "disabled"
	// UpdateSourceNone indicates no update information is available.
	UpdateSourceNone UpdateSource = "none"
	// UpdateSourceError indicates the update check failed.
	UpdateSourceError UpdateSource = "error"
)

// updateState represents the cached update information.
type updateState struct {
	Version    string       `json:"version"`         // Latest version found
	CheckedAt  string       `json:"checked_at"`      // ISO8601 timestamp
	Available  bool         `json:"available"`       // Whether update is available
	Prerelease bool         `json:"prerelease"`      // Whether latest is prerelease
	Source     UpdateSource `json:"source"`          // Update source status
	Error      string       `json:"error,omitempty"` // Last error message
}

// stateStore handles loading and saving update state.
type stateStore struct {
	mu       sync.RWMutex
	state    *updateState
	path     string
	interval time.Duration
	fs       afero.Fs
}

// newStateStore creates a new state store with the given path and check interval.
func newStateStore(path string, interval time.Duration) *stateStore {
	return newStateStoreWithFs(path, interval, afero.NewOsFs())
}

// newStateStoreWithFs creates a new state store with the given filesystem.
func newStateStoreWithFs(path string, interval time.Duration, fs afero.Fs) *stateStore {
	return &stateStore{
		path:     path,
		interval: interval,
		fs:       fs,
	}
}

// LoadState loads the update state from file.
// Returns a copy of the state to prevent race conditions.
func (s *stateStore) LoadState() (*updateState, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Return cached copy if already loaded
	if s.state != nil {
		// Return a copy to prevent mutation of internal state
		copy := *s.state
		return &copy, nil
	}

	// Ensure data directory exists
	dir := filepath.Dir(s.path)
	if err := s.fs.MkdirAll(dir, config.DirPerm); err != nil {
		return nil, err
	}

	data, err := afero.ReadFile(s.fs, s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state updateState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	s.state = &state
	// Return a copy to prevent mutation of internal state
	copy := state
	return &copy, nil
}

// SaveState saves the update state to file atomically.
func (s *stateStore) SaveState(state *updateState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure data directory exists
	dir := filepath.Dir(s.path)
	if err := s.fs.MkdirAll(dir, config.DirPerm); err != nil {
		return err
	}

	// Marshal to JSON
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	// Write to temp file first, then rename (atomic on most systems)
	tmpPath := s.path + ".tmp"
	if err := afero.WriteFile(s.fs, tmpPath, data, config.FilePerm); err != nil {
		return err
	}

	if err := s.fs.Rename(tmpPath, s.path); err != nil {
		// Clean up temp file on error
		_ = s.fs.Remove(tmpPath)
		return err
	}

	s.state = state
	if state == nil {
		s.state = nil
		return nil
	}
	// Cache a defensive copy so caller-owned *updateState values cannot mutate
	// store state outside the lock (matches LoadState/GetState/SetState copy
	// semantics).
	cached := *state
	s.state = &cached
	return nil
}

// ShouldCheck returns true if enough time has passed since last check.
func (s *stateStore) ShouldCheck() bool {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.state == nil || s.state.CheckedAt == "" {
		return true
	}

	checkedAt, err := time.Parse(time.RFC3339, s.state.CheckedAt)
	if err != nil {
		return true
	}

	return time.Since(checkedAt) >= s.interval
}

// ClearState clears the cached state.
func (s *stateStore) ClearState() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = nil
	return s.fs.Remove(s.path)
}

// SetState sets the state directly without file I/O (for testing).
func (s *stateStore) SetState(state *updateState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store a copy to prevent external mutation
	copy := *state
	s.state = &copy
}

// GetState returns the current state (thread-safe).
// Returns a copy to prevent race conditions.
func (s *stateStore) GetState() *updateState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.state == nil {
		return nil
	}
	// Return a copy to prevent race conditions
	copy := *s.state
	return &copy
}

// loadStateFromFile loads state from file using the provided filesystem.
func loadStateFromFile(fs afero.Fs, path string) (*updateState, error) {
	data, err := afero.ReadFile(fs, path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state updateState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// saveStateToFile saves state to file using the provided filesystem.
func saveStateToFile(fs afero.Fs, path string, state *updateState) error {
	dir := filepath.Dir(path)
	if err := fs.MkdirAll(dir, config.DirPerm); err != nil {
		return err
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := afero.WriteFile(fs, tmpPath, data, config.FilePerm); err != nil {
		return err
	}

	if err := fs.Rename(tmpPath, path); err != nil {
		_ = fs.Remove(tmpPath)
		return err
	}

	return nil
}

// writeTestState writes an update state file for testing using the OS filesystem.
func writeTestState(path, ver, checkedAt string, available, prerelease bool, source UpdateSource) error {
	return writeTestStateWithFs(afero.NewOsFs(), path, ver, checkedAt, available, prerelease, source)
}

// writeTestStateWithFs writes an update state file for testing using the provided filesystem.
func writeTestStateWithFs(fs afero.Fs, path, ver, checkedAt string, available, prerelease bool, source UpdateSource) error {
	state := &updateState{
		Version:    ver,
		CheckedAt:  checkedAt,
		Available:  available,
		Prerelease: prerelease,
		Source:     source,
	}
	return saveStateToFile(fs, path, state)
}

// writeTestStateWithError writes an update state file with an error message for testing using the OS filesystem.
func writeTestStateWithError(path, ver, checkedAt string, available, prerelease bool, source UpdateSource, errMsg string) error {
	return writeTestStateWithErrorFs(afero.NewOsFs(), path, ver, checkedAt, available, prerelease, source, errMsg)
}

// writeTestStateWithErrorFs writes an update state file with an error message for testing using the provided filesystem.
func writeTestStateWithErrorFs(fs afero.Fs, path, ver, checkedAt string, available, prerelease bool, source UpdateSource, errMsg string) error {
	state := &updateState{
		Version:    ver,
		CheckedAt:  checkedAt,
		Available:  available,
		Prerelease: prerelease,
		Source:     source,
		Error:      errMsg,
	}
	return saveStateToFile(fs, path, state)
}

// defaultCheckInterval is the default interval between update checks.
const defaultCheckInterval = 24 * time.Hour

// newDefaultStateStore creates a state store with default settings.
func newDefaultStateStore() *stateStore {
	return newStateStore(updateStatePath(), defaultCheckInterval)
}

// nowISO8601 returns the current time in ISO8601 format.
func nowISO8601() string {
	return time.Now().UTC().Format(time.RFC3339)
}
