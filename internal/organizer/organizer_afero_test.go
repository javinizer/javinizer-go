package organizer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrganizerWithAfero_MoveFile tests file move with afero.MemMapFs
func TestOrganizerWithAfero_MoveFile(t *testing.T) {
	// Use in-memory filesystem (Architecture Decision 7)
	fs := afero.NewMemMapFs()

	// Create source file
	sourcePath := "/source/IPX-123.mp4"
	sourceContent := []byte("test video content")
	err := afero.WriteFile(fs, sourcePath, sourceContent, 0644)
	require.NoError(t, err)

	// Create organizer with afero
	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	// Use testutil builder for Movie
	movie := testutil.NewMovieBuilder().
		WithID("IPX-123").
		WithTitle("Test Movie").
		Build()

	match := models.FileMatchInfo{
		Path: sourcePath, Name: "IPX-123.mp4", Extension: ".mp4",
		MovieID: "IPX-123",
	}

	// Plan and execute
	plan, err := org.plan(match, movie, "/movies", false, "")
	require.NoError(t, err)

	result, err := org.execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)

	// Verify source is gone
	_, err = fs.Stat(sourcePath)
	assert.True(t, os.IsNotExist(err), "Source should be deleted after move")

	// Verify destination exists
	exists, err := afero.Exists(fs, result.NewPath)
	require.NoError(t, err)
	assert.True(t, exists)

	// Verify content preserved
	destContent, err := afero.ReadFile(fs, result.NewPath)
	require.NoError(t, err)
	assert.Equal(t, sourceContent, destContent)
}

// TestOrganizerWithAfero_MoveWithDirectoryCreation tests nested directory creation
func TestOrganizerWithAfero_MoveWithDirectoryCreation(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/temp/IPX-123.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	cfg := &Config{
		FolderFormat:    "<STUDIO>",
		SubfolderFormat: []string{"<YEAR>"},
		FileFormat:      "<ID>",
		RenameFile:      true,
		MoveSubtitles:   false,
		OperationMode:   operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	releaseDate := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	movie := testutil.NewMovieBuilder().
		WithID("IPX-123").
		WithStudio("IdeaPocket").
		WithReleaseDate(releaseDate).
		Build()

	match := models.FileMatchInfo{
		Path: sourcePath, Name: "IPX-123.mp4", Extension: ".mp4",
		MovieID: "IPX-123",
	}

	plan, err := org.plan(match, movie, "/movies", false, "")
	require.NoError(t, err)

	result, err := org.execute(plan)
	require.NoError(t, err)
	assert.True(t, result.Moved)

	// Verify nested path created
	expectedPath := filepath.Join("/movies", "2023", "IdeaPocket", "IPX-123.mp4")
	exists, err := afero.Exists(fs, expectedPath)
	require.NoError(t, err)
	assert.True(t, exists, "File should exist at nested path")
}

// TestOrganizerWithAfero_CopyPreservesOriginal tests copy operation
func TestOrganizerWithAfero_CopyPreservesOriginal(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/source/IPX-123.mp4"
	sourceContent := []byte("video content")
	err := afero.WriteFile(fs, sourcePath, sourceContent, 0644)
	require.NoError(t, err)

	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := models.FileMatchInfo{
		Path: sourcePath, Name: "IPX-123.mp4", Extension: ".mp4",
		MovieID: "IPX-123",
	}

	// Plan is no longer needed since we use Organize directly
	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:     match,
		Movie:     movie,
		DestDir:   "/movies",
		MoveFiles: false,
		LinkMode:  LinkModeNone,
	})
	require.NoError(t, err)
	assert.True(t, result.Moved, "Copy should mark as 'moved' (success)")

	// Verify source still exists
	sourceExists, err := afero.Exists(fs, sourcePath)
	require.NoError(t, err)
	assert.True(t, sourceExists, "Source should still exist after copy")

	// Verify destination exists
	destExists, err := afero.Exists(fs, result.NewPath)
	require.NoError(t, err)
	assert.True(t, destExists)

	// Verify identical content
	destContent, err := afero.ReadFile(fs, result.NewPath)
	require.NoError(t, err)
	assert.Equal(t, sourceContent, destContent)
}

// TestOrganizerWithAfero_MoveCollision tests collision handling
func TestOrganizerWithAfero_MoveCollision(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create source and destination files
	sourcePath := "/source/IPX-123.mp4"
	destDir := "/movies/IPX-123"
	destPath := filepath.Join(destDir, "IPX-123.mp4")

	err := afero.WriteFile(fs, sourcePath, []byte("source content"), 0644)
	require.NoError(t, err)

	err = fs.MkdirAll(destDir, 0755)
	require.NoError(t, err)
	err = afero.WriteFile(fs, destPath, []byte("existing content"), 0644)
	require.NoError(t, err)

	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := models.FileMatchInfo{
		Path: sourcePath, Name: "IPX-123.mp4", Extension: ".mp4",
		MovieID: "IPX-123",
	}

	// Plan without forceUpdate
	plan, err := org.plan(match, movie, "/movies", false, "")
	require.NoError(t, err)

	// Should detect conflict
	assert.NotEmpty(t, plan.Conflicts, "Should detect conflict")

	// Execute should fail
	_, err = org.execute(plan)
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "conflicts detected")

	// Source should still exist (move failed)
	exists, _ := afero.Exists(fs, sourcePath)
	assert.True(t, exists, "Source should remain after failed move")
}

