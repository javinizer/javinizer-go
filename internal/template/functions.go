package template

import (
	"strings"
	"unicode"
)

// SanitizeFilename removes or replaces characters that are invalid in filenames
// This is a standalone function for use by callers
func SanitizeFilename(s string) string {
	// Replace invalid characters with safe alternatives
	replacements := map[rune]string{
		'/':  "-",
		'\\': "-",
		':':  " -",
		'*':  "",
		'?':  "",
		'"':  "'",
		'<':  "(",
		'>':  ")",
		'|':  "-",
	}

	var result strings.Builder
	for _, r := range s {
		if replacement, exists := replacements[r]; exists {
			result.WriteString(replacement)
		} else if unicode.IsPrint(r) {
			result.WriteRune(r)
		}
	}

	// Trim spaces and dots from ends (Windows doesn't like these)
	trimmed := strings.Trim(result.String(), " .")

	// Collapse multiple spaces
	for strings.Contains(trimmed, "  ") {
		trimmed = strings.ReplaceAll(trimmed, "  ", " ")
	}

	return trimmed
}

// SanitizeFolderPath sanitizes a folder name by replacing invalid filesystem characters
func SanitizeFolderPath(s string) string {
	// Replace invalid characters including forward slashes to prevent unintended subdirectories
	replacements := map[rune]string{
		'\\': "_", // Convert backslash to underscore
		'/':  "_", // Convert forward slash to underscore (prevents accidental subdirectories)
		':':  " -",
		'*':  "",
		'?':  "",
		'"':  "'",
		'<':  "(",
		'>':  ")",
		'|':  "-",
	}

	var result strings.Builder
	for _, r := range s {
		if replacement, exists := replacements[r]; exists {
			result.WriteString(replacement)
		} else if unicode.IsPrint(r) {
			result.WriteRune(r)
		}
	}

	// Windows/SMB can mangle names that end with a period.
	// Match original Javinizer behavior by trimming only trailing dots/spaces.
	return strings.TrimRight(result.String(), " .")
}
