package update

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/javinizer/javinizer-go/internal/configutil"
	"github.com/javinizer/javinizer-go/internal/runtime"
)

// UpdateState represents the cached update information.
type UpdateState struct {
	Version    string `json:"version"`         // Latest version found
	CheckedAt  string `json:"checked_at"`      // ISO8601 timestamp
	Available  bool   `json:"available"`       // Whether update is available
	Prerelease bool   `json:"prerelease"`      // Whether latest is prerelease
	Source     string `json:"source"`          // "cached" or "fresh"
	Error      string `json:"error,omitempty"` // Last error message
}

// StateStore handles loading and saving update state.
type StateStore struct {
	mu       sync.RWMutex
	state    *UpdateState
	path     string
	interval time.Duration
}

// NewStateStore creates a new state store with the given path and check interval.
func NewStateStore(path string, interval time.Duration) *StateStore {
	return &StateStore{
		path:     path,
		interval: interval,
	}
}

// LoadState loads the update state from file.
// Returns a copy of the state to prevent race conditions.
func (s *StateStore) LoadState() (*UpdateState, error) {
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
	if err := os.MkdirAll(dir, configutil.DirPerm); err != nil {
		return nil, err
	}

	data, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state UpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	s.state = &state
	// Return a copy to prevent mutation of internal state
	copy := state
	return &copy, nil
}

// SaveState saves the update state to file atomically.
func (s *StateStore) SaveState(state *UpdateState) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Ensure data directory exists
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, configutil.DirPerm); err != nil {
		return err
	}

	// Marshal to JSON
	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	// Write to temp file first, then rename (atomic on most systems)
	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, data, configutil.FilePerm); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, s.path); err != nil {
		// Clean up temp file on error
		_ = os.Remove(tmpPath)
		return err
	}

	s.state = state
	return nil
}

// ShouldCheck returns true if enough time has passed since last check.
func (s *StateStore) ShouldCheck() bool {
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
func (s *StateStore) ClearState() error {
	s.mu.Lock()
	defer s.mu.Unlock()

	s.state = nil
	return os.Remove(s.path)
}

// SetState sets the state directly without file I/O (for testing).
func (s *StateStore) SetState(state *UpdateState) {
	s.mu.Lock()
	defer s.mu.Unlock()

	// Store a copy to prevent external mutation
	copy := *state
	s.state = &copy
}

// GetState returns the current state (thread-safe).
// Returns a copy to prevent race conditions.
func (s *StateStore) GetState() *UpdateState {
	s.mu.RLock()
	defer s.mu.RUnlock()

	if s.state == nil {
		return nil
	}
	// Return a copy to prevent race conditions
	copy := *s.state
	return &copy
}

// LoadStateFromFile loads state from file directly (for testing).
func LoadStateFromFile(path string) (*UpdateState, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	var state UpdateState
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}

	return &state, nil
}

// SaveStateToFile saves state to file directly (for testing).
func SaveStateToFile(path string, state *UpdateState) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, configutil.DirPerm); err != nil {
		return err
	}

	data, err := json.Marshal(state)
	if err != nil {
		return err
	}

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, data, configutil.FilePerm); err != nil {
		return err
	}

	if err := os.Rename(tmpPath, path); err != nil {
		_ = os.Remove(tmpPath)
		return err
	}

	return nil
}

// DefaultCheckInterval is the default interval between update checks.
const DefaultCheckInterval = 24 * time.Hour

// NewDefaultStateStore creates a state store with default settings.
func NewDefaultStateStore() *StateStore {
	return NewStateStore(runtime.UpdateStatePath(), DefaultCheckInterval)
}

// NowISO8601 returns the current time in ISO8601 format.
func NowISO8601() string {
	return time.Now().UTC().Format(time.RFC3339)
}
