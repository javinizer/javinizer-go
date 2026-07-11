package organizer

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrganizer_Plan_OperationModeOverride_PicksOverrideStrategy covers the
// override branch in Organizer.plan():
//
//	if modeOverride != "" && modeOverride != o.config.OperationMode {
//	    overrideCfg := *o.config
//	    overrideCfg.OperationMode = modeOverride
//	    strategy = ResolveStrategy(o.fs, &overrideCfg, o.matcher, o.templateEngine)
//	}
//
// The organizer package's own tests never passed a modeOverride that differed
// from o.config.OperationMode, so the shallow-copy + ResolveStrategy branch
// stayed uncovered (the workflow-package override test does not contribute to
// the organizer package's coverage profile). This test configures the organizer
// with OperationMode=organize and plans with modeOverride=in-place-norenamefolder,
// asserting the resolved strategy is the in-place-norenamefolder one (file
// renamed in place, no folder move) rather than the config-default organize
// strategy (file moved into a generated folder under destDir).
func TestOrganizer_Plan_OperationModeOverride_PicksOverrideStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourceDir := "/source/folder"
	sourceFile := filepath.Join(sourceDir, "old-name.mp4")
	require.NoError(t, fs.MkdirAll(sourceDir, 0755))
	require.NoError(t, afero.WriteFile(fs, sourceFile, []byte("video"), 0644))

	cfg := &Config{
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	movie := &models.Movie{ID: "ABC-123"}
	match := models.FileMatchInfo{
		Path:      sourceFile,
		Name:      "old-name.mp4",
		Extension: ".mp4",
		MovieID:   "ABC-123",
	}

	plan, err := org.plan(match, movie, "/dest", false, operationmode.OperationModeInPlaceNoRenameFolder)
	require.NoError(t, err)
	require.NotNil(t, plan)

	// The override branch must resolve the in-place-norenamefolder strategy:
	// file is renamed in place inside its source dir, not moved under /dest.
	assert.Equal(t, strategyInPlaceNoRenameFolder, plan.strategy,
		"override to in-place-norenamefolder must resolve that strategy, not the config-default organize strategy")
	assert.True(t, plan.PreserveSourcePath, "in-place-norenamefolder keeps files in the source directory")
	assert.False(t, plan.InPlace, "in-place-norenamefolder does not set InPlace (no folder rename)")
	assert.False(t, plan.RenameFolder, "in-place-norenamefolder does not rename the folder")
	assert.Equal(t, filepath.ToSlash(sourceDir), filepath.ToSlash(plan.TargetDir),
		"target dir must be the source dir (rename in place), not a generated folder under /dest")
	assert.Equal(t, "ABC-123.mp4", plan.TargetFile)
	assert.Equal(t, filepath.ToSlash(filepath.Join(sourceDir, "ABC-123.mp4")), filepath.ToSlash(plan.TargetPath))
	assert.Equal(t, "in-place-norenamefolder mode - file rename only", plan.SkipInPlaceReason)
}

// TestOrganizer_Organize_OperationModeOverride_RenamesInPlace exercises the
// override branch end-to-end through the public Organize seam: the override
// mode reaches plan() via OrganizeCmd.OperationMode and the file is renamed in
// place rather than copied/moved into a generated destination folder.
func TestOrganizer_Organize_OperationModeOverride_RenamesInPlace(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourceDir := "/source/folder"
	sourceFile := filepath.Join(sourceDir, "old-name.mp4")
	require.NoError(t, fs.MkdirAll(sourceDir, 0755))
	require.NoError(t, afero.WriteFile(fs, sourceFile, []byte("video"), 0644))

	cfg := &Config{
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match: models.FileMatchInfo{
			Path:      sourceFile,
			Name:      "old-name.mp4",
			Extension: ".mp4",
			MovieID:   "ABC-123",
		},
		Movie:         &models.Movie{ID: "ABC-123"},
		DestDir:       "/dest",
		MoveFiles:     true,
		OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		ForceUpdate:   true,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	// In-place rename: original file is gone, renamed file lives in sourceDir.
	assert.True(t, result.Moved)
	assert.Equal(t, filepath.ToSlash(filepath.Join(sourceDir, "ABC-123.mp4")), filepath.ToSlash(result.NewPath))

	oldExists, _ := afero.Exists(fs, sourceFile)
	assert.False(t, oldExists, "source file should be renamed away")
	newExists, _ := afero.Exists(fs, filepath.Join(sourceDir, "ABC-123.mp4"))
	assert.True(t, newExists, "renamed file should exist in the source dir")

	// No relative generated folder should be created under /dest.
	destFolderExists, _ := afero.DirExists(fs, "/dest/ABC-123")
	assert.False(t, destFolderExists, "override must not create a generated folder under dest")
}
