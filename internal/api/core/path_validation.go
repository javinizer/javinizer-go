package core

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/config"
)

// validateScanPath validates and sanitizes user-provided paths for scanning.
// It performs multiple security checks:
// 1. Expands home directory (~)
// 2. Cleans the path (removes ../, ./, etc.)
// 3. Windows: Strips trailing dots/spaces (Win32 API silently strips these)
// 4. Windows: Checks for reserved device names (CON, NUL, etc.) BEFORE filesystem access
// 5. Converts to absolute path
// 6. Canonicalizes path (resolves symlinks) - CRITICAL for security
// 7. Windows: Validates UNC paths (blocks by default to prevent NTLM leaks)
// 8. Windows: Normalizes path for platform (resolves 8.3 short names, trailing chars)
// 9. Checks against allowlist (if provided in config)
// 10. Blocks sensitive system directories (built-in + config denylist)
// 11. Verifies path exists and is a directory
//
// Returns: cleaned absolute path, error
func validateScanPath(userPath string, cfg *config.SecurityConfig) (string, error) {
	if len(cfg.AllowedDirectories) == 0 {
		return "", apperrors.ErrAllowedDirsEmpty
	}

	hasValidEntry := false
	for _, dir := range cfg.AllowedDirectories {
		if strings.TrimSpace(dir) != "" {
			hasValidEntry = true
			break
		}
	}
	if !hasValidEntry {
		return "", apperrors.ErrAllowedDirsEmpty
	}

	expandedPath := expandHomeDir(userPath)

	cleanPath := filepath.Clean(expandedPath)

	// Windows: Strip trailing dots and spaces before further processing.
	// Win32 API silently strips these, so we must too for accurate comparison.
	cleanPath = stripTrailingChars(cleanPath)

	// Windows: Check for reserved device names BEFORE any filesystem access.
	// Accessing paths like COM1, NUL, etc. can hang operations.
	for _, component := range strings.Split(cleanPath, string(filepath.Separator)) {
		if isReservedDeviceName(component) {
			return "", apperrors.NewPathError(apperrors.ErrReservedDeviceName, cleanPath)
		}
	}

	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", apperrors.ErrPathInvalid
	}

	// Windows: Normalize extended-path prefixes before security checks.
	// This converts \??\UNC\, \\?\UNC\, \\.\UNC\ variants to standard \\ format
	// for consistent UNC detection.
	absPath = normalizeWindowsPath(absPath)

	// Windows: Block UNC paths BEFORE any filesystem access.
	// os.Lstat on UNC paths triggers SMB connection and NTLM authentication.
	if isUNCPath(absPath) {
		if !cfg.AllowUNC {
			return "", apperrors.NewPathError(apperrors.ErrUNCPathBlocked, absPath)
		}
		// Early whitelist check to prevent connection to malicious servers
		normalizedUNC, err := normalizeUNCPath(absPath, cfg.AllowUNC, cfg.AllowedUNCServers)
		if err != nil {
			return "", err
		}
		absPath = normalizedUNC
	}

	canonicalPath, err := filepath.EvalSymlinks(absPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", apperrors.NewPathError(apperrors.ErrPathNotExist, absPath)
		}
		return "", apperrors.NewPathError(apperrors.ErrPathUnresolvable, absPath)
	}

	if !filepath.IsAbs(canonicalPath) {
		canonicalPath, err = filepath.Abs(canonicalPath)
		if err != nil {
			return "", apperrors.ErrPathInvalid
		}
	}

	info, err := os.Stat(canonicalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", apperrors.NewPathError(apperrors.ErrPathNotExist, canonicalPath)
		}
		return "", apperrors.ErrPathInvalid
	}

	// Windows: Normalize path for platform comparison.
	// This resolves 8.3 short names and ensures consistent path format.
	canonicalPath = normalizePathForPlatform(canonicalPath)

	allowed := false
	for _, baseDir := range cfg.AllowedDirectories {
		if strings.TrimSpace(baseDir) == "" {
			continue
		}
		expandedBase := expandHomeDir(baseDir)
		absBase, err := filepath.Abs(expandedBase)
		if err != nil {
			continue
		}

		// Windows: Normalize allowlist entry for platform comparison.
		absBase = normalizePathForPlatform(absBase)

		canonicalBase, err := filepath.EvalSymlinks(absBase)
		if err != nil {
			continue
		}

		rel, err := filepath.Rel(canonicalBase, canonicalPath)
		if err == nil && !strings.HasPrefix(rel, "..") && rel != ".." {
			allowed = true
			break
		}
	}
	if !allowed {
		return "", apperrors.NewPathError(apperrors.ErrPathOutsideAllowed, canonicalPath)
	}

	deniedPrefixes := getDeniedDirectories()
	for _, denied := range cfg.DeniedDirectories {
		expandedDenied := expandHomeDir(denied)
		absDenied, err := filepath.Abs(expandedDenied)
		if err == nil {
			if canonicalDenied, err := filepath.EvalSymlinks(absDenied); err == nil {
				deniedPrefixes = append(deniedPrefixes, canonicalDenied)
			} else {
				deniedPrefixes = append(deniedPrefixes, absDenied)
			}
		}
	}

	for _, denied := range deniedPrefixes {
		if isPathWithin(canonicalPath, denied) {
			return "", apperrors.NewPathError(apperrors.ErrPathInDenylist, canonicalPath)
		}
	}

	if !info.IsDir() {
		return "", apperrors.NewPathError(apperrors.ErrPathNotDir, canonicalPath)
	}

	return canonicalPath, nil
}

