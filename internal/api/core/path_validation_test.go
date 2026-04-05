package core

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestValidateScanPath(t *testing.T) {
	// Create temp directory for testing
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	tests := []struct {
		name          string
		inputPath     string
		securityCfg   *config.SecurityConfig
		expectedError bool
		errorContains string
	}{
		{
			name:      "valid path within allowed directory",
			inputPath: allowedDir,
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: false,
		},
		{
			name:      "valid path - with allowlist",
			inputPath: tempDir,
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: false,
		},
		{
			name:      "path traversal attempt with ../",
			inputPath: filepath.Join(allowedDir, "../etc/passwd"),
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{allowedDir},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: true,
			errorContains: "does not exist", // Path validation happens before allowlist check
		},
		{
			name:      "absolute path outside allowed directory",
			inputPath: tempDir,
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{allowedDir},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: true,
			errorContains: "outside allowed directories",
		},
		{
			name:      "path with multiple ../ sequences",
			inputPath: filepath.Join(tempDir, "../../etc"),
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: true,
			errorContains: "does not exist", // Path validation happens before allowlist check
		},
		{
			name:      "nonexistent path",
			inputPath: "/nonexistent/path/12345",
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{"/"},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: true,
			errorContains: "does not exist",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validPath, err := validateScanPath(tt.inputPath, tt.securityCfg)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, validPath)
				// Verify returned path is absolute and clean
				assert.True(t, filepath.IsAbs(validPath))
			}
		})
	}
}

func TestValidateScanPath_SystemDirectories(t *testing.T) {
	systemDirs := []string{
		"/proc",
		"/sys",
		"/dev",
	}

	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{"/"},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	for _, dir := range systemDirs {
		t.Run("blocks "+dir, func(t *testing.T) {
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				t.Skip("System directory doesn't exist on this platform")
			}

			_, err := validateScanPath(dir, securityCfg)
			require.Error(t, err)
			assert.Contains(t, err.Error(), "system directory")
		})
	}
}

func TestValidateScanPath_FileVsDirectory(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "testfile.txt")
	require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	t.Run("rejects file path", func(t *testing.T) {
		_, err := validateScanPath(tempFile, securityCfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not a directory")
	})

	t.Run("accepts directory path", func(t *testing.T) {
		validPath, err := validateScanPath(tempDir, securityCfg)
		assert.NoError(t, err)
		expectedPath, _ := filepath.EvalSymlinks(tempDir)
		assert.Equal(t, expectedPath, validPath)
	})
}

func TestGetDeniedDirectories(t *testing.T) {
	denied := getDeniedDirectories()

	assert.Contains(t, denied, "/proc")
	assert.Contains(t, denied, "/sys")
	assert.Contains(t, denied, "/dev")

	assert.NotContains(t, denied, "/etc")
	assert.NotContains(t, denied, "/var/log")
	assert.NotContains(t, denied, "/usr/bin")
}

func BenchmarkValidateScanPath(b *testing.B) {
	tempDir := b.TempDir()
	testPath := tempDir
	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = validateScanPath(testPath, securityCfg)
	}
}

func TestValidateScanPath_EdgeCases(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name          string
		inputPath     string
		securityCfg   *config.SecurityConfig
		expectedError bool
		errorContains string
	}{
		{
			name:      "empty path defaults to current directory",
			inputPath: "",
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{"/"},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: false,
		},
		{
			name:      "path with trailing slash",
			inputPath: tempDir + "/",
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: false,
		},
		{
			name:      "path with ./ prefix",
			inputPath: "./",
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{"/"},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: false,
		},
		{
			name:      "relative path cleaned to absolute",
			inputPath: tempDir,
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{tempDir},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validPath, err := validateScanPath(tt.inputPath, tt.securityCfg)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.True(t, filepath.IsAbs(validPath))
			}
		})
	}
}

