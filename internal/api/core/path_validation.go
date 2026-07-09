package core

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
)

// validateScanPath validates and sanitizes user-provided paths for scanning.
// It delegates to PathValidator.ValidateDir for the shared validation pipeline.
//
// Security checks performed:
//  1. Expands home directory (~)
//  2. Cleans the path (removes ../, ./, etc.)
//  3. Windows: Strips trailing dots/spaces (Win32 API silently strips these)
//  4. Windows: Checks for reserved device names (CON, NUL, etc.) BEFORE filesystem access
//  5. Converts to absolute path
//  6. Canonicalizes path (resolves symlinks) - CRITICAL for security
//  7. Windows: Validates UNC paths (blocks by default to prevent NTLM leaks)
//  8. Windows: Normalizes path for platform (resolves 8.3 short names, trailing chars)
//  9. Checks against allowlist (if provided in config)
//  10. Blocks sensitive system directories (built-in + config denylist)
//  11. Verifies path exists and is a directory
//
// Returns: cleaned absolute path, error
func validateScanPath(userPath string, cfg *SecurityNarrowConfig) (string, error) {
	if cfg == nil {
		// Programmer error — a nil security config must never reach validation.
		// Return a descriptive error rather than apperrors.ErrAllowedDirsEmpty
		// (403 Forbidden), which would mischaracterize this as a user-facing
		// authorization failure instead of an internal wiring bug.
		return "", fmt.Errorf("validateScanPath: security config is required")
	}
	v := NewPathValidatorWithUNC(
		afero.NewOsFs(),
		cfg.AllowedDirectories,
		cfg.DeniedDirectories,
		cfg.AllowUNC,
		cfg.AllowedUNCServers,
	)
	return v.ValidateDir(userPath)
}

// validateBrowsePath mirrors validateScanPath but runs the browse validator,
// which skips the allowlist gate. Used by the browse and autocomplete endpoints
// so an admin can list directories to configure the allowlist — the bootstrap
// operation that the (possibly empty or '.'-only) allowlist would otherwise
// block. The allowlist is a safety guard for file operations (scan/organize),
// not a restriction on browsing. The denylist still applies.
func validateBrowsePath(userPath string, cfg *SecurityNarrowConfig) (string, error) {
	if cfg == nil {
		return "", fmt.Errorf("validateBrowsePath: security config is required")
	}
	v := NewPathValidatorBrowse(
		afero.NewOsFs(),
		cfg.DeniedDirectories,
		cfg.AllowUNC,
		cfg.AllowedUNCServers,
	)
	return v.ValidateDir(userPath)
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
	return ExpandHomeDir(path)
}

// ValidateScanPath validates and sanitizes user-provided paths for scanning.
// Returns the canonical path string. For TOCTOU-safe operations, use ValidateAndOpenPath.
func ValidateScanPath(userPath string, cfg *SecurityNarrowConfig) (string, error) {
	return validateScanPath(userPath, cfg)
}

// ValidateBrowsePath is the browse variant of ValidateScanPath: it skips the
// allowlist gate so the configure-scope browse/autocomplete endpoints can list
// directories outside the allowlist (used to configure the allowlist/denylist).
// Operational browse/autocomplete use ValidateScanPath instead. The denylist
// always applies.
func ValidateBrowsePath(userPath string, cfg *SecurityNarrowConfig) (string, error) {
	return validateBrowsePath(userPath, cfg)
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
func ValidateAndOpenPath(userPath string, cfg *SecurityNarrowConfig) (*os.File, string, error) {
	return validateAndOpenPath(userPath, cfg, false)
}

// ValidateAndOpenBrowsePath is the browse variant of ValidateAndOpenPath: it
// skips the allowlist gate so the configure-scope browse endpoint can list
// directories outside the allowlist (used to configure the allowlist).
// Operational browse uses ValidateAndOpenPath instead. The denylist always
// applies.
func ValidateAndOpenBrowsePath(userPath string, cfg *SecurityNarrowConfig) (*os.File, string, error) {
	return validateAndOpenPath(userPath, cfg, true)
}

func validateAndOpenPath(userPath string, cfg *SecurityNarrowConfig, browse bool) (*os.File, string, error) {
	var canonicalPath string
	var err error
	if browse {
		canonicalPath, err = validateBrowsePath(userPath, cfg)
	} else {
		canonicalPath, err = validateScanPath(userPath, cfg)
	}
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

// ExpandHomeDir expands "~/" and "~" paths.
func ExpandHomeDir(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
		return path
	}
	if len(path) >= 2 && path[0] == '~' && (path[1] == '/' || path[1] == filepath.Separator) {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

// GetDeniedDirectories returns built-in denied directories.
func GetDeniedDirectories() []string {
	return getDeniedDirectories()
}
