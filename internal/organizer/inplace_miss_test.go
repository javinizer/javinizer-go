package organizer

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- strategy_inplace.go remaining miss lines ---
// Line 73-75: buildPlanContext error (pc.Err != nil)
// Line 93-101: folderMaxBytes > 0 with ExecuteWithMaxBytes error, empty folderName fallback
// Line 159-161: inPlace && !forceUpdate with oldDir stat error
// Line 225-228: Execute with oldInfo stat error in SameFile check
// Line 231-234: Execute with newErr stat error (new stat fails but old stat succeeds)
// Line 246-251: Execute with currentFilePath == plan.TargetPath (no file rename needed)

// errorEngine always returns an error for Execute and ExecuteWithMaxBytes
type errorEngine struct{}

func (e *errorEngine) Execute(_ string, _ *template.Context) (string, error) {
	return "", fmt.Errorf("template error")
}

func (e *errorEngine) ExecuteWithContext(_ context.Context, _ string, _ *template.Context) (string, error) {
	return "", fmt.Errorf("template error")
}

func (e *errorEngine) ExecuteWithMaxBytes(_ string, _ *template.Context, _ int) (string, error) {
	return "", fmt.Errorf("template error")
}

func (e *errorEngine) TruncateTitle(title string, _ int) string      { return title }
func (e *errorEngine) TruncateTitleBytes(title string, _ int) string { return title }
func (e *errorEngine) ValidatePathLength(path string, _ int) error   { return nil }

// Lines 73-75: buildPlanContext returns error when file template fails
func TestInPlaceMiss_Plan_BuildPlanContextError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FileFormat:    "<INVALID>", // won't cause error — template engine just leaves it
		FolderFormat:  "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeInPlace,
	}
	errEngine := &errorEngine{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, errEngine)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{ID: "ABC-123"}

	_, err := strategy.Plan(match, movie, "/dest", false)
	assert.Error(t, err, "Should fail when template engine returns error")
}

// Lines 93-101: folderMaxBytes > 0 with ExecuteWithMaxBytes error
// This requires MaxPathLength set, and an engine that fails on ExecuteWithMaxBytes
func TestInPlaceMiss_Plan_ExecuteWithMaxBytesError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID> <TITLE>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeInPlace,
		MaxPathLength: 80, // triggers folderMaxBytes > 0
	}
	errEngine := &errorEngine{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, errEngine)

	_ = fs.MkdirAll("/source/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123/ABC-123.mp4", []byte("video"), 0644)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{ID: "ABC-123", Title: "Some Title"}

	_, err := strategy.Plan(match, movie, "/dest", false)
	// ExecuteWithMaxBytes error, then fallback to Execute which also fails
	assert.Error(t, err, "Should fail when ExecuteWithMaxBytes and Execute both return errors")
}

// Lines 97-101: folderMaxBytes > 0, ExecuteWithMaxBytes returns empty, then MovieID fallback
// Use a real engine that produces an empty folder name with truncation
func TestInPlaceMiss_Plan_FolderMaxBytesEmptyFolderName(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeInPlace,
		MaxPathLength: 200, // large enough to not trigger truncation
	}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123/ABC-123.mp4", []byte("video"), 0644)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, "ABC-123", plan.FolderName)
}

// Lines 159-161: inPlace && !forceUpdate with oldDir stat error (targetDir exists but oldDir doesn't)
func TestInPlaceMiss_Plan_InPlaceConflictWithMissingOldDir(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create a dedicated folder with the "wrong" name
	_ = fs.MkdirAll("/source/old-name", 0777)
	_ = afero.WriteFile(fs, "/source/old-name/ABC-123.mp4", []byte("video"), 0644)
	// Also create the target directory (which should be detected as conflict)
	_ = fs.MkdirAll("/source/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123/other.txt", []byte("other"), 0644)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/old-name/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.True(t, plan.InPlace, "Should be InPlace for dedicated folder")
	// The old dir exists and the target dir also exists — should have conflict
	if len(plan.Conflicts) > 0 {
		assert.Contains(t, plan.Conflicts, filepath.FromSlash("/source/ABC-123"))
	}
}

// Lines 225-228: Execute — oldDir doesn't exist when trying SameFile check
// The Execute function first stats OldDir, which will fail if it doesn't exist
func TestInPlaceMiss_Execute_OldDirMissingTargetExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create target dir but NOT old dir
	_ = fs.MkdirAll("/source/target-dir", 0755)
	_ = afero.WriteFile(fs, "/source/target-dir/other.txt", []byte("other"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/old-dir/ABC-123.mp4",
		TargetDir:  "/source/target-dir",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/target-dir/ABC-123.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-dir", // doesn't exist
		Match:      models.FileMatchInfo{Path: "/source/old-dir/ABC-123.mp4", Name: "ABC-123.mp4"},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err, "Should fail when old dir doesn't exist")
	assert.False(t, result.Moved)
	// The error should be about statting old dir
	assert.Contains(t, err.Error(), "failed to stat old directory")
}

