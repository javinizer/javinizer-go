package organizer

import (
	"context"
	"fmt"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestHandleSubtitles_NilFileOp_SetsPlannedTrue(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.srt", []byte("subtitle"), 0644)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			MovieID: "ABC-123",
			Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
		},
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
	}

	result := &OrganizeResult{}
	org.handleSubtitles(plan, result, nil)

	require.Len(t, result.Subtitles, 1)
	assert.True(t, result.Subtitles[0].Planned, "Planned should be true when fileOp is nil")
	assert.False(t, result.Subtitles[0].Moved, "Moved should be false when fileOp is nil")
	assert.Equal(t, filepath.ToSlash("/source/ABC-123.srt"), filepath.ToSlash(result.Subtitles[0].OriginalPath))
	assert.Equal(t, filepath.ToSlash("/dest/ABC-123/ABC-123.srt"), filepath.ToSlash(result.Subtitles[0].NewPath))
}

func TestHandleSubtitles_MoveFileOp_SetsMovedTrue(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.srt", []byte("subtitle"), 0644)
	_ = fs.MkdirAll("/dest/ABC-123", 0777)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			MovieID: "ABC-123",
			Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
		},
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
	}

	result := &OrganizeResult{}
	org.handleSubtitles(plan, result, fsutil.MoveFileFs)

	require.Len(t, result.Subtitles, 1)
	assert.True(t, result.Subtitles[0].Moved, "Moved should be true after MoveFileFs succeeds")
	assert.False(t, result.Subtitles[0].Planned, "Planned should be false when fileOp is provided")
}

func TestHandleSubtitles_CopyFileOp_SetsMovedTrue(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.srt", []byte("subtitle"), 0644)
	_ = fs.MkdirAll("/dest/ABC-123", 0777)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			MovieID: "ABC-123",
			Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
		},
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
	}

	result := &OrganizeResult{}
	org.handleSubtitles(plan, result, fsutil.CopyFileFs)

	require.Len(t, result.Subtitles, 1)
	assert.True(t, result.Subtitles[0].Moved, "Moved should be true after CopyFileFs succeeds")

	exists, _ := afero.Exists(fs, "/source/ABC-123.srt")
	assert.True(t, exists, "Source subtitle should still exist after copy")
}

func TestHandleSubtitles_SkipsWhenTargetExists(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.srt", []byte("subtitle"), 0644)
	_ = fs.MkdirAll("/dest/ABC-123", 0777)
	_ = afero.WriteFile(fs, "/dest/ABC-123/ABC-123.srt", []byte("existing"), 0644)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			MovieID: "ABC-123",
			Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
		},
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
	}

	result := &OrganizeResult{}
	org.handleSubtitles(plan, result, fsutil.MoveFileFs)

	require.Len(t, result.Subtitles, 1)
	assert.True(t, result.Subtitles[0].Skipped, "Skipped should be true when target already exists")
	assert.False(t, result.Subtitles[0].Moved, "Moved should be false when skipped")
}

func TestHandleSubtitles_NoSubtitles_NoResults(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	_ = fs.MkdirAll("/source", 0777)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			MovieID: "ABC-123",
			Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
		},
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
	}

	result := &OrganizeResult{}
	org.handleSubtitles(plan, result, nil)

	assert.Len(t, result.Subtitles, 0, "No subtitles found should produce no results")
}

func TestHandleSubtitles_FileOpError_SetsError(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.srt", []byte("subtitle"), 0644)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			MovieID: "ABC-123",
			Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
		},
		TargetDir:  "/dest/ABC-123",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/dest/ABC-123/ABC-123.mp4",
	}

	failingOp := func(fs afero.Fs, src, dst string) error {
		return fmt.Errorf("simulated failure")
	}

	result := &OrganizeResult{}
	org.handleSubtitles(plan, result, failingOp)

	require.Len(t, result.Subtitles, 1)
	assert.Error(t, result.Subtitles[0].Error, "Error should be set when fileOp fails")
	assert.False(t, result.Subtitles[0].Moved, "Moved should be false on error")
}

func TestExecute_DelegatesToPlanExecuteStrategy(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		MoveSubtitles: false,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644)

	strategy := newOrganizeStrategy(fs, &Config{MoveSubtitles: false}, nil, &MemLinker{})
	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			MovieID: "ABC-123",
			Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
		},
		SourcePath:      "/source/ABC-123.mp4",
		TargetDir:       "/dest/ABC-123",
		TargetFile:      "ABC-123.mp4",
		TargetPath:      "/dest/ABC-123/ABC-123.mp4",
		WillMove:        true,
		Conflicts:       []string{},
		executeStrategy: strategy,
	}

	result, err := org.execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved, "Execute should delegate to plan.executeStrategy")
}

func TestExecute_UsesHandleSubtitlesForPlanPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	plan := &OrganizePlan{
		Match: models.FileMatchInfo{
			MovieID: "ABC-123",
			Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
		},
		SourcePath: "/source/ABC-123.mp4",
		TargetDir:  "/source",
		TargetFile: "ABC-123.mp4",
		TargetPath: "/source/ABC-123.mp4",
		WillMove:   false,
		Conflicts:  []string{},
	}

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.srt", []byte("subtitle"), 0644)

	result, err := org.execute(plan)
	require.NoError(t, err)
	assert.False(t, result.Moved)
	require.Len(t, result.Subtitles, 1)
	assert.True(t, result.Subtitles[0].Planned, "No-move path should plan subtitles (not move them)")
}

func TestOrganize_UsesHandleSubtitlesForCopyPath(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
		FolderFormat:       "<ID>",
		FileFormat:         "<ID>",
		RenameFile:         true,
		OperationMode:      operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	_ = fs.MkdirAll("/source", 0777)
	_ = afero.WriteFile(fs, "/source/ABC-123.mp4", []byte("video"), 0644)
	_ = afero.WriteFile(fs, "/source/ABC-123.srt", []byte("subtitle"), 0644)

	match := models.FileMatchInfo{
		MovieID: "ABC-123",
		Path:    "/source/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
	}

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:     match,
		Movie:     &models.Movie{ID: "ABC-123"},
		DestDir:   "/dest",
		MoveFiles: false,
		LinkMode:  LinkModeNone,
	})
	require.NoError(t, err)
	assert.True(t, result.Moved)
	require.Len(t, result.Subtitles, 1, "Copy path via Organize should copy subtitles via handleSubtitles")
	assert.True(t, result.Subtitles[0].Moved, "Subtitle should be copied (Moved=true)")
}
