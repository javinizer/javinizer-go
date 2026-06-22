package core

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// Helper to simulate Windows in tests regardless of actual OS.
func setWindowsOSForTest(t *testing.T) {
	t.Helper()
	orig := currentOS
	currentOS = "windows"
	t.Cleanup(func() { currentOS = orig })
}

// ---------------------------------------------------------------------------
// isReservedDeviceName - comprehensive branch coverage via simulated Windows
// ---------------------------------------------------------------------------

func TestIsReservedDeviceName_SimWin_BasicReserved(t *testing.T) {
	setWindowsOSForTest(t)
	reservedBasic := []string{"CON", "PRN", "AUX", "NUL"}
	for _, name := range reservedBasic {
		assert.True(t, isReservedDeviceName(name), "isReservedDeviceName(%q) should be true", name)
	}
	for i := 1; i <= 9; i++ {
		assert.True(t, isReservedDeviceName(fmt.Sprintf("COM%d", i)))
		assert.True(t, isReservedDeviceName(fmt.Sprintf("LPT%d", i)))
	}
}

func TestIsReservedDeviceName_SimWin_CaseInsensitive(t *testing.T) {
	setWindowsOSForTest(t)
	assert.True(t, isReservedDeviceName("con"))
	assert.True(t, isReservedDeviceName("nul"))
	assert.True(t, isReservedDeviceName("aux"))
	assert.True(t, isReservedDeviceName("prn"))
	assert.True(t, isReservedDeviceName("com1"))
	assert.True(t, isReservedDeviceName("lpt9"))
	assert.True(t, isReservedDeviceName("Con"))
	assert.True(t, isReservedDeviceName("NuL"))
	assert.True(t, isReservedDeviceName("Com3"))
	assert.True(t, isReservedDeviceName("LpT7"))
}

func TestIsReservedDeviceName_SimWin_WithExtension(t *testing.T) {
	setWindowsOSForTest(t)
	// CON.txt -> SplitN("CON.txt", ".", 2) -> namePart="CON" -> TrimRight(" ") -> "CON"
	assert.True(t, isReservedDeviceName("CON.txt"))
	assert.True(t, isReservedDeviceName("CON.doc"))
	assert.True(t, isReservedDeviceName("NUL.dat"))
	assert.True(t, isReservedDeviceName("com1.txt"))
	assert.True(t, isReservedDeviceName("lpt9.log"))
	// Multiple dots: CON.txt.bak -> SplitN gives "CON" as namePart
	assert.True(t, isReservedDeviceName("CON.txt.bak"))
}

func TestIsReservedDeviceName_SimWin_TrailingSpaces(t *testing.T) {
	setWindowsOSForTest(t)
	// "CON " -> namePart="CON " -> TrimRight(" ") -> "CON"
	assert.True(t, isReservedDeviceName("CON "))
	assert.True(t, isReservedDeviceName("CON  "))
	assert.True(t, isReservedDeviceName("NUL "))
}

func TestIsReservedDeviceName_SimWin_DriveLetterPrefix(t *testing.T) {
	setWindowsOSForTest(t)
	// Drive letter + reserved name bypass: "C:CON" -> strip "C:" -> "CON"
	assert.True(t, isReservedDeviceName("C:CON"))
	assert.True(t, isReservedDeviceName("c:con"))
	assert.True(t, isReservedDeviceName("C:NUL"))
	assert.True(t, isReservedDeviceName("D:COM1"))
	assert.True(t, isReservedDeviceName("Z:LPT9"))
	// Numeric drive letter: "0:CON" -> strip "0:" -> "CON"
	assert.True(t, isReservedDeviceName("0:CON"))
	assert.True(t, isReservedDeviceName("9:AUX"))
	// Drive letter with non-reserved name
	assert.False(t, isReservedDeviceName("C:Videos"))
	assert.False(t, isReservedDeviceName("C:NormalFile"))
}

func TestIsReservedDeviceName_SimWin_NotReserved(t *testing.T) {
	setWindowsOSForTest(t)
	assert.False(t, isReservedDeviceName("Videos"))
	assert.False(t, isReservedDeviceName("Documents"))
	assert.False(t, isReservedDeviceName("CON2"))
	assert.False(t, isReservedDeviceName("COM0"))
	assert.False(t, isReservedDeviceName("LPT0"))
	assert.False(t, isReservedDeviceName("COM10"))
	assert.False(t, isReservedDeviceName("LPT10"))
	assert.False(t, isReservedDeviceName("CONFIG"))
}

