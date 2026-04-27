package organizer

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInPlaceNoRenameFolderStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m := &matcher.Matcher{}
	strategy := NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)
	assert.NotNil(t, strategy)
	assert.NotNil(t, strategy.fs)
	assert.NotNil(t, strategy.config)
	assert.NotNil(t, strategy.templateEngine)
}

func TestInPlaceNoRenameFolderStrategy_ImplementsInterface(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m := &matcher.Matcher{}
	var _ OperationStrategy = NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)
}

func TestInPlaceNoRenameFolderStrategy_Plan_FileRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FileFormat: "<ID> <TITLE>",
		RenameFile: true,
	}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/some-folder", 0777)
	_ = afero.WriteFile(fs, "/source/some-folder/old-name.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/some-folder/old-name.mp4",
			Name:      "old-name.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Test Movie",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.False(t, plan.InPlace, "Should never set InPlace=true")
	assert.Equal(t, "in-place-norenamefolder mode - file rename only", plan.SkipInPlaceReason)
	assert.True(t, plan.WillMove, "Should move file since filename changes")
	assert.Equal(t, filepath.ToSlash("/source/some-folder"), filepath.ToSlash(plan.TargetDir), "Should stay in same directory")
	assert.Equal(t, "ABC-123 Test Movie.mp4", plan.TargetFile)
	assert.Contains(t, filepath.ToSlash(plan.TargetPath), "/source/some-folder")
	assert.False(t, plan.IsDedicated, "Should not check dedicated status")
}

func TestInPlaceNoRenameFolderStrategy_Plan_NoRenameNeeded(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FileFormat: "<ID>",
		RenameFile: true,
	}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/some-folder", 0777)
	_ = afero.WriteFile(fs, "/source/some-folder/ABC-123.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/some-folder/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.False(t, plan.WillMove, "Should not move if filename already matches")
	assert.Equal(t, filepath.ToSlash("/source/some-folder/ABC-123.mp4"), filepath.ToSlash(plan.TargetPath))
}

func TestInPlaceNoRenameFolderStrategy_Plan_RenameFileOff(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FileFormat: "<ID> <TITLE>",
		RenameFile: false,
	}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/some-folder", 0777)
	_ = afero.WriteFile(fs, "/source/some-folder/old-name.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/some-folder/old-name.mp4",
			Name:      "old-name.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Test Movie",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.False(t, plan.WillMove, "Should not move when RenameFile is off and filename stays the same")
	assert.Equal(t, filepath.ToSlash("/source/some-folder/old-name.mp4"), filepath.ToSlash(plan.TargetPath))
}

func TestInPlaceNoRenameFolderStrategy_Execute_FileRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/folder", 0777)
	_ = afero.WriteFile(fs, "/source/folder/old-name.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		Match: matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      "/source/folder/old-name.mp4",
				Name:      "old-name.mp4",
				Extension: ".mp4",
			},
		},
		SourcePath: "/source/folder/old-name.mp4",
		TargetDir:  "/source/folder",
		TargetFile: "new-name.mp4",
		TargetPath: "/source/folder/new-name.mp4",
		WillMove:   true,
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.False(t, result.InPlaceRenamed, "Should not set InPlaceRenamed for no-rename-folder mode")

	exists, _ := afero.Exists(fs, "/source/folder/new-name.mp4")
	assert.True(t, exists, "File should be renamed")

	oldExists, _ := afero.Exists(fs, "/source/folder/old-name.mp4")
	assert.False(t, oldExists, "Old file should not exist after rename")

	sourceDirExists, _ := afero.Exists(fs, "/source/folder")
	assert.True(t, sourceDirExists, "Parent directory should still exist untouched")
}

