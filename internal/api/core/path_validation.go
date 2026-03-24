package core

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
)

// validateScanPath validates and sanitizes user-provided paths for scanning.
// It performs multiple security checks:
// 1. Expands home directory (~)
// 2. Cleans the path (removes ../, ./, etc.)
// 3. Converts to absolute path
// 4. Canonicalizes path (resolves symlinks) - CRITICAL for security
// 5. Checks against allowlist (if provided in config)
// 6. Blocks sensitive system directories (built-in + config denylist)
// 7. Verifies path exists and is a directory
//
// Returns: cleaned absolute path, error
func validateScanPath(userPath string, cfg *config.SecurityConfig) (string, error) {
	// 1. Expand home directory
	expandedPath := expandHomeDir(userPath)

	// 2. Clean the path (removes ../, ./, etc.)
	cleanPath := filepath.Clean(expandedPath)

	// 3. Convert to absolute path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", fmt.Errorf("invalid path")
	}

	// 4. Verify path exists before canonicalization
	info, err := os.Lstat(absPath) // Use Lstat to detect symlinks
	if err != nil {
		if os.IsNotExist(err) {
			return "", fmt.Errorf("path does not exist")
		}
		return "", fmt.Errorf("cannot access path")
	}

	// 5. Canonicalize path (resolve symlinks) - CRITICAL to prevent symlink traversal attacks
	canonicalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		return "", fmt.Errorf("cannot resolve path")
	}

	// Ensure canonical path is absolute
	if !filepath.IsAbs(canonicalPath) {
		canonicalPath, err = filepath.Abs(canonicalPath)
		if err != nil {
			return "", fmt.Errorf("invalid path")
		}
	}

	// 6. Check if path is within allowed base directories (if allowlist is provided in config)
	if len(cfg.AllowedDirectories) > 0 {
		allowed := false
		for _, baseDir := range cfg.AllowedDirectories {
			// Expand and canonicalize allowed directory
			expandedBase := expandHomeDir(baseDir)
			absBase, err := filepath.Abs(expandedBase)
			if err != nil {
				continue
			}
			// Canonicalize the allowed directory as well
			canonicalBase, err := filepath.EvalSymlinks(absBase)
			if err != nil {
				continue
			}

			// Check if canonicalPath is within canonicalBase
			rel, err := filepath.Rel(canonicalBase, canonicalPath)
			if err == nil && !strings.HasPrefix(rel, "..") && rel != ".." {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", fmt.Errorf("access denied: path outside allowed directories")
		}
	}

	// 7. Deny sensitive system directories (built-in + config denylist)
	deniedPrefixes := getDeniedDirectories()
	// Merge with config denied directories (canonicalized)
	for _, denied := range cfg.DeniedDirectories {
		expandedDenied := expandHomeDir(denied)
		absDenied, err := filepath.Abs(expandedDenied)
		if err == nil {
			// Canonicalize denied paths too
			if canonicalDenied, err := filepath.EvalSymlinks(absDenied); err == nil {
				deniedPrefixes = append(deniedPrefixes, canonicalDenied)
			} else {
				// If symlink resolution fails, use the absolute path
				deniedPrefixes = append(deniedPrefixes, absDenied)
			}
		}
	}

	// Check denied paths with case-insensitive comparison on Windows
	for _, denied := range deniedPrefixes {
		if pathHasPrefix(canonicalPath, denied) {
			return "", fmt.Errorf("access denied: cannot scan system directory")
		}
	}

	// 8. Verify it's a directory (we already checked with Lstat earlier)
	if !info.IsDir() {
		return "", fmt.Errorf("path is not a directory")
	}

	return canonicalPath, nil
}

// getDeniedDirectories returns a list of system directories that should never be scanned
func getDeniedDirectories() []string {
	denied := []string{
		"/etc",
		"/var/log",
		"/var/spool",
		"/var/mail",
		"/usr/bin",
		"/usr/sbin",
		"/bin",
		"/sbin",
		"/boot",
		"/dev",
		"/proc",
		"/sys",
		"/root",
		"/private/etc",
		"/private/var/log",
		"/private/var/spool",
		"/private/var/mail",
	}

	// Add Windows-specific directories
	if runtime.GOOS == "windows" {
		denied = append(denied,
			"C:\\Windows",
			"C:\\Program Files",
			"C:\\Program Files (x86)",
			"C:\\ProgramData",
		)
	}

	// Add macOS-specific directories
	if runtime.GOOS == "darwin" {
		denied = append(denied,
			"/System",
			"/Library/Application Support",
		)
	}

	return denied
}