func TestIsReservedDeviceName_SimWin_EmptyString(t *testing.T) {
	setWindowsOSForTest(t)
	assert.False(t, isReservedDeviceName(""))
}

func TestIsReservedDeviceName_SimWin_DriveLetterEdgeCases(t *testing.T) {
	setWindowsOSForTest(t)
	// Single char - no drive letter prefix possible
	assert.False(t, isReservedDeviceName("C"))
	// Two chars but not a drive letter: "AB" (B is not colon)
	assert.False(t, isReservedDeviceName("AB"))
	// No letter before colon
	assert.False(t, isReservedDeviceName(":CON"))
	// Lowercase drive letter: "a:CON" -> upper="A:CON" -> A is alpha -> strip -> "CON"
	assert.True(t, isReservedDeviceName("a:CON"))
	// Drive letter with extension after reserved name
	assert.True(t, isReservedDeviceName("C:CON.txt"))
}

// ---------------------------------------------------------------------------
// stripTrailingChars - comprehensive branch coverage via simulated Windows
// ---------------------------------------------------------------------------

func TestStripTrailingChars_SimWin_BasicStripping(t *testing.T) {
	setWindowsOSForTest(t)
	sep := string(filepath.Separator)
	// Trailing dots
	assert.Equal(t, "Videos", stripTrailingChars("Videos."))
	assert.Equal(t, "Videos", stripTrailingChars("Videos.."))
	assert.Equal(t, "Videos", stripTrailingChars("Videos..."))
	// Trailing spaces
	assert.Equal(t, "Videos", stripTrailingChars("Videos "))
	assert.Equal(t, "Videos", stripTrailingChars("Videos  "))
	// Mixed trailing dots and spaces
	assert.Equal(t, "Videos", stripTrailingChars("Videos. "))
	assert.Equal(t, "Videos", stripTrailingChars("Videos ."))
	assert.Equal(t, "Videos", stripTrailingChars("Videos . ."))
	// Multi-component paths with platform separator
	assert.Equal(t, "root"+sep+"Videos", stripTrailingChars("root."+sep+"Videos."))
	assert.Equal(t, "root"+sep+"Videos", stripTrailingChars("root "+sep+"Videos "))
	assert.Equal(t, "a"+sep+"b"+sep+"c", stripTrailingChars("a."+sep+"b ."+sep+"c. "))
}

func TestStripTrailingChars_SimWin_NoTrailingChars(t *testing.T) {
	setWindowsOSForTest(t)
	assert.Equal(t, "Videos", stripTrailingChars("Videos"))
	sep := string(filepath.Separator)
	assert.Equal(t, "a"+sep+"b", stripTrailingChars("a"+sep+"b"))
}

func TestStripTrailingChars_SimWin_EmptyString(t *testing.T) {
	setWindowsOSForTest(t)
	assert.Equal(t, "", stripTrailingChars(""))
}

func TestStripTrailingChars_SimWin_DotsInMiddlePreserved(t *testing.T) {
	setWindowsOSForTest(t)
	// Only TRAILING dots/spaces are stripped; dots in the middle are preserved
	assert.Equal(t, "file.txt", stripTrailingChars("file.txt"))
	assert.Equal(t, "file.tar", stripTrailingChars("file.tar."))
	assert.Equal(t, "my.file.txt", stripTrailingChars("my.file.txt "))
}

// ---------------------------------------------------------------------------
// isUNCPath - additional edge cases (no OS guard, fully testable)
// Note: uses Go interpreted strings, so "\\" = one literal backslash
// ---------------------------------------------------------------------------

func TestIsUNCPath_SimWin_StandardUNCPaths(t *testing.T) {
	setWindowsOSForTest(t)
	// \\server\share in Go: "\\\\server\\share"
	assert.True(t, isUNCPath("\\\\server\\share"))
	assert.True(t, isUNCPath("\\\\server\\share\\folder"))
	assert.True(t, isUNCPath("\\\\s\\s"))
	assert.True(t, isUNCPath("\\\\"))  // just double backslash
	assert.True(t, isUNCPath("\\\\a")) // starts with \\
}

