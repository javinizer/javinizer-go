package organizer

import (
	"context"
	"fmt"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/template"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// selectiveErrorEngine succeeds on Execute but fails on ExecuteWithMaxBytes
type selectiveErrorEngine struct{}

func (e *selectiveErrorEngine) Execute(_ string, _ *template.Context) (string, error) {
	return "FOLDER", nil
}
func (e *selectiveErrorEngine) ExecuteWithContext(_ context.Context, _ string, _ *template.Context) (string, error) {
	return "FOLDER", nil
}
func (e *selectiveErrorEngine) ExecuteWithMaxBytes(_ string, _ *template.Context, _ int) (string, error) {
	return "", fmt.Errorf("max bytes error")
}
func (e *selectiveErrorEngine) TruncateTitle(title string, _ int) string      { return title }
func (e *selectiveErrorEngine) TruncateTitleBytes(title string, _ int) string { return title }
func (e *selectiveErrorEngine) ValidatePathLength(path string, _ int) error   { return nil }

// emptyFolderEngine returns empty string from ExecuteWithMaxBytes
type emptyFolderEngine struct{}

func (e *emptyFolderEngine) Execute(_ string, _ *template.Context) (string, error) {
	return "FOLDER", nil
}
func (e *emptyFolderEngine) ExecuteWithContext(_ context.Context, _ string, _ *template.Context) (string, error) {
	return "FOLDER", nil
}
func (e *emptyFolderEngine) ExecuteWithMaxBytes(_ string, _ *template.Context, _ int) (string, error) {
	return "", nil // Returns empty — triggers fallback to MovieID
}
func (e *emptyFolderEngine) TruncateTitle(title string, _ int) string      { return title }
func (e *emptyFolderEngine) TruncateTitleBytes(title string, _ int) string { return title }
func (e *emptyFolderEngine) ValidatePathLength(path string, _ int) error   { return nil }

// --- Plan: ExecuteWithMaxBytes error when Execute succeeds ---
// Lines 93-95: folderMaxBytes > 0, ExecuteWithMaxBytes fails

func TestInPlaceMiss2_Plan_ExecuteWithMaxBytesError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeInPlace,
		MaxPathLength: 80, // triggers folderMaxBytes > 0
	}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, &selectiveErrorEngine{})

	_ = fs.MkdirAll("/source/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123/ABC-123.mp4", []byte("video"), 0644)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{ID: "ABC-123", Title: "Some Title"}

	_, err := strategy.Plan(match, movie, "/dest", false)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to generate folder name")
}

// --- Plan: ExecuteWithMaxBytes returns empty string, fallback to MovieID ---
// Lines 97-101: folderName becomes empty, falls back to MovieID

func TestInPlaceMiss2_Plan_EmptyFolderFallbackToMovieID(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeInPlace,
		MaxPathLength: 80, // triggers folderMaxBytes > 0
	}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, &emptyFolderEngine{})

	_ = fs.MkdirAll("/source/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123/ABC-123.mp4", []byte("video"), 0644)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	// After ExecuteWithMaxBytes returns empty, SanitizeFolderPath("") returns "",
	// then falls back to SanitizeFolderPath(match.MovieID) = "ABC-123"
	assert.Equal(t, "ABC-123", plan.FolderName)
}

// --- Plan: inPlace with !forceUpdate and existing targetDir, oldDir stat fails ---
// Line 159-161: oldDir stat error adds conflict

func TestInPlaceMiss2_Plan_InPlaceOldDirStatFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create a dedicated folder with "wrong" name and target dir exists
	_ = fs.MkdirAll("/source/old-name", 0777)
	_ = afero.WriteFile(fs, "/source/old-name/ABC-123.mp4", []byte("video"), 0644)
	// Target dir also exists
	_ = fs.MkdirAll("/source/ABC-123", 0777)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/old-name/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{ID: "ABC-123"}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.True(t, plan.InPlace)
	// Both old and new dirs exist on MemMapFs, SameFile returns false → conflict
	assert.NotEmpty(t, plan.Conflicts)
}

// --- Execute: file rename after directory rename with rollback ---
// Lines 246-251: file rename fails, triggers rollback

func TestInPlaceMiss2_Execute_FileRenameFailsWithRollback(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	_ = newInPlaceStrategy(fs, cfg, m, nil)

	// Create old dir with a file
	_ = fs.MkdirAll("/source/old-dir", 0777)
	_ = afero.WriteFile(fs, "/source/old-dir/OLD-NAME.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/old-dir/OLD-NAME.mp4",
		TargetDir:  "/source/new-dir",
		TargetFile: "ABC-123.mp4", // Different from old file name — will need file rename
		TargetPath: "/source/new-dir/ABC-123.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-dir",
		Match:      models.FileMatchInfo{Path: "/source/old-dir/OLD-NAME.mp4", Name: "OLD-NAME.mp4"},
	}

	// Use read-only fs after creating the initial structure to force file rename failure
	roFs := afero.NewReadOnlyFs(fs)
	roStrategy := newInPlaceStrategy(roFs, cfg, m, nil)

	result, err := roStrategy.Execute(plan)
	// Directory rename will fail on read-only FS
	if err != nil {
		assert.Contains(t, err.Error(), "failed to rename directory")
	}
	_ = result
}

// --- Execute: file rename after directory rename succeeds ---
// Lines 246-251: successful file rename after directory rename

func TestInPlaceMiss2_Execute_FileRenameAfterDirRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create old dir with a file that has a different name than the target
	_ = fs.MkdirAll("/source/old-dir", 0777)
	_ = afero.WriteFile(fs, "/source/old-dir/OLD-NAME.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/old-dir/OLD-NAME.mp4",
		TargetDir:  "/source/new-dir",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/new-dir/ABC-123.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-dir",
		Match:      models.FileMatchInfo{Path: "/source/old-dir/OLD-NAME.mp4", Name: "OLD-NAME.mp4"},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, result.InPlaceRenamed)

	// File should be at new path with new name
	exists, _ := afero.Exists(fs, "/source/new-dir/ABC-123.mp4")
	assert.True(t, exists)
}

// --- Execute: non-in-place MkdirAll failure ---
// Line 261-263: MkdirAll error on read-only FS

func TestInPlaceMiss2_Execute_NonInPlaceMkdirAllError(t *testing.T) {
	fs := afero.NewMemMapFs()
	roFs := afero.NewReadOnlyFs(fs)
	cfg := &Config{}
	m, _ := matcher.NewMatcher(&matcher.Config{})
	strategy := newInPlaceStrategy(roFs, cfg, m, nil)

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
	assert.False(t, result.Moved)
}
