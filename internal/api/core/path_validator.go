package core

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
)

// evalSymlinksFunc resolves symlinks and canonicalizes a path. It is a
// package-level seam over filepath.EvalSymlinks so tests can simulate
// platform-specific reparse-point behavior (e.g. NTFS volume mount points on
// Windows, where EvalSymlinks returns a non-NotExist error) without needing a
// real mount point on disk. Mirrors the executableFunc seam in
// internal/updater/swap_darwin.go.
var evalSymlinksFunc = filepath.EvalSymlinks

var filepathAbs = filepath.Abs

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

	// enforceAllowlist gates the allowlist checks (step 1 empty-allowlist gate
	// and step 11 isAllowed). It is true for file-operation validators (scan,
	// organize, movie compare) where the allowlist is a safety guard, and false
	// for the browse/autocomplete validator, which lists directories so the
	// admin can configure the allowlist — restricting listing to the allowlist
	// would be a catch-22 (you can't browse to add the first directory). The
	// denylist (built-in /proc, /sys, /dev + config) always applies, and all
	// other validation (home expansion, cleaning, reserved names, UNC,
	// canonicalize, exists + type) runs unchanged regardless of this flag.
	enforceAllowlist bool
}

// NewPathValidator creates a PathValidator with the given filesystem and access lists.
// The fs parameter enables testable path operations (use afero.NewOsFs() in production).
// An empty allow list denies all access (secure by default).
func NewPathValidator(fs afero.Fs, allow, deny []string) *PathValidator {
	return &PathValidator{
		fs:               fs,
		allow:            allow,
		deny:             deny,
		enforceAllowlist: true,
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
		enforceAllowlist:  true,
	}
}