func TestIsUNCPath_SimWin_ExtendedLengthUNC(t *testing.T) {
	setWindowsOSForTest(t)
	// \\?\UNC\server\share in Go: "\\\\?\\UNC\\server\\share"
	assert.True(t, isUNCPath("\\\\?\\UNC\\server\\share"))
	assert.True(t, isUNCPath("\\\\?\\unc\\server\\share"))
	assert.True(t, isUNCPath("\\\\?\\Unc\\server\\share"))
	assert.True(t, isUNCPath("\\\\?\\uNc\\server\\share"))
	assert.True(t, isUNCPath("\\\\?\\UNC")) // exactly 7 chars
}

func TestIsUNCPath_SimWin_NTNamespaceUNC(t *testing.T) {
	setWindowsOSForTest(t)
	// \??\UNC\server\share in Go: "\\??\\UNC\\server\\share"
	assert.True(t, isUNCPath("\\??\\UNC\\server\\share"))
	assert.True(t, isUNCPath("\\??\\unc\\server\\share"))
	assert.True(t, isUNCPath("\\??\\Unc\\server\\share"))
	assert.True(t, isUNCPath("\\??\\UNC")) // exactly 7 chars
}

func TestIsUNCPath_SimWin_DeviceNamespaceUNC(t *testing.T) {
	setWindowsOSForTest(t)
	// \\.\UNC\server\share in Go: "\\\\.\\UNC\\server\\share"
	assert.True(t, isUNCPath("\\\\.\\UNC\\server\\share"))
	assert.True(t, isUNCPath("\\\\.\\unc\\server\\share"))
	assert.True(t, isUNCPath("\\\\.\\Unc\\server\\share"))
	assert.True(t, isUNCPath("\\\\.\\UNC")) // exactly 7 chars
}

func TestIsUNCPath_SimWin_NonUNCPaths(t *testing.T) {
	setWindowsOSForTest(t)
	// C:\Videos in Go: "C:\\Videos"
	assert.False(t, isUNCPath("C:\\Videos"))
	assert.False(t, isUNCPath("Videos"))
	assert.False(t, isUNCPath("/unix/path"))
	assert.False(t, isUNCPath(""))
	assert.False(t, isUNCPath("\\"))       // single backslash
	assert.False(t, isUNCPath("a"))        // 1 char
	assert.False(t, isUNCPath("ab"))       // 2 chars, no backslash
	assert.False(t, isUNCPath("//server")) // forward slashes
	assert.False(t, isUNCPath("\\server")) // single backslash prefix
}

func TestIsUNCPath_SimWin_BoundaryLengths(t *testing.T) {
	setWindowsOSForTest(t)
	// Length < 2
	assert.False(t, isUNCPath(""))
	assert.False(t, isUNCPath("x"))
	// Length exactly 2
	assert.True(t, isUNCPath("\\\\")) // standard UNC (\\)
	assert.False(t, isUNCPath("ab"))  // not backslashes

	// Extended-length UNC prefix: \\?\unc requires len >= 7
	// But \\?\UN starts with \\ so it IS a standard UNC
	assert.True(t, isUNCPath("\\\\?\\UN"))  // starts with \\, standard UNC
	assert.True(t, isUNCPath("\\\\?\\UNC")) // matches extended-length UNC AND standard UNC
	// \\?\UNX starts with \\ so isUNCPath returns true via standard UNC check
	assert.True(t, isUNCPath("\\\\?\\UNX")) // starts with \\ = standard UNC

	// NT namespace UNC prefix: \??\unc requires len >= 7
	// \??\UN does NOT start with \\ so it is NOT standard UNC
	assert.False(t, isUNCPath("\\??\\UN"))  // starts with \? (single), too short for extended
	assert.False(t, isUNCPath("\\??\\UNX")) // starts with \?, not matching UNC pattern
	assert.True(t, isUNCPath("\\??\\UNC"))  // matches NT namespace UNC pattern

	// Device namespace UNC prefix: \\.\unc requires len >= 7
	// \\.\UN starts with \\ so it IS a standard UNC
	assert.True(t, isUNCPath("\\\\.\\UN"))  // starts with \\, standard UNC
	assert.True(t, isUNCPath("\\\\.\\UNC")) // matches device namespace UNC AND standard UNC
	assert.True(t, isUNCPath("\\\\.\\UNX")) // starts with \\ = standard UNC
}

