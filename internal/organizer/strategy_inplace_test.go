package organizer

import (
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/javinizer/javinizer-go/internal/types"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewInPlaceStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m := &matcher.Matcher{}
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)
	assert.NotNil(t, strategy)
	assert.NotNil(t, strategy.fs)
	assert.NotNil(t, strategy.config)
	assert.NotNil(t, strategy.templateEngine)
	assert.NotNil(t, strategy.matcher)
}

func TestInPlaceStrategy_ImplementsInterface(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m := &matcher.Matcher{}
	var _ OperationStrategy = NewInPlaceStrategy(fs, cfg, m, nil)
}

func TestInPlaceStrategy_isDedicatedFolder(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
	}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123/ABC-123.mp4", []byte("video1"), 0644)
	_ = afero.WriteFile(fs, "/source/ABC-123/ABC-123-pt2.mp4", []byte("video2"), 0644)

	dedicated := strategy.isDedicatedFolder("/source/ABC-123", "ABC-123", m)
	assert.True(t, dedicated, "Folder with all files matching ID should be dedicated")
}

func TestInPlaceStrategy_isDedicatedFolder_MixedIDs(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/mixed", 0777)
	_ = afero.WriteFile(fs, "/source/mixed/ABC-123.mp4", []byte("video1"), 0644)
	_ = afero.WriteFile(fs, "/source/mixed/DEF-456.mp4", []byte("video2"), 0644)

	dedicated := strategy.isDedicatedFolder("/source/mixed", "ABC-123", m)
	assert.False(t, dedicated, "Folder with mixed IDs should not be dedicated")
}

func TestInPlaceStrategy_isDedicatedFolder_NoVideos(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/empty", 0777)
	_ = afero.WriteFile(fs, "/source/empty/readme.txt", []byte("text"), 0644)

	dedicated := strategy.isDedicatedFolder("/source/empty", "ABC-123", m)
	assert.False(t, dedicated, "Folder with no videos should not be dedicated")
}

