package core

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
)

// PathValidator provides a unified, testable path validation pipeline.
// It consolidates the allowlist/denylist/symlink-resolution logic previously
// duplicated across batch/paths.go, core/path_validation.go, and
// movie/compare_validate.go.
//
// The validation pipeline:
//  1. Expand home directory (~)
//  2. Clean the path (remove ../, ./, etc.)
//  3. Windows: strip trailing dots/spaces, check reserved device names
//  4. Convert to absolute path
//  5. Windows: normalize extended-path prefixes, block/validate UNC paths
//  6. Canonicalize path (resolve symlinks)
//  7. Check against allowlist (deny by default when empty)
//  8. Check against denylist (built-in + config)
//  9. Verify path exists and is the expected type (dir or file)
type PathValidator struct {
	fs    afero.Fs
	allow []string
	deny  []string

	// Windows UNC settings
	allowUNC          bool
	allowedUNCServers []string
}

// NewPathValidator creates a PathValidator with the given filesystem and access lists.
// The fs parameter enables testable path operations (use afero.NewOsFs() in production).
// An empty allow list denies all access (secure by default).
func NewPathValidator(fs afero.Fs, allow, deny []string) *PathValidator {
	return &PathValidator{
		fs:    fs,
		allow: allow,
		deny:  deny,
	}
}

// NewPathValidatorWithUNC creates a PathValidator with UNC path settings for Windows.
func NewPathValidatorWithUNC(fs afero.Fs, allow, deny []string, allowUNC bool, allowedUNCServers []string) *PathValidator {
	return &PathValidator{
		fs:                fs,
		allow:             allow,
		deny:              deny,
		allowUNC:          allowUNC,
		allowedUNCServers: allowedUNCServers,
	}
}

// ValidateDir validates and sanitizes a user-provided directory path.
// Returns the cleaned absolute path or a typed apperrors.PathError.
func (v *PathValidator) ValidateDir(userPath string) (string, error) {
	return v.validate(userPath, validateDir)
}

// ValidateFile validates and sanitizes a user-provided file path.
// Returns the cleaned absolute path or a typed apperrors.PathError.
func (v *PathValidator) ValidateFile(userPath string) (string, error) {
	return v.validate(userPath, validateFile)
}

// validateTarget indicates whether we expect a directory or a file.
type validateTarget int

const (
	validateDir validateTarget = iota
	validateFile
)

// validate runs the shared pipeline and then checks the path type.
func (v *PathValidator) validate(userPath string, target validateTarget) (string, error) {
	// Step 1: Allowlist gate — deny by default when empty
	if len(v.allow) == 0 {
		return "", apperrors.ErrAllowedDirsEmpty
	}
	hasValidEntry := false
	for _, dir := range v.allow {
		if strings.TrimSpace(dir) != "" {
			hasValidEntry = true
			break
		}
	}
	if !hasValidEntry {
		return "", apperrors.ErrAllowedDirsEmpty
	}

	// Step 2: Expand home directory
	expandedPath := expandHomeDir(userPath)

	// Step 3: Clean the path
	cleanPath := filepath.Clean(expandedPath)

	// Step 4: Windows — strip trailing dots/spaces
	cleanPath = stripTrailingChars(cleanPath)

	// Step 5: Windows — check reserved device names
	for _, component := range strings.Split(cleanPath, string(filepath.Separator)) {
		if isReservedDeviceName(component) {
			return "", apperrors.NewPathError(apperrors.ErrReservedDeviceName, cleanPath)
		}
	}

	// Step 6: Convert to absolute path
	absPath, err := filepath.Abs(cleanPath)
	if err != nil {
		return "", apperrors.ErrPathInvalid
	}

	// Step 7: Windows — normalize extended-path prefixes
	absPath = normalizeWindowsPath(absPath)

	// Step 8: Windows — block/validate UNC paths
	if isUNCPath(absPath) {
		if !v.allowUNC {
			return "", apperrors.NewPathError(apperrors.ErrUNCPathBlocked, absPath)
		}
		normalizedUNC, err := normalizeUNCPath(absPath, v.allowUNC, v.allowedUNCServers)
		if err != nil {
			return "", err
		}
		absPath = normalizedUNC
	}

	// Step 9: Canonicalize (resolve symlinks)
	canonicalPath, err := v.canonicalizePath(absPath)
	if err != nil {
		return "", err
	}

	if !filepath.IsAbs(canonicalPath) {
		canonicalPath, err = filepath.Abs(canonicalPath)
		if err != nil {
			return "", apperrors.ErrPathInvalid
		}
	}

	// Step 10: Windows — normalize path for platform comparison
	canonicalPath = normalizePathForPlatform(canonicalPath)

	// Step 11: Allowlist check
	if !v.isAllowed(canonicalPath) {
		return "", apperrors.NewPathError(apperrors.ErrPathOutsideAllowed, canonicalPath)
	}

	// Step 12: Denylist check (built-in + config)
	if v.isDenied(canonicalPath) {
		return "", apperrors.NewPathError(apperrors.ErrPathInDenylist, canonicalPath)
	}

	// Step 13: Verify path exists and is the correct type
	info, err := v.fs.Stat(canonicalPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", apperrors.NewPathError(apperrors.ErrPathNotExist, canonicalPath)
		}
		return "", apperrors.ErrPathInvalid
	}

	switch target {
	case validateDir:
		if !info.IsDir() {
			return "", apperrors.NewPathError(apperrors.ErrPathNotDir, canonicalPath)
		}
	case validateFile:
		if info.IsDir() {
			return "", apperrors.NewPathError(apperrors.ErrPathNotFile, canonicalPath)
		}
	}

	return canonicalPath, nil
}

