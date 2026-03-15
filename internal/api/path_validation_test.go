package api

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

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
			name:      "valid path - no allowlist restriction",
			inputPath: tempDir,
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{},
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
				AllowedDirectories: []string{},
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
	// Test that system directories are blocked regardless of allowlist
	systemDirs := []string{
		"/etc",
		"/var/log",
		"/usr/bin",
	}

	// Add Windows-specific test paths if on Windows
	if runtime.GOOS == "windows" {
		systemDirs = append(systemDirs, "C:\\Windows")
	}

	// Add macOS-specific test paths if on macOS
	if runtime.GOOS == "darwin" {
		systemDirs = append(systemDirs, "/System")
	}

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{},
		DeniedDirectories:  []string{},
		MaxFilesPerScan:    10000,
		ScanTimeoutSeconds: 30,
	}

	for _, dir := range systemDirs {
		t.Run("blocks "+dir, func(t *testing.T) {
			// Skip if directory doesn't exist (won't be blocked if it doesn't exist)
			if _, err := os.Stat(dir); os.IsNotExist(err) {
				t.Skip("System directory doesn't exist on this platform")
			}

			_, err := validateScanPath(dir, securityCfg)
			assert.Error(t, err)
			assert.Contains(t, err.Error(), "system directory")
		})
	}
}

func TestValidateScanPath_FileVsDirectory(t *testing.T) {
	// Create temp file
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "testfile.txt")
	require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))

	securityCfg := &config.SecurityConfig{
		AllowedDirectories: []string{},
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
		// Compare canonical paths since validateScanPath returns canonical path
		expectedPath, _ := filepath.EvalSymlinks(tempDir)
		assert.Equal(t, expectedPath, validPath)
	})
}

func TestGetDeniedDirectories(t *testing.T) {
	denied := getDeniedDirectories()

	// Should always include these cross-platform directories
	assert.Contains(t, denied, "/etc")
	assert.Contains(t, denied, "/var/log")

	// Platform-specific checks
	if runtime.GOOS == "windows" {
		assert.Contains(t, denied, "C:\\Windows")
	}

	if runtime.GOOS == "darwin" {
		assert.Contains(t, denied, "/System")
	}
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
				AllowedDirectories: []string{},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: false, // Empty path becomes "." which resolves to current directory
		},
		{
			name:      "path with trailing slash",
			inputPath: tempDir + "/",
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{},
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
				AllowedDirectories: []string{},
				DeniedDirectories:  []string{},
				MaxFilesPerScan:    10000,
				ScanTimeoutSeconds: 30,
			},
			expectedError: false, // ./ resolves to current directory
		},
		{
			name:      "relative path cleaned to absolute",
			inputPath: tempDir,
			securityCfg: &config.SecurityConfig{
				AllowedDirectories: []string{},
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
	// Test that built-in system directories are always denied
	tests := []struct {
		name     string
		dir      string
		expected bool
		skipOS   string
	}{
		{
			name:     "deny /etc",
			dir:      "/etc",
			expected: false,
			skipOS:   "windows",
		},
		{
			name:     "deny /var/log",
			dir:      "/var/log",
			expected: false,
			skipOS:   "windows",
		},
		{
			name:     "deny /usr/bin",
			dir:      "/usr/bin",
			expected: false,
			skipOS:   "windows",
		},
		{
			name:     "deny C:\\Windows",
			dir:      `C:\Windows`,
			expected: false,
			skipOS:   "!windows",
		},
		{
			name:     "deny C:\\Program Files",
			dir:      `C:\Program Files`,
			expected: false,
			skipOS:   "!windows",
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

			// Skip if directory doesn't exist (can't test denied check)
			if _, err := os.Stat(tt.dir); os.IsNotExist(err) {
				t.Skip("System directory doesn't exist on this platform")
			}

			// Even with allowlist that includes everything, built-in denied dirs should be blocked
			allow := []string{"/"}
			if runtime.GOOS == "windows" {
				allow = []string{`C:\`}
			}
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
