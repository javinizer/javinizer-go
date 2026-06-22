package organizer

import (
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- DetectPathEncodingInfo ---

func TestDetectPathEncodingInfo_EmptySource(t *testing.T) {
	info := DetectPathEncodingInfo("", "")
	assert.Equal(t, PathEncodingPOSIX, info.Encoding)
}

func TestDetectPathEncodingInfo_UNCPath(t *testing.T) {
	info := DetectPathEncodingInfo(`\\server\share\file.mp4`, `\\dest\share`)
	assert.Equal(t, PathEncodingUNC, info.Encoding)
	assert.Equal(t, `\\server\share\file.mp4`, info.OriginalSource)
	assert.Equal(t, `\\dest\share`, info.Destination)
}

func TestDetectPathEncodingInfo_WindowsLocalPath(t *testing.T) {
	info := DetectPathEncodingInfo(`C:\Users\test\file.mp4`, "")
	assert.Equal(t, PathEncodingWindows, info.Encoding)
}

func TestDetectPathEncodingInfo_POSIXPath(t *testing.T) {
	info := DetectPathEncodingInfo("/home/user/file.mp4", "")
	assert.Equal(t, PathEncodingPOSIX, info.Encoding)
}

func TestDetectPathEncodingInfo_ShortPathNotWindows(t *testing.T) {
	// Path with only 1 character — should not match drive letter check
	info := DetectPathEncodingInfo("A", "")
	assert.Equal(t, PathEncodingPOSIX, info.Encoding)
}

// --- PathEncodingInfo.PrepareMatchPath ---

func TestPrepareMatchPath_UNC(t *testing.T) {
	info := PathEncodingInfo{Encoding: PathEncodingUNC}
	result := info.PrepareMatchPath(`\\server\share\file.mp4`)
	assert.Equal(t, "//server/share/file.mp4", result)
}

func TestPrepareMatchPath_POSIX(t *testing.T) {
	info := PathEncodingInfo{Encoding: PathEncodingPOSIX}
	result := info.PrepareMatchPath("/home/user/file.mp4")
	assert.Equal(t, "/home/user/file.mp4", result)
}

func TestPrepareMatchPath_Windows(t *testing.T) {
	info := PathEncodingInfo{Encoding: PathEncodingWindows}
	// Windows local paths returned as-is
	result := info.PrepareMatchPath(`C:\Users\file.mp4`)
	assert.Equal(t, `C:\Users\file.mp4`, result)
}

// --- PathEncodingInfo.PrepareDestination ---

func TestPrepareDestination_UNC(t *testing.T) {
	info := PathEncodingInfo{Encoding: PathEncodingUNC}
	result := info.PrepareDestination(`\\server\share\dest`)
	assert.Equal(t, "//server/share/dest", result)
}

func TestPrepareDestination_POSIX(t *testing.T) {
	info := PathEncodingInfo{Encoding: PathEncodingPOSIX}
	result := info.PrepareDestination("/home/user/dest")
	assert.Equal(t, "/home/user/dest", result)
}

func TestPrepareDestination_Windows(t *testing.T) {
	info := PathEncodingInfo{Encoding: PathEncodingWindows}
	result := info.PrepareDestination(`D:\output`)
	assert.Equal(t, `D:\output`, result)
}

// --- OrganizePlan.EncodePaths ---

func TestEncodePaths_POSIX(t *testing.T) {
	plan := &OrganizePlan{
		TargetPath:    "/dest/ABC-123/ABC-123.mp4",
		TargetDir:     "/dest/ABC-123",
		SourcePath:    "/source/ABC-123.mp4",
		SubfolderPath: "subfolder",
	}
	info := PathEncodingInfo{Encoding: PathEncodingPOSIX}
	encoded := plan.EncodePaths(info)
	assert.Equal(t, "/dest/ABC-123/ABC-123.mp4", encoded.TargetPath)
	assert.Equal(t, "/dest/ABC-123", encoded.TargetDir)
	assert.Equal(t, "/source/ABC-123.mp4", encoded.SourcePath)
	assert.Equal(t, "subfolder", encoded.SubfolderPath)
}

func TestEncodePaths_Windows(t *testing.T) {
	plan := &OrganizePlan{
		TargetPath:    "/dest/ABC-123/ABC-123.mp4",
		TargetDir:     "/dest/ABC-123",
		SourcePath:    "/source/ABC-123.mp4",
		SubfolderPath: "sub/folder",
	}
	info := PathEncodingInfo{Encoding: PathEncodingWindows}
	encoded := plan.EncodePaths(info)
	assert.Equal(t, `\dest\ABC-123\ABC-123.mp4`, encoded.TargetPath)
	assert.Equal(t, `\dest\ABC-123`, encoded.TargetDir)
	assert.Equal(t, `\source\ABC-123.mp4`, encoded.SourcePath)
	assert.Equal(t, `sub\folder`, encoded.SubfolderPath)
}

func TestEncodePaths_UNC(t *testing.T) {
	plan := &OrganizePlan{
		TargetPath:    "/share/ABC-123/ABC-123.mp4",
		TargetDir:     "/share/ABC-123",
		TargetFile:    "ABC-123.mp4",
		SourcePath:    "/share/original.mp4",
		SubfolderPath: "sub/folder",
		FolderName:    "ABC-123",
	}
	info := PathEncodingInfo{
		Encoding:       PathEncodingUNC,
		OriginalSource: `\\server\share\original.mp4`,
		Destination:    `\\server\output`,
	}
	encoded := plan.EncodePaths(info)
	// SourcePath should be original UNC source
	assert.Equal(t, `\\server\share\original.mp4`, encoded.SourcePath)
	// SubfolderPath should be backslash-converted
	assert.Equal(t, `sub\folder`, encoded.SubfolderPath)
	// TargetPath should be a UNC path (rebuilt)
	assert.Contains(t, encoded.TargetPath, `\\server`)
}

// --- toPosixPath ---

func TestToPosixPath_WindowsPath(t *testing.T) {
	result := toPosixPath(`C:\Users\file.mp4`)
	assert.Equal(t, "C:/Users/file.mp4", result)
}

func TestToPosixPath_UNCPath(t *testing.T) {
	result := toPosixPath(`\\server\share\file.mp4`)
	assert.Equal(t, "//server/share/file.mp4", result)
}

func TestToPosixPath_POSIXPath(t *testing.T) {
	result := toPosixPath("/home/user/file.mp4")
	assert.Equal(t, "/home/user/file.mp4", result)
}

func TestToPosixPath_MixedSlashes(t *testing.T) {
	result := toPosixPath(`/home\user/file.mp4`)
	assert.Equal(t, "/home/user/file.mp4", result)
}

// --- toBackslashPath ---

func TestToBackslashPath_POSIXPath(t *testing.T) {
	result := toBackslashPath("/home/user/file.mp4")
	assert.Equal(t, `\home\user\file.mp4`, result)
}

func TestToBackslashPath_Empty(t *testing.T) {
	result := toBackslashPath("")
	assert.Equal(t, "", result)
}

func TestToBackslashPath_NoSlashes(t *testing.T) {
	result := toBackslashPath("file.mp4")
	assert.Equal(t, "file.mp4", result)
}

// --- IsWindowsPathLike ---

func TestIsWindowsPathLike_DriveLetter(t *testing.T) {
	assert.True(t, IsWindowsPathLike(`C:\path`))
}

func TestIsWindowsPathLike_UNC(t *testing.T) {
	assert.True(t, IsWindowsPathLike(`\\server\share`))
}

func TestIsWindowsPathLike_POSIX(t *testing.T) {
	assert.False(t, IsWindowsPathLike("/home/user"))
}

func TestIsWindowsPathLike_Empty(t *testing.T) {
	assert.False(t, IsWindowsPathLike(""))
}

func TestIsWindowsPathLike_Short(t *testing.T) {
	assert.False(t, IsWindowsPathLike("A"))
}

// --- JoinPath (exported) ---

func TestJoinPath_POSIXBase(t *testing.T) {
	result := JoinPath("/base", "sub", "file.mp4")
	assert.Equal(t, filepath.Join("/base", "sub", "file.mp4"), result)
}

func TestJoinPath_WindowsBase(t *testing.T) {
	result := JoinPath(`C:\base`, `sub`, `file.mp4`)
	assert.Equal(t, `C:\base\sub\file.mp4`, result)
}

func TestJoinPath_UNCBase(t *testing.T) {
	result := JoinPath(`\\server\share`, `folder`, `file.mp4`)
	assert.Equal(t, `\\server\share\folder\file.mp4`, result)
}

func TestJoinPath_EmptyBase(t *testing.T) {
	result := JoinPath("", "sub", "file.mp4")
	assert.Equal(t, filepath.Join("sub", "file.mp4"), result)
}

func TestJoinPath_EmptyElems(t *testing.T) {
	result := JoinPath(`/base`, "", `file.mp4`)
	assert.Equal(t, filepath.Join("/base", "file.mp4"), result)
}

func TestJoinPath_TrailingSeparator(t *testing.T) {
	result := JoinPath(`C:\base\`, `sub`, `file.mp4`)
	assert.Equal(t, `C:\base\sub\file.mp4`, result)
}

// --- joinPathUNC internal ---

func TestJoinPathUNC_WindowsBase(t *testing.T) {
	result := joinPathUNC(`C:\base`, `sub`)
	assert.Equal(t, `C:\base\sub`, result)
}

func TestJoinPathUNC_POSIXBase(t *testing.T) {
	result := joinPathUNC("/base", "sub")
	assert.Equal(t, filepath.FromSlash("/base/sub"), result)
}

// --- pathDir ---

func TestPathDir_POSIXPath(t *testing.T) {
	assert.Equal(t, "/home/user", pathDir("/home/user/file.mp4"))
}

func TestPathDir_SingleComponent(t *testing.T) {
	assert.Equal(t, ".", pathDir("file.mp4"))
}

func TestPathDir_Empty(t *testing.T) {
	assert.Equal(t, ".", pathDir(""))
}

func TestPathDir_RootSlash(t *testing.T) {
	assert.Equal(t, "/", pathDir("/file.mp4"))
}

func TestPathDir_WindowsDriveRoot(t *testing.T) {
	assert.Equal(t, `C:\`, pathDir(`C:\file.mp4`))
}

func TestPathDir_UNCPath(t *testing.T) {
	assert.Equal(t, `\\server\share`, pathDir(`\\server\share\file.mp4`))
}

func TestPathDir_UNCPathDeep(t *testing.T) {
	assert.Equal(t, `\\server\share\folder`, pathDir(`\\server\share\folder\file.mp4`))
}

func TestPathDir_TrailingSeparator(t *testing.T) {
	assert.Equal(t, "/home/user", pathDir("/home/user/dir/"))
}

func TestPathDir_AllSeparators(t *testing.T) {
	result := pathDir("///")
	// After trimTrailingSeparator, "///" becomes "/" — then LastIndexAny finds no additional separator
	// so pathDir returns "."
	assert.Equal(t, ".", result)
}

func TestPathDir_WindowsDeepPath(t *testing.T) {
	assert.Equal(t, `C:\Users\test`, pathDir(`C:\Users\test\file.mp4`))
}

// --- isUNCPath ---

func TestIsUNCPath_Backslash(t *testing.T) {
	assert.True(t, isUNCPath(`\\server\share`))
}

func TestIsUNCPath_ForwardSlash(t *testing.T) {
	assert.True(t, isUNCPath("//server/share"))
}

func TestIsUNCPath_POSIX(t *testing.T) {
	assert.False(t, isUNCPath("/home/user"))
}

func TestIsUNCPath_Empty(t *testing.T) {
	assert.False(t, isUNCPath(""))
}

// --- uncShareRoot ---

func TestUncShareRoot_UNCPath(t *testing.T) {
	result := uncShareRoot(`\\server\share\folder\file.mp4`)
	assert.Equal(t, `\\server\share`, result)
}

func TestUncShareRoot_UNCPathNoFolder(t *testing.T) {
	result := uncShareRoot(`\\server\share`)
	assert.Equal(t, `\\server\share`, result)
}

func TestUncShareRoot_UNCPathServerOnly(t *testing.T) {
	result := uncShareRoot(`\\server`)
	assert.Equal(t, `\\server`, result)
}

func TestUncShareRoot_NonUNC(t *testing.T) {
	result := uncShareRoot(`/home/user`)
	assert.Equal(t, "", result)
}

func TestUncShareRoot_ForwardSlashUNC(t *testing.T) {
	result := uncShareRoot("//server/share/folder")
	assert.Equal(t, "//server/share", result)
}

// --- TrimTrailingSeparators ---

func TestTrimTrailingSeparators_Slashes(t *testing.T) {
	assert.Equal(t, "/home/user", TrimTrailingSeparators("/home/user/"))
}

func TestTrimTrailingSeparators_Backslashes(t *testing.T) {
	assert.Equal(t, `C:\path`, TrimTrailingSeparators(`C:\path\`))
}

func TestTrimTrailingSeparators_Mixed(t *testing.T) {
	assert.Equal(t, "/path", TrimTrailingSeparators("/path/\\"))
}

func TestTrimTrailingSeparators_None(t *testing.T) {
	assert.Equal(t, "/path", TrimTrailingSeparators("/path"))
}

func TestTrimTrailingSeparators_Empty(t *testing.T) {
	assert.Equal(t, "", TrimTrailingSeparators(""))
}

func TestTrimTrailingSeparators_AllSeparators(t *testing.T) {
	assert.Equal(t, "", TrimTrailingSeparators("///\\\\"))
}

// --- trimTrailingSeparator (internal) ---

func TestTrimTrailingSeparator_Basic(t *testing.T) {
	assert.Equal(t, "path", trimTrailingSeparator("path/"))
}

// --- rebuildUNCPath (tested via EncodePaths, but direct test for completeness) ---

func TestRebuildUNCPath_Basic(t *testing.T) {
	plan := &OrganizePlan{
		TargetFile:         "ABC-123.mp4",
		TargetDir:          "/share/ABC-123",
		FolderName:         "ABC-123",
		SubfolderPath:      "",
		PreserveSourcePath: false,
		RenameFolder:       false,
	}
	result := rebuildUNCPath(plan, `\\server\share\original.mp4`, `\\server\output`)
	assert.Contains(t, result, `\\server`)
	assert.Contains(t, result, "ABC-123.mp4")
}

func TestRebuildUNCPath_PreserveSourcePath(t *testing.T) {
	plan := &OrganizePlan{
		TargetFile:         "ABC-123.mp4",
		PreserveSourcePath: true,
	}
	result := rebuildUNCPath(plan, `\\server\share\original.mp4`, `\\server\output`)
	// With PreserveSourcePath, targetDir = sourceDir, then join with TargetFile
	assert.Contains(t, result, `\\server\share`)
	assert.Contains(t, result, "ABC-123.mp4")
}

// --- rebuildUNCTargetDir ---

func TestRebuildUNCTargetDir_PreserveSourcePath(t *testing.T) {
	plan := &OrganizePlan{PreserveSourcePath: true}
	result := rebuildUNCTargetDir(plan, `\\server\share\folder\file.mp4`, `\\server\output`)
	assert.Equal(t, `\\server\share\folder`, result)
}

func TestRebuildUNCTargetDir_RenameFolderInPlace(t *testing.T) {
	plan := &OrganizePlan{
		PreserveSourcePath: false,
		RenameFolder:       true,
		InPlace:            true,
		FolderName:         "NewFolder",
	}
	result := rebuildUNCTargetDir(plan, `\\server\share\OldFolder\file.mp4`, `\\server\output`)
	assert.Equal(t, `\\server\share\NewFolder`, result)
}

func TestRebuildUNCTargetDir_RenameFolderInPlaceEmptyFolderName(t *testing.T) {
	plan := &OrganizePlan{
		PreserveSourcePath: false,
		RenameFolder:       true,
		InPlace:            true,
		FolderName:         "",
	}
	result := rebuildUNCTargetDir(plan, `\\server\share\OldFolder\file.mp4`, `\\server\output`)
	// When FolderName is empty in InPlace+RenameFolder, falls back to sourceDir
	assert.Equal(t, `\\server\share\OldFolder`, result)
}

func TestRebuildUNCTargetDir_RenameFolderNotInPlace(t *testing.T) {
	plan := &OrganizePlan{
		PreserveSourcePath: false,
		RenameFolder:       true,
		InPlace:            false,
		FolderName:         "NewFolder",
		SubfolderPath:      "sub1/sub2",
	}
	result := rebuildUNCTargetDir(plan, `\\server\share\OldFolder\file.mp4`, `\\server\output`)
	// Not in-place: pathBase = destination, add subfolders, add FolderName
	assert.Contains(t, result, `\\server\output`)
	assert.Contains(t, result, "sub1")
	assert.Contains(t, result, "NewFolder")
}

func TestRebuildUNCTargetDir_WithSubfolderPath(t *testing.T) {
	plan := &OrganizePlan{
		PreserveSourcePath: false,
		RenameFolder:       false,
		SubfolderPath:      "genre/action",
		FolderName:         "ABC-123",
	}
	result := rebuildUNCTargetDir(plan, `\\server\share\original.mp4`, `\\server\output`)
	assert.Contains(t, result, `\\server\output`)
	assert.Contains(t, result, "genre")
	assert.Contains(t, result, "action")
	assert.Contains(t, result, "ABC-123")
}

func TestRebuildUNCTargetDir_EmptyDestination(t *testing.T) {
	plan := &OrganizePlan{
		PreserveSourcePath: false,
		RenameFolder:       false,
		FolderName:         "ABC-123",
	}
	result := rebuildUNCTargetDir(plan, `\\server\share\original.mp4`, "")
	// Empty destination falls back to sourceDir
	assert.Contains(t, result, `\\server\share`)
	assert.Contains(t, result, "ABC-123")
}

func TestRebuildUNCTargetDir_NoFolderName(t *testing.T) {
	plan := &OrganizePlan{
		PreserveSourcePath: false,
		RenameFolder:       false,
		FolderName:         "",
	}
	result := rebuildUNCTargetDir(plan, `\\server\share\original.mp4`, `\\server\output`)
	assert.Equal(t, `\\server\output`, result)
}

func TestRebuildUNCTargetDir_SubfolderPathWithEmptyParts(t *testing.T) {
	plan := &OrganizePlan{
		PreserveSourcePath: false,
		RenameFolder:       false,
		SubfolderPath:      "/genre//action/",
		FolderName:         "ABC-123",
	}
	result := rebuildUNCTargetDir(plan, `\\server\share\original.mp4`, `\\server\output`)
	assert.Contains(t, result, "genre")
	assert.Contains(t, result, "action")
	assert.Contains(t, result, "ABC-123")
}
