package organizer

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
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
	cfg := &config.OutputConfig{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		MoveToFolder:  true,
	}
	org := NewOrganizer(fs, cfg, nil)

	// Use testutil builder for Movie
	movie := testutil.NewMovieBuilder().
		WithID("IPX-123").
		WithTitle("Test Movie").
		Build()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-123.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	// Plan and execute
	plan, err := org.Plan(match, movie, "/movies", false)
	require.NoError(t, err)

	result, err := org.Execute(plan, false)
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

	cfg := &config.OutputConfig{
		FolderFormat:    "<STUDIO>",
		SubfolderFormat: []string{"<YEAR>"},
		FileFormat:      "<ID>",
		RenameFile:      true,
		MoveSubtitles:   false,
		MoveToFolder:    true,
	}
	org := NewOrganizer(fs, cfg, nil)

	releaseDate := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
	movie := testutil.NewMovieBuilder().
		WithID("IPX-123").
		WithStudio("IdeaPocket").
		WithReleaseDate(releaseDate).
		Build()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-123.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	plan, err := org.Plan(match, movie, "/movies", false)
	require.NoError(t, err)

	result, err := org.Execute(plan, false)
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

	cfg := &config.OutputConfig{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		MoveToFolder:  true,
	}
	org := NewOrganizer(fs, cfg, nil)

	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-123.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	plan, err := org.Plan(match, movie, "/movies", false)
	require.NoError(t, err)

	result, err := org.Copy(plan, false)
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

	cfg := &config.OutputConfig{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		MoveToFolder:  true,
	}
	org := NewOrganizer(fs, cfg, nil)

	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-123.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	// Plan without forceUpdate
	plan, err := org.Plan(match, movie, "/movies", false)
	require.NoError(t, err)

	// Should detect conflict
	assert.NotEmpty(t, plan.Conflicts, "Should detect conflict")

	// Execute should fail
	_, err = org.Execute(plan, false)
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

	cfg := &config.OutputConfig{
		FolderFormat:  "<ID> [<STUDIO>] - <TITLE> (<YEAR>)",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		MoveToFolder:  true,
	}
	org := NewOrganizer(fs, cfg, nil)

	releaseDate := time.Date(2023, 6, 15, 0, 0, 0, 0, time.UTC)
	movie := testutil.NewMovieBuilder().
		WithID("IPX-123").
		WithStudio("IdeaPocket").
		WithTitle("Test Movie").
		WithReleaseDate(releaseDate).
		Build()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-123.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	plan, err := org.Plan(match, movie, "/movies", false)
	require.NoError(t, err)

	// Expected: "IPX-123 [IdeaPocket] - Test Movie (2023)"
	expectedDirName := "IPX-123 [IdeaPocket] - Test Movie (2023)"
	expectedDir := filepath.Join("/movies", expectedDirName)
	assert.Equal(t, expectedDir, plan.TargetDir, "Folder should match complex template")
}

// TestOrganizerWithAfero_ValidatePlan tests plan validation
func TestOrganizerWithAfero_ValidatePlan(t *testing.T) {
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	org := NewOrganizer(fs, cfg, nil)

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

		issues := org.ValidatePlan(plan)
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

		issues := org.ValidatePlan(plan)
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

		issues := org.ValidatePlan(plan)
		assert.NotEmpty(t, issues, "Should detect empty target directory")
	})
}

