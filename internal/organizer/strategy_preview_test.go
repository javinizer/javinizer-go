package organizer

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewPreviewStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	delegate := NewOrganizeStrategy(fs, cfg, nil)
	strategy := NewPreviewStrategy(delegate)
	assert.NotNil(t, strategy)
	assert.NotNil(t, strategy.delegate)
}

func TestPreviewStrategy_ImplementsInterface(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	delegate := NewOrganizeStrategy(fs, cfg, nil)
	var _ OperationStrategy = NewPreviewStrategy(delegate)
}

func TestPreviewStrategy_Plan_DelegatesToOrganizeStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	delegate := NewOrganizeStrategy(fs, cfg, nil)
	strategy := NewPreviewStrategy(delegate)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, "/dest/ABC-123/ABC-123.mp4", plan.TargetPath)
	assert.False(t, plan.InPlace)
}

func TestPreviewStrategy_Plan_DelegatesToInPlaceStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	delegate := NewInPlaceStrategy(fs, cfg, m, nil)
	strategy := NewPreviewStrategy(delegate)

	_ = fs.MkdirAll("/source/old-folder", 0777)
	_ = afero.WriteFile(fs, "/source/old-folder/ABC-123.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/old-folder/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.True(t, plan.InPlace, "Should delegate to InPlaceStrategy and detect dedicated folder")
}

func TestPreviewStrategy_Plan_DelegatesToMetadataOnlyStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FileFormat: "<ID>",
		RenameFile: true,
	}
	delegate := NewMetadataOnlyStrategy(fs, cfg)
	strategy := NewPreviewStrategy(delegate)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, "/source", plan.TargetDir, "Should delegate to MetadataOnlyStrategy and keep file in source")
	assert.Contains(t, plan.SkipInPlaceReason, "metadata-only")
}

func TestPreviewStrategy_Execute_NoFilesystemChanges(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	delegate := NewOrganizeStrategy(fs, cfg, nil)
	strategy := NewPreviewStrategy(delegate)

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		WillMove:   true,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.False(t, result.Moved, "Preview should never actually move files")

	exists, _ := afero.Exists(fs, "/source/ABC-123.mp4")
	assert.True(t, exists, "Source file should still exist")
	exists, _ = afero.Exists(fs, "/dest/ABC-123/ABC-123.mp4")
	assert.False(t, exists, "Target file should not exist")
}

func TestPreviewStrategy_Execute_ReturnsCorrectPaths(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	delegate := NewOrganizeStrategy(fs, cfg, nil)
	strategy := NewPreviewStrategy(delegate)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		WillMove:   true,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.Equal(t, "/source/ABC-123.mp4", result.OriginalPath)
	assert.Equal(t, "/dest/ABC-123/ABC-123.mp4", result.NewPath)
	assert.Equal(t, "/dest/ABC-123", result.FolderPath)
	assert.Equal(t, "ABC-123.mp4", result.FileName)
}

func TestPreviewStrategy_Execute_NoMoveWhenWillMoveFalse(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	delegate := NewOrganizeStrategy(fs, cfg, nil)
	strategy := NewPreviewStrategy(delegate)

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
	assert.False(t, result.Moved)
}