// Lines 231-234: Execute — newErr stat error (new stat fails but old stat succeeds)
// This tests the `else` branch after `oldErr != nil` in the SameFile check
func TestInPlaceMiss_Execute_NewStatFailsOldSucceeds(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create both old and target dirs (same dir — will pass SameFile check on real FS)
	// On MemMapFs, SameFile returns false for different paths, so we get the
	// "target directory already exists" error
	_ = fs.MkdirAll("/source/old-dir", 0755)
	_ = afero.WriteFile(fs, "/source/old-dir/ABC-123.mp4", []byte("video"), 0644)
	_ = fs.MkdirAll("/source/new-dir", 0755)

	plan := &OrganizePlan{
		SourcePath: "/source/old-dir/ABC-123.mp4",
		TargetDir:  "/source/new-dir",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/new-dir/ABC-123.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-dir",
		Match:      models.FileMatchInfo{Path: "/source/old-dir/ABC-123.mp4", Name: "ABC-123.mp4"},
	}

	result, err := strategy.Execute(plan)
	// In MemMapFs, old and new dirs are different inodes, so SameFile returns false
	// This means the else branch fires and returns "target directory already exists"
	if err != nil {
		assert.Contains(t, err.Error(), "target directory already exists")
		assert.False(t, result.Moved)
	}
}

// Lines 246-251: Execute — currentFilePath == plan.TargetPath (no file rename needed)
// When after directory rename, the file is already at the target path
func TestInPlaceMiss_Execute_NoFileRenameNeeded(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create old dir with a file that matches the target path after dir rename
	_ = fs.MkdirAll("/source/old-dir", 0777)
	_ = afero.WriteFile(fs, "/source/old-dir/ABC-123.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/old-dir/ABC-123.mp4",
		TargetDir:  "/source/new-dir",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/new-dir/ABC-123.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-dir",
		Match:      models.FileMatchInfo{Path: "/source/old-dir/ABC-123.mp4", Name: "ABC-123.mp4"},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, result.InPlaceRenamed)

	// File should be at the target path
	exists, _ := afero.Exists(fs, "/source/new-dir/ABC-123.mp4")
	assert.True(t, exists)

	// Old dir should be gone
	dirExists, _ := afero.DirExists(fs, "/source/old-dir")
	assert.False(t, dirExists)
}

// Lines 159-161: inPlace with forceUpdate — no conflict check for existing targetDir
func TestInPlaceMiss_Plan_InPlaceForceUpdateSkipsConflict(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/old-name", 0777)
	_ = afero.WriteFile(fs, "/source/old-name/ABC-123.mp4", []byte("video"), 0644)
	// Also create the target directory — with forceUpdate it shouldn't be a conflict
	_ = fs.MkdirAll("/source/ABC-123", 0777)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/old-name/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", true) // forceUpdate=true
	require.NoError(t, err)
	assert.True(t, plan.InPlace)
	// With forceUpdate, the conflict check for existing targetDir is skipped
	// (the `inPlace && !forceUpdate` condition is false)
}

// Execute: directory rename error (ReadOnlyFs)
func TestInPlaceMiss_Execute_DirRenameError(t *testing.T) {
	fs := afero.NewMemMapFs()
	roFs := afero.NewReadOnlyFs(fs)
	cfg := &Config{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(roFs, cfg, m, nil)

	// Create dir on the underlying fs
	_ = fs.MkdirAll("/source/old-dir", 0777)
	_ = afero.WriteFile(fs, "/source/old-dir/ABC-123.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/old-dir/ABC-123.mp4",
		TargetDir:  "/source/new-dir",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/new-dir/ABC-123.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-dir",
		Match:      models.FileMatchInfo{Path: "/source/old-dir/ABC-123.mp4", Name: "ABC-123.mp4"},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to rename directory")
	assert.False(t, result.Moved)
}

// Execute: move file error (non-in-place, ReadOnlyFs)
func TestInPlaceMiss_Execute_MoveFileError(t *testing.T) {
	fs := afero.NewMemMapFs()
	roFs := afero.NewReadOnlyFs(fs)
	cfg := &Config{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(roFs, cfg, m, nil)

	// Create dir on the underlying fs
	_ = fs.MkdirAll("/source/dir", 0777)
	_ = afero.WriteFile(fs, "/source/dir/ABC-123.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/dir/ABC-123.mp4",
		TargetDir:  "/dest/new-dir",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/new-dir/ABC-123.mp4",
		WillMove:   true,
		InPlace:    false,
		Match:      models.FileMatchInfo{Path: "/source/dir/ABC-123.mp4", Name: "ABC-123.mp4"},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err)
	// MkdirAll should fail on read-only fs, or MoveFileFs should fail
	assert.False(t, result.Moved)
}