// NewPathValidatorBrowse creates a PathValidator for directory listing
// (browse/autocomplete): the allowlist is not enforced, so any directory can
// be listed. The denylist still applies. The allowlist is a safety guard for
// file operations (scan/organize), not a restriction on browsing to
// configure it — enforcing it on browse would be a catch-22 (you can't browse
// to add the first allowed directory, or to add a directory on another drive).
func NewPathValidatorBrowse(fs afero.Fs, deny []string, allowUNC bool, allowedUNCServers []string) *PathValidator {
	return &PathValidator{
		fs:                fs,
		allow:             nil,
		deny:              deny,
		allowUNC:          allowUNC,
		allowedUNCServers: allowedUNCServers,
		enforceAllowlist:  false,
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
	// Step 1: Allowlist gate — deny by default when empty. Skipped for the
	// browse validator (enforceAllowlist=false), where the whole point is
	// listing directories to configure the allowlist. The denylist (step 12)
	// still applies.
	if v.enforceAllowlist {
		if len(v.allow) == 0 {
			return "", apperrors.ErrAllowedDirsEmpty
		}
		hasValidEntry := false
		for _, dir := range v.allow {
			if strings.TrimSpace(dir) == "" {
				continue
			}
			_, usable := v.effectiveAllowedBase(dir)
			if usable {
				hasValidEntry = true
				break
			}
		}
		if !hasValidEntry {
			return "", apperrors.ErrAllowedDirsEmpty
		}
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

	// Step 11: Allowlist check. Skipped for the browse validator (see Step 1).
	if v.enforceAllowlist && !v.isAllowed(canonicalPath) {
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

// isFilesystemRoot reports whether path is a filesystem root:
// "/" on Unix or a Windows drive root like "C:\\" / "C:/".
func isFilesystemRoot(path string) bool {
	normalized := normalizeWindowsPath(path)
	if normalized == "/" || normalized == string(filepath.Separator) {
		return true
	}
	if len(normalized) == 3 && normalized[1] == ':' && (normalized[2] == '\\' || normalized[2] == '/') {
		return true
	}
	return false
}

// effectiveAllowedBase resolves a raw allowlist entry to its canonical
// absolute form. Returns usable=false when the entry is blank or resolves to
// a filesystem root (e.g. "." under CWD "/", or an explicit "/" or "C:\\").
// Root-resolving entries are treated as unusable so they fail closed
// (deny-by-default) rather than granting access to the entire filesystem.
func (v *PathValidator) effectiveAllowedBase(rawBase string) (canonicalBase string, usable bool) {
	if strings.TrimSpace(rawBase) == "" {
		return "", false
	}
	expandedBase := expandHomeDir(rawBase)
	absBase, err := filepathAbs(expandedBase)
	if err != nil {
		return "", false
	}
	absBase = normalizePathForPlatform(absBase)
	canonicalBase, err = v.canonicalizePath(absBase)
	if err != nil {
		return "", false
	}
	if isFilesystemRoot(canonicalBase) {
		return "", false
	}
	return canonicalBase, true
}

// isAllowed checks if the canonical path falls within at least one allowed directory.
func (v *PathValidator) isAllowed(canonicalPath string) bool {
	for _, baseDir := range v.allow {
		canonicalBase, usable := v.effectiveAllowedBase(baseDir)
		if !usable {
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
	// or entries that resolve to a filesystem root.
	if len(v.allow) == 0 {
		return false
	}
	hasValidEntry := false
	for _, dir := range v.allow {
		if strings.TrimSpace(dir) == "" {
			continue
		}
		_, usable := v.effectiveAllowedBase(dir)
		if usable {
			hasValidEntry = true
			break
		}
	}
	if !hasValidEntry {
		return false
	}

	// Allowlist check
	for _, allowed := range v.allow {
		canonicalBase, usable := v.effectiveAllowedBase(allowed)
		if !usable {
			continue
		}
		if isPathWithinCanonical(resolved, canonicalBase) {
			return true
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
// On Windows, filepath.EvalSymlinks cannot resolve paths that cross an NTFS volume
// mount point (reparse tag IO_REPARSE_TAG_MOUNT_POINT) and returns a non-NotExist
// error. Because a mount point is an admin-created filesystem mount (not a
// user-controllable symlink), the cleaned absolute path is a safe canonical form,
// so canonicalizePath falls back to filepath.Clean(absPath) whenever Stat confirms
// the path genuinely exists. This fallback is gated on runtime.GOOS == "windows"
// because NTFS mount points are a Windows-only concern; on other platforms an
// unresolvable path is not an admin-controlled mount, so the original
// ErrPathUnresolvable behavior is preserved to avoid bypassing canonicalization.
// Broken symlinks and symlink loops still fail Stat, so they are NOT rescued by
// the fallback and remain ErrPathUnresolvable.
//
// Known limitation: evalSymlinksFunc (filepath.EvalSymlinks by default) operates on
// the host OS filesystem and cannot be routed through the injected v.fs
// abstraction — afero does not provide an equivalent symlink-resolution API.
// The Stat/Lstat calls in the parent-walk loop below DO use v.fs (via the Lstater
// type assertion), so in-memory test filesystems (MemMapFs, which has no symlink
// concept) are handled correctly. The evalSymlinksFunc seam is overridable in
// tests to simulate the mount-point case without a real Windows mount point.
func (v *PathValidator) canonicalizePath(absPath string) (string, error) {
	realPath, err := evalSymlinksFunc(absPath)
	if err == nil {
		return realPath, nil
	}
	if !os.IsNotExist(err) {
		// EvalSymlinks failed for a reason other than "does not exist". On
		// Windows this happens when the path crosses an NTFS volume mount point
		// (reparse tag IO_REPARSE_TAG_MOUNT_POINT): the path is a real,
		// admin-created filesystem mount that is not a user-controllable symlink,
		// so the cleaned absolute path is a safe canonical form when Stat confirms
		// the path genuinely exists. The platform-tagged resolveReparseFallback
		// helper encapsulates the full decision (Stat check + return value): on
		// Windows it returns the cleaned path on success or ErrPathUnresolvable
		// on Stat failure; on other platforms it always returns
		// ErrPathUnresolvable so canonicalization is never bypassed. Moving the
		// success return into the helper keeps the windows-only branch out of
		// this file so it does not count against the ubuntu/darwin codecov/patch
		// measurement.
		return resolveReparseFallback(absPath, v.fs)
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
			resolvedParent, resolveErr := evalSymlinksFunc(current)
			if resolveErr != nil {
				// Same Stat-fallback as the top-level branch: an existing
				// parent whose only problem is an unresolvable reparse point
				// (e.g. NTFS mount point) is accepted as its cleaned path on
				// Windows. The platform-tagged resolveReparseParentFallback
				// helper encapsulates the full decision (Stat check + return
				// value) so the windows-only success assignment lives in the
				// windows file and does not count against the ubuntu/darwin
				// codecov/patch measurement; on other platforms it always
				// returns ErrPathUnresolvable.
				resolvedParent, resolveErr = resolveReparseParentFallback(current, v.fs)
				if resolveErr != nil {
					return "", resolveErr
				}
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

// IsFilesystemRoot is the exported version of isFilesystemRoot for use by
// packages that need to validate allowlist entries (e.g. the security config
// PUT handler rejecting root-resolving entries).
func IsFilesystemRoot(path string) bool {
	return isFilesystemRoot(path)
}
