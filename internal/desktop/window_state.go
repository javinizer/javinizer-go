package desktop

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

// windowStateFile is the JSON file under UserDataDir that persists the last
// known desktop window geometry so the app reopens at the same size/position
// the user left it.
const windowStateFile = "window-state.json"

// windowStateMinWidth/Height mirror the MinWidth/MinHeight set on the wails
// window options (app.go). A saved state below these is clamped up so a
// previously-shrunk-then-min-bumped config can't produce an unresizable window.
const (
	windowStateMinWidth  = 900
	windowStateMinHeight = 600
	// windowStateMaxWidth/Height guard against absurd values from a corrupted
	// file or a display that was disconnected. 8K per side is a generous ceiling.
	windowStateMaxWidth  = 8192
	windowStateMaxHeight = 8192
)

// WindowState holds the persisted geometry of the desktop window. It is
// serialised as JSON to windowStateFile under UserDataDir.
type WindowState struct {
	Width     int  `json:"width"`
	Height    int  `json:"height"`
	Maximised bool `json:"maximised"`
}

// windowStatePath returns the absolute path to the persisted window state
// file. It is package-scoped (lowercase) because callers outside desktop
// should not know about this file; tests in the same package use it directly.
func windowStatePath() (string, error) {
	dir, err := UserDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, windowStateFile), nil
}

// LoadWindowState reads the persisted window state from disk. A missing file
// returns the zero value (caller applies defaults) and no error. A corrupt
// file is treated as missing rather than fatal so a bad state never blocks
// app launch — the user just gets default geometry once.
func LoadWindowState() (WindowState, error) {
	path, err := windowStatePath()
	if err != nil {
		return WindowState{}, fmt.Errorf("desktop: cannot resolve window state path: %w", err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return WindowState{}, nil
		}
		return WindowState{}, fmt.Errorf("desktop: cannot read window state: %w", err)
	}
	var s WindowState
	if err := json.Unmarshal(data, &s); err != nil {
		// Corrupt state file: fall back to defaults, never block app launch.
		//nolint:nilerr // intentional fallback: an unparseable file is treated as missing.
		return WindowState{}, nil
	}
	return clampWindowState(s), nil
}

// SaveWindowState persists the window geometry atomically. It writes to a
// temp file in the same directory and renames over the target so a crash
// mid-write never leaves a truncated file (which LoadWindowState would then
// silently drop, resetting geometry to defaults).
func SaveWindowState(s WindowState) error {
	s = clampWindowState(s)
	path, err := windowStatePath()
	if err != nil {
		return fmt.Errorf("desktop: cannot resolve window state path: %w", err)
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return fmt.Errorf("desktop: cannot marshal window state: %w", err)
	}
	return persistWindowState(path, data, func(dir, pattern string) (windowStateTempFile, error) {
		return os.CreateTemp(dir, pattern)
	})
}

// windowStateTempFile is the staging resource that must be written and closed
// before its pathname is permissioned and published. Production uses an OS file
// in the target directory so replacement remains atomic.
type windowStateTempFile interface {
	Name() string
	Write([]byte) (int, error)
	Close() error
}

// persistWindowState owns persistence after path resolution and serialization.
// The per-call factory permits deterministic staging failures without replacing
// process-wide filesystem operations.
func persistWindowState(path string, data []byte, createTemp func(string, string) (windowStateTempFile, error)) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		return fmt.Errorf("desktop: cannot create window state dir %s: %w", dir, err)
	}
	tmp, err := createTemp(dir, ".window-state-*.json")
	if err != nil {
		return fmt.Errorf("desktop: cannot create temp window state file: %w", err)
	}
	tmpName := tmp.Name()
	defer func() { _ = os.Remove(tmpName) }()
	if _, err := tmp.Write(data); err != nil {
		_ = tmp.Close()
		return fmt.Errorf("desktop: cannot write window state: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("desktop: cannot close temp window state: %w", err)
	}
	if err := os.Chmod(tmpName, 0o600); err != nil {
		return fmt.Errorf("desktop: cannot chmod window state: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("desktop: cannot commit window state: %w", err)
	}
	return nil
}

// clampWindowState enforces the min/max bounds. Width/Height of 0 (the zero
// value when no state exists yet) are left at 0 so the caller can detect
// "no saved state" and apply its own defaults.
func clampWindowState(s WindowState) WindowState {
	if s.Width == 0 || s.Height == 0 {
		return WindowState{}
	}
	if s.Width < windowStateMinWidth {
		s.Width = windowStateMinWidth
	}
	if s.Height < windowStateMinHeight {
		s.Height = windowStateMinHeight
	}
	if s.Width > windowStateMaxWidth {
		s.Width = windowStateMaxWidth
	}
	if s.Height > windowStateMaxHeight {
		s.Height = windowStateMaxHeight
	}
	return s
}
