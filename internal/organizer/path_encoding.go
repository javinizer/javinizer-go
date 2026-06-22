package organizer

import (
	"path/filepath"
	"strings"
)

// PathEncoding specifies how paths in an OrganizePlan should be encoded
// for the consumer. Different source path characteristics (POSIX, Windows
// local, Windows UNC) require different path representations in the output.
type PathEncoding int

const (
	// PathEncodingPOSIX leaves paths as-is (POSIX/Unix style).
	PathEncodingPOSIX PathEncoding = iota
	// PathEncodingWindows converts forward slashes to backslashes for
	// Windows local paths (e.g. C:\Users\...).
	PathEncodingWindows
	// PathEncodingUNC reconstructs UNC paths from the plan using the
	// original source and destination paths.
	PathEncodingUNC
)

// EncodedPaths holds the platform-appropriate path representations derived
// from an OrganizePlan. Populated by OrganizePlan.EncodePaths().
type EncodedPaths struct {
	// TargetPath is the full target file path with platform encoding.
	TargetPath string
	// TargetDir is the target directory with platform encoding.
	TargetDir string
	// SourcePath is the source path with platform encoding.
	SourcePath string
	// SubfolderPath is the subfolder path with platform encoding.
	SubfolderPath string
}

// PathEncodingInfo carries the information needed to encode plan paths
// for a specific source path context.
type PathEncodingInfo struct {
	// Encoding selects the path encoding strategy.
	Encoding PathEncoding
	// OriginalSource is the original source path (before POSIX conversion).
	// Used only for UNC encoding.
	OriginalSource string
	// Destination is the original destination path (before POSIX conversion).
	// Used only for UNC encoding.
	Destination string
}

// DetectPathEncodingInfo inspects a source path and determines the
// appropriate path encoding strategy. Returns PathEncodingInfo with
// POSIX encoding for empty or non-Windows paths.
func DetectPathEncodingInfo(sourcePath string, destination string) PathEncodingInfo {
	if sourcePath == "" {
		return PathEncodingInfo{Encoding: PathEncodingPOSIX}
	}
	if strings.HasPrefix(sourcePath, `\\`) {
		return PathEncodingInfo{
			Encoding:       PathEncodingUNC,
			OriginalSource: sourcePath,
			Destination:    destination,
		}
	}
	if len(sourcePath) >= 2 && sourcePath[1] == ':' {
		return PathEncodingInfo{Encoding: PathEncodingWindows}
	}
	return PathEncodingInfo{Encoding: PathEncodingPOSIX}
}

// PrepareMatchPath transforms a source file path before passing it to
// the organizer's Plan method. For UNC paths, this converts to POSIX
// so the organizer can work with it. For other paths, returns as-is.
func (info PathEncodingInfo) PrepareMatchPath(path string) string {
	if info.Encoding == PathEncodingUNC {
		return toPosixPath(path)
	}
	return path
}

// PrepareDestination transforms a destination path before passing it to
// the organizer's Plan method. For UNC paths, this converts to POSIX.
func (info PathEncodingInfo) PrepareDestination(dest string) string {
	if info.Encoding == PathEncodingUNC {
		return toPosixPath(dest)
	}
	return dest
}

// EncodePaths produces platform-encoded versions of the plan's key paths.
// Call this after Plan() returns to get the correct path representation
// for the consumer (preview display, API response, etc.).
func (p *OrganizePlan) EncodePaths(info PathEncodingInfo) EncodedPaths {
	switch info.Encoding {
	case PathEncodingUNC:
		return EncodedPaths{
			TargetPath:    rebuildUNCPath(p, info.OriginalSource, info.Destination),
			TargetDir:     rebuildUNCTargetDir(p, info.OriginalSource, info.Destination),
			SourcePath:    info.OriginalSource,
			SubfolderPath: toBackslashPath(p.SubfolderPath),
		}
	case PathEncodingWindows:
		return EncodedPaths{
			TargetPath:    toBackslashPath(p.TargetPath),
			TargetDir:     toBackslashPath(p.TargetDir),
			SourcePath:    toBackslashPath(p.SourcePath),
			SubfolderPath: toBackslashPath(p.SubfolderPath),
		}
	default: // POSIX
		return EncodedPaths{
			TargetPath:    p.TargetPath,
			TargetDir:     p.TargetDir,
			SourcePath:    p.SourcePath,
			SubfolderPath: p.SubfolderPath,
		}
	}
}

// rebuildUNCPath reconstructs a UNC target path from the organize plan.
func rebuildUNCPath(plan *OrganizePlan, originalSource string, destination string) string {
	targetDir := rebuildUNCTargetDir(plan, originalSource, destination)
	return joinPathUNC(targetDir, plan.TargetFile)
}