// TestOrganizerWithAfero_DryRun tests dry run mode
func TestOrganizerWithAfero_DryRun(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/source/IPX-123.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	cfg := &config.OutputConfig{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		MoveToFolder:  true,
	}
	org := NewOrganizer(fs, cfg, nil)

	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-123.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	plan, err := org.Plan(match, movie, "/movies", false)
	require.NoError(t, err)

	// Execute in dry run mode
	result, err := org.Execute(plan, true)
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

// TestOrganizerWithAfero_CleanEmptyDirectories tests empty directory cleanup
// Note: CleanEmptyDirectories uses filepath.EvalSymlinks which doesn't work with MemMapFs
// This test validates the error path - comprehensive tests exist in organizer_test.go with OsFs
func TestOrganizerWithAfero_CleanEmptyDirectories(t *testing.T) {
	fs := afero.NewMemMapFs()

	// Create base directory and nested structure
	baseDir := "/movies"
	nestedPath := filepath.Join(baseDir, "studio", "year", "movie", "file.mp4")
	err := fs.MkdirAll(filepath.Dir(nestedPath), 0755)
	require.NoError(t, err)

	// Write and remove file to leave empty directories
	err = afero.WriteFile(fs, nestedPath, []byte("content"), 0644)
	require.NoError(t, err)
	err = fs.Remove(nestedPath)
	require.NoError(t, err)

	cfg := &config.OutputConfig{}
	org := NewOrganizer(fs, cfg, nil)

	// CleanEmptyDirectories will fail with MemMapFs due to EvalSymlinks limitation
	// This is expected behavior - the function is designed for real filesystem with symlink support
	err = org.CleanEmptyDirectories(nestedPath, baseDir)
	assert.Error(t, err, "MemMapFs doesn't support EvalSymlinks - error expected")

	if runtime.GOOS != "windows" {
		assert.Contains(t, err.Error(), "no such file or directory",
			"Should fail because EvalSymlinks doesn't work with MemMapFs")
	}

	// Note: Comprehensive CleanEmptyDirectories tests exist in organizer_test.go using afero.NewOsFs()
}

// TestOrganizerWithAfero_Revert tests revert operation
func TestOrganizerWithAfero_Revert(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/source/IPX-123.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	cfg := &config.OutputConfig{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		MoveToFolder:  true,
	}
	org := NewOrganizer(fs, cfg, nil)

	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-123.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	plan, err := org.Plan(match, movie, "/movies", false)
	require.NoError(t, err)

	result, err := org.Execute(plan, false)
	require.NoError(t, err)
	assert.True(t, result.Moved)

	// Verify file moved
	exists, _ := afero.Exists(fs, sourcePath)
	assert.False(t, exists, "Source should be gone after move")

	// Revert operation
	err = org.Revert(result)
	require.NoError(t, err)

	// Verify file is back
	exists, _ = afero.Exists(fs, sourcePath)
	assert.True(t, exists, "Source should be restored after revert")

	// Destination should be gone
	exists, _ = afero.Exists(fs, result.NewPath)
	assert.False(t, exists, "Destination should be removed after revert")
}

// TestOrganizerWithAfero_PathLengthTruncation tests path length handling
func TestOrganizerWithAfero_PathLengthTruncation(t *testing.T) {
	fs := afero.NewMemMapFs()
	sourcePath := "/temp/IPX-123.mp4"
	err := afero.WriteFile(fs, sourcePath, []byte("content"), 0644)
	require.NoError(t, err)

	cfg := &config.OutputConfig{
		FolderFormat:  "<ID> - <TITLE>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		MoveSubtitles: false,
		MaxPathLength: 50, // Very short limit
	}
	org := NewOrganizer(fs, cfg, nil)

	longTitle := "This is an extremely long movie title that will cause path length issues"
	movie := testutil.NewMovieBuilder().
		WithID("IPX-123").
		WithTitle(longTitle).
		Build()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-123.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	// Plan should handle path length by truncating
	plan, err := org.Plan(match, movie, "/movies", false)

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

	cfg := &config.OutputConfig{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    false,
		MoveSubtitles: false,
		MoveToFolder:  true,
	}
	org := NewOrganizer(fs, cfg, nil)

	movie := testutil.NewMovieBuilder().WithID("IPX-123").Build()
	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      originalFilename,
			Extension: ".mp4",
		},
		ID: "IPX-123",
	}

	plan, err := org.Plan(match, movie, "/movies", false)
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
		cfg := &config.OutputConfig{
			FolderFormat:  "<ID> - <TITLE>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			MoveSubtitles: false,
			MoveToFolder:  true,
		}
		org := NewOrganizer(fs, cfg, nil)

		movie := testutil.NewMovieBuilder().
			WithID("").
			WithTitle("Test").
			Build()

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourcePath,
				Name:      "ABF-345.sd 5 (1).mkv",
				Extension: ".mkv",
			},
			ID: "ABF-345",
		}

		plan, err := org.Plan(match, movie, "/dest", false)
		require.NoError(t, err)

		assert.Equal(t, "ABF-345.mkv", plan.TargetFile)
	})

	t.Run("title with only invalid chars still produces valid filename", func(t *testing.T) {
		cfg := &config.OutputConfig{
			FolderFormat:  "<ID>",
			FileFormat:    "<ID> - <TITLE>",
			RenameFile:    true,
			MoveSubtitles: false,
			MoveToFolder:  true,
		}
		org := NewOrganizer(fs, cfg, nil)

		movie := testutil.NewMovieBuilder().
			WithID("ABF-345").
			WithTitle("::***??").
			Build()

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourcePath,
				Name:      "ABF-345.sd 5 (1).mkv",
				Extension: ".mkv",
			},
			ID: "ABF-345",
		}

		plan, err := org.Plan(match, movie, "/dest", false)
		require.NoError(t, err)

		assert.Equal(t, "ABF-345 - - -.mkv", plan.TargetFile)
	})

	t.Run("file format with only sanitizable content falls back to match ID", func(t *testing.T) {
		cfg := &config.OutputConfig{
			FolderFormat:  "<ID>",
			FileFormat:    "<TITLE>",
			RenameFile:    true,
			MoveSubtitles: false,
			MoveToFolder:  true,
		}
		org := NewOrganizer(fs, cfg, nil)

		movie := testutil.NewMovieBuilder().
			WithID("ABF-345").
			WithTitle("").
			Build()

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourcePath,
				Name:      "ABF-345.sd 5 (1).mkv",
				Extension: ".mkv",
			},
			ID: "ABF-345",
		}

		plan, err := org.Plan(match, movie, "/dest", false)
		require.NoError(t, err)

		assert.Equal(t, "ABF-345.mkv", plan.TargetFile)
	})

	t.Run("all fallbacks sanitize to empty uses safe default", func(t *testing.T) {
		cfg := &config.OutputConfig{
			FolderFormat:  "<ID>",
			FileFormat:    "<TITLE>",
			RenameFile:    true,
			MoveSubtitles: false,
			MoveToFolder:  true,
		}
		org := NewOrganizer(fs, cfg, nil)

		movie := testutil.NewMovieBuilder().
			WithID("").
			WithTitle("").
			Build()

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      "/source/   ....mkv",
				Name:      "   ....mkv",
				Extension: ".mkv",
			},
			ID: "",
		}

		plan, err := org.Plan(match, movie, "/dest", false)
		require.NoError(t, err)

		assert.Equal(t, "file.mkv", plan.TargetFile,
			"Should use safe default when all fallbacks sanitize to empty")
	})
}