// TestOrganizerWithAfero_ComplexTemplate tests complex template rendering
func TestOrganizerWithAfero_ComplexTemplate(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/temp/IPX-123.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	cfg := &Config{
		FolderFormat:  "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	releaseDate := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)
	movie := testutil.NewMovieBuilder().
		WithID("IPX-123").
		WithStudio("IdeaPocket").
		WithTitle("Test Movie").
		WithReleaseDate(releaseDate).
		Build()

	match := models.FileMatchInfo{
		Path: sourcePath, Name: "IPX-123.mp4", Extension: ".mp4",
		MovieID: "IPX-123",
	}

	plan, err := org.plan(match, movie, "/movies", false, "")
	require.NoError(t, err)

	// Expected: "IPX-123 [IdeaPocket] - Test Movie (2023)"
	expectedDirName := "IPX-123 [IdeaPocket] - Test Movie (2023)"
	expectedDir := filepath.Join("/movies", expectedDirName)
	assert.Equal(t, expectedDir, plan.TargetDir, "Folder should match complex template")
}

// TestOrganizerWithAfero_ValidatePlan tests plan validation
func TestOrganizerWithAfero_ValidatePlan(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &Config{}
	org := NewOrganizer(fs, cfg, nil, nil)

	t.Run("double slashes in path", func(t *testing.T) {
		// Create source file so validation can proceed
		sourcePath := "/source/file.mp4"
		err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
		require.NoError(t, err)

		plan := &OrganizePlan{
			SourcePath: sourcePath,
			TargetDir:  "/movies//IPX-123",
			TargetFile: "file.mp4",
			TargetPath: "/movies//IPX-123/file.mp4",
			Conflicts:  []string{},
		}

		issues := org.validatePlan(plan)
		assert.NotEmpty(t, issues, "Should detect double slashes")
		assert.Contains(t, issues[0], "double slashes")
	})

	t.Run("source equals target, WillMove=true - no issue (dead code removed)", func(t *testing.T) {
		samePath := "/movies/IPX-123/IPX-123.mp4"
		err := fs.MkdirAll(filepath.Dir(samePath), 0755)
		require.NoError(t, err)
		err = afero.WriteFile(fs, samePath, []byte("content"), 0644)
		require.NoError(t, err)

		plan := &OrganizePlan{
			SourcePath: samePath,
			TargetDir:  filepath.Dir(samePath),
			TargetFile: filepath.Base(samePath),
			TargetPath: samePath,
			WillMove:   true,
			Conflicts:  []string{},
		}

		issues := org.validatePlan(plan)
		for _, issue := range issues {
			if strings.Contains(issue, "identical") {
				t.Errorf("Should not report identical paths as issue (validation removed), got: %s", issue)
			}
		}
	})

	t.Run("empty target directory", func(t *testing.T) {
		// Create source file
		sourcePath := "/source/file2.mp4"
		err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
		require.NoError(t, err)

		plan := &OrganizePlan{
			SourcePath: sourcePath,
			TargetDir:  "",
			TargetFile: "file.mp4",
			TargetPath: "file.mp4",
			Conflicts:  []string{},
		}

		issues := org.validatePlan(plan)
		assert.NotEmpty(t, issues, "Should detect empty target directory")
	})
}

// TestOrganizerWithAfero_DryRun tests dry run mode
func TestOrganizerWithAfero_DryRun(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/source/IPX-123.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := models.FileMatchInfo{
		Path: sourcePath, Name: "IPX-123.mp4", Extension: ".mp4",
		MovieID: "IPX-123",
	}

	// Execute in dry run mode via Organize seam
	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:     match,
		Movie:     movie,
		DestDir:   "/movies",
		MoveFiles: true,
		DryRun:    true,
	})
	require.NoError(t, err)

	// Result populated but no actual move
	assert.NotEmpty(t, result.NewPath)
	assert.False(t, result.Moved, "Should not mark as moved in dry run")

	// Source should still exist
	exists, _ := afero.Exists(fs, sourcePath)
	assert.True(t, exists, "Source should remain in dry run")

	// Destination should not exist
	exists, _ = afero.Exists(fs, result.NewPath)
	assert.False(t, exists, "Destination should not be created in dry run")
}