// isAllowed checks if the canonical path falls within at least one allowed directory.
func (v *PathValidator) isAllowed(canonicalPath string) bool {
	for _, baseDir := range v.allow {
		if strings.TrimSpace(baseDir) == "" {
			continue
		}
		expandedBase := expandHomeDir(baseDir)
		absBase, err := filepath.Abs(expandedBase)
		if err != nil {
			continue
		}
		absBase = normalizePathForPlatform(absBase)
		canonicalBase, err := v.canonicalizePath(absBase)
		if err != nil {
			continue
		}
		if isPathWithinCanonical(canonicalPath, canonicalBase) {
			return true
		}
	}
	return false
}

// isDenied checks if the canonical path falls within any denied directory
// (built-in system directories + config-provided deny list).
func (v *PathValidator) isDenied(canonicalPath string) bool {
	// Built-in denylist
	for _, blocked := range getDeniedDirectories() {
		cleanBlocked := filepath.Clean(blocked)
		if absBlocked, err := filepath.Abs(cleanBlocked); err == nil {
			canonicalBlocked, err := v.canonicalizePath(absBlocked)
			if err != nil {
				continue
			}
			if isPathWithinCanonical(canonicalPath, canonicalBlocked) {
				return true
			}
		}
	}

	// Config-provided denylist
	for _, blocked := range v.deny {
		// Skip blank entries: filepath.Clean("") == "." would resolve to the
		// process working directory and unintentionally deny it and its children.
		// Mirrors the allowlist blank-skip below in IsDirAllowed.
		if strings.TrimSpace(blocked) == "" {
			continue
		}
		expandedBlocked := expandHomeDir(blocked)
		cleanBlocked := filepath.Clean(expandedBlocked)
		if absBlocked, err := filepath.Abs(cleanBlocked); err == nil {
			canonicalBlocked, err := v.canonicalizePath(absBlocked)
			if err != nil {
				continue
			}
			if isPathWithinCanonical(canonicalPath, canonicalBlocked) {
				return true
			}
		}
	}

	return false
}

// IsDirAllowed checks if a directory is allowed based on API security settings.
// This is the boolean counterpart used by batch handlers that need a simple
// allow/deny answer without detailed error information.
func (v *PathValidator) IsDirAllowed(dir string) bool {
	expandedDir := expandHomeDir(dir)
	d := filepath.Clean(expandedDir)

	absPath, err := filepath.Abs(d)
	if err != nil {
		return false
	}
	resolved, err := v.canonicalizePath(absPath)
	if err != nil {
		return false
	}

	// Built-in denylist
	for _, blocked := range getDeniedDirectories() {
		cleanBlocked := filepath.Clean(blocked)
		if absBlocked, err := filepath.Abs(cleanBlocked); err == nil {
			realBlocked, err := v.canonicalizePath(absBlocked)
			if err != nil {
				continue
			}
			if isPathWithinCanonical(resolved, realBlocked) {
				return false
			}
		}
	}

	// Config-provided denylist
	for _, blocked := range v.deny {
		// Skip blank entries: filepath.Clean("") == "." would resolve to the
		// process working directory and unintentionally deny it and its children.
		if strings.TrimSpace(blocked) == "" {
			continue
		}
		expandedBlocked := expandHomeDir(blocked)
		cleanBlocked := filepath.Clean(expandedBlocked)
		if absBlocked, err := filepath.Abs(cleanBlocked); err == nil {
			realBlocked, err := v.canonicalizePath(absBlocked)
			if err != nil {
				continue
			}
			if isPathWithinCanonical(resolved, realBlocked) {
				return false
			}
		}
	}

	// Deny by default when no allow list or allow list contains only blank strings
	if len(v.allow) == 0 {
		return false
	}
	hasValidEntry := false
	for _, dir := range v.allow {
		if strings.TrimSpace(dir) != "" {
			hasValidEntry = true
			break
		}
	}
	if !hasValidEntry {
		return false
	}

	// Allowlist check
	for _, allowed := range v.allow {
		if strings.TrimSpace(allowed) == "" {
			continue
		}
		expandedAllowed := expandHomeDir(allowed)
		cleanAllowed := filepath.Clean(expandedAllowed)
		if absAllowed, err := filepath.Abs(cleanAllowed); err == nil {
			realAllowed, err := v.canonicalizePath(absAllowed)
			if err != nil {
				continue
			}
			if isPathWithinCanonical(resolved, realAllowed) {
				return true
			}
		}
	}

	return false
}