func TestIsUNCPath_SimWin_ForwardSlashNotUNC(t *testing.T) {
	setWindowsOSForTest(t)
	assert.False(t, isUNCPath("//?/UNC/server"))
	assert.False(t, isUNCPath("/??/UNC/server"))
}

// ---------------------------------------------------------------------------
// normalizeWindowsPath - comprehensive branch coverage via simulated Windows
// Note: uses Go interpreted strings, so "\\" = one literal backslash
// \\?\ prefix in Go: "\\\\?\\"
// \??\ prefix in Go: "\\??\\"
// \\.\ prefix in Go: "\\\\.\\"
// ---------------------------------------------------------------------------

func TestNormalizeWindowsPath_SimWin_Win32UNCPrefix(t *testing.T) {
	setWindowsOSForTest(t)
	// \\?\UNC\ prefix stripping: \\?\UNC\server\share -> \\server\share
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\?\\UNC\\server\\share"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\?\\unc\\server\\share"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\?\\Unc\\server\\share"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\?\\uNc\\server\\share"))
	assert.Equal(t, "\\\\server", normalizeWindowsPath("\\\\?\\UNC\\server"))
	assert.Equal(t, "\\\\", normalizeWindowsPath("\\\\?\\UNC\\"))
	assert.Equal(t, "UNC", normalizeWindowsPath("\\\\?\\UNC"))
}

func TestNormalizeWindowsPath_SimWin_NTUNCPrefix(t *testing.T) {
	setWindowsOSForTest(t)
	// \??\UNC\ prefix stripping: \??\UNC\server\share -> \\server\share
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\??\\UNC\\server\\share"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\??\\unc\\server\\share"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\??\\Unc\\server\\share"))
	assert.Equal(t, "\\\\server", normalizeWindowsPath("\\??\\UNC\\server"))
	assert.Equal(t, "\\\\", normalizeWindowsPath("\\??\\UNC\\"))
	assert.Equal(t, "UNC", normalizeWindowsPath("\\??\\UNC"))
}

func TestNormalizeWindowsPath_SimWin_DeviceUNCPrefix(t *testing.T) {
	setWindowsOSForTest(t)
	// \\.\UNC\ prefix stripping: \\.\UNC\server\share -> \\server\share
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\.\\UNC\\server\\share"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\.\\unc\\server\\share"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\.\\Unc\\server\\share"))
	assert.Equal(t, "\\\\server", normalizeWindowsPath("\\\\.\\UNC\\server"))
	assert.Equal(t, "\\\\", normalizeWindowsPath("\\\\.\\UNC\\"))
	assert.Equal(t, "UNC", normalizeWindowsPath("\\\\.\\UNC"))
}

func TestNormalizeWindowsPath_SimWin_Win32ExtendedPrefix(t *testing.T) {
	setWindowsOSForTest(t)
	// \\?\ prefix stripping: \\?\C:\Windows -> C:\Windows
	assert.Equal(t, "C:\\Windows", normalizeWindowsPath("\\\\?\\C:\\Windows"))
	assert.Equal(t, "D:\\Data", normalizeWindowsPath("\\\\?\\D:\\Data"))
	assert.Equal(t, "c:\\windows", normalizeWindowsPath("\\\\?\\c:\\windows"))
	assert.Equal(t, "", normalizeWindowsPath("\\\\?\\"))
	assert.Equal(t, "C:", normalizeWindowsPath("\\\\?\\C:"))
}

func TestNormalizeWindowsPath_SimWin_NTNamespacePrefix(t *testing.T) {
	setWindowsOSForTest(t)
	// \??\ prefix stripping: \??\C:\Windows -> C:\Windows
	assert.Equal(t, "C:\\Windows", normalizeWindowsPath("\\??\\C:\\Windows"))
	assert.Equal(t, "D:\\Data", normalizeWindowsPath("\\??\\D:\\Data"))
	assert.Equal(t, "c:\\windows", normalizeWindowsPath("\\??\\c:\\windows"))
	assert.Equal(t, "", normalizeWindowsPath("\\??\\"))
}

