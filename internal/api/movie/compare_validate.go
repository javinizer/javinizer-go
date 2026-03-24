package movie

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
)

// Sentinel errors for NFO validation
var (
	ErrNFONotFound     = errors.New("nfo file not found")
	ErrNFOAccessDenied = errors.New("access denied: path is outside allowed directories")
	ErrNFOInvalidPath  = errors.New("invalid file path")
	ErrNFOIsDirectory  = errors.New("path is a directory, not a file")
)

// validateNFOPath validates an NFO file path against security constraints
// Returns the validated absolute path or a sentinel error
func validateNFOPath(requestedPath string, allowedDirs []string) (string, error) {
	// Expand ~ in requested path (security: consistent with allowlist handling)
	expandedPath := requestedPath
	if strings.HasPrefix(requestedPath, "~") {
		if home, err := os.UserHomeDir(); err == nil {
			if requestedPath == "~" {
				expandedPath = home
			} else if strings.HasPrefix(requestedPath, "~/") {
				expandedPath = filepath.Join(home, strings.TrimPrefix(requestedPath, "~/"))
			}
		}
	}

	// Clean and normalize the path
	cleanPath := filepath.Clean(expandedPath)

	// Convert to absolute path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", ErrNFOInvalidPath
	}

	// Resolve symlinks to prevent symlink attacks
	resolvedPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		// Path doesn't exist or can't be resolved
		if os.IsNotExist(err) {
			return "", ErrNFONotFound
		}
		return "", ErrNFOInvalidPath
	}

	// Verify it's a regular file (not a directory)
	info, err := os.Stat(resolvedPath)
	if err != nil {
		return "", ErrNFONotFound
	}
	if info.IsDir() {
		return "", ErrNFOIsDirectory
	}

	// Security: Deny by default when allowedDirs is empty to prevent arbitrary file access
	// Operators must explicitly configure allowed directories in config
	if len(allowedDirs) == 0 {
		return "", ErrNFOAccessDenied
	}

	// Check if resolved path is within one of the allowed directories
	{
		allowed := false
		for _, allowedDir := range allowedDirs {
			// Expand tilde (~) to user home directory
			if strings.HasPrefix(allowedDir, "~") {
				if home, err := os.UserHomeDir(); err == nil {
					if allowedDir == "~" {
						// Bare tilde expands to home directory
						allowedDir = home
					} else if strings.HasPrefix(allowedDir, "~/") {
						// Tilde with path expands to home + path
						allowedDir = filepath.Join(home, strings.TrimPrefix(allowedDir, "~/"))
					}
					// Note: "~otheruser" format is not supported
				}
			}

			// Clean and normalize the allowed directory path
			allowedDir = filepath.Clean(allowedDir)

			// Resolve allowed directory to handle symlinks
			absAllowedDir, err := filepath.Abs(allowedDir)
			if err != nil {
				continue
			}
			resolvedAllowedDir, err := filepath.EvalSymlinks(absAllowedDir)
			if err != nil {
				// If allowed dir doesn't exist, skip it
				continue
			}

			// Check if resolved path is within this allowed directory
			// Use filepath.Rel to check if path is under allowed directory
			rel, err := filepath.Rel(resolvedAllowedDir, resolvedPath)
			if err == nil && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel) {
				allowed = true
				break
			}
		}

		if !allowed {
			return "", ErrNFOAccessDenied
		}
	}

	return resolvedPath, nil
}
