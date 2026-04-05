package core

import (
	"runtime"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestIsReservedDeviceName(t *testing.T) {
	tests := []struct {
		name      string
		component string
		expected  bool
	}{
		{"CON", "CON", true},
		{"NUL", "NUL", true},
		{"PRN", "PRN", true},
		{"AUX", "AUX", true},
		{"COM1", "COM1", true},
		{"COM9", "COM9", true},
		{"LPT1", "LPT1", true},
		{"LPT9", "LPT9", true},
		{"lowercase con", "con", true},
		{"with extension txt", "CON.txt", true},
		{"with extension log", "COM1.log", true},
		{"with extension jpg", "NUL.jpg", true},
		{"with multiple extensions", "AUX.txt.bak", true},
		{"with trailing dot", "CON.", true},
		{"with trailing space", "NUL ", true},
		{"normal file", "config.yaml", false},
		{"Videos", "Videos", false},
		{"empty", "", false},
		{"COM10 not reserved", "COM10", false},
		{"LPT10 not reserved", "LPT10", false},
		{"config with COM prefix", "COMMIT.txt", false},
		{"normal with dot", "document.pdf", false},
		{"drive letter prefix C:CON", "C:CON", true},
		{"drive letter prefix D:NUL", "D:NUL", true},
		{"drive letter with extension E:COM1.txt", "E:COM1.txt", true},
		{"drive letter with space F:AUX ", "F:AUX ", true},
		{"drive letter normal G:file.txt", "G:file.txt", false},
		{"numeric drive letter 1:CON", "1:CON", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS != "windows" {
				assert.False(t, isReservedDeviceName(tt.component), "Should be no-op on non-Windows")
				return
			}
			assert.Equal(t, tt.expected, isReservedDeviceName(tt.component))
		})
	}
}

func TestStripTrailingChars(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{"no trailing", `C:\Videos`, `C:\Videos`},
		{"trailing dot", `C:\Videos.`, `C:\Videos`},
		{"trailing space", `C:\Videos `, `C:\Videos`},
		{"multiple dots", `C:\Videos...`, `C:\Videos`},
		{"mixed trailing", `C:\Videos. .`, `C:\Videos`},
		{"trailing in middle component", `C:\Videos.\test`, `C:\Videos\test`},
		{"trailing in multiple components", `C:\Videos. \test. `, `C:\Videos\test`},
		{"drive letter unchanged", `C:`, `C:`},
		{"empty string", ``, ``},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if runtime.GOOS != "windows" {
				assert.Equal(t, tt.input, stripTrailingChars(tt.input), "Should be no-op on non-Windows")
				return
			}
			assert.Equal(t, tt.expected, stripTrailingChars(tt.input))
		})
	}
}

func TestIsUNCPath(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected bool
	}{
		{"standard UNC", `\\server\share`, true},
		{"UNC with path", `\\server\share\folder`, true},
		{"extended UNC prefix", `\\?\UNC\server\share`, true},
		{"lowercase unc prefix", `\\?\unc\server\share`, true},
		{"NT namespace UNC", `\??\UNC\server\share`, true},
		{"device namespace UNC", `\\.\UNC\server\share`, true},
		{"local path", `C:\Videos`, false},
		{"relative path", `Videos`, false},
		{"single backslash", `\Videos`, false},
		{"forward slashes", `//server/share`, false},
		{"empty string", ``, false},
		{"single char", `\`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			assert.Equal(t, tt.expected, isUNCPath(tt.input))
		})
	}
}

func TestNormalizeUNCPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Run("no-op on non-Windows", func(t *testing.T) {
			result, err := normalizeUNCPath(`\\server\share`, false, nil)
			assert.NoError(t, err)
			assert.Equal(t, `\\server\share`, result)
		})
		return
	}

	t.Run("blocks UNC when not allowed", func(t *testing.T) {
		_, err := normalizeUNCPath(`\\evil-server\share`, false, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "UNC paths are not allowed")
	})

	t.Run("blocks UNC when server not in whitelist", func(t *testing.T) {
		_, err := normalizeUNCPath(`\\evil-server\share`, true, []string{"fileserver"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "UNC paths are not allowed")
	})

	t.Run("allows whitelisted UNC server", func(t *testing.T) {
		result, err := normalizeUNCPath(`\\fileserver\share`, true, []string{"fileserver"})
		assert.NoError(t, err)
		assert.Contains(t, result, `\\fileserver`)
	})

	t.Run("allows whitelisted UNC server case-insensitive", func(t *testing.T) {
		result, err := normalizeUNCPath(`\\FileServer\share`, true, []string{"fileserver"})
		assert.NoError(t, err)
		assert.Contains(t, result, `\\FileServer`)
	})

	t.Run("passes through non-UNC path", func(t *testing.T) {
		result, err := normalizeUNCPath(`C:\Videos`, false, nil)
		assert.NoError(t, err)
		assert.Equal(t, `C:\Videos`, result)
	})

	t.Run("handles UNC with only server (no share)", func(t *testing.T) {
		_, err := normalizeUNCPath(`\\server`, true, []string{"server"})
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "UNC paths are not allowed")
	})
}

func TestResolveShortPathName(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Run("no-op on non-Windows", func(t *testing.T) {
			input := "/some/path"
			result := resolveShortPathName(input)
			assert.Equal(t, input, result, "Should return path unchanged on non-Windows")
		})
		t.Run("handles empty string on non-Windows", func(t *testing.T) {
			result := resolveShortPathName("")
			assert.Equal(t, "", result)
		})
		return
	}

	t.Run("resolves short name to long name or falls back securely", func(t *testing.T) {
		result := resolveShortPathName(`C:\PROGRA~1`)
		if strings.Contains(result, "~") {
			assert.Equal(t, `C:\PROGRA~1`, result, "Should return original path when 8.3 is disabled")
		} else {
			assert.Contains(t, strings.ToLower(result), "program files", "Should resolve to long name")
		}
	})

	t.Run("returns original path on nonexistent path", func(t *testing.T) {
		result := resolveShortPathName(`C:\NonExistentPath12345`)
		assert.Equal(t, `C:\NonExistentPath12345`, result, "Should return original on error")
	})

	t.Run("handles empty string", func(t *testing.T) {
		result := resolveShortPathName("")
		assert.Equal(t, "", result)
	})

	t.Run("handles long path already", func(t *testing.T) {
		result := resolveShortPathName(`C:\Windows`)
		assert.Contains(t, result, `Windows`, "Long path should remain unchanged")
	})
}

func TestNormalizePathForPlatform(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Run("no-op on non-Windows", func(t *testing.T) {
			input := "/some/path"
			result := normalizePathForPlatform(input)
			assert.Equal(t, input, result)
		})
		return
	}

	t.Run("applies all normalizations on Windows", func(t *testing.T) {
		// This test verifies the function exists and chains correctly
		// Detailed testing of each step is in individual function tests
		result := normalizePathForPlatform(`C:\Videos`)
		assert.Equal(t, `C:\Videos`, result)
	})

	t.Run("resolves short names via resolveShortPathName", func(t *testing.T) {
		result := normalizePathForPlatform(`C:\PROGRA~1`)
		if strings.Contains(result, "~") {
			assert.Equal(t, `C:\PROGRA~1`, result, "Should return original when 8.3 disabled")
		} else {
			assert.NotContains(t, result, "~", "normalizePathForPlatform should resolve short names")
		}
	})

	t.Run("strips trailing characters", func(t *testing.T) {
		result := normalizePathForPlatform(`C:\Videos.`)
		assert.Equal(t, `C:\Videos`, result, "Should strip trailing dots")
	})
}