// expandHomeDir expands ~ to the user's home directory
func expandHomeDir(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// contains checks if a string contains a substring (case-sensitive)
func contains(s, substr string) bool {
	return strings.Contains(s, substr)
}

// normalizeWindowsPath removes Windows extended-path prefixes (\\?\, \\?\UNC\, \??\, \??\UNC\, \\.\, \\.\UNC\)
// to prevent denylist bypass via extended-length path syntax
// Uses case-insensitive comparison to handle mixed-case prefixes (e.g., \\?\Unc\)
// Handles Win32 namespace (\\?\), NT namespace (\??\), and device namespace (\\.\) aliases
func normalizeWindowsPath(path string) string {
	if runtime.GOOS != "windows" {
		return path
	}

	// Use case-insensitive check for extended path prefixes
	lowerPath := strings.ToLower(path)

	// Remove \\?\UNC\ prefix (UNC paths: \\?\UNC\server\share -> \\server\share)
	// Check case-insensitively to prevent \\?\Unc\ or \\?\uNc\ bypass
	if strings.HasPrefix(lowerPath, `\\?\unc\`) {
		return `\\` + path[8:] // Keep the \\ prefix for UNC paths
	}

	// Remove \??\UNC\ prefix (NT namespace UNC: \??\UNC\server\share -> \\server\share)
	if strings.HasPrefix(lowerPath, `\??\unc\`) {
		return `\\` + path[8:] // Keep the \\ prefix for UNC paths
	}

	// Remove \\.\UNC\ prefix (device namespace UNC: \\.\UNC\server\share -> \\server\share)
	if strings.HasPrefix(lowerPath, `\\.\unc\`) {
		return `\\` + path[8:] // Keep the \\ prefix for UNC paths
	}

	// Remove \\?\ prefix (extended paths: \\?\C:\Windows -> C:\Windows)
	if strings.HasPrefix(lowerPath, `\\?\`) {
		return path[4:]
	}

	// Remove \??\ prefix (NT namespace: \??\C:\Windows -> C:\Windows)
	if strings.HasPrefix(lowerPath, `\??\`) {
		return path[4:]
	}

	// Remove \\.\ prefix (device namespace: \\.\C:\Windows -> C:\Windows)
	if strings.HasPrefix(lowerPath, `\\.\`) {
		return path[4:]
	}

	return path
}

// pathHasPrefix checks if path starts with prefix, using case-insensitive comparison on Windows
// This prevents bypassing the denylist with different case (e.g., c:\Windows vs C:\Windows)
// and extended-path prefixes (e.g., \\?\C:\Windows)
func pathHasPrefix(path, prefix string) bool {
	if runtime.GOOS == "windows" {
		// Normalize both paths to remove extended-path prefixes
		normalizedPath := normalizeWindowsPath(path)
		normalizedPrefix := normalizeWindowsPath(prefix)
		// Windows filesystems are case-insensitive, so we must compare case-insensitively
		return strings.HasPrefix(strings.ToLower(normalizedPath), strings.ToLower(normalizedPrefix))
	}
	// Unix filesystems are case-sensitive
	return strings.HasPrefix(path, prefix)
}

// ValidateScanPath validates and sanitizes user-provided paths for scanning.
func ValidateScanPath(userPath string, cfg *config.SecurityConfig) (string, error) {
	return validateScanPath(userPath, cfg)
}

// ExpandHomeDir expands "~/" paths.
func ExpandHomeDir(path string) string {
	return expandHomeDir(path)
}

// Contains reports whether s contains substr.
func Contains(s, substr string) bool {
	return contains(s, substr)
}

// GetDeniedDirectories returns built-in denied directories.
func GetDeniedDirectories() []string {
	return getDeniedDirectories()
}

// PathHasPrefix checks prefix with platform-aware behavior.
func PathHasPrefix(path, prefix string) bool {
	return pathHasPrefix(path, prefix)
}
