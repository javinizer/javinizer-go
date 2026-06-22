package organizer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// --- Organizer.Organize: context cancelled ---

func TestOrganizer_Organize_ContextCancelled_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeOrganize,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-001"}
	o := NewOrganizer(fs, cfg, nil, m)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	_, err := o.Organize(ctx, OrganizeCmd{
		Match: models.FileMatchInfo{Path: "/source/ABC-001.mp4", Name: "ABC-001.mp4", Extension: ".mp4", MovieID: "ABC-001"},
		Movie: &models.Movie{ID: "ABC-001"},
	})
	assert.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

// --- Organizer.Organize: dry run does not move files ---

func TestOrganizer_Organize_DryRunNoMove_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeOrganize,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-001"}
	o := NewOrganizer(fs, cfg, nil, m)

	// Create source file
	fs.Create("/source/ABC-001.mp4")

	result, err := o.Organize(context.Background(), OrganizeCmd{
		Match:       models.FileMatchInfo{Path: "/source/ABC-001.mp4", Name: "ABC-001.mp4", Extension: ".mp4", MovieID: "ABC-001"},
		Movie:       &models.Movie{ID: "ABC-001"},
		DestDir:     "/dest",
		MoveFiles:   true,
		DryRun:      true,
		ForceUpdate: true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.False(t, result.Moved)
	assert.True(t, result.ShouldGenerateMetadata)

	// Source file should still exist (not moved)
	exists, _ := afero.Exists(fs, "/source/ABC-001.mp4")
	assert.True(t, exists)
}

// --- Organizer.Organize: validation failure ---

func TestOrganizer_Organize_ValidationFails_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeOrganize,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-001"}
	o := NewOrganizer(fs, cfg, nil, m)

	// Don't create source file, don't force update → validation should fail
	_, err := o.Organize(context.Background(), OrganizeCmd{
		Match:     models.FileMatchInfo{Path: "/nonexistent/ABC-001.mp4", Name: "ABC-001.mp4", Extension: ".mp4", MovieID: "ABC-001"},
		Movie:     &models.Movie{ID: "ABC-001"},
		DestDir:   "/dest",
		MoveFiles: true,
	})
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "validation failed")
}

// --- Organizer.execute: WillMove=false generates metadata only ---

func TestOrganizer_Execute_WillNotMove_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeOrganize,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-001"}
	o := NewOrganizer(fs, cfg, nil, m)

	plan := &OrganizePlan{
		Match:      models.FileMatchInfo{Path: "/source/ABC-001.mp4", Name: "ABC-001.mp4"},
		SourcePath: "/source/ABC-001.mp4",
		TargetPath: "/source/ABC-001.mp4", // Same path = won't move
		TargetDir:  "/source",
		TargetFile: "ABC-001.mp4",
		WillMove:   false,
	}

	result, err := o.execute(plan)
	require.NoError(t, err)
	assert.NotNil(t, result)
	assert.True(t, result.ShouldGenerateMetadata)
	assert.False(t, result.Moved)
}

// --- InPlaceStrategy.Execute: non-in-place moves file to new directory ---

func TestInPlaceStrategy_Execute_NonInPlaceMove_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlace,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-001"}
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create source file
	fs.Create("/source/ABC-001.mp4")

	plan := &OrganizePlan{
		InPlace:    false,
		TargetDir:  "/dest",
		SourcePath: "/source/ABC-001.mp4",
		TargetPath: "/dest/ABC-001.mp4",
		TargetFile: "ABC-001.mp4",
		Match:      models.FileMatchInfo{Path: "/source/ABC-001.mp4", Name: "ABC-001.mp4"},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)

	exists, _ := afero.Exists(fs, "/dest/ABC-001.mp4")
	assert.True(t, exists)
}

// --- InPlaceStrategy.Execute: directory rename + file rename within ---

func TestInPlaceStrategy_Execute_DirRenameAndFileRename_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlace,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-001"}
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create old directory with a file whose name differs from target
	fs.MkdirAll("/source/old-dir", 0755)
	afero.WriteFile(fs, "/source/old-dir/old-name.mp4", []byte("video data"), 0644)

	plan := &OrganizePlan{
		InPlace:    true,
		OldDir:     "/source/old-dir",
		TargetDir:  "/source/new-dir",
		SourcePath: "/source/old-dir/old-name.mp4",
		TargetPath: "/source/new-dir/ABC-001.mp4",
		TargetFile: "ABC-001.mp4",
		Match:      models.FileMatchInfo{Path: "/source/old-dir/old-name.mp4", Name: "old-name.mp4"},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, result.InPlaceRenamed)
	assert.Equal(t, "/source/old-dir", result.OldDirectoryPath)
	assert.Equal(t, "/source/new-dir", result.NewDirectoryPath)

	// Verify file was moved
	exists, _ := afero.Exists(fs, "/source/new-dir/ABC-001.mp4")
	assert.True(t, exists)
}