// getDeniedDirectories returns a list of system directories that should never be scanned
func getDeniedDirectories() []string {
	return []string{
		"/proc",
		"/sys",
		"/dev",
	}
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
// NOTE: This uses raw string prefix matching and should NOT be used for denylist checks
// because it doesn't distinguish between /dev and /devmedia. Use isPathWithin for denylist.
func pathHasPrefix(path, prefix string) bool {
	if runtime.GOOS == "windows" {
		normalizedPath := normalizeWindowsPath(path)
		normalizedPrefix := normalizeWindowsPath(prefix)
		return strings.HasPrefix(strings.ToLower(normalizedPath), strings.ToLower(normalizedPrefix))
	}
	return strings.HasPrefix(path, prefix)
}

// ValidateScanPath validates and sanitizes user-provided paths for scanning.
// Returns the canonical path string. For TOCTOU-safe operations, use ValidateAndOpenPath.
func ValidateScanPath(userPath string, cfg *config.SecurityConfig) (string, error) {
	return validateScanPath(userPath, cfg)
}

// ValidateAndOpenPath validates a user-provided path and returns an open *os.File
// to the validated directory, along with its canonical path.
//
// This is the TOCTOU-safe version of ValidateScanPath. By holding the file
// descriptor open, symlink swap attacks between validation and use are prevented.
// On Unix, inode verification detects symlink swap attacks between the pre-open
// stat and the post-open file handle. On Windows, pre-open identity is unavailable,
// so only post-open TOCTOU protection is provided (the open handle references the
// actual file object).
//
// The caller MUST close the returned file when done:
//
//	f, path, err := core.ValidateAndOpenPath(req.Path, cfg)
//	if err != nil { ... }
//	defer f.Close()
//	// Use f.ReadDir() or path (file remains open, preventing swap)
func ValidateAndOpenPath(userPath string, cfg *config.SecurityConfig) (*os.File, string, error) {
	canonicalPath, err := validateScanPath(userPath, cfg)
	if err != nil {
		return nil, "", err
	}

	// Get pre-open file identity for TOCTOU verification
	preInfo, err := os.Stat(canonicalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", apperrors.NewPathError(apperrors.ErrPathNotExist, canonicalPath)
		}
		return nil, "", apperrors.ErrPathInvalid
	}

	preIdentity, err := getFileIdentity(preInfo)
	if err != nil {
		// On platforms without inode support, we skip verification but still proceed
		// The file handle still provides TOCTOU protection via the open descriptor
		preIdentity = fileIdentity{}
	}

	// Open the validated directory to prevent TOCTOU symlink swap attacks.
	// The file descriptor keeps a reference to the validated directory inode.
	f, err := os.Open(canonicalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, "", apperrors.NewPathError(apperrors.ErrPathNotExist, canonicalPath)
		}
		return nil, "", apperrors.ErrPathInvalid
	}

	// Post-open inode verification: detect symlink swap attacks
	// This compares the identity of the opened file with the pre-open stat.
	// If they differ, a swap attack occurred between stat and open.
	postIdentity, err := getFileIdentityFromFd(f)
	if err == nil && preIdentity != (fileIdentity{}) {
		if preIdentity != postIdentity {
			_ = f.Close()
			return nil, "", apperrors.NewPathError(apperrors.ErrInodeMismatch, canonicalPath)
		}
	}

	// Verify the opened file is still a directory (extra safety check)
	info, err := f.Stat()
	if err != nil {
		_ = f.Close()
		return nil, "", apperrors.ErrPathInvalid
	}
	if !info.IsDir() {
		_ = f.Close()
		return nil, "", apperrors.NewPathError(apperrors.ErrPathNotDir, canonicalPath)
	}

	return f, canonicalPath, nil
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