func TestNormalizeWindowsPath_SimWin_DeviceNamespacePrefix(t *testing.T) {
	setWindowsOSForTest(t)
	// \\.\ prefix stripping: \\.\C:\Windows -> C:\Windows
	assert.Equal(t, "C:\\Windows", normalizeWindowsPath("\\\\.\\C:\\Windows"))
	assert.Equal(t, "D:\\Data", normalizeWindowsPath("\\\\.\\D:\\Data"))
	assert.Equal(t, "c:\\windows", normalizeWindowsPath("\\\\.\\c:\\windows"))
	assert.Equal(t, "", normalizeWindowsPath("\\\\.\\"))
}

func TestNormalizeWindowsPath_SimWin_RegularPaths(t *testing.T) {
	setWindowsOSForTest(t)
	assert.Equal(t, "C:\\Windows", normalizeWindowsPath("C:\\Windows"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\server\\share"))
	assert.Equal(t, "folder\\file.txt", normalizeWindowsPath("folder\\file.txt"))
	assert.Equal(t, "", normalizeWindowsPath(""))
}

func TestNormalizeWindowsPath_SimWin_AttackVectors(t *testing.T) {
	setWindowsOSForTest(t)
	// GLOBALROOT shadow copy: \\?\GLOBALROOT\... -> GLOBALROOT\...
	assert.Equal(t, "GLOBALROOT\\Device\\HarddiskVolumeShadowCopy1\\Windows",
		normalizeWindowsPath("\\\\?\\GLOBALROOT\\Device\\HarddiskVolumeShadowCopy1\\Windows"))
	assert.Equal(t, "GLOBALROOT\\Device\\HarddiskVolumeShadowCopy1\\Windows",
		normalizeWindowsPath("\\??\\GLOBALROOT\\Device\\HarddiskVolumeShadowCopy1\\Windows"))
	assert.Equal(t, "GLOBALROOT\\Device\\HarddiskVolumeShadowCopy1\\Windows",
		normalizeWindowsPath("\\\\.\\GLOBALROOT\\Device\\HarddiskVolumeShadowCopy1\\Windows"))
	// Named pipes: \\?\pipe\mypipe -> pipe\mypipe
	assert.Equal(t, "pipe\\mypipe", normalizeWindowsPath("\\\\?\\pipe\\mypipe"))
	assert.Equal(t, "pipe\\mypipe", normalizeWindowsPath("\\??\\pipe\\mypipe"))
	assert.Equal(t, "pipe\\mypipe", normalizeWindowsPath("\\\\.\\pipe\\mypipe"))
	// Volume GUID: \\?\Volume{...}\Windows -> Volume{...}\Windows
	assert.Equal(t, "Volume{12345678-1234-1234-1234-123456789012}\\Windows",
		normalizeWindowsPath("\\\\?\\Volume{12345678-1234-1234-1234-123456789012}\\Windows"))
	assert.Equal(t, "Volume{12345678-1234-1234-1234-123456789012}\\Windows",
		normalizeWindowsPath("\\??\\Volume{12345678-1234-1234-1234-123456789012}\\Windows"))
	assert.Equal(t, "Volume{12345678-1234-1234-1234-123456789012}\\Windows",
		normalizeWindowsPath("\\\\.\\Volume{12345678-1234-1234-1234-123456789012}\\Windows"))
}

func TestNormalizeWindowsPath_SimWin_UNCPrefixPrecedence(t *testing.T) {
	setWindowsOSForTest(t)
	// UNC prefixes are checked BEFORE non-UNC prefixes
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\?\\UNC\\server\\share"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\\\.\\UNC\\server\\share"))
	assert.Equal(t, "\\\\server\\share", normalizeWindowsPath("\\??\\UNC\\server\\share"))
}

// ---------------------------------------------------------------------------
// normalizeUNCPath - comprehensive branch coverage via simulated Windows
// ---------------------------------------------------------------------------

func TestNormalizeUNCPath_SimWin_NonUNCPath(t *testing.T) {
	setWindowsOSForTest(t)
	result, err := normalizeUNCPath("C:\\Videos", true, nil)
	assert.NoError(t, err)
	assert.Equal(t, "C:\\Videos", result)
}

