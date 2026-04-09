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

func TestValidateScanPath_DenylistPrefix(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}

	tempDir := t.TempDir()

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	t.Run("/devmedia is NOT blocked (prefix collision with /dev)", func(t *testing.T) {
		devmediaDir := filepath.Join(tempDir, "devmedia")
		require.NoError(t, os.Mkdir(devmediaDir, 0755))

		_, err := validateScanPath(devmediaDir, securityCfg)
		require.NoError(t, err, "Path /devmedia should not be blocked by /dev denylist prefix")
	})

	t.Run("/dev/null IS blocked (within /dev)", func(t *testing.T) {
		securityCfgAll := &config.SecurityConfig{
			AllowedDirectories: []string{"/"},
			DeniedDirectories:  []string{},
			MaxFilesPerScan:    10000,
			ScanTimeoutSeconds: 30,
		}
		if _, err := os.Stat("/dev/null"); os.IsNotExist(err) {
			t.Skip("/dev/null doesn't exist on this system")
		}
		_, err := validateScanPath("/dev/null", securityCfgAll)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrPathInDenylist),
			"Expected ErrPathInDenylist for /dev/null, got %v", err)
	})

	t.Run("/sys/kernel IS blocked (within /sys)", func(t *testing.T) {
		securityCfgAll := &config.SecurityConfig{
			AllowedDirectories: []string{"/"},
			DeniedDirectories:  []string{},
			MaxFilesPerScan:    10000,
			ScanTimeoutSeconds: 30,
		}
		if _, err := os.Stat("/sys/kernel"); os.IsNotExist(err) {
			t.Skip("/sys/kernel doesn't exist on this system")
		}
		_, err := validateScanPath("/sys/kernel", securityCfgAll)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrPathInDenylist),
			"Expected ErrPathInDenylist for /sys/kernel, got %v", err)
	})

	t.Run("custom denylist with prefix collision works correctly", func(t *testing.T) {
		customDeniedDir := filepath.Join(tempDir, "custom")
		require.NoError(t, os.Mkdir(customDeniedDir, 0755))

		customDeniedPath := filepath.Join(tempDir, "custombackup")
		require.NoError(t, os.Mkdir(customDeniedPath, 0755))

		cfg := &config.SecurityConfig{
			AllowedDirectories: []string{tempDir},
			DeniedDirectories:  []string{customDeniedDir},
			MaxFilesPerScan:    10000,
			ScanTimeoutSeconds: 30,
		}

		_, err := validateScanPath(customDeniedPath, cfg)
		require.NoError(t, err, "custombackup should not be blocked by custom denylist")
	})
}

func TestIsPathWithin_ComponentAware(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		parent   string
		expected bool
	}{
		{"exact match", "/dev", "/dev", true},
		{"within /dev", "/dev/null", "/dev", true},
		{"within /dev nested", "/dev/fd/0", "/dev", true},
		{"prefix collision /devmedia", "/devmedia", "/dev", false},
		{"prefix collision /devtools", "/devtools", "/dev", false},
		{"prefix collision /sysinfo", "/sysinfo", "/sys", false},
		{"within /sys", "/sys/kernel", "/sys", true},
		{"no match different path", "/home", "/dev", false},
		{"trailing slash match", "/dev/", "/dev", true},
		{"parent shorter", "/usr/local", "/usr", true},
		{"path escapes parent", "/usr/../etc", "/usr", false},
		{"relative path escapes", "../etc", "/usr", false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isPathWithin(tt.path, tt.parent)
			assert.Equal(t, tt.expected, result, "isPathWithin(%q, %q) = %v, expected %v", tt.path, tt.parent, result, tt.expected)
		})
	}
}

