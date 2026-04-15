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

func TestNewOrganizeStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	strategy := NewOrganizeStrategy(fs, cfg, nil)
	assert.NotNil(t, strategy)
	assert.NotNil(t, strategy.fs)
	assert.NotNil(t, strategy.config)
	assert.NotNil(t, strategy.templateEngine)
	assert.NotNil(t, strategy.subtitleHandler)
}

func TestOrganizeStrategy_ImplementsInterface(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	var _ OperationStrategy = NewOrganizeStrategy(fs, cfg, nil)
}

func TestOrganizeStrategy_Plan(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

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
	assert.NotNil(t, plan)
	assert.Equal(t, "/source/ABC-123.mp4", plan.SourcePath)
	assert.Equal(t, "/dest/ABC-123/ABC-123.mp4", plan.TargetPath)
	assert.Equal(t, "/dest/ABC-123", plan.TargetDir)
	assert.Equal(t, "ABC-123.mp4", plan.TargetFile)
	assert.False(t, plan.InPlace, "OrganizeStrategy should never set InPlace=true")
	assert.False(t, plan.IsDedicated, "OrganizeStrategy should never set IsDedicated=true")
	assert.True(t, plan.WillMove, "Should move when source != target")
}

func TestOrganizeStrategy_Plan_WithSubfolders(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat:    "<ID>",
		FileFormat:      "<ID>",
		RenameFile:      true,
		SubfolderFormat: []string{"<LABEL>"},
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Label: "JAV",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, "/dest/JAV/ABC-123/ABC-123.mp4", plan.TargetPath)
}

func TestOrganizeStrategy_Plan_NoRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		RenameFile:   false,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/original-name.mp4",
			Name:      "original-name.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Equal(t, "/dest/ABC-123/original-name.mp4", plan.TargetPath)
	assert.Equal(t, "original-name.mp4", plan.TargetFile)
}

func TestOrganizeStrategy_Plan_TitleTruncation(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat:   "<ID> <TITLE>",
		FileFormat:     "<ID> <TITLE>",
		RenameFile:     true,
		MaxTitleLength: 20,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "This is a very long title that should be truncated",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	// TruncateTitle adds ~ to indicate truncation
	assert.Contains(t, plan.TargetDir, "ABC-123 This is")
	assert.Contains(t, plan.TargetFile, "ABC-123 This is")
	assert.LessOrEqual(t, len(plan.TargetDir), 50)
}

func TestOrganizeStrategy_Plan_ConflictDetection(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	// Create existing target file
	_ = fs.MkdirAll("/dest/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/dest/ABC-123/ABC-123.mp4", []byte("existing"), 0644)

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
	assert.NotEmpty(t, plan.Conflicts, "Should detect existing target file as conflict")
	assert.Contains(t, plan.Conflicts, "/dest/ABC-123/ABC-123.mp4")
}

func TestOrganizeStrategy_Execute(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		MoveSubtitles: false,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	// Create source file
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
	assert.True(t, result.Moved)
	assert.Equal(t, "/source/ABC-123.mp4", result.OriginalPath)
	assert.Equal(t, "/dest/ABC-123/ABC-123.mp4", result.NewPath)

	// Verify file moved
	exists, _ := afero.Exists(fs, "/dest/ABC-123/ABC-123.mp4")
	assert.True(t, exists, "Target file should exist")
	exists, _ = afero.Exists(fs, "/source/ABC-123.mp4")
	assert.False(t, exists, "Source file should not exist")
}

func TestOrganizeStrategy_Execute_NoMove(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/source",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/ABC-123.mp4",
		WillMove:   false, // Same location
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.False(t, result.Moved)
}

func TestOrganizeStrategy_Execute_Conflict(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		WillMove:   true,
		Conflicts:  []string{"/dest/ABC-123/ABC-123.mp4"},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflict")
	assert.False(t, result.Moved)
}

func TestOrganizeStrategy_Plan_ForceUpdate(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	_ = fs.MkdirAll("/dest/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/dest/ABC-123/ABC-123.mp4", []byte("existing"), 0644)

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

	plan, err := strategy.Plan(match, movie, "/dest", true)
	require.NoError(t, err)
	assert.Empty(t, plan.Conflicts, "ForceUpdate should skip conflict detection")
}

func TestOrganizeStrategy_Plan_MaxPathLength(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat:  "<ID> <TITLE>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MaxPathLength: 40,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "A Very Long Movie Title That Exceeds The Path Limit",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(plan.TargetPath), 40, "Path should be truncated to fit MaxPathLength")
}

func TestOrganizeStrategy_Plan_MaxPathLengthTooShort(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat:  "<ID> <TITLE>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MaxPathLength: 10,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Test Movie With Very Long Title",
	}

	_, err := strategy.Plan(match, movie, "/dest", false)
	assert.Error(t, err, "Should return error when path is too long even after truncation")
	assert.Contains(t, err.Error(), "path validation failed")
}

func TestOrganizeStrategy_Plan_SubfolderEmpty(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat:    "<ID>",
		FileFormat:      "<ID>",
		RenameFile:      true,
		SubfolderFormat: []string{""},
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

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
	assert.Equal(t, "/dest/ABC-123/ABC-123.mp4", plan.TargetPath, "Empty subfolder should be skipped")
}

func TestOrganizeStrategy_Execute_MkdirError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	fs = afero.NewReadOnlyFs(fs)
	strategy.fs = fs

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		WillMove:   true,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err, "Should fail when directory creation is not permitted")
	assert.Contains(t, err.Error(), "failed to create directory")
	assert.False(t, result.Moved)
}

func TestOrganizeStrategy_Execute_RenameError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	plan := &OrganizePlan{
		SourcePath: "/source/nonexistent.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		WillMove:   true,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err, "Should fail when source file does not exist for rename")
	assert.Contains(t, err.Error(), "failed to move file")
	assert.False(t, result.Moved)
}

func TestOrganizeStrategy_Execute_WithSubtitles(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt", ".ass", ".ssa"},
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	// Create source files
	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644)
	_ = afero.WriteFile(fs, "/source/ABC-123.en.srt", []byte("subtitle"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		WillMove:   true,
		Conflicts:  []string{},
		Match: matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      "/source/ABC-123.mp4",
				Name:      "ABC-123.mp4",
				Extension: ".mp4",
			},
		},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.Len(t, result.Subtitles, 1, "Should have moved subtitle")
	assert.True(t, result.Subtitles[0].Moved)
}

func TestOrganizeStrategy_Plan_MultiPart(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123-pt1.mp4",
			Name:      "ABC-123-pt1.mp4",
			Extension: ".mp4",
		},
		PartNumber:  1,
		PartSuffix:  "-pt1",
		IsMultiPart: true,
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Contains(t, plan.TargetPath, "ABC-123")
}

func TestOrganizeStrategy_Plan_SameFileNoConflict(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/dest/ABC-123/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.False(t, plan.WillMove, "Should not move when source == target")
	assert.Empty(t, plan.Conflicts, "Should have no conflicts when no move needed")
}

func TestOrganizeStrategy_Plan_SubfolderWithMultiple(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat:    "<ID>",
		FileFormat:      "<ID>",
		RenameFile:      true,
		SubfolderFormat: []string{"<LABEL>", "<ID>"},
	}
	strategy := NewOrganizeStrategy(fs, cfg, nil)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Label: "JAV",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.Contains(t, plan.TargetPath, "JAV")
	assert.Contains(t, plan.TargetPath, "ABC-123")
}
