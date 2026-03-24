package core

import (
	"os"
	"path/filepath"
	"strings"
)

// IsDirAllowed checks if a directory is allowed based on allow/deny lists.
func IsDirAllowed(dir string, allow, deny []string) bool {
	return isDirAllowed(dir, allow, deny)
}

// isDirAllowed is package-local for legacy tests.
func isDirAllowed(dir string, allow, deny []string) bool {
	expandedDir := ExpandHomeDir(dir)
	d := filepath.Clean(expandedDir)

	absPath, err := filepath.Abs(d)
	if err != nil {
		return false
	}
	resolved, err := canonicalizePath(absPath)
	if err != nil {
		return false
	}

	for _, blocked := range GetDeniedDirectories() {
		cleanBlocked := filepath.Clean(blocked)
		if absBlocked, err := filepath.Abs(cleanBlocked); err == nil {
			realBlocked, err := canonicalizePath(absBlocked)
			if err != nil {
				continue
			}
			if isPathWithin(resolved, realBlocked) {
				return false
			}
		}
	}

	for _, blocked := range deny {
		expandedBlocked := ExpandHomeDir(blocked)
		cleanBlocked := filepath.Clean(expandedBlocked)
		if absBlocked, err := filepath.Abs(cleanBlocked); err == nil {
			realBlocked, err := canonicalizePath(absBlocked)
			if err != nil {
				continue
			}
			if isPathWithin(resolved, realBlocked) {
				return false
			}
		}
	}

	if len(allow) == 0 {
		return false
	}

	for _, allowed := range allow {
		expandedAllowed := ExpandHomeDir(allowed)
		cleanAllowed := filepath.Clean(expandedAllowed)
		if absAllowed, err := filepath.Abs(cleanAllowed); err == nil {
			realAllowed, err := canonicalizePath(absAllowed)
			if err != nil {
				continue
			}
			if isPathWithin(resolved, realAllowed) {
				return true
			}
		}
	}

	return false
}

func canonicalizePath(absPath string) (string, error) {
	realPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return realPath, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	current := absPath
	missingSegments := make([]string, 0, 4)
	for {
		parent := filepath.Dir(current)
		if parent == current {
			return absPath, nil
		}

		missingSegments = append(missingSegments, filepath.Base(current))
		current = parent

		if _, statErr := os.Lstat(current); statErr == nil {
			resolvedParent, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", resolveErr
			}
			for i := len(missingSegments) - 1; i >= 0; i-- {
				resolvedParent = filepath.Join(resolvedParent, missingSegments[i])
			}
			return resolvedParent, nil
		} else if !os.IsNotExist(statErr) {
			return "", statErr
		}
	}
}

func isPathWithin(path, base string) bool {
	return strings.HasPrefix(path, base+string(os.PathSeparator)) || path == base
}