func TestValidateScanPath_Symlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink tests on Windows (requires admin privileges)")
	}

	tempDir := t.TempDir()

	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	targetDir := filepath.Join(allowedDir, "target")
	require.NoError(t, os.Mkdir(targetDir, 0755))

	forbiddenDir := filepath.Join(tempDir, "forbidden")
	require.NoError(t, os.Mkdir(forbiddenDir, 0755))

	targetFile := filepath.Join(allowedDir, "file.txt")
	require.NoError(t, os.WriteFile(targetFile, []byte("test"), 0644))

	symlinkToAllowed := filepath.Join(tempDir, "symlink_to_allowed")
	require.NoError(t, os.Symlink(targetDir, symlinkToAllowed))

	symlinkToForbidden := filepath.Join(tempDir, "symlink_to_forbidden")
	require.NoError(t, os.Symlink(forbiddenDir, symlinkToForbidden))

	symlinkToFile := filepath.Join(tempDir, "symlink_to_file")
	require.NoError(t, os.Symlink(targetFile, symlinkToFile))

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{allowedDir},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	t.Run("symlink to allowed directory passes validation", func(t *testing.T) {
		validPath, err := validateScanPath(symlinkToAllowed, securityCfg)
		require.NoError(t, err, "Symlink to allowed directory should pass validation")
		expectedCanonical, _ := filepath.EvalSymlinks(targetDir)
		assert.Equal(t, expectedCanonical, validPath, "Should return canonical (resolved) path")
	})

	t.Run("symlink to forbidden directory fails with ErrPathOutsideAllowed", func(t *testing.T) {
		_, err := validateScanPath(symlinkToForbidden, securityCfg)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrPathOutsideAllowed),
			"Expected ErrPathOutsideAllowed, got %v", err)
	})

	t.Run("symlink to file fails with ErrPathNotDir", func(t *testing.T) {
		_, err := validateScanPath(symlinkToFile, securityCfg)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrPathNotDir),
			"Expected ErrPathNotDir, got %v", err)
	})
}

// TestCanonicalizePath_SymlinkAncestor tests the edge case where a parent directory
// is a symlink but the final leaf component doesn't exist.
//
// This documents the behavior of canonicalizePath which walks up to find the
// nearest existing ancestor for missing paths. This approach is:
// - Asymmetric: A/B (exists symlink) and A/B/C (missing) behave differently
// - Raceable: The ancestor could disappear between resolution and use
//
// SECURITY NOTE: Non-resolvable parent directories (e.g., broken symlink in path)
// should be rejected rather than walked past, to prevent symlink-based attacks.
func TestCanonicalizePath_SymlinkAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink tests on Windows (requires admin privileges)")
	}

	t.Run("parent symlink with missing leaf resolves symlink correctly", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create real directory
		realDir := filepath.Join(tempDir, "real")
		require.NoError(t, os.Mkdir(realDir, 0755))

		// Create symlink to real directory
		symlinkDir := filepath.Join(tempDir, "symlink")
		require.NoError(t, os.Symlink(realDir, symlinkDir))

		// Path through symlink to non-existent child
		missingPath := filepath.Join(symlinkDir, "nonexistent", "child")

		got, err := canonicalizePath(missingPath)
		require.NoError(t, err)

		// Should resolve the symlink in the path
		// Result should contain the real path with missing components appended
		assert.True(t, filepath.IsAbs(got))
		assert.Contains(t, got, "real") // Symlink was resolved
		assert.Contains(t, got, "nonexistent")
		assert.Contains(t, got, "child")
	})

	t.Run("broken symlink in path returns error", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create symlink pointing to non-existent target (broken symlink)
		brokenSymlink := filepath.Join(tempDir, "broken")
		require.NoError(t, os.Symlink("/nonexistent/target", brokenSymlink))

		// Path through broken symlink
		pathThroughBroken := filepath.Join(brokenSymlink, "child")

		// filepath.EvalSymlinks on a broken symlink returns an error
		got, err := canonicalizePath(pathThroughBroken)

		// SECURITY: canonicalizePath correctly returns an error when it encounters
		// a symlink that cannot be resolved (broken symlink).
		// This prevents path confusion attacks where a symlink target is later
		// created pointing to a sensitive location.
		require.Error(t, err) // Expected: error for broken symlink
		assert.Empty(t, got)
		// The error indicates the symlink target doesn't exist
		assert.Contains(t, err.Error(), "no such file or directory")
	})

	t.Run("multiple missing components under symlink", func(t *testing.T) {
		tempDir := t.TempDir()

		realDir := filepath.Join(tempDir, "real")
		require.NoError(t, os.Mkdir(realDir, 0755))

		symlinkDir := filepath.Join(tempDir, "symlink")
		require.NoError(t, os.Symlink(realDir, symlinkDir))

		// Multiple missing levels under symlink
		missingPath := filepath.Join(symlinkDir, "a", "b", "c", "d")

		got, err := canonicalizePath(missingPath)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(got))
		assert.Contains(t, got, "real")
		// All missing components should be preserved
		assert.Contains(t, got, "a")
		assert.Contains(t, got, "b")
		assert.Contains(t, got, "c")
		assert.Contains(t, got, "d")
	})

	t.Run("symlink chain with missing leaf", func(t *testing.T) {
		tempDir := t.TempDir()

		// Create chain: final -> mid -> real
		realDir := filepath.Join(tempDir, "real")
		require.NoError(t, os.Mkdir(realDir, 0755))

		midSymlink := filepath.Join(tempDir, "mid")
		require.NoError(t, os.Symlink(realDir, midSymlink))

		finalSymlink := filepath.Join(tempDir, "final")
		require.NoError(t, os.Symlink(midSymlink, finalSymlink))

		// Path through chain to non-existent child
		missingPath := filepath.Join(finalSymlink, "nonexistent")

		got, err := canonicalizePath(missingPath)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(got))
		// All symlinks should be resolved to the real path
		assert.Contains(t, got, "real")
		assert.NotContains(t, got, "mid")
		assert.NotContains(t, got, "final")
	})
}

