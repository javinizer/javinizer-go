package organizer

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMetadataArtworkStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	strategy := newMetadataArtworkStrategy(fs, cfg)
	assert.NotNil(t, strategy)
	assert.NotNil(t, strategy.fs)
	assert.NotNil(t, strategy.config)
}

func TestMetadataArtworkStrategy_ImplementsInterface(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	var _ OperationStrategy = newMetadataArtworkStrategy(fs, cfg)
}

func TestMetadataArtworkStrategy_Plan(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FileFormat: "<ID>",
		RenameFile: true,
	}
	strategy := newMetadataArtworkStrategy(fs, cfg)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.NotNil(t, plan)
	assert.Equal(t, filepath.ToSlash("/source"), filepath.ToSlash(plan.TargetDir), "Should keep file in source directory")
	assert.Equal(t, filepath.ToSlash("/source/ABC-123.mp4"), filepath.ToSlash(plan.TargetPath), "Should preserve original filename even with RenameFile=true")
	assert.False(t, plan.WillMove, "Metadata-artwork mode should never set WillMove=true")
	assert.False(t, plan.InPlace, "metadataArtworkStrategy should never set InPlace=true")
	assert.False(t, plan.IsDedicated, "metadataArtworkStrategy should never set IsDedicated=true")
	assert.Contains(t, plan.SkipInPlaceReason, "metadata-artwork")
}

func TestMetadataArtworkStrategy_Plan_NoRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		RenameFile: false,
	}
	strategy := newMetadataArtworkStrategy(fs, cfg)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/original-name.mp4", Name: "original-name.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, filepath.ToSlash("/source/original-name.mp4"), filepath.ToSlash(plan.TargetPath))
	assert.False(t, plan.WillMove, "Metadata-artwork mode should never set WillMove=true")
}

func TestMetadataArtworkStrategy_Plan_IgnoresRenameFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FileFormat: "<ID> <TITLE>",
		RenameFile: true,
	}
	strategy := newMetadataArtworkStrategy(fs, cfg)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/original-name.mp4", Name: "original-name.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Test Movie",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, filepath.ToSlash("/source/original-name.mp4"), filepath.ToSlash(plan.TargetPath), "Metadata-artwork mode should preserve original filename even with RenameFile=true")
	assert.False(t, plan.WillMove, "Metadata-artwork mode should never set WillMove=true")
	assert.Equal(t, filepath.ToSlash("/source"), filepath.ToSlash(plan.TargetDir))
}

func TestMetadataArtworkStrategy_Execute_NoMove(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	strategy := newMetadataArtworkStrategy(fs, cfg)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/source",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/ABC-123.mp4",
		WillMove:   false,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.False(t, result.Moved, "Metadata-artwork should not move files")
	assert.Equal(t, filepath.ToSlash("/source/ABC-123.mp4"), filepath.ToSlash(result.NewPath))
}

func TestMetadataArtworkStrategy_Execute_NoMoveNoError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	strategy := newMetadataArtworkStrategy(fs, cfg)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/source",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/ABC-123.mp4",
		WillMove:   false,
		Conflicts:  nil,
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.False(t, result.Moved, "Metadata-artwork should not move files")
	assert.Equal(t, filepath.ToSlash("/source/ABC-123.mp4"), filepath.ToSlash(result.NewPath))
	assert.Equal(t, filepath.ToSlash("/source/ABC-123.mp4"), filepath.ToSlash(result.OriginalPath))
}

func TestMetadataArtworkStrategy_Plan_AlwaysNoConflicts(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		RenameFile: true,
	}
	strategy := newMetadataArtworkStrategy(fs, cfg)

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("existing"), 0644)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/original.mp4", Name: "original.mp4", Extension: ".mp4",
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Nil(t, plan.Conflicts, "Metadata-artwork mode should never produce conflicts since it never renames")
	assert.False(t, plan.WillMove)

	planWithForce, err := strategy.Plan(match, movie, "/dest", true)
	require.NoError(t, err)
	assert.Nil(t, planWithForce.Conflicts, "ForceUpdate should also have no conflicts in metadata-artwork mode")
}