// TestOrganizerWithAfero_PathLengthTruncation tests path length handling
func TestOrganizerWithAfero_PathLengthTruncation(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/temp/IPX-123.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	cfg := &Config{
		FolderFormat:  "<ID> - <TITLE>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		MaxPathLength: 50, // Very short limit
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	longTitle := "This is an extremely long movie title that will cause path length issues"
	movie := testutil.NewMovieBuilder().
		WithID("IPX-123").
		WithTitle(longTitle).
		Build()

	match := models.FileMatchInfo{
		Path: sourcePath, Name: "IPX-123.mp4", Extension: ".mp4",
		MovieID: "IPX-123",
	}

	// Plan should handle path length by truncating
	plan, err := org.plan(match, movie, "/movies", false, "")

	// Either plan succeeds with truncation, or returns error
	if err != nil {
		assert.Contains(t, err.Error(), "path", "Error should mention path issue")
	} else {
		// Path should be within limit
		assert.True(t, len(plan.TargetPath) <= cfg.MaxPathLength,
			"Path should be truncated to fit within MaxPathLength")
	}
}

// TestOrganizerWithAfero_RenameFileDisabled tests RenameFile=false
func TestOrganizerWithAfero_RenameFileDisabled(t *testing.T) {
	fs := afero.NewMemMapFs()
	originalFilename := "original-name.mp4"
	sourcePath := filepath.Join("/temp", originalFilename)
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	cfg := &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    false,
		MoveSubtitles: false,
		OperationMode: operationmode.OperationModeOrganize,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := models.FileMatchInfo{
		Path: sourcePath, Name: originalFilename, Extension: ".mp4",
		MovieID: "IPX-123",
	}

	plan, err := org.plan(match, movie, "/movies", false, "")
	require.NoError(t, err)

	// File name should be preserved
	assert.Equal(t, originalFilename, plan.TargetFile,
		"Should keep original filename when RenameFile=false")
}

func TestOrganizerWithAfero_EmptyFilenameAfterSanitization(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/source/ABF-345.sd 5 (1).mkv"
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	t.Run("empty movie ID falls back to match ID", func(t *testing.T) {
		cfg := &Config{
			FolderFormat:  "<ID> - <TITLE>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			MoveSubtitles: false,
			OperationMode: operationmode.OperationModeOrganize,
		}
		org := NewOrganizer(fs, cfg, nil, nil)

		movie := testutil.NewMovieBuilder().
			WithID("").
			WithTitle("Test").
			Build()

		match := models.FileMatchInfo{
			Path: sourcePath, Name: "ABF-345.sd 5 (1).mkv", Extension: ".mkv",
			MovieID: "ABF-345",
		}

		plan, err := org.plan(match, movie, "/dest", false, "")
		require.NoError(t, err)

		assert.Equal(t, "ABF-345.mkv", plan.TargetFile)
	})

	t.Run("title with only invalid chars still produces valid filename", func(t *testing.T) {
		cfg := &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<ID> - <TITLE>",
			RenameFile:    true,
			MoveSubtitles: false,
			OperationMode: operationmode.OperationModeOrganize,
		}
		org := NewOrganizer(fs, cfg, nil, nil)

		movie := testutil.NewMovieBuilder().
			WithID("ABF-345").
			WithTitle("::***??").
			Build()

		match := models.FileMatchInfo{
			Path: sourcePath, Name: "ABF-345.sd 5 (1).mkv", Extension: ".mkv",
			MovieID: "ABF-345",
		}

		plan, err := org.plan(match, movie, "/dest", false, "")
		require.NoError(t, err)

		assert.Equal(t, "ABF-345 - - -.mkv", plan.TargetFile)
	})

	t.Run("file format with only sanitizable content falls back to match ID", func(t *testing.T) {
		cfg := &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<TITLE>",
			RenameFile:    true,
			MoveSubtitles: false,
			OperationMode: operationmode.OperationModeOrganize,
		}
		org := NewOrganizer(fs, cfg, nil, nil)

		movie := testutil.NewMovieBuilder().
			WithID("ABF-345").
			WithTitle("").
			Build()

		match := models.FileMatchInfo{
			Path: sourcePath, Name: "ABF-345.sd 5 (1).mkv", Extension: ".mkv",
			MovieID: "ABF-345",
		}

		plan, err := org.plan(match, movie, "/dest", false, "")
		require.NoError(t, err)

		assert.Equal(t, "ABF-345.mkv", plan.TargetFile)
	})

	t.Run("all fallbacks sanitize to empty uses safe default", func(t *testing.T) {
		cfg := &Config{
			FolderFormat:  "<ID>",
			FileFormat:    "<TITLE>",
			RenameFile:    true,
			MoveSubtitles: false,
			OperationMode: operationmode.OperationModeOrganize,
		}
		org := NewOrganizer(fs, cfg, nil, nil)

		movie := testutil.NewMovieBuilder().
			WithID("").
			WithTitle("").
			Build()

		match := models.FileMatchInfo{
			Path: "/source/   ....mkv", Name: "   ....mkv", Extension: ".mkv",
			MovieID: "",
		}

		plan, err := org.plan(match, movie, "/dest", false, "")
		require.NoError(t, err)

		assert.Equal(t, "file.mkv", plan.TargetFile,
			"Should use safe default when all fallbacks sanitize to empty")
	})
}