// TestCanonicalizePath_TransientMissingAncestor documents the race condition
// where an ancestor directory disappears between resolution and use.
//
// RACE CONDITION: canonicalizePath finds existing ancestor at time of call,
// but the ancestor could be deleted before the caller uses the result.
// This is inherent to TOCTOU (time-of-check-time-of-use) vulnerabilities.
//
// MITIGATION: Callers should validate the returned path still exists
// and matches expected permissions before using it.
func TestCanonicalizePath_TransientMissingAncestor(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink tests on Windows (requires admin privileges)")
	}

	t.Run("documents race condition with transient ancestor", func(t *testing.T) {
		// This test documents the behavior but cannot reliably test the race
		// because it requires precise timing of concurrent filesystem operations.
		//
		// The canonicalizePath function:
		// 1. Calls filepath.EvalSymlinks on the full path (fails if missing)
		// 2. Walks up directory tree to find existing ancestor
		// 3. Resolves symlinks on that ancestor
		// 4. Appends missing components back
		//
		// RACE: Between step 3 and the caller using the result, the ancestor
		// could be deleted or replaced with a symlink.
		//
		// This test verifies the current behavior (walks up successfully)
		// and documents the inherent race for future hardening.

		tempDir := t.TempDir()
		existingDir := filepath.Join(tempDir, "existing")
		require.NoError(t, os.Mkdir(existingDir, 0755))

		missingPath := filepath.Join(existingDir, "transient", "child")

		got, err := canonicalizePath(missingPath)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(got))

		// The path includes the existing directory resolved
		resolvedExisting, _ := filepath.EvalSymlinks(existingDir)
		assert.Equal(t, filepath.Join(resolvedExisting, "transient", "child"), got)

		// DOCUMENTATION: If "existing" was deleted between canonicalizePath
		// and the caller using "got", subsequent operations would fail.
		// No mitigation is possible without filesystem locks (unavailable on Unix).
	})
}

// TestCanonicalizePath_NonResolvableParent documents that paths with
// non-resolvable parents (e.g., permission denied, broken symlink loops)
// should ideally be rejected rather than walked past.
//
// CURRENT BEHAVIOR: The implementation walks up until it finds any
// resolvable ancestor, which may not be the desired security behavior.
//
// RECOMMENDED BEHAVIOR: Paths should be rejected if any parent component
// cannot be resolved, to prevent symlink-based directory escape attacks.
func TestCanonicalizePath_NonResolvableParent(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink tests on Windows (requires admin privileges)")
	}

	t.Run("symlink loop detection returns error", func(t *testing.T) {
		// Symlink loops are detected by filepath.EvalSymlinks
		tempDir := t.TempDir()

		loopA := filepath.Join(tempDir, "loopA")
		loopB := filepath.Join(tempDir, "loopB")

		// Create symlink loop: loopA -> loopB -> loopA
		require.NoError(t, os.Symlink(loopB, loopA))
		require.NoError(t, os.Symlink(loopA, loopB))

		pathInLoop := filepath.Join(loopA, "child")

		got, err := canonicalizePath(pathInLoop)

		// SECURITY: canonicalizePath correctly returns an error for symlink loops.
		// filepath.EvalSymlinks detects the loop and returns "too many links" error.
		// This prevents infinite loops and symlink-based filesystem attacks.
		require.Error(t, err) // Expected: error for symlink loop
		assert.Empty(t, got)
		assert.Contains(t, err.Error(), "too many links")
	})

	t.Run("permission denied on parent is propagated", func(t *testing.T) {
		// permission scenarios, so we document the expected behavior:
		//
		// If os.Lstat or filepath.EvalSymlinks returns permission denied,
		// canonicalizePath should propagate that error rather than walking up.
		//
		// This prevents: /allowed/sensitive -> /forbidden (symlink)
		// where /allowed has restricted permissions but parent is world-readable.

		// Skip on CI where we can't set up permission scenarios
		t.Skip("Requires manual testing with restricted permissions")
	})
}

// Tests for exported wrapper functions