// IsUNCAllowed reports whether dir is permitted under the validator's Windows
// UNC policy. Non-UNC paths are always allowed; UNC paths require allowUNC and
// a matching allowedUNCServers entry (mirroring the UNC gate in ValidateDir so
// callers that only use IsDirAllowed still enforce the UNC restriction).
func (v *PathValidator) IsUNCAllowed(dir string) bool {
	if !isUNCPath(dir) {
		return true
	}
	if !v.allowUNC {
		return false
	}
	if _, err := normalizeUNCPath(dir, v.allowUNC, v.allowedUNCServers); err != nil {
		return false
	}
	return true
}

// canonicalizePath resolves symlinks and canonicalizes non-existent child paths by
// resolving the nearest existing ancestor. This keeps path checks consistent across
// platforms where temp paths may include symlinked segments (e.g., /var -> /private/var on macOS).
//
// Known limitation: filepath.EvalSymlinks operates on the host OS filesystem and
// cannot be routed through the injected v.fs abstraction — afero does not provide
// an equivalent symlink-resolution API. The Stat/Lstat calls in the parent-walk
// loop below DO use v.fs (via the Lstater type assertion), so in-memory test
// filesystems (MemMapFs, which has no symlink concept) are handled correctly.
// The two EvalSymlinks calls below are intentional and only affect the real OS
// filesystem path used in production.
func (v *PathValidator) canonicalizePath(absPath string) (string, error) {
	realPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return realPath, nil
	}
	if !os.IsNotExist(err) {
		return "", apperrors.NewPathError(apperrors.ErrPathUnresolvable, absPath)
	}

	// For non-existent paths, resolve the nearest existing parent and append missing segments.
	current := absPath
	missingSegments := make([]string, 0, 4)
	for {
		parent := filepath.Dir(current)
		if parent == current {
			// Root should exist, but fail open to absolute path fallback.
			return absPath, nil
		}

		missingSegments = append(missingSegments, filepath.Base(current))
		current = parent

		// Use LstatIfPossible when the filesystem supports it (e.g. OsFs),
		// otherwise fall back to Stat (e.g. MemMapFs has no symlink concept).
		var statErr error
		if lstater, ok := v.fs.(afero.Lstater); ok {
			_, _, statErr = lstater.LstatIfPossible(current)
		} else {
			_, statErr = v.fs.Stat(current)
		}
		if statErr == nil {
			resolvedParent, resolveErr := filepath.EvalSymlinks(current)
			if resolveErr != nil {
				return "", apperrors.NewPathError(apperrors.ErrPathUnresolvable, current)
			}
			for i := len(missingSegments) - 1; i >= 0; i-- {
				resolvedParent = filepath.Join(resolvedParent, missingSegments[i])
			}
			return resolvedParent, nil
		} else if !os.IsNotExist(statErr) {
			return "", apperrors.NewPathError(apperrors.ErrPathUnresolvable, current)
		}
	}
}

// isPathWithinCanonical checks whether path is within or equal to base.
// Both path and base must be canonical (symlink-resolved) absolute paths.
//
// This is the unified implementation that fixes the behavioral divergence
// between the three previous copies:
//   - batch/paths.go used: rel != ".." && !strings.HasPrefix(rel, ".."+sep)
//   - core/dir_allow.go used: !strings.HasPrefix(rel, "..") && rel != ".."
//   - movie/compare_validate.go used: !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel)
//
// The batch version was the most precise: it correctly allowed filenames starting
// with ".." (like "..hidden") while blocking actual parent traversal (".." and "../").
// We add !filepath.IsAbs(rel) from the movie version to prevent bypass on Windows
// when paths are on different drives (e.g. C:\ vs D:\).
func isPathWithinCanonical(path, base string) bool {
	if path == base {
		return true
	}

	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}

	// Block parent traversal (".." and "../foo"), cross-drive references,
	// but allow filenames starting with ".." (like "..hidden" or "..dotfile")
	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel)
}

// IsPathWithin is the exported version of isPathWithinCanonical for use by
// packages that need the unified path-within check.
func IsPathWithin(path, base string) bool {
	return isPathWithinCanonical(path, base)
}