// --- InPlaceStrategy.Execute: same file after dir rename (no file rename needed) ---

func TestInPlaceStrategy_Execute_DirRenameSameFileName_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlace,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-001"}
	strategy := newInPlaceStrategy(fs, cfg, m, nil)

	// Create old directory with a file whose name matches the target
	fs.MkdirAll("/source/old-dir", 0755)
	afero.WriteFile(fs, "/source/old-dir/ABC-001.mp4", []byte("video data"), 0644)

	plan := &OrganizePlan{
		InPlace:    true,
		OldDir:     "/source/old-dir",
		TargetDir:  "/source/new-dir",
		SourcePath: "/source/old-dir/ABC-001.mp4",
		TargetPath: "/source/new-dir/ABC-001.mp4",
		TargetFile: "ABC-001.mp4",
		Match:      models.FileMatchInfo{Path: "/source/old-dir/ABC-001.mp4", Name: "ABC-001.mp4"},
	}

	result, err := strategy.Execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)
	assert.True(t, result.InPlaceRenamed)

	// Verify file was moved
	exists, _ := afero.Exists(fs, "/source/new-dir/ABC-001.mp4")
	assert.True(t, exists)
}

// --- InPlaceStrategy.Execute: MkdirAll failure in non-in-place path ---

func TestInPlaceStrategy_Execute_MkdirAllFailure_Miss(t *testing.T) {
	memFS := afero.NewMemMapFs()
	readOnlyFS := afero.NewReadOnlyFs(memFS)
	cfg := &Config{
		OperationMode: operationmode.OperationModeInPlace,
		FileFormat:    "<ID>",
		FolderFormat:  "<ID>",
		RenameFile:    true,
	}
	m := &stubMatcherForOrgMiss{result: "ABC-001"}
	strategy := newInPlaceStrategy(readOnlyFS, cfg, m, nil)

	plan := &OrganizePlan{
		InPlace:    false,
		TargetDir:  "/readonly/dest",
		SourcePath: "/readonly/ABC-001.mp4",
		TargetPath: "/readonly/dest/ABC-001.mp4",
		TargetFile: "ABC-001.mp4",
		Match:      models.FileMatchInfo{Path: "/readonly/ABC-001.mp4", Name: "ABC-001.mp4"},
	}

	result, err := strategy.Execute(plan)
	assert.Error(t, err)
	assert.NotNil(t, result)
}

// --- Organizer.Organize: MoveFiles=false with copy/link subtitles ---

func TestOrganizer_Organize_CopyModeWithSubtitles_Miss(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		OperationMode:      operationmode.OperationModeOrganize,
		FileFormat:         "<ID>",
		FolderFormat:       "<ID>",
		RenameFile:         true,
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt", ".ass"},
	}
	m := &stubMatcherForOrgMiss{result: "ABC-001"}
	o := NewOrganizer(fs, cfg, nil, m)

	// Create source file and subtitle
	fs.Create("/source/ABC-001.mp4")
	fs.Create("/source/ABC-001.srt")

	result, err := o.Organize(context.Background(), OrganizeCmd{
		Match:       models.FileMatchInfo{Path: "/source/ABC-001.mp4", Name: "ABC-001.mp4", Extension: ".mp4", MovieID: "ABC-001"},
		Movie:       &models.Movie{ID: "ABC-001"},
		DestDir:     "/dest",
		MoveFiles:   false,        // Copy/link mode
		LinkMode:    LinkModeNone, // Copy mode (LinkModeNone = copy when MoveFiles=false)
		ForceUpdate: true,
	})
	require.NoError(t, err)
	assert.NotNil(t, result)
}

// --- Stub matcher for organizer miss tests ---

type stubMatcherForOrgMiss struct {
	result string
}

func (s *stubMatcherForOrgMiss) MatchString(_ string) string                           { return s.result }
func (s *stubMatcherForOrgMiss) Match(_ []models.FileMatchInfo) []matcher.MatchResult  { return nil }
func (s *stubMatcherForOrgMiss) MatchFile(_ models.FileMatchInfo) *matcher.MatchResult { return nil }
func (s *stubMatcherForOrgMiss) ValidateMultipartInDirectory(_ []matcher.MatchResult) []matcher.MatchResult {
	return nil
}

// Suppress unused imports
var _ = os.ReadFile
var _ = filepath.Join