func TestNormalizeWindowsPath(t *testing.T) {
	// Only run on Windows or skip if testing Windows-specific behavior
	if runtime.GOOS != "windows" {
		t.Skip("Skipping Windows-specific tests on non-Windows platform")
	}

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		// Win32 namespace (\\?\) tests
		{
			name:     "Win32 extended path - C drive",
			input:    `\\?\C:\Windows\System32`,
			expected: `C:\Windows\System32`,
		},
		{
			name:     "Win32 extended path - D drive",
			input:    `\\?\D:\Data\Files`,
			expected: `D:\Data\Files`,
		},
		{
			name:     "Win32 UNC path",
			input:    `\\?\UNC\server\share\folder`,
			expected: `\\server\share\folder`,
		},
		{
			name:     "Win32 extended path - lowercase",
			input:    `\\?\c:\windows`,
			expected: `c:\windows`,
		},
		{
			name:     "Win32 UNC - lowercase",
			input:    `\\?\unc\server\share`,
			expected: `\\server\share`,
		},
		// Mixed case variants
		{
			name:     "Win32 extended path - mixed case prefix",
			input:    `\\?\C:\Windows`,
			expected: `C:\Windows`,
		},
		{
			name:     "Win32 UNC - Unc",
			input:    `\\?\Unc\server\share`,
			expected: `\\server\share`,
		},
		{
			name:     "Win32 UNC - uNc",
			input:    `\\?\uNc\server\share`,
			expected: `\\server\share`,
		},
		{
			name:     "Win32 UNC - UnC",
			input:    `\\?\UnC\server\share`,
			expected: `\\server\share`,
		},

		// NT namespace (\??\) tests
		{
			name:     "NT namespace path - C drive",
			input:    `\??\C:\Windows\System32`,
			expected: `C:\Windows\System32`,
		},
		{
			name:     "NT namespace path - D drive",
			input:    `\??\D:\Data`,
			expected: `D:\Data`,
		},
		{
			name:     "NT namespace UNC",
			input:    `\??\UNC\server\share\folder`,
			expected: `\\server\share\folder`,
		},
		{
			name:     "NT namespace - lowercase",
			input:    `\??\c:\windows`,
			expected: `c:\windows`,
		},
		{
			name:     "NT namespace UNC - lowercase",
			input:    `\??\unc\server\share`,
			expected: `\\server\share`,
		},
		// Mixed case variants
		{
			name:     "NT namespace UNC - Unc",
			input:    `\??\Unc\server\share`,
			expected: `\\server\share`,
		},
		{
			name:     "NT namespace UNC - uNc",
			input:    `\??\uNc\server\share`,
			expected: `\\server\share`,
		},

		// Device namespace (\\.\) tests
		{
			name:     "Device namespace - C drive",
			input:    `\\.\C:\Windows\System32`,
			expected: `C:\Windows\System32`,
		},
		{
			name:     "Device namespace - D drive",
			input:    `\\.\D:\Data`,
			expected: `D:\Data`,
		},
		{
			name:     "Device namespace UNC",
			input:    `\\.\UNC\server\share\folder`,
			expected: `\\server\share\folder`,
		},
		{
			name:     "Device namespace - lowercase",
			input:    `\\.\c:\windows`,
			expected: `c:\windows`,
		},
		{
			name:     "Device namespace UNC - lowercase",
			input:    `\\.\unc\server\share`,
			expected: `\\server\share`,
		},
		// Mixed case variants
		{
			name:     "Device namespace UNC - Unc",
			input:    `\\.\Unc\server\share`,
			expected: `\\server\share`,
		},
		{
			name:     "Device namespace UNC - uNc",
			input:    `\\.\uNc\server\share`,
			expected: `\\server\share`,
		},

		// Regular paths (no normalization needed)
		{
			name:     "Regular path - absolute",
			input:    `C:\Windows\System32`,
			expected: `C:\Windows\System32`,
		},
		{
			name:     "Regular UNC path",
			input:    `\\server\share\folder`,
			expected: `\\server\share\folder`,
		},
		{
			name:     "Relative path",
			input:    `folder\file.txt`,
			expected: `folder\file.txt`,
		},

		// Edge cases
		{
			name:     "Empty string",
			input:    "",
			expected: "",
		},
		{
			name:     "Just prefix - Win32",
			input:    `\\?\`,
			expected: ``,
		},
		{
			name:     "Just prefix - NT",
			input:    `\??\`,
			expected: ``,
		},
		{
			name:     "Just prefix - Device",
			input:    `\\.\`,
			expected: ``,
		},
		{
			name:     "Short path with prefix",
			input:    `\\?\C:`,
			expected: `C:`,
		},
		{
			name:     "UNC with only server",
			input:    `\\?\UNC\server`,
			expected: `\\server`,
		},

		// Malformed inputs (should pass through unchanged or strip prefix)
		{
			name:     "Prefix without path",
			input:    `\\?\UNC`,
			expected: `\\?\UNC`, // Too short, no match
		},
		{
			name:     "Prefix with slash only",
			input:    `\\?\UNC\`,
			expected: `\\`, // Strips prefix, keeps UNC \\ prefix
		},
		{
			name:     "Mixed prefix styles",
			input:    `\\?\??\C:\Windows`, // Malformed, but should strip first prefix
			expected: `??\C:\Windows`,
		},

		// Special characters in path
		{
			name:     "Path with spaces",
			input:    `\\?\C:\Program Files\App`,
			expected: `C:\Program Files\App`,
		},
		{
			name:     "Path with special chars",
			input:    `\\?\C:\Users\name@domain.com\Documents`,
			expected: `C:\Users\name@domain.com\Documents`,
		},

		// Volume GUIDs (should strip prefix)
		{
			name:     "Volume GUID path - Win32",
			input:    `\\?\Volume{12345678-1234-1234-1234-123456789012}\Windows`,
			expected: `Volume{12345678-1234-1234-1234-123456789012}\Windows`,
		},
		{
			name:     "Volume GUID path - NT namespace",
			input:    `\??\Volume{12345678-1234-1234-1234-123456789012}\Windows`,
			expected: `Volume{12345678-1234-1234-1234-123456789012}\Windows`,
		},
		{
			name:     "Volume GUID path - Device namespace",
			input:    `\\.\Volume{12345678-1234-1234-1234-123456789012}\Windows`,
			expected: `Volume{12345678-1234-1234-1234-123456789012}\Windows`,
		},

		// GLOBALROOT and shadow copy volumes (common attack vectors)
		{
			name:     "GLOBALROOT shadow copy - Win32",
			input:    `\\?\GLOBALROOT\Device\HarddiskVolumeShadowCopy1\Windows`,
			expected: `GLOBALROOT\Device\HarddiskVolumeShadowCopy1\Windows`,
		},
		{
			name:     "GLOBALROOT shadow copy - NT namespace",
			input:    `\??\GLOBALROOT\Device\HarddiskVolumeShadowCopy1\Windows`,
			expected: `GLOBALROOT\Device\HarddiskVolumeShadowCopy1\Windows`,
		},
		{
			name:     "GLOBALROOT shadow copy - Device namespace",
			input:    `\\.\GLOBALROOT\Device\HarddiskVolumeShadowCopy1\Windows`,
			expected: `GLOBALROOT\Device\HarddiskVolumeShadowCopy1\Windows`,
		},
		{
			name:     "GLOBALROOT with mixed case - Win32",
			input:    `\\?\GlobalRoot\Device\HarddiskVolumeShadowCopy2\System`,
			expected: `GlobalRoot\Device\HarddiskVolumeShadowCopy2\System`,
		},

		// Device namespace - named pipes, mailslots, reserved devices
		// Test all three namespace prefixes for comprehensive coverage
		{
			name:     "Named pipe - Device namespace",
			input:    `\\.\pipe\mypipe`,
			expected: `pipe\mypipe`,
		},
		{
			name:     "Named pipe - Win32 namespace",
			input:    `\\?\pipe\mypipe`,
			expected: `pipe\mypipe`,
		},
		{
			name:     "Named pipe - NT namespace",
			input:    `\??\pipe\mypipe`,
			expected: `pipe\mypipe`,
		},
		{
			name:     "Mailslot - Device namespace",
			input:    `\\.\mailslot\myslot`,
			expected: `mailslot\myslot`,
		},
		{
			name:     "Mailslot - Win32 namespace",
			input:    `\\?\mailslot\myslot`,
			expected: `mailslot\myslot`,
		},
		{
			name:     "Mailslot - NT namespace",
			input:    `\??\mailslot\myslot`,
			expected: `mailslot\myslot`,
		},
		{
			name:     "COM port device - Device namespace",
			input:    `\\.\COM1`,
			expected: `COM1`,
		},
		{
			name:     "COM port device - Win32 namespace",
			input:    `\\?\COM1`,
			expected: `COM1`,
		},
		{
			name:     "COM port device - NT namespace",
			input:    `\??\COM1`,
			expected: `COM1`,
		},
		{
			name:     "Physical drive device - Device namespace",
			input:    `\\.\PhysicalDrive0`,
			expected: `PhysicalDrive0`,
		},
		{
			name:     "Physical drive device - Win32 namespace",
			input:    `\\?\PhysicalDrive0`,
			expected: `PhysicalDrive0`,
		},
		{
			name:     "Physical drive device - NT namespace",
			input:    `\??\PhysicalDrive0`,
			expected: `PhysicalDrive0`,
		},
		{
			name:     "CDROM device - Device namespace",
			input:    `\\.\CdRom0`,
			expected: `CdRom0`,
		},
		{
			name:     "CDROM device - Win32 namespace",
			input:    `\\?\CdRom0`,
			expected: `CdRom0`,
		},
		{
			name:     "CDROM device - NT namespace",
			input:    `\??\CdRom0`,
			expected: `CdRom0`,
		},
		{
			name:     "Harddisk device - Device namespace",
			input:    `\\.\Harddisk0\Partition1`,
			expected: `Harddisk0\Partition1`,
		},
		{
			name:     "Harddisk device - Win32 namespace",
			input:    `\\?\Harddisk0\Partition1`,
			expected: `Harddisk0\Partition1`,
		},
		{
			name:     "Harddisk device - NT namespace",
			input:    `\??\Harddisk0\Partition1`,
			expected: `Harddisk0\Partition1`,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := normalizeWindowsPath(tt.input)
			assert.Equal(t, tt.expected, result, "normalizeWindowsPath(%q) = %q, expected %q", tt.input, result, tt.expected)
		})
	}
}

func TestNormalizeWindowsPath_NonWindows(t *testing.T) {
	// Test that normalization is a no-op on non-Windows platforms
	if runtime.GOOS == "windows" {
		t.Skip("Skipping non-Windows test on Windows platform")
	}

	tests := []string{
		`\\?\C:\Windows`,
		`\??\C:\Windows`,
		`\\.\C:\Windows`,
		`/etc/passwd`,
		`/var/log`,
		`~/Documents`,
	}

	for _, input := range tests {
		t.Run(input, func(t *testing.T) {
			result := normalizeWindowsPath(input)
			assert.Equal(t, input, result, "On non-Windows, normalizeWindowsPath should return input unchanged")
		})
	}
}

func BenchmarkNormalizeWindowsPath(b *testing.B) {
	if runtime.GOOS != "windows" {
		b.Skip("Skipping Windows benchmark on non-Windows platform")
	}

	testCases := []string{
		`\\?\C:\Windows\System32`,
		`\??\C:\Windows\System32`,
		`\\.\C:\Windows\System32`,
		`\\?\UNC\server\share`,
		`C:\Windows\System32`,
	}

	for _, tc := range testCases {
		b.Run(tc, func(b *testing.B) {
			for i := 0; i < b.N; i++ {
				_ = normalizeWindowsPath(tc)
			}
		})
	}
}

// Security tests for path traversal prevention

func TestValidateScanPath_PathTraversal(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{allowedDir},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	tests := []struct {
		name          string
		inputPath     string
		expectedError bool
		errorContains string
	}{
		{
			name:          "path traversal with ../ to escape allowed dir",
			inputPath:     filepath.Join(allowedDir, "..", "forbidden"),
			expectedError: true,
			errorContains: "does not exist", // Path won't exist, caught before allowlist check
		},
		{
			name:          "path traversal with multiple ../",
			inputPath:     filepath.Join(allowedDir, "..", "..", "etc"),
			expectedError: true,
			errorContains: "does not exist",
		},
		{
			name:          "path traversal attempt with mixed slashes",
			inputPath:     filepath.Join(allowedDir, ".."+string(filepath.Separator)+"forbidden"),
			expectedError: true,
			errorContains: "does not exist",
		},
		{
			name:          "clean path within allowed directory",
			inputPath:     allowedDir,
			expectedError: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validPath, err := validateScanPath(tt.inputPath, securityCfg)

			if tt.expectedError {
				assert.Error(t, err)
				if tt.errorContains != "" {
					assert.Contains(t, err.Error(), tt.errorContains)
				}
			} else {
				assert.NoError(t, err)
				assert.NotEmpty(t, validPath)
				assert.True(t, filepath.IsAbs(validPath))
			}
		})
	}
}

func TestValidateScanPath_SymlinkResolution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink tests on Windows (requires admin privileges)")
	}

	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	// Create a directory outside allowed
	forbiddenDir := filepath.Join(tempDir, "forbidden")
	require.NoError(t, os.Mkdir(forbiddenDir, 0755))

	// Create symlink pointing to forbidden directory
	symlinkPath := filepath.Join(allowedDir, "link_to_forbidden")
	require.NoError(t, os.Symlink(forbiddenDir, symlinkPath))

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{allowedDir},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	// Attempt to access via symlink should be blocked (symlink resolves to forbidden path)
	_, err := validateScanPath(symlinkPath, securityCfg)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "outside allowed directories")
}

func TestPathHasPrefix_CaseSensitivity(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		prefix   string
		expected bool
		skipOS   string // Skip test on specific OS
	}{
		{
			name:     "Windows case-insensitive match - lowercase path",
			path:     `c:\windows\system32`,
			prefix:   `C:\Windows`,
			expected: true,
			skipOS:   "!windows", // Only run on Windows
		},
		{
			name:     "Windows case-insensitive match - uppercase path",
			path:     `C:\WINDOWS\SYSTEM32`,
			prefix:   `c:\windows`,
			expected: true,
			skipOS:   "!windows",
		},
		{
			name:     "Windows case-insensitive match - mixed case",
			path:     `C:\WiNdOwS\SyStEm32`,
			prefix:   `c:\windows`,
			expected: true,
			skipOS:   "!windows",
		},
		{
			name:     "Unix case-sensitive - exact match",
			path:     `/etc/passwd`,
			prefix:   `/etc`,
			expected: true,
		},
		{
			name:     "Unix case-sensitive - different case should not match",
			path:     `/ETC/passwd`,
			prefix:   `/etc`,
			expected: false,
			skipOS:   "windows", // Skip on Windows (case-insensitive FS)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipOS == "!windows" && runtime.GOOS != "windows" {
				t.Skip("Test requires Windows")
			}
			if tt.skipOS == "windows" && runtime.GOOS == "windows" {
				t.Skip("Test not applicable on Windows")
			}

			result := pathHasPrefix(tt.path, tt.prefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestPathHasPrefix_WindowsExtendedPaths(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	tests := []struct {
		name     string
		path     string
		prefix   string
		expected bool
	}{
		{
			name:     "Extended path prefix with normal prefix",
			path:     `\\?\C:\Windows\System32`,
			prefix:   `C:\Windows`,
			expected: true,
		},
		{
			name:     "Normal path with extended prefix",
			path:     `C:\Windows\System32`,
			prefix:   `\\?\C:\Windows`,
			expected: true,
		},
		{
			name:     "Both extended paths",
			path:     `\\?\C:\Windows\System32`,
			prefix:   `\\?\C:\Windows`,
			expected: true,
		},
		{
			name:     "NT namespace path",
			path:     `\??\C:\Windows\System32`,
			prefix:   `C:\Windows`,
			expected: true,
		},
		{
			name:     "Device namespace path",
			path:     `\\.\C:\Windows\System32`,
			prefix:   `C:\Windows`,
			expected: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := pathHasPrefix(tt.path, tt.prefix)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsDirAllowed_Allowlist(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir1 := filepath.Join(tempDir, "allowed1")
	allowedDir2 := filepath.Join(tempDir, "allowed2")
	forbiddenDir := filepath.Join(tempDir, "forbidden")

	require.NoError(t, os.Mkdir(allowedDir1, 0755))
	require.NoError(t, os.Mkdir(allowedDir2, 0755))
	require.NoError(t, os.Mkdir(forbiddenDir, 0755))

	allow := []string{allowedDir1, allowedDir2}
	deny := []string{}

	tests := []struct {
		name     string
		dir      string
		expected bool
	}{
		{
			name:     "allowed directory 1",
			dir:      allowedDir1,
			expected: true,
		},
		{
			name:     "allowed directory 2",
			dir:      allowedDir2,
			expected: true,
		},
		{
			name:     "forbidden directory",
			dir:      forbiddenDir,
			expected: false,
		},
		{
			name:     "subdirectory of allowed",
			dir:      filepath.Join(allowedDir1, "subdir"),
			expected: true, // Non-existent subdirectory is allowed when its parent is allowed
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDirAllowed(tt.dir, allow, deny)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsDirAllowed_Denylist(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	deniedDir := filepath.Join(tempDir, "denied")

	require.NoError(t, os.Mkdir(allowedDir, 0755))
	require.NoError(t, os.Mkdir(deniedDir, 0755))

	allow := []string{tempDir}  // Allow entire tempDir
	deny := []string{deniedDir} // But deny deniedDir

	tests := []struct {
		name     string
		dir      string
		expected bool
	}{
		{
			name:     "allowed directory",
			dir:      allowedDir,
			expected: true,
		},
		{
			name:     "explicitly denied directory",
			dir:      deniedDir,
			expected: false,
		},
		{
			name:     "subdirectory of denied",
			dir:      filepath.Join(deniedDir, "subdir"),
			expected: false, // Should be denied even if doesn't exist
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isDirAllowed(tt.dir, allow, deny)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestIsDirAllowed_BuiltInDenied(t *testing.T) {
	tests := []struct {
		name     string
		dir      string
		expected bool
		skipOS   string
	}{
		{
			name:     "deny /proc",
			dir:      "/proc",
			expected: false,
			skipOS:   "darwin",
		},
		{
			name:     "deny /sys",
			dir:      "/sys",
			expected: false,
			skipOS:   "darwin",
		},
		{
			name:     "deny /dev",
			dir:      "/dev",
			expected: false,
			skipOS:   "windows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.skipOS == runtime.GOOS {
				t.Skipf("Skipping on %s", runtime.GOOS)
			}

			if _, err := os.Stat(tt.dir); os.IsNotExist(err) {
				t.Skip("System directory doesn't exist on this platform")
			}

			allow := []string{"/"}
			deny := []string{}

			result := isDirAllowed(tt.dir, allow, deny)
			assert.Equal(t, tt.expected, result, "Built-in denied directory should be blocked even with root allowlist")
		})
	}
}

func TestIsDirAllowed_EmptyAllowlist(t *testing.T) {
	tempDir := t.TempDir()

	// Empty allowlist should deny everything (secure by default)
	allow := []string{}
	deny := []string{}

	result := isDirAllowed(tempDir, allow, deny)
	assert.False(t, result, "Empty allowlist should deny all access (secure by default)")
}

func TestIsDirAllowed_SymlinkResolution(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink tests on Windows (requires admin privileges)")
	}

	tempDir := t.TempDir()
	realDir := filepath.Join(tempDir, "real")
	require.NoError(t, os.Mkdir(realDir, 0755))

	// Create symlink to real directory
	symlinkDir := filepath.Join(tempDir, "symlink")
	require.NoError(t, os.Symlink(realDir, symlinkDir))

	// Allow only the real directory
	allow := []string{realDir}
	deny := []string{}

	// Access via symlink should be allowed (symlink resolves to allowed path)
	result := isDirAllowed(symlinkDir, allow, deny)
	assert.True(t, result, "Access via symlink should be allowed when symlink resolves to allowed path")

	// Now test the inverse: allow symlink, access real dir
	allow2 := []string{symlinkDir}
	result2 := isDirAllowed(realDir, allow2, []string{})
	assert.True(t, result2, "Real directory should be accessible when symlink to it is in allowlist")
}

func TestWrapperFunctions(t *testing.T) {
	tempDir := t.TempDir()

	allow := []string{tempDir}
	deny := []string{}
	assert.True(t, IsDirAllowed(tempDir, allow, deny))

	assert.Equal(t, ExpandHomeDir("~/test"), expandHomeDir("~/test"))
	assert.Equal(t, Contains("abcdef", "bcd"), contains("abcdef", "bcd"))
	assert.Equal(t, GetDeniedDirectories(), getDeniedDirectories())
	assert.Equal(t, PathHasPrefix(tempDir, tempDir), pathHasPrefix(tempDir, tempDir))
}

func TestCanonicalizePath_NonExistentChildUnderExistingParent(t *testing.T) {
	base := t.TempDir()
	missing := filepath.Join(base, "nested", "child")

	got, err := canonicalizePath(missing)
	require.NoError(t, err)
	assert.True(t, filepath.IsAbs(got))
	assert.Equal(t, filepath.Base(missing), filepath.Base(got))
	assert.Equal(t, "nested", filepath.Base(filepath.Dir(got)))
}

func TestValidateScanPath_TypedErrors(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	tests := []struct {
		name        string
		inputPath   string
		securityCfg *config.SecurityConfig
		expectedErr error
		skipIf      string
	}{
		{
			name:      "path outside allowed directory returns ErrPathOutsideAllowed",
			inputPath: tempDir,
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{allowedDir},
				DeniedDirectories:  []string{},
			},
			expectedErr: apperrors.ErrPathOutsideAllowed,
		},
		{
			name:      "nonexistent path returns ErrPathNotExist",
			inputPath: "/nonexistent/path/12345",
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{"/tmp"},
			},
			expectedErr: apperrors.ErrPathNotExist,
		},
		{
			name:      "empty allowlist returns ErrAllowedDirsEmpty",
			inputPath: tempDir,
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{},
			},
			expectedErr: apperrors.ErrAllowedDirsEmpty,
		},
		{
			name:      "pseudo-filesystem (/proc) returns ErrPathInDenylist even with allowlist",
			inputPath: "/proc",
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{"/"},
			},
			expectedErr: apperrors.ErrPathInDenylist,
			skipIf:      "darwin windows",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if strings.Contains(tt.skipIf, runtime.GOOS) {
				t.Skipf("Skipping on %s", runtime.GOOS)
			}

			_, err := validateScanPath(tt.inputPath, tt.securityCfg)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tt.expectedErr),
				"Expected error to be %v, got %v", tt.expectedErr, err)
		})
	}
}

func TestValidateScanPath_EmptyAllowlistDeniesByDefault(t *testing.T) {
	tempDir := t.TempDir()

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{},
		DeniedDirectories:  []string{},
	}

	_, err := validateScanPath(tempDir, securityCfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrAllowedDirsEmpty),
		"Empty allowlist should return ErrAllowedDirsEmpty, got %v", err)
}

func TestValidateScanPath_BlankEntriesIgnored(t *testing.T) {
	tempDir := t.TempDir()

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{"", "  ", tempDir},
		DeniedDirectories:  []string{},
	}

	_, err := validateScanPath(tempDir, securityCfg)
	require.NoError(t, err, "Blank entries should be ignored, valid entry should allow access")
}

func TestValidateScanPath_OnlyBlankEntriesDeniesByDefault(t *testing.T) {
	tempDir := t.TempDir()

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{"", "  ", "\t"},
		DeniedDirectories:  []string{},
	}

	_, err := validateScanPath(tempDir, securityCfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrAllowedDirsEmpty),
		"Only blank entries should be treated as empty allowlist, got %v", err)
}

func TestValidateScanPath_FileNotDirectory(t *testing.T) {
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "testfile.txt")
	require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
	}

	t.Run("rejects file path", func(t *testing.T) {
		_, err := validateScanPath(tempFile, securityCfg)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrPathNotDir),
			"File path should return ErrPathNotDir, got %v", err)
	})

	t.Run("accepts directory path", func(t *testing.T) {
		validPath, err := validateScanPath(tempDir, securityCfg)
		require.NoError(t, err)
		expectedPath, _ := filepath.EvalSymlinks(tempDir)
		assert.Equal(t, expectedPath, validPath)
	})
}

func TestGetDeniedDirectories_MinimalDenylist(t *testing.T) {
	denied := getDeniedDirectories()

	assert.Contains(t, denied, "/proc")
	assert.Contains(t, denied, "/sys")
	assert.Contains(t, denied, "/dev")

	assert.NotContains(t, denied, "/etc")
	assert.NotContains(t, denied, "/var/log")
	assert.NotContains(t, denied, "/usr/bin")
	assert.NotContains(t, denied, "/root")
}

func TestValidateScanPath_MinimalDenylistOnly(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}

	tests := []struct {
		name        string
		path        string
		shouldExist bool
	}{
		{name: "/proc is blocked", path: "/proc", shouldExist: true},
		{name: "/sys is blocked", path: "/sys", shouldExist: true},
		{name: "/dev is blocked", path: "/dev", shouldExist: true},
	}

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{"/"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := os.Stat(tt.path); os.IsNotExist(err) {
				t.Skip("Directory doesn't exist on this system")
			}

			_, err := validateScanPath(tt.path, securityCfg)
			require.Error(t, err)
			assert.True(t, errors.Is(err, apperrors.ErrPathInDenylist),
				"Expected ErrPathInDenylist for %s, got %v", tt.path, err)
		})
	}
}

// Windows-specific security tests

func TestValidateScanPath_ReservedDeviceName(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	tempDir := t.TempDir()

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{tempDir},
	}

	tests := []struct {
		name        string
		inputPath   string
		expectedErr error
	}{
		{"CON is rejected", `CON`, apperrors.ErrReservedDeviceName},
		{"NUL is rejected", `NUL`, apperrors.ErrReservedDeviceName},
		{"COM1 is rejected", `COM1`, apperrors.ErrReservedDeviceName},
		{"LPT1 is rejected", `LPT1`, apperrors.ErrReservedDeviceName},
		{"lowercase con is rejected", `con`, apperrors.ErrReservedDeviceName},
		{"CON with extension is rejected", `CON.txt`, apperrors.ErrReservedDeviceName},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := validateScanPath(tt.inputPath, securityCfg)
			require.Error(t, err)
			assert.True(t, errors.Is(err, tt.expectedErr),
				"Expected %v, got %v", tt.expectedErr, err)
		})
	}
}

func TestValidateScanPath_UNCPath(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("Windows-specific test")
	}

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{`C:\Videos`},
		AllowUNC:           false,
	}

	t.Run("UNC path blocked by default", func(t *testing.T) {
		_, err := validateScanPath(`\\server\share`, securityCfg)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrUNCPathBlocked))
	})

	t.Run("UNC path blocked when server not in whitelist", func(t *testing.T) {
		cfg := &config.SecurityConfig{
			AllowedDirectories: []string{`C:\Videos`},
			AllowUNC:           true,
			AllowedUNCServers:  []string{"trusted-server"},
		}
		_, err := validateScanPath(`\\evil-server\share`, cfg)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrUNCPathBlocked))
	})
}

func TestStripTrailingChars_NoOp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Non-Windows test")
	}

	input := "/some/path"
	result := stripTrailingChars(input)
	assert.Equal(t, input, result, "Should be no-op on non-Windows")
}

func TestIsReservedDeviceName_NoOp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Non-Windows test")
	}

	assert.False(t, isReservedDeviceName("CON"), "Should return false on non-Windows")
	assert.False(t, isReservedDeviceName("NUL"), "Should return false on non-Windows")
}

func TestNormalizeUNCPath_NoOp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Non-Windows test")
	}

	result, err := normalizeUNCPath(`\\server\share`, false, nil)
	assert.NoError(t, err)
	assert.Equal(t, `\\server\share`, result, "Should be no-op on non-Windows")
}

func TestNormalizePathForPlatform_NoOp(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Non-Windows test")
	}

	input := "/some/path"
	result := normalizePathForPlatform(input)
	assert.Equal(t, input, result, "Should be no-op on non-Windows")
}