func TestNormalizeUNCPath_SimWin_UNCBlocked(t *testing.T) {
	setWindowsOSForTest(t)
	_, err := normalizeUNCPath("\\\\server\\share", false, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrUNCPathBlocked))
}

func TestNormalizeUNCPath_SimWin_UNCAllowed_ServerAllowed(t *testing.T) {
	setWindowsOSForTest(t)
	result, err := normalizeUNCPath("\\\\server\\share", true, []string{"server"})
	require.NoError(t, err)
	assert.Equal(t, "\\\\server\\share", result)

	// Case-insensitive server matching
	result, err = normalizeUNCPath("\\\\Server\\share", true, []string{"SERVER"})
	require.NoError(t, err)
	assert.Equal(t, "\\\\Server\\share", result)

	result, err = normalizeUNCPath("\\\\MY-SERVER\\share", true, []string{"my-server"})
	require.NoError(t, err)
	assert.Equal(t, "\\\\MY-SERVER\\share", result)
}

func TestNormalizeUNCPath_SimWin_UNCAllowed_ServerNotAllowed(t *testing.T) {
	setWindowsOSForTest(t)
	_, err := normalizeUNCPath("\\\\evil\\share", true, []string{"good"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrUNCPathBlocked))

	// Empty allowed list
	_, err = normalizeUNCPath("\\\\server\\share", true, []string{})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrUNCPathBlocked))

	// Nil allowed list
	_, err = normalizeUNCPath("\\\\server\\share", true, nil)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrUNCPathBlocked))
}

func TestNormalizeUNCPath_SimWin_UNCServerOnlyNoShare(t *testing.T) {
	setWindowsOSForTest(t)
	// Server name without trailing backslash: serverEnd == -1
	_, err := normalizeUNCPath("\\\\server", true, []string{"server"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrUNCPathBlocked))
}

func TestNormalizeUNCPath_SimWin_ExtendedPrefixNormalization(t *testing.T) {
	setWindowsOSForTest(t)
	// Extended UNC prefix gets normalized before server extraction
	result, err := normalizeUNCPath("\\\\?\\UNC\\server\\share", true, []string{"server"})
	require.NoError(t, err)
	assert.Equal(t, "\\\\server\\share", result)

	result, err = normalizeUNCPath("\\\\.\\UNC\\server\\share", true, []string{"server"})
	require.NoError(t, err)
	assert.Equal(t, "\\\\server\\share", result)

	result, err = normalizeUNCPath("\\??\\UNC\\server\\share", true, []string{"server"})
	require.NoError(t, err)
	assert.Equal(t, "\\\\server\\share", result)
}

func TestNormalizeUNCPath_SimWin_ExtendedPrefixServerNotAllowed(t *testing.T) {
	setWindowsOSForTest(t)
	_, err := normalizeUNCPath("\\\\?\\UNC\\evil\\share", true, []string{"good"})
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrUNCPathBlocked))
}

func TestNormalizeUNCPath_SimWin_StandardPathAllowed(t *testing.T) {
	setWindowsOSForTest(t)
	result, err := normalizeUNCPath("\\\\server\\share", true, []string{"server"})
	require.NoError(t, err)
	assert.Equal(t, "\\\\server\\share", result)
}

// ---------------------------------------------------------------------------
// normalizePathForPlatform - comprehensive branch coverage via simulated Windows
// ---------------------------------------------------------------------------

func TestNormalizePathForPlatform_SimWin_Windows(t *testing.T) {
	setWindowsOSForTest(t)
	// stripTrailingChars uses string(filepath.Separator) which is "/" on macOS
	assert.Equal(t, "Videos", normalizePathForPlatform("Videos."))
	assert.Equal(t, "Videos", normalizePathForPlatform("Videos "))
	assert.Equal(t, "", normalizePathForPlatform(""))
	assert.Equal(t, "Videos", normalizePathForPlatform("Videos"))
}

func TestNormalizePathForPlatform_SimWin_ExtendedPrefix(t *testing.T) {
	setWindowsOSForTest(t)
	// normalizeWindowsPath strips the prefix, stripTrailingChars is no-op on clean paths
	assert.Equal(t, "C:\\Windows", normalizePathForPlatform("\\\\?\\C:\\Windows"))
}