func TestInPlaceNoRenameFolderStrategy_Execute_Subtitles(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt", ".ass"},
	}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})

	_ = fs.MkdirAll("/source/folder", 0777)
	_ = afero.WriteFile(fs, "/source/folder/ABC-123.mp4", []byte("video"), 0644)
	_ = afero.WriteFile(fs, "/source/folder/ABC-123.en.srt", []byte("subtitle"), 0644)
	_ = afero.WriteFile(fs, "/source/folder/ABC-123.ja.ass", []byte("subtitle2"), 0644)

	plan := &OrganizePlan{
		Match: matcher.MatchResult{
			ID: "ABC-123",
			File: scanner.FileInfo{
				Path:      "/source/folder/ABC-123.mp4",
				Name:      "ABC-123.mp4",
				Extension: ".mp4",
			},
		},
		SourcePath: "/source/folder/ABC-123.mp4",
		TargetDir:  "/source/folder",
		TargetFile: "ABC-123 Test Movie.mp4",
		TargetPath: "/source/folder/ABC-123 Test Movie.mp4",
		WillMove:   true,
		Conflicts:  []string{},
		Strategy:   StrategyTypeInPlaceNoRenameFolder,
	}

	org := NewOrganizer(fs, cfg, nil)
	org.SetMatcher(m)
	result, err := org.Execute(plan, false)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.Len(t, result.Subtitles, 2)

	subtitleRenamed := 0
	for _, sub := range result.Subtitles {
		if sub.Moved {
			subtitleRenamed++
		}
	}
	assert.Equal(t, 2, subtitleRenamed, "Both subtitles should be renamed")
}

func TestInPlaceNoRenameFolderStrategy_Execute_NoDirRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/original-folder", 0777)
	_ = afero.WriteFile(fs, "/source/original-folder/video.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/original-folder/video.mp4",
		TargetDir:  "/source/original-folder",
		TargetFile: "new-name.mp4",
		TargetPath: "/source/original-folder/new-name.mp4",
		WillMove:   true,
		InPlace:    false,
		OldDir:     "",
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.False(t, result.InPlaceRenamed, "Should never rename directory")
	assert.Empty(t, result.OldDirectoryPath, "Should not have old directory path")
	assert.Empty(t, result.NewDirectoryPath, "Should not have new directory path")

	dirExists, _ := afero.Exists(fs, "/source/original-folder")
	assert.True(t, dirExists, "Original directory should still exist")

	fileExists, _ := afero.Exists(fs, "/source/original-folder/new-name.mp4")
	assert.True(t, fileExists, "Renamed file should exist in original directory")
}

func TestInPlaceNoRenameFolderStrategy_Plan_MaxPathLength(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FileFormat:    "<ID> <TITLE>",
		RenameFile:    true,
		MaxPathLength: 50,
	}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/some-folder", 0777)
	_ = afero.WriteFile(fs, "/source/some-folder/ABC-123.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/some-folder/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Short",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.LessOrEqual(t, len(plan.TargetPath), 50, "Path should be truncated to fit MaxPathLength")
}

func TestInPlaceNoRenameFolderStrategy_Plan_StaysInSourceDirectory(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID> <TITLE>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/videos/JAV", 0777)
	_ = afero.WriteFile(fs, "/videos/JAV/ABC-123.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/videos/JAV/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Test Movie",
	}

	plan, err := strategy.Plan(match, movie, "/organized", false)
	require.NoError(t, err)
	assert.Equal(t, filepath.ToSlash("/videos/JAV"), filepath.ToSlash(plan.TargetDir), "Should stay in source directory, not move to destDir")
	assert.Equal(t, filepath.ToSlash("/videos/JAV/ABC-123.mp4"), filepath.ToSlash(plan.TargetPath), "Should rename file in source directory")
}

func TestInPlaceNoRenameFolderStrategy_Plan_TruncationEdgeCase(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FileFormat:    "<ID> <TITLE>",
		RenameFile:    true,
		MaxPathLength: 30,
	}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceNoRenameFolderStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/folder", 0777)
	_ = afero.WriteFile(fs, "/source/folder/ABC-123.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/folder/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Very Long Title That Will Exceed Max Path Length Limit",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.NotEqual(t, ".mp4", plan.TargetFile, "Should not produce empty basename")
	assert.True(t, strings.HasSuffix(plan.TargetFile, ".mp4"), "Should preserve extension")
	baseName := strings.TrimSuffix(plan.TargetFile, ".mp4")
	assert.NotEmpty(t, baseName, "Basename should not be empty after truncation")
	assert.NotContains(t, baseName, "...", "Truncated file name should use ~ not ...")
}