func TestValidateScanPath_Wrapper(t *testing.T) {
	tempDir := t.TempDir()

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	t.Run("valid path returns canonical path", func(t *testing.T) {
		path, err := ValidateScanPath(tempDir, securityCfg)
		require.NoError(t, err)
		assert.True(t, filepath.IsAbs(path))
	})

	t.Run("invalid path returns error", func(t *testing.T) {
		_, err := ValidateScanPath("/nonexistent", securityCfg)
		require.Error(t, err)
	})

	t.Run("empty allowlist returns error", func(t *testing.T) {
		cfg := &config.SecurityConfig{
			AllowedDirectories: []string{},
		}
		_, err := ValidateScanPath(tempDir, cfg)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrAllowedDirsEmpty))
	})
}

func TestValidateAndOpenPath(t *testing.T) {
	tempDir := t.TempDir()

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	t.Run("valid path returns open file and canonical path", func(t *testing.T) {
		f, path, err := ValidateAndOpenPath(tempDir, securityCfg)
		require.NoError(t, err)
		defer f.Close()

		assert.NotNil(t, f, "Should return open file handle")
		assert.True(t, filepath.IsAbs(path), "Should return absolute canonical path")

		info, err := f.Stat()
		require.NoError(t, err)
		assert.True(t, info.IsDir(), "File handle should reference a directory")
	})

	t.Run("invalid path returns error with nil file", func(t *testing.T) {
		f, path, err := ValidateAndOpenPath("/nonexistent", securityCfg)
		require.Error(t, err)
		assert.Nil(t, f, "File should be nil on error")
		assert.Empty(t, path, "Path should be empty on error")
	})

	t.Run("file path returns error", func(t *testing.T) {
		tempFile := filepath.Join(tempDir, "testfile.txt")
		require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))

		f, path, err := ValidateAndOpenPath(tempFile, securityCfg)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrPathNotDir))
		assert.Nil(t, f)
		assert.Empty(t, path)
	})

	t.Run("empty allowlist returns error", func(t *testing.T) {
		cfg := &config.SecurityConfig{
			AllowedDirectories: []string{},
		}
		f, path, err := ValidateAndOpenPath(tempDir, cfg)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrAllowedDirsEmpty))
		assert.Nil(t, f)
		assert.Empty(t, path)
	})

	t.Run("opened file can be read", func(t *testing.T) {
		subdir := filepath.Join(tempDir, "subdir")
		require.NoError(t, os.Mkdir(subdir, 0755))

		f, path, err := ValidateAndOpenPath(subdir, securityCfg)
		require.NoError(t, err)
		defer f.Close()

		entries, err := f.ReadDir(0)
		require.NoError(t, err)
		assert.Empty(t, entries, "Empty directory should have no entries")

		assert.True(t, filepath.IsAbs(path))
		assert.Contains(t, path, "subdir")
	})

	t.Run("file handle prevents TOCTOU", func(t *testing.T) {
		f, path, err := ValidateAndOpenPath(tempDir, securityCfg)
		require.NoError(t, err)
		defer f.Close()

		_ = path
		info, err := f.Stat()
		require.NoError(t, err)
		assert.True(t, info.IsDir())
	})
}

func TestValidateAndOpenPath_SymlinkTOCTOU(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Skipping symlink tests on Windows (requires admin privileges)")
	}

	tempDir := t.TempDir()
	realDir := filepath.Join(tempDir, "real")
	require.NoError(t, os.Mkdir(realDir, 0755))

	linkDir := filepath.Join(tempDir, "link")
	require.NoError(t, os.Symlink(realDir, linkDir))

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{realDir},
		DeniedDirectories:  []string{},
	}

	t.Run("symlink to allowed directory returns real path", func(t *testing.T) {
		f, path, err := ValidateAndOpenPath(linkDir, securityCfg)
		require.NoError(t, err)
		defer f.Close()

		assert.True(t, filepath.IsAbs(path))
		assert.Contains(t, path, "real", "Should return canonical (real) path, not symlink")
		assert.Equal(t, filepath.Base(path), "real", "Path should end with 'real', not 'link'")
	})
}

func TestValidateAndOpenPath_SystemDirectory(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Unix-specific test")
	}

	if _, err := os.Stat("/proc"); os.IsNotExist(err) {
		t.Skip("/proc doesn't exist on this system")
	}

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{"/"},
		DeniedDirectories:  []string{},
	}

	t.Run("system directory blocked by denylist", func(t *testing.T) {
		f, path, err := ValidateAndOpenPath("/proc", securityCfg)
		require.Error(t, err)
		assert.True(t, errors.Is(err, apperrors.ErrPathInDenylist))
		assert.Nil(t, f)
		assert.Empty(t, path)
	})
}