func TestInPlaceStrategy_Plan_DedicatedFolder(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID> <TITLE>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/old-name", 0777)
	_ = afero.WriteFile(fs, "/source/old-name/ABC-123.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/old-name/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID:    "ABC-123",
		Title: "Test Movie",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.True(t, plan.InPlace, "Should set InPlace=true for dedicated folder with different name")
	assert.True(t, plan.IsDedicated, "Should set IsDedicated=true")
	assert.Equal(t, filepath.ToSlash("/source/old-name"), filepath.ToSlash(plan.OldDir))
	assert.Contains(t, filepath.ToSlash(plan.TargetDir), "ABC-123 Test Movie")
	assert.Equal(t, filepath.ToSlash("/source"), filepath.ToSlash(filepath.Dir(plan.TargetDir)), "Target should be in same parent directory")
}

func TestInPlaceStrategy_Plan_FolderAlreadyCorrect(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123/ABC-123.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.False(t, plan.InPlace, "Should not set InPlace if folder already has correct name")
	assert.Contains(t, plan.SkipInPlaceReason, "already has correct name")
}

func TestInPlaceStrategy_Plan_NotDedicated(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	m, err := matcher.NewMatcher(&config.MatchingConfig{})
	require.NoError(t, err)
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/mixed", 0777)
	_ = afero.WriteFile(fs, "/source/mixed/ABC-123.mp4", []byte("video1"), 0644)
	_ = afero.WriteFile(fs, "/source/mixed/DEF-456.mp4", []byte("video2"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/mixed/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	plan, err := strategy.Plan(match, movie, "/dest", false)
	require.NoError(t, err)
	assert.False(t, plan.InPlace, "Should not set InPlace for non-dedicated folder")
	assert.Contains(t, plan.SkipInPlaceReason, "mixed IDs")
}

func TestInPlaceStrategy_Execute(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/old-folder", 0777)
	_ = afero.WriteFile(fs, "/source/old-folder/ABC-123.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/old-folder/ABC-123.mp4",
		TargetDir:  "/source/new-folder",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/new-folder/ABC-123.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-folder",
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, result.InPlaceRenamed)
	assert.Equal(t, filepath.ToSlash("/source/old-folder"), filepath.ToSlash(result.OldDirectoryPath))
	assert.Equal(t, filepath.ToSlash("/source/new-folder"), filepath.ToSlash(result.NewDirectoryPath))

	exists, _ := afero.Exists(fs, "/source/new-folder/ABC-123.mp4")
	assert.True(t, exists, "File should be in renamed directory")
	exists, _ = afero.Exists(fs, "/source/old-folder")
	assert.False(t, exists, "Old directory should not exist")
}

func TestInPlaceStrategy_Execute_RenameFile(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/old-folder", 0777)
	_ = afero.WriteFile(fs, "/source/old-folder/old-name.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/old-folder/old-name.mp4",
		TargetDir:  "/source/new-folder",
		TargetFile: "new-name.mp4",
		TargetPath: "/source/new-folder/new-name.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-folder",
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)

	exists, _ := afero.Exists(fs, "/source/new-folder/new-name.mp4")
	assert.True(t, exists, "File should be renamed in new directory")
}

func TestInPlaceStrategy_Execute_NoRenameNeeded(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/old-folder", 0777)
	_ = afero.WriteFile(fs, "/source/old-folder/ABC-123.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/old-folder/ABC-123.mp4",
		TargetDir:  "/source/new-folder",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/new-folder/ABC-123.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-folder",
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, result.InPlaceRenamed)
}

func TestInPlaceStrategy_Plan_MaxPathLength(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat:  "<ID> <TITLE>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MaxPathLength: 50,
	}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123/ABC-123.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123/ABC-123.mp4",
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

func TestInPlaceStrategy_Plan_MatcherNotSet(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	strategy := NewInPlaceStrategy(fs, cfg, nil, nil)

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
	assert.Contains(t, plan.SkipInPlaceReason, "matcher not set")
}

func TestInPlaceStrategy_Plan_ForceUpdate(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123/ABC-123.mp4", []byte("video"), 0644)

	match := matcher.MatchResult{
		ID: "ABC-123",
		File: scanner.FileInfo{
			Path:      "/source/ABC-123/ABC-123.mp4",
			Name:      "ABC-123.mp4",
			Extension: ".mp4",
		},
	}
	movie := &models.Movie{
		ID: "ABC-123",
	}

	planForce, err := strategy.Plan(match, movie, "/dest", true)
	require.NoError(t, err)
	assert.Empty(t, planForce.Conflicts, "With ForceUpdate, should skip conflict detection")
}

func TestInPlaceStrategy_Execute_DirRenameError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	plan := &OrganizePlan{
		SourcePath: "/source/nonexistent-dir/ABC-123.mp4",
		TargetDir:  "/source/new-folder",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/new-folder/ABC-123.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/nonexistent-dir",
		Conflicts:  []string{},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err, "Should fail when directory rename source doesn't exist")
	assert.Contains(t, err.Error(), "failed to stat old directory")
	assert.False(t, result.Moved)
}

func TestInPlaceStrategy_Execute_FileRenameAfterDirRename(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/old-folder", 0777)
	_ = afero.WriteFile(fs, "/source/old-folder/old-name.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/old-folder/old-name.mp4",
		TargetDir:  "/source/new-folder",
		TargetFile: "new-name.mp4",
		TargetPath: "/source/new-folder/new-name.mp4",
		WillMove:   true,
		InPlace:    true,
		OldDir:     "/source/old-folder",
		Conflicts:  []string{},
		Match: matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      "/source/old-folder/old-name.mp4",
				Name:      "old-name.mp4",
				Extension: ".mp4",
			},
		},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, result.InPlaceRenamed)
	assert.Equal(t, filepath.ToSlash("/source/old-folder"), filepath.ToSlash(result.OldDirectoryPath))
	assert.Equal(t, filepath.ToSlash("/source/new-folder"), filepath.ToSlash(result.NewDirectoryPath))

	exists, _ := afero.Exists(fs, "/source/new-folder/new-name.mp4")
	assert.True(t, exists, "File should be renamed in new directory")
	exists, _ = afero.Exists(fs, "/source/old-folder")
	assert.False(t, exists, "Old directory should not exist")
}

func TestInPlaceStrategy_Execute_MoveToOrganizeFolder(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{
		OperationMode: types.OperationModeOrganize,
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
	}
	m, _ := matcher.NewMatcher(&config.MatchingConfig{})
	strategy := NewInPlaceStrategy(fs, cfg, m, nil)

	_ = fs.MkdirAll("/source/folder", 0777)
	_ = afero.WriteFile(fs, "/source/folder/ABC-123.mp4", []byte("video"), 0644)

	plan := &OrganizePlan{
		SourcePath: "/source/folder/ABC-123.mp4",
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
		WillMove:   true,
		InPlace:    false,
		OldDir:     "",
		Conflicts:  []string{},
		Match: matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      "/source/folder/ABC-123.mp4",
				Name:      "ABC-123.mp4",
				Extension: ".mp4",
			},
		},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.False(t, result.InPlaceRenamed, "Should not have renamed directory")

	exists, _ := afero.Exists(fs, "/dest/ABC-123/ABC-123.mp4")
	assert.True(t, exists, "File should exist at destination")
}