// ---------------------------------------------------------------------------
// ValidateAndOpenPath - additional branch coverage
// ---------------------------------------------------------------------------

func TestValidateAndOpenPath_SimWin_SuccessPath(t *testing.T) {
	setWindowsOSForTest(t)
	tempDir := t.TempDir()
	cfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{tempDir},
		DeniedDirectories:  []string{},
	}
	f, canonicalPath, err := ValidateAndOpenPath(tempDir, cfg)
	require.NoError(t, err)
	defer f.Close()
	assert.NotNil(t, f)
	assert.True(t, filepath.IsAbs(canonicalPath))
	info, err := f.Stat()
	require.NoError(t, err)
	assert.True(t, info.IsDir())
}

func TestValidateAndOpenPath_SimWin_FileNotDir(t *testing.T) {
	setWindowsOSForTest(t)
	tempDir := t.TempDir()
	tempFile := filepath.Join(tempDir, "file.txt")
	require.NoError(t, os.WriteFile(tempFile, []byte("test"), 0644))
	cfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{tempDir},
	}
	f, path, err := ValidateAndOpenPath(tempFile, cfg)
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrPathNotDir))
	assert.Nil(t, f)
	assert.Empty(t, path)
}

func TestValidateAndOpenPath_SimWin_Subdirectory(t *testing.T) {
	setWindowsOSForTest(t)
	tempDir := t.TempDir()
	subDir := filepath.Join(tempDir, "subdir")
	require.NoError(t, os.Mkdir(subDir, 0755))
	cfg := &SecurityNarrowConfig{
		AllowedDirectories: []string{tempDir},
	}
	f, path, err := ValidateAndOpenPath(subDir, cfg)
	require.NoError(t, err)
	defer f.Close()
	assert.NotNil(t, f)
	assert.True(t, filepath.IsAbs(path))
	assert.Contains(t, path, "subdir")
}

// ---------------------------------------------------------------------------
// Integration: currentOS variable affects end-to-end path validation
// ---------------------------------------------------------------------------

func TestPathValidation_SimWin_ReservedNameViaPathValidator(t *testing.T) {
	setWindowsOSForTest(t)
	tempDir := t.TempDir()
	v := NewPathValidator(afero.NewOsFs(), []string{tempDir}, nil)
	_, err := v.ValidateDir(filepath.Join(tempDir, "CON"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrReservedDeviceName))
	_, err = v.ValidateDir(filepath.Join(tempDir, "NUL"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrReservedDeviceName))
	_, err = v.ValidateDir(filepath.Join(tempDir, "COM1"))
	require.Error(t, err)
	assert.True(t, errors.Is(err, apperrors.ErrReservedDeviceName))
}

func TestPathValidation_SimWin_UNCBlocked(t *testing.T) {
	setWindowsOSForTest(t)
	// When currentOS is "windows", UNC paths should be checked and blocked
	// But on macOS, the UNC path \\server\share won't resolve through
	// filepath.Abs/Clean the same way. The test verifies the code path
	// reaches the UNC check in the validator.
	// On macOS, UNC path strings with backslashes may not flow through
	// the validator the same as on Windows. Test the core UNC check directly.
	assert.True(t, isUNCPath("\\\\server\\share"))
	assert.True(t, isUNCPath("\\\\server\\share"))
	_, err := normalizeUNCPath("\\\\server\\share", false, nil)
	assert.Error(t, err, "UNC path with allowUNC=false should be blocked")
}

func TestPathValidation_SimWin_UNCAllowedButPathMissing(t *testing.T) {
	setWindowsOSForTest(t)
	v := NewPathValidatorWithUNC(
		afero.NewOsFs(),
		[]string{"\\\\server\\share"},
		nil,
		true,
		[]string{"server"},
	)
	_, err := v.ValidateDir("\\\\server\\share")
	// Should not be blocked by UNC, but path doesn't exist on disk
	if err != nil {
		assert.False(t, errors.Is(err, apperrors.ErrUNCPathBlocked),
			"UNC should be allowed but got ErrUNCPathBlocked: %v", err)
	}
}