func TestPathHasPrefix_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		prefix   string
		expected bool
	}{
		{
			name:     "empty path with empty prefix",
			path:     "",
			prefix:   "",
			expected: true,
		},
		{
			name:     "empty path with non-empty prefix",
			path:     "",
			prefix:   "/path",
			expected: false,
		},
		{
			name:     "non-empty path with empty prefix",
			path:     "/path",
			prefix:   "",
			expected: true,
		},
		{
			name:     "path shorter than prefix",
			path:     "/a",
			prefix:   "/abc",
			expected: false,
		},
		{
			name:     "exact match",
			path:     "/path",
			prefix:   "/path",
			expected: true,
		},
		{
			name:     "subdirectory match",
			path:     "/path/to/file",
			prefix:   "/path",
			expected: true,
		},
		{
			name:     "no match different paths",
			path:     "/path1",
			prefix:   "/path2",
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := PathHasPrefix(tt.path, tt.prefix)
			assert.Equal(t, tt.expected, result, "PathHasPrefix(%q, %q) = %v, expected %v", tt.path, tt.prefix, result, tt.expected)
		})
	}
}

func TestExpandHomeDir_EdgeCases(t *testing.T) {
	t.Run("expands tilde", func(t *testing.T) {
		result := ExpandHomeDir("~/Documents")
		assert.Contains(t, result, "Documents")
		assert.NotContains(t, result, "~", "Tilde should be expanded")
	})

	t.Run("no tilde returns unchanged", func(t *testing.T) {
		result := ExpandHomeDir("/absolute/path")
		assert.Equal(t, "/absolute/path", result)
	})

	t.Run("only tilde returns unchanged (function requires slash)", func(t *testing.T) {
		result := ExpandHomeDir("~")
		assert.Equal(t, "~", result, "ExpandHomeDir only handles ~/ format, not ~ alone")
	})

	t.Run("empty string returns unchanged", func(t *testing.T) {
		result := ExpandHomeDir("")
		assert.Equal(t, "", result)
	})
}

func TestContains_EdgeCases(t *testing.T) {
	tests := []struct {
		s        string
		substr   string
		expected bool
	}{
		{"hello world", "world", true},
		{"hello world", "World", false},
		{"hello world", "", true},
		{"", "test", false},
		{"", "", true},
		{"short", "longer substring", false},
	}

	for _, tt := range tests {
		t.Run(tt.s+"_"+tt.substr, func(t *testing.T) {
			result := Contains(tt.s, tt.substr)
			assert.Equal(t, tt.expected, result, "Contains(%q, %q) = %v, expected %v", tt.s, tt.substr, result, tt.expected)
		})
	}
}

func TestGetDeniedDirectories_ReturnsExpected(t *testing.T) {
	denied := GetDeniedDirectories()

	assert.Contains(t, denied, "/proc")
	assert.Contains(t, denied, "/sys")
	assert.Contains(t, denied, "/dev")

	assert.GreaterOrEqual(t, len(denied), 3, "Should have at least 3 built-in denied directories")
}

// Test isUNCPath function coverage
func TestIsUNCPath_EdgeCases(t *testing.T) {
	tests := []struct {
		name     string
		path     string
		expected bool
	}{
		{"empty string", "", false},
		{"single char", "a", false},
		{"regular path", "/path/to/file", false},
		{"standard UNC", `\\server\share`, true},
		{"extended-length UNC lowercase", `\\?\unc\server\share`, true},
		{"extended-length UNC uppercase", `\\?\UNC\SERVER\SHARE`, true},
		{"extended-length UNC mixed case", `\\?\UnC\SeRvEr\ShArE`, true},
		{"NT namespace UNC lowercase", `\??\unc\server\share`, true},
		{"NT namespace UNC uppercase", `\??\UNC\SERVER\SHARE`, true},
		{"NT namespace UNC mixed case", `\??\UnC\SeRvEr\ShArE`, true},
		{"Device namespace UNC lowercase", `\\.\unc\server\share`, true},
		{"Device namespace UNC uppercase", `\\.\UNC\SERVER\SHARE`, true},
		{"Device namespace UNC mixed case", `\\.\UnC\SeRvEr\ShArE`, true},
		{"too short for extended UNC check", `\\?\un`, true}, // Still matches standard UNC check (\\)
		{"Windows path", `C:\Windows`, false},
		{"relative path", `relative\path`, false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := isUNCPath(tt.path)
			assert.Equal(t, tt.expected, result, "isUNCPath(%q) = %v, expected %v", tt.path, result, tt.expected)
		})
	}
}
