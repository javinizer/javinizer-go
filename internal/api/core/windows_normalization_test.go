package core

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
)

// ---------------------------------------------------------------------------
// isUNCPath — no runtime.GOOS guard, fully testable on all platforms
// ---------------------------------------------------------------------------

func TestIsUNCPath_StandardUNC(t *testing.T) {
	assert.True(t, isUNCPath(`\\server\share`))
	assert.True(t, isUNCPath(`\\server\share\folder`))
}

func TestIsUNCPath_ExtendedUNCPrefix(t *testing.T) {
	assert.True(t, isUNCPath(`\\?\UNC\server\share`))
	assert.True(t, isUNCPath(`\\?\unc\server\share`)) // lowercase
	assert.True(t, isUNCPath(`\\?\Unc\server\share`)) // mixed case
}

func TestIsUNCPath_NTNamespaceUNC(t *testing.T) {
	assert.True(t, isUNCPath(`\??\UNC\server\share`))
	assert.True(t, isUNCPath(`\??\unc\server\share`)) // lowercase
}

func TestIsUNCPath_DeviceNamespaceUNC(t *testing.T) {
	assert.True(t, isUNCPath(`\\.\UNC\server\share`))
	assert.True(t, isUNCPath(`\\.\unc\server\share`)) // lowercase
}

func TestIsUNCPath_NonUNCPaths(t *testing.T) {
	assert.False(t, isUNCPath(`C:\Videos`))
	assert.False(t, isUNCPath(`Videos`))
	assert.False(t, isUNCPath(`/unix/path`))
	assert.False(t, isUNCPath(``))         // empty
	assert.False(t, isUNCPath(`\`))        // single backslash
	assert.True(t, isUNCPath(`\\`))        // double backslash triggers standard UNC check
	assert.False(t, isUNCPath(`//server`)) // forward slashes
	assert.True(t, isUNCPath(`\\a`))       // starts with \\ so it's a standard UNC
}

func TestIsUNCPath_NormalizationEdgeCases(t *testing.T) {
	// Exactly 2 chars: "\\"
	assert.False(t, isUNCPath(`ab`)) // no backslash prefix
	// Paths starting with single backslash
	assert.False(t, isUNCPath(`\server\share`))
	// Extended prefixes at exactly length 7
	assert.True(t, isUNCPath(`\\?\UNC`)) // exactly 7 chars, but no trailing separator
	assert.True(t, isUNCPath(`\\.\UNC`)) // exactly 7 chars
	assert.True(t, isUNCPath(`\??\UNC`)) // exactly 7 chars
}

// ---------------------------------------------------------------------------
// isReservedDeviceName — has runtime.GOOS guard; on non-Windows returns false
// ---------------------------------------------------------------------------

func TestIsReservedDeviceName_NonWindowsAlwaysFalse(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows-specific behavior tested separately")
	}
	// On non-Windows, all reserved names return false
	reservedNames := []string{"CON", "PRN", "AUX", "NUL", "COM1", "LPT9"}
	for _, name := range reservedNames {
		assert.False(t, isReservedDeviceName(name), "isReservedDeviceName(%q) should be false on non-Windows", name)
	}
	assert.False(t, isReservedDeviceName(""))
}

// ---------------------------------------------------------------------------
// stripTrailingChars — has runtime.GOOS guard; on non-Windows returns input
// ---------------------------------------------------------------------------

func TestStripTrailingChars_NonWindowsPassthrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows-specific behavior tested separately")
	}
	// On non-Windows, all inputs are returned unchanged
	inputs := []string{
		`C:\Videos.`,
		`C:\Videos `,
		`C:\Videos. .`,
		``,
		`no-trailing`,
	}
	for _, input := range inputs {
		assert.Equal(t, input, stripTrailingChars(input), "stripTrailingChars(%q) should be identity on non-Windows", input)
	}
}

// ---------------------------------------------------------------------------
// normalizeWindowsPath — has runtime.GOOS guard; on non-Windows returns input
// This function had NO tests at all before.
// ---------------------------------------------------------------------------

func TestNormalizeWindowsPath_NonWindowsPassthrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows-specific behavior tested separately")
	}
	// On non-Windows, all inputs are returned unchanged
	inputs := []string{
		`C:\Videos`,
		`\\?\C:\Videos`,
		`\\?\UNC\server\share`,
		`\??\C:\Windows`,
		`\??\UNC\server\share`,
		`\\.\C:\Windows`,
		`\\.\UNC\server\share`,
		`relative/path`,
		``,
	}
	for _, input := range inputs {
		assert.Equal(t, input, normalizeWindowsPath(input), "normalizeWindowsPath(%q) should be identity on non-Windows", input)
	}
}

// ---------------------------------------------------------------------------
// normalizeUNCPath — has runtime.GOOS guard; on non-Windows returns (path, nil)
// ---------------------------------------------------------------------------

func TestNormalizeUNCPath_NonWindowsPassthrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows-specific behavior tested separately")
	}
	result, err := normalizeUNCPath(`\\server\share`, false, nil)
	assert.NoError(t, err)
	assert.Equal(t, `\\server\share`, result)

	result, err = normalizeUNCPath(`C:\Videos`, true, []string{"server"})
	assert.NoError(t, err)
	assert.Equal(t, `C:\Videos`, result)
}

// ---------------------------------------------------------------------------
// normalizePathForPlatform — has runtime.GOOS guard; on non-Windows returns input
// ---------------------------------------------------------------------------

func TestNormalizePathForPlatform_NonWindowsPassthrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows-specific behavior tested separately")
	}
	inputs := []string{
		`/unix/path`,
		`/some/other/path`,
		``,
	}
	for _, input := range inputs {
		assert.Equal(t, input, normalizePathForPlatform(input), "normalizePathForPlatform(%q) should be identity on non-Windows", input)
	}
}

// ---------------------------------------------------------------------------
// resolveShortPathName — build-tag guarded; on non-Windows returns input
// ---------------------------------------------------------------------------

func TestResolveShortPathName_NonWindowsPassthrough(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows-specific behavior tested separately")
	}
	assert.Equal(t, "/some/path", resolveShortPathName("/some/path"))
	assert.Equal(t, "", resolveShortPathName(""))
}