// rebuildUNCTargetDir reconstructs a UNC target directory from the organize plan.
func rebuildUNCTargetDir(plan *OrganizePlan, originalSource string, destination string) string {
	sourceDir := pathDir(originalSource)

	if plan.PreserveSourcePath {
		return sourceDir
	}

	if plan.RenameFolder {
		if plan.InPlace {
			parentDir := pathDir(sourceDir)
			if plan.FolderName != "" {
				return joinPathUNC(parentDir, plan.FolderName)
			}
			return sourceDir
		}
	}

	pathBase := destination
	if pathBase == "" {
		pathBase = sourceDir
	}

	if plan.SubfolderPath != "" {
		parts := strings.Split(plan.SubfolderPath, `/`)
		for _, sp := range parts {
			clean := strings.Trim(sp, `/\`)
			if clean != "" {
				pathBase = joinPathUNC(pathBase, clean)
			}
		}
	}

	if plan.FolderName != "" {
		return joinPathUNC(pathBase, plan.FolderName)
	}

	return pathBase
}

// toPosixPath converts Windows-style backslash paths to POSIX forward slashes.
func toPosixPath(path string) string {
	if !IsWindowsPathLike(path) && !strings.Contains(path, `\`) {
		return path
	}
	return strings.ReplaceAll(path, `\`, `/`)
}

// toBackslashPath converts forward slashes to backslashes.
func toBackslashPath(path string) string {
	if path == "" {
		return ""
	}
	return strings.ReplaceAll(path, `/`, `\`)
}

// IsWindowsPathLike returns true if the path looks like a Windows path
// (drive letter or UNC prefix).
// IsWindowsPathLike returns true if the path looks like a Windows path
// (drive letter or UNC prefix).
func IsWindowsPathLike(path string) bool {
	return (len(path) >= 2 && path[1] == ':') || strings.HasPrefix(path, `\\`)
}

// JoinPath joins path elements, detecting Windows-style base paths
// and using backslash joining when appropriate. This is the exported
// equivalent of joinPathUNC, used by consumers (e.g., the preview
// orchestrator) that need platform-aware path joining without
// duplicating the Windows/POSIX detection logic.
func JoinPath(base string, elems ...string) string {
	return joinPathUNC(base, elems...)
}

// joinPathUNC joins path elements, detecting Windows-style base paths
// and using backslash joining when appropriate.
func joinPathUNC(base string, elems ...string) string {
	if base == "" {
		return filepath.Join(elems...)
	}

	windowsStyle := IsWindowsPathLike(base) || strings.Contains(base, `\`)
	if !windowsStyle {
		parts := make([]string, 0, len(elems)+1)
		parts = append(parts, base)
		parts = append(parts, elems...)
		return filepath.Join(parts...)
	}

	// Windows-style path joining using backslashes.
	var buf strings.Builder
	buf.WriteString(trimTrailingSeparator(base))
	for _, elem := range elems {
		elem = trimTrailingSeparator(elem)
		if elem == "" {
			continue
		}
		buf.WriteByte('\\')
		buf.WriteString(elem)
	}
	return buf.String()
}

// pathDir returns the directory component of a path, handling both
// POSIX and Windows-style paths including UNC shares.
func pathDir(path string) string {
	trimmed := trimTrailingSeparator(path)
	if trimmed == "" {
		return "."
	}

	idx := strings.LastIndexAny(trimmed, `/\`)
	if idx == -1 {
		return "."
	}

	switch {
	case idx == 0:
		return trimmed[:1]
	case idx == 2 && len(trimmed) >= 3 && trimmed[1] == ':' && (trimmed[2] == '\\' || trimmed[2] == '/'):
		return trimmed[:3]
	default:
		dir := trimmed[:idx]
		if isUNCPath(trimmed) {
			uncRoot := uncShareRoot(trimmed)
			if uncRoot != "" && len(dir) < len(uncRoot) {
				return uncRoot
			}
		}
		return dir
	}
}

func isUNCPath(path string) bool {
	return strings.HasPrefix(path, `\\`) || strings.HasPrefix(path, `//`)
}

func uncShareRoot(path string) string {
	if !isUNCPath(path) {
		return ""
	}
	rest := path[2:]
	idx := strings.IndexAny(rest, `/\`)
	if idx == -1 {
		return path
	}
	shareRest := rest[idx+1:]
	shareEnd := strings.IndexAny(shareRest, `/\`)
	if shareEnd == -1 {
		return path
	}
	return path[:2+idx+1+shareEnd]
}

// TrimTrailingSeparators removes trailing path separators (both / and \).
// Exported for use by consumers that need to normalize paths.
func TrimTrailingSeparators(path string) string {
	return trimTrailingSeparator(path)
}

func trimTrailingSeparator(path string) string {
	return strings.TrimRight(path, `/\`)
}
