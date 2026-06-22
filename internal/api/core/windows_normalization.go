package core

import (
	"path/filepath"
	"runtime"
	"strings"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
)

const osWindows = "windows"

// currentOS allows tests to override the OS check for Windows-specific logic.
// In production, it mirrors runtime.GOOS. Tests can set it to "windows" to
// exercise the Windows code paths on non-Windows platforms.
var currentOS = runtime.GOOS

var windowsReservedNames = []string{
	"CON", "PRN", "AUX", "NUL",
	"COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
	"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9",
}

func isReservedDeviceName(component string) bool {
	if currentOS != osWindows {
		return false
	}

	if component == "" {
		return false
	}

	upper := strings.ToUpper(component)

	// Strip drive letter prefix (e.g., "C:CON" -> "CON") to prevent device access bypass
	if len(upper) >= 2 && upper[1] == ':' && ((upper[0] >= 'A' && upper[0] <= 'Z') || (upper[0] >= '0' && upper[0] <= '9')) {
		upper = upper[2:]
	}

	namePart := strings.SplitN(upper, ".", 2)[0]
	base := strings.TrimRight(namePart, " ")

	for _, reserved := range windowsReservedNames {
		if base == reserved {
			return true
		}
	}
	return false
}

func stripTrailingChars(path string) string {
	if currentOS != osWindows {
		return path
	}

	if path == "" {
		return path
	}

	parts := strings.Split(path, string(filepath.Separator))
	for i, part := range parts {
		parts[i] = strings.TrimRight(part, ". ")
	}
	return strings.Join(parts, string(filepath.Separator))
}

func isUNCPath(path string) bool {
	if len(path) < 2 {
		return false
	}

	// Standard UNC path: \\server\share
	if path[0] == '\\' && path[1] == '\\' {
		return true
	}

	// Extended-length UNC: \\?\UNC\server\share
	if len(path) >= 7 && strings.ToLower(path[:7]) == `\\?\unc` {
		return true
	}

	// NT namespace UNC: \??\UNC\server\share
	if len(path) >= 7 && strings.ToLower(path[:7]) == `\??\unc` {
		return true
	}

	// Device namespace UNC: \\.\UNC\server\share
	if len(path) >= 7 && strings.ToLower(path[:7]) == `\\.\unc` {
		return true
	}

	return false
}

func normalizeUNCPath(path string, allowUNC bool, allowedUNCServers []string) (string, error) {
	if currentOS != osWindows {
		return path, nil
	}

	if !isUNCPath(path) {
		return path, nil
	}

	if !allowUNC {
		return "", apperrors.NewPathError(apperrors.ErrUNCPathBlocked, path)
	}

	normalized := normalizeWindowsPath(path)

	if strings.HasPrefix(normalized, `\\`) {
		serverEnd := strings.Index(normalized[2:], `\`)
		if serverEnd == -1 {
			return "", apperrors.NewPathError(apperrors.ErrUNCPathBlocked, path)
		}
		server := normalized[2 : 2+serverEnd]

		allowed := false
		for _, allowedServer := range allowedUNCServers {
			if strings.EqualFold(server, allowedServer) {
				allowed = true
				break
			}
		}
		if !allowed {
			return "", apperrors.NewPathError(apperrors.ErrUNCPathBlocked, path)
		}
	}

	return normalized, nil
}

// normalizeWindowsPath removes Windows extended-path prefixes (\\?\, \\?\UNC\, \??\, \??\UNC\, \\.\, \\.\UNC\)
// to prevent denylist bypass via extended-length path syntax.
// Uses case-insensitive comparison to handle mixed-case prefixes (e.g., \\?\Unc\).
// Handles Win32 namespace (\\?\), NT namespace (\??\), and device namespace (\\.\) aliases.
func normalizeWindowsPath(path string) string {
	if currentOS != osWindows {
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

func normalizePathForPlatform(path string) string {
	if currentOS != osWindows {
		return path
	}

	path = normalizeWindowsPath(path)
	path = stripTrailingChars(path)
	path = resolveShortPathName(path)
	return path
}
