package batch

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
)

const (
	// minScanTimeout is the minimum timeout duration for directory scans.
	// This prevents immediate context cancellation when ScanTimeoutSeconds is 0 or negative.
	minScanTimeout = 5 * time.Second
)

// isDirAllowed checks if a directory is allowed based on API security settings.
// It enforces both denied (blocklist) and allowed (allowlist) directory rules.
// Also enforces the built-in denylist to match behavior of validateScanPath.
func isDirAllowed(dir string, allow, deny []string) bool {
	// Expand home directory first
	expandedDir := core.ExpandHomeDir(dir)
	d := filepath.Clean(expandedDir)

	// Resolve symlinks for the checked directory (security: prevent symlink traversal)
	absPath, err := filepath.Abs(d)
	if err != nil {
		// Fail closed: deny access if absolute path resolution fails
		return false
	}
	resolved, err := canonicalizePath(absPath)
	if err != nil {
		// Fail closed: deny access if canonicalization fails
		return false
	}

	// Get built-in denied directories (system directories that should never be accessed)
	builtInDenied := core.GetDeniedDirectories()

	// Check built-in denylist first (with symlink resolution)
	for _, blocked := range builtInDenied {
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

	// Check config-provided denied directories (with home expansion and symlink resolution)
	for _, blocked := range deny {
		expandedBlocked := core.ExpandHomeDir(blocked)
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

	// If no allow list specified, deny by default (secure by default)
	// This matches the behavior of validateNFOPath and prevents unrestricted filesystem access
	if len(allow) == 0 {
		return false
	}

	// Check if directory is in allow list (with home expansion and symlink resolution)
	for _, allowed := range allow {
		expandedAllowed := core.ExpandHomeDir(allowed)
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

// canonicalizePath resolves symlinks and canonicalizes non-existent child paths by
// resolving the nearest existing ancestor. This keeps path checks consistent across
// platforms where temp paths may include symlinked segments (e.g., /var -> /private/var on macOS).
func canonicalizePath(absPath string) (string, error) {
	realPath, err := filepath.EvalSymlinks(absPath)
	if err == nil {
		return realPath, nil
	}
	if !os.IsNotExist(err) {
		return "", err
	}

	// For non-existent paths, resolve the nearest existing parent and append missing segments.
	current := absPath
	missingSegments := make([]string, 0, 4)
	for {
		parent := filepath.Dir(current)
		if parent == current {
			// Should not happen in practice (root should exist), but fail open to absolute path fallback.
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
	if path == base {
		return true
	}

	rel, err := filepath.Rel(base, path)
	if err != nil {
		return false
	}

	return rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

// discoverSiblingPartsWithMetadata finds all multi-part files and returns match metadata.
// This preserves multipart info (IsMultiPart, PartNumber, PartSuffix) from the discovery phase
// so it's available when creating FileResults during scraping.
func discoverSiblingPartsWithMetadata(files []string, fileMatcher *matcher.Matcher, cfg *config.Config) ([]string, map[string]worker.FileMatchInfo) {
	if len(files) == 0 {
		return files, nil
	}

	// First, match all submitted files to understand what we have
	scan := scanner.NewScanner(afero.NewOsFs(), &cfg.Matching)
	seenPaths := make(map[string]bool)
	fileInfos := make([]scanner.FileInfo, 0, len(files))

	for _, filePath := range files {
		seenPaths[filePath] = true
		fileInfos = append(fileInfos, scanner.FileInfo{
			Path:      filePath,
			Name:      filepath.Base(filePath),
			Extension: filepath.Ext(filePath),
			Dir:       filepath.Dir(filePath),
		})
	}

	// Match submitted files to detect multi-part status
	submittedMatches := fileMatcher.Match(fileInfos)

	// Validate letter-based multipart patterns using directory context
	submittedMatches = matcher.ValidateMultipartInDirectory(submittedMatches)

	// Build metadata map from submitted files
	fileMatchInfo := make(map[string]worker.FileMatchInfo)
	for _, match := range submittedMatches {
		fileMatchInfo[match.File.Path] = worker.FileMatchInfo{
			MovieID:     match.ID,
			IsMultiPart: match.IsMultiPart,
			PartNumber:  match.PartNumber,
			PartSuffix:  match.PartSuffix,
		}
	}

	// Group submitted files by movie ID and check if any are multi-part
	movieIDsToProcess := make(map[string]bool)
	directoriesScanned := make(map[string]bool)

	for _, match := range submittedMatches {
		if match.IsMultiPart {
			movieIDsToProcess[match.ID] = true
			logging.Debugf("Detected multi-part file: %s (movie ID: %s, part: %d)",
				match.File.Name, match.ID, match.PartNumber)
		}
	}

	// If no multi-part files detected, return original list with metadata
	if len(movieIDsToProcess) == 0 {
		return files, fileMatchInfo
	}

	// Start with original files
	allFiles := make([]string, 0, len(files))
	allFiles = append(allFiles, files...)

	// Scan parent directories to find all siblings for multi-part movies
	for _, match := range submittedMatches {
		if !movieIDsToProcess[match.ID] {
			continue
		}

		dir := match.File.Dir
		if directoriesScanned[dir] {
			continue
		}
		directoriesScanned[dir] = true

		// Security: Check if directory is allowed before scanning
		if !isDirAllowed(dir, cfg.API.Security.AllowedDirectories, cfg.API.Security.DeniedDirectories) {
			logging.Debugf("Skipping auto-discovery in disallowed directory: %s", dir)
			continue
		}

		// Check if directory exists
		if _, err := os.Stat(dir); os.IsNotExist(err) {
			logging.Debugf("Directory does not exist: %s", dir)
			continue
		}

		// Create context with timeout
		timeout := time.Duration(cfg.API.Security.ScanTimeoutSeconds) * time.Second
		if timeout <= 0 {
			timeout = minScanTimeout
		}
		scanCtx, cancelScan := context.WithTimeout(context.Background(), timeout)

		// Scan the directory
		result, err := scan.ScanWithLimits(scanCtx, dir, cfg.API.Security.MaxFilesPerScan)
		cancelScan()
		if err != nil {
			logging.Debugf("Failed to scan directory %s: %v", dir, err)
			continue
		}

		// Match all files in the directory
		matchResults := fileMatcher.Match(result.Files)
		matchResults = matcher.ValidateMultipartInDirectory(matchResults)

		// Find siblings for the multi-part movies we're processing
		for _, dirMatch := range matchResults {
			if movieIDsToProcess[dirMatch.ID] && dirMatch.IsMultiPart {
				if !seenPaths[dirMatch.File.Path] {
					// Sanity check: Ensure file is actually in the scanned directory
					parent := filepath.Dir(dirMatch.File.Path)
					if filepath.Clean(parent) != filepath.Clean(dir) {
						logging.Warnf("Scanner returned file outside scanned directory: %s (expected: %s)", parent, dir)
						continue
					}

					seenPaths[dirMatch.File.Path] = true
					allFiles = append(allFiles, dirMatch.File.Path)
					logging.Infof("Auto-discovered multi-part sibling: %s (movie ID: %s, part: %d)",
						dirMatch.File.Name, dirMatch.ID, dirMatch.PartNumber)
				}
				// Add metadata for discovered files too
				fileMatchInfo[dirMatch.File.Path] = worker.FileMatchInfo{
					MovieID:     dirMatch.ID,
					IsMultiPart: dirMatch.IsMultiPart,
					PartNumber:  dirMatch.PartNumber,
					PartSuffix:  dirMatch.PartSuffix,
				}
			}
		}
	}

	return allFiles, fileMatchInfo
}
