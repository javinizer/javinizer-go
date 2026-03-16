package organizer

import (
	"github.com/spf13/afero"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestOrganizer_Copy_ErrorPaths tests error handling in Copy operation
func TestOrganizer_Copy_ErrorPaths(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	t.Run("Copy with conflicts", func(t *testing.T) {
		// Create source file
		sourceFile := filepath.Join(tmpDir, "copy-conflict.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("source"), 0644))

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourceFile,
				Name:      "copy-conflict.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		plan, err := org.Plan(match, movie, tmpDir, false)
		require.NoError(t, err)

		// Create conflicting target file
		require.NoError(t, os.MkdirAll(plan.TargetDir, 0755))
		require.NoError(t, os.WriteFile(plan.TargetPath, []byte("existing"), 0644))

		// Re-plan to detect conflict
		plan, err = org.Plan(match, movie, tmpDir, false)
		require.NoError(t, err)
		assert.NotEmpty(t, plan.Conflicts)

		// Copy should fail due to conflict
		result, err := org.Copy(plan, false)
		assert.Error(t, err)
		assert.NotNil(t, result.Error)
		assert.Contains(t, result.Error.Error(), "conflicts detected")
	})

	t.Run("Copy with no move needed", func(t *testing.T) {
		// Create source file that's already in target location
		sourceFile := filepath.Join(tmpDir, "already-there", "ipx-535.mp4")
		require.NoError(t, os.MkdirAll(filepath.Dir(sourceFile), 0755))
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		// Create a plan where source == target
		plan := &OrganizePlan{
			SourcePath: sourceFile,
			TargetPath: sourceFile,
			TargetDir:  filepath.Dir(sourceFile),
			TargetFile: "ipx-535.mp4",
			WillMove:   false,
			Conflicts:  []string{},
		}

		// Copy should succeed but not actually copy
		result, err := org.Copy(plan, false)
		require.NoError(t, err)
		assert.False(t, result.Moved)
	})

	t.Run("Copy dry run", func(t *testing.T) {
		sourceFile := filepath.Join(tmpDir, "copy-dryrun.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourceFile,
				Name:      "copy-dryrun.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		destDir := filepath.Join(tmpDir, "copy-dest")
		plan, err := org.Plan(match, movie, destDir, false)
		require.NoError(t, err)

		// Dry run copy
		result, err := org.Copy(plan, true)
		require.NoError(t, err)
		assert.False(t, result.Moved)

		// Source should still exist
		assert.FileExists(t, sourceFile)

		// Target should not exist
		_, err = os.Stat(result.NewPath)
		assert.True(t, os.IsNotExist(err))
	})

	t.Run("Copy with nonexistent source", func(t *testing.T) {
		nonexistentFile := filepath.Join(tmpDir, "nonexistent.mp4")

		plan := &OrganizePlan{
			SourcePath: nonexistentFile,
			TargetDir:  filepath.Join(tmpDir, "copy-target"),
			TargetFile: "target.mp4",
			TargetPath: filepath.Join(tmpDir, "copy-target", "target.mp4"),
			WillMove:   true,
			Conflicts:  []string{},
		}

		result, err := org.Copy(plan, false)
		assert.Error(t, err)
		assert.NotNil(t, result.Error)
		assert.Contains(t, result.Error.Error(), "failed to open source file")
	})
}

// TestOrganizer_Execute_InPlaceErrors tests in-place rename error paths
func TestOrganizer_Execute_InPlaceErrors(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat:        "<ID>",
		FileFormat:          "<ID>",
		RenameFile:          true,
		RenameFolderInPlace: true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	m, err := matcher.NewMatcher(&config.MatchingConfig{
		RegexEnabled: true,
		RegexPattern: `(?P<id>[A-Z]+-\d+)`,
	})
	require.NoError(t, err)
	org.SetMatcher(m)

	t.Run("In-place rename with old dir not existing", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: filepath.Join(tmpDir, "nonexistent-dir", "file.mp4"),
			TargetDir:  filepath.Join(tmpDir, "new-dir"),
			TargetFile: "IPX-535.mp4",
			TargetPath: filepath.Join(tmpDir, "new-dir", "IPX-535.mp4"),
			WillMove:   true,
			InPlace:    true,
			OldDir:     filepath.Join(tmpDir, "nonexistent-dir"),
		}

		result, err := org.Execute(plan, false)
		assert.Error(t, err)
		assert.NotNil(t, result.Error)
		assert.Contains(t, result.Error.Error(), "failed to stat old directory")
	})

	t.Run("In-place rename with old path is file not directory", func(t *testing.T) {
		// Create a file instead of directory
		filePath := filepath.Join(tmpDir, "not-a-dir")
		require.NoError(t, os.WriteFile(filePath, []byte("test"), 0644))

		plan := &OrganizePlan{
			SourcePath: filepath.Join(filePath, "file.mp4"),
			TargetDir:  filepath.Join(tmpDir, "new-dir"),
			TargetFile: "IPX-535.mp4",
			TargetPath: filepath.Join(tmpDir, "new-dir", "IPX-535.mp4"),
			WillMove:   true,
			InPlace:    true,
			OldDir:     filePath,
		}

		result, err := org.Execute(plan, false)
		assert.Error(t, err)
		assert.NotNil(t, result.Error)
		assert.Contains(t, result.Error.Error(), "old path is not a directory")
	})

	t.Run("In-place rename with target directory already exists", func(t *testing.T) {
		// Create old directory
		oldDir := filepath.Join(tmpDir, "old-dir-conflict")
		require.NoError(t, os.MkdirAll(oldDir, 0755))
		sourceFile := filepath.Join(oldDir, "file.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		// Create conflicting target directory
		newDir := filepath.Join(tmpDir, "new-dir-conflict")
		require.NoError(t, os.MkdirAll(newDir, 0755))

		plan := &OrganizePlan{
			SourcePath: sourceFile,
			TargetDir:  newDir,
			TargetFile: "IPX-535.mp4",
			TargetPath: filepath.Join(newDir, "IPX-535.mp4"),
			WillMove:   true,
			InPlace:    true,
			OldDir:     oldDir,
		}

		result, err := org.Execute(plan, false)
		assert.Error(t, err)
		assert.NotNil(t, result.Error)
		assert.Contains(t, result.Error.Error(), "target directory already exists")
	})

	t.Run("In-place rename with file rename failure", func(t *testing.T) {
		// This test is hard to simulate on most systems without special permissions
		// We'll test the normal path and trust the error handling code
		// The code has rollback logic: if file rename fails after dir rename,
		// it attempts to rollback the directory rename
		t.Skip("Difficult to simulate file rename failure reliably")
	})

	t.Run("Normal move with mkdir failure", func(t *testing.T) {
		// This is also difficult to simulate without special permissions
		// The code would fail at os.MkdirAll if permissions are wrong
		t.Skip("Difficult to simulate mkdir failure reliably")
	})

	t.Run("Normal move with rename failure", func(t *testing.T) {
		// Similar to above - hard to simulate reliably
		t.Skip("Difficult to simulate rename failure reliably")
	})
}

// TestOrganizer_Execute_PermissionErrors tests permission-related error handling
func TestOrganizer_Execute_PermissionErrors(t *testing.T) {
	if os.Getuid() == 0 {
		t.Skip("Skipping permission tests when running as root")
	}

	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	t.Run("Execute with read-only destination directory", func(t *testing.T) {
		// Create source file
		sourceFile := filepath.Join(tmpDir, "readonly-dest.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		// Create read-only destination directory
		readonlyDir := filepath.Join(tmpDir, "readonly-dest")
		require.NoError(t, os.MkdirAll(readonlyDir, 0755))
		defer func() { _ = os.Chmod(readonlyDir, 0755) }() // Restore permissions for cleanup

		// Make directory read-only (no write permission)
		require.NoError(t, os.Chmod(readonlyDir, 0444))

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourceFile,
				Name:      "readonly-dest.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		// Try to plan (this should work)
		plan, err := org.Plan(match, movie, readonlyDir, false)
		require.NoError(t, err)

		// Try to execute (this should fail due to permissions)
		result, err := org.Execute(plan, false)

		// Should get an error about permissions
		assert.Error(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Error)

		// Error message should mention permission or mkdir failure
		errMsg := err.Error()
		assert.True(t,
			strings.Contains(errMsg, "permission") ||
				strings.Contains(errMsg, "mkdir") ||
				strings.Contains(errMsg, "failed to create"),
			"Expected permission-related error, got: %s", errMsg)
	})

	t.Run("Execute with unwritable source file", func(t *testing.T) {
		// Create source file
		sourceFile := filepath.Join(tmpDir, "unwritable-source.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		// Make source file read-only
		require.NoError(t, os.Chmod(sourceFile, 0444))
		defer func() { _ = os.Chmod(sourceFile, 0644) }() // Restore for cleanup

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourceFile,
				Name:      "unwritable-source.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		destDir := filepath.Join(tmpDir, "normal-dest")
		plan, err := org.Plan(match, movie, destDir, false)
		require.NoError(t, err)

		// Try to execute move operation
		result, err := org.Execute(plan, false)

		// Move might succeed even if file is read-only (depends on OS)
		// But if it fails, error should be clear
		if err != nil {
			assert.NotNil(t, result)
			assert.NotNil(t, result.Error)
			errMsg := err.Error()
			assert.NotEmpty(t, errMsg, "Error message should not be empty")
		}
	})

	t.Run("Copy with unreadable source file", func(t *testing.T) {
		// Create source file
		sourceFile := filepath.Join(tmpDir, "unreadable-source.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		// Make source file unreadable (no read permission)
		require.NoError(t, os.Chmod(sourceFile, 0000))
		defer func() { _ = os.Chmod(sourceFile, 0644) }() // Restore for cleanup

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourceFile,
				Name:      "unreadable-source.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		destDir := filepath.Join(tmpDir, "copy-dest-unreadable")
		plan, err := org.Plan(match, movie, destDir, false)
		require.NoError(t, err)

		// Try to copy (should fail to open file)
		result, err := org.Copy(plan, false)

		assert.Error(t, err)
		assert.NotNil(t, result)
		assert.NotNil(t, result.Error)

		// Error should mention permission or failed to open
		errMsg := err.Error()
		assert.True(t,
			strings.Contains(errMsg, "permission") ||
				strings.Contains(errMsg, "failed to open"),
			"Expected permission-related error, got: %s", errMsg)
	})

	t.Run("Organize returns clear permission error message", func(t *testing.T) {
		// Create source file
		sourceFile := filepath.Join(tmpDir, "perm-test.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		// Create read-only destination
		readonlyDir := filepath.Join(tmpDir, "readonly-organize")
		require.NoError(t, os.MkdirAll(readonlyDir, 0755))
		defer func() { _ = os.Chmod(readonlyDir, 0755) }() // Restore permissions
		require.NoError(t, os.Chmod(readonlyDir, 0444))

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourceFile,
				Name:      "perm-test.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		// Call the high-level Organize function
		result, err := org.Organize(match, movie, readonlyDir, false, false, false)

		// Should get an error
		assert.Error(t, err)

		// Result should contain the error
		if result != nil {
			assert.NotNil(t, result.Error)

			// Error message should be descriptive
			errMsg := result.Error.Error()
			assert.NotEmpty(t, errMsg)

			// Should not be a generic error - should give actual reason
			assert.False(t, errMsg == "unknown error",
				"Error message should be specific, got: %s", errMsg)
		}
	})
}

// TestOrganizer_Revert_Errors tests Revert error handling
func TestOrganizer_Revert_Errors(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	t.Run("Revert when not moved", func(t *testing.T) {
		result := &OrganizeResult{
			OriginalPath: "/some/path.mp4",
			NewPath:      "/other/path.mp4",
			Moved:        false,
		}

		err := org.Revert(result)
		assert.NoError(t, err) // Should succeed (nothing to revert)
	})

	t.Run("Revert with nonexistent new path", func(t *testing.T) {
		result := &OrganizeResult{
			OriginalPath: filepath.Join(tmpDir, "original.mp4"),
			NewPath:      filepath.Join(tmpDir, "nonexistent-new.mp4"),
			Moved:        true,
		}

		err := org.Revert(result)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "failed to revert move")
	})
}

// TestOrganizer_Plan_EdgeCases tests edge cases in Plan function
func TestOrganizer_Plan_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	t.Run("Plan with subfolder hierarchy", func(t *testing.T) {
		// Test that subfolder hierarchy is properly constructed
		cfg := &config.OutputConfig{
			FolderFormat:    "<ID>",
			FileFormat:      "<ID>",
			RenameFile:      true,
			SubfolderFormat: []string{"<STUDIO>"}, // Single subfolder for simplicity
		}

		org := NewOrganizer(afero.NewOsFs(), cfg)
		movie := createTestMovie()

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      filepath.Join(tmpDir, "test.mp4"),
				Name:      "test.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		plan, err := org.Plan(match, movie, tmpDir, false)
		require.NoError(t, err)
		assert.NotNil(t, plan)
		// Should include studio subfolder in path
		assert.Contains(t, plan.TargetDir, "IdeaPocket")
		assert.Contains(t, plan.TargetDir, "IPX-535")
	})

	t.Run("Plan with max path length validation", func(t *testing.T) {
		cfg := &config.OutputConfig{
			FolderFormat:  "<ID> - <TITLE>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			MaxPathLength: 50, // Very short to trigger validation error
		}

		org := NewOrganizer(afero.NewOsFs(), cfg)
		movie := createTestMovie()

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      filepath.Join(tmpDir, "test.mp4"),
				Name:      "test.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		plan, err := org.Plan(match, movie, tmpDir, false)
		// Should fail path length validation
		assert.Error(t, err)
		assert.Nil(t, plan)
		assert.Contains(t, err.Error(), "path validation failed")
	})

	t.Run("Plan with title truncation", func(t *testing.T) {
		cfg := &config.OutputConfig{
			FolderFormat:   "<ID> - <TITLE>",
			FileFormat:     "<ID>",
			RenameFile:     true,
			MaxTitleLength: 10, // Long enough to keep ID but truncate title
		}

		org := NewOrganizer(afero.NewOsFs(), cfg)
		movie := createTestMovie()

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      filepath.Join(tmpDir, "test.mp4"),
				Name:      "test.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		plan, err := org.Plan(match, movie, tmpDir, false)
		require.NoError(t, err)

		// The folder name should have truncated title
		baseName := filepath.Base(plan.TargetDir)
		originalName := "IPX-535 - Beautiful Day"
		assert.True(t, len(baseName) < len(originalName), "Expected truncation: %s vs %s", baseName, originalName)
		// Should still contain the ID
		assert.Contains(t, baseName, "IPX-535")
	})

	t.Run("Plan with multi-part file", func(t *testing.T) {
		cfg := &config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID><PARTSUFFIX>", // Use <PARTSUFFIX> placeholder for multi-part support
			RenameFile:   true,
		}

		org := NewOrganizer(afero.NewOsFs(), cfg)
		movie := createTestMovie()

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      filepath.Join(tmpDir, "test-cd1.mp4"),
				Name:      "test-cd1.mp4",
				Extension: ".mp4",
			},
			ID:          "IPX-535",
			IsMultiPart: true,
			PartNumber:  1,
			PartSuffix:  "-cd1",
		}

		plan, err := org.Plan(match, movie, tmpDir, false)
		require.NoError(t, err)

		// Part suffix should be included in filename
		assert.Equal(t, "IPX-535-cd1.mp4", plan.TargetFile)
	})

	t.Run("Plan with subfolder format", func(t *testing.T) {
		cfg := &config.OutputConfig{
			FolderFormat:    "<ID>",
			FileFormat:      "<ID>",
			SubfolderFormat: []string{"<STUDIO>", "<YEAR>"},
			RenameFile:      true,
		}

		org := NewOrganizer(afero.NewOsFs(), cfg)
		movie := createTestMovie()

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      filepath.Join(tmpDir, "test.mp4"),
				Name:      "test.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		plan, err := org.Plan(match, movie, tmpDir, false)
		require.NoError(t, err)

		// Should include subfolder hierarchy
		assert.Contains(t, plan.TargetDir, "IdeaPocket")
		assert.Contains(t, plan.TargetDir, "2020")
	})

	t.Run("Plan with rename file disabled", func(t *testing.T) {
		cfg := &config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
			RenameFile:   false, // Keep original filename
		}

		org := NewOrganizer(afero.NewOsFs(), cfg)
		movie := createTestMovie()

		originalFilename := "my-original-name.mp4"
		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      filepath.Join(tmpDir, originalFilename),
				Name:      originalFilename,
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		plan, err := org.Plan(match, movie, tmpDir, false)
		require.NoError(t, err)

		// Should keep original filename
		assert.Equal(t, originalFilename, plan.TargetFile)
	})

	t.Run("Plan with force update (ignore conflicts)", func(t *testing.T) {
		cfg := &config.OutputConfig{
			FolderFormat: "<ID>",
			FileFormat:   "<ID>",
			RenameFile:   true,
		}

		org := NewOrganizer(afero.NewOsFs(), cfg)
		movie := createTestMovie()

		sourceFile := filepath.Join(tmpDir, "force-update.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourceFile,
				Name:      "force-update.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		// First plan
		plan, err := org.Plan(match, movie, tmpDir, false)
		require.NoError(t, err)

		// Create conflicting target
		require.NoError(t, os.MkdirAll(plan.TargetDir, 0755))
		require.NoError(t, os.WriteFile(plan.TargetPath, []byte("existing"), 0644))

		// Plan without force update - should detect conflict
		plan, err = org.Plan(match, movie, tmpDir, false)
		require.NoError(t, err)
		assert.NotEmpty(t, plan.Conflicts)

		// Plan with force update - should ignore conflict
		plan, err = org.Plan(match, movie, tmpDir, true)
		require.NoError(t, err)
		assert.Empty(t, plan.Conflicts)
	})
}

// TestOrganizer_OrganizeBatch_EdgeCases tests edge cases in batch operations
func TestOrganizer_OrganizeBatch_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	t.Run("Batch with missing movie data", func(t *testing.T) {
		sourceFile := filepath.Join(tmpDir, "missing-movie.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		matches := []matcher.MatchResult{
			{
				File: scanner.FileInfo{
					Path:      sourceFile,
					Name:      "missing-movie.mp4",
					Extension: ".mp4",
				},
				ID: "MISSING-999",
			},
		}

		// Empty movie map
		movies := map[string]*models.Movie{}

		results, err := org.OrganizeBatch(matches, movies, tmpDir, false, false, false)
		require.NoError(t, err)
		require.Len(t, results, 1)

		// Should have error about missing movie data
		assert.NotNil(t, results[0].Error)
		assert.Contains(t, results[0].Error.Error(), "no movie data found")
	})

	t.Run("Batch with copyOnly flag", func(t *testing.T) {
		sourceDir := filepath.Join(tmpDir, "copy-batch")
		require.NoError(t, os.MkdirAll(sourceDir, 0755))

		sourceFile := filepath.Join(sourceDir, "copy-test.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		matches := []matcher.MatchResult{
			{
				File: scanner.FileInfo{
					Path:      sourceFile,
					Name:      "copy-test.mp4",
					Extension: ".mp4",
				},
				ID: "IPX-535",
			},
		}

		releaseDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		movies := map[string]*models.Movie{
			"IPX-535": {ID: "IPX-535", Title: "Test", ReleaseDate: &releaseDate},
		}

		destDir := filepath.Join(tmpDir, "copy-batch-dest")
		results, err := org.OrganizeBatch(matches, movies, destDir, false, false, true)
		require.NoError(t, err)
		require.Len(t, results, 1)

		// Should have copied (not moved)
		assert.True(t, results[0].Moved)         // "Moved" means operation succeeded
		assert.FileExists(t, sourceFile)         // Source should still exist (copy)
		assert.FileExists(t, results[0].NewPath) // Target should exist
	})

	t.Run("Batch dry run", func(t *testing.T) {
		sourceDir := filepath.Join(tmpDir, "dryrun-batch")
		require.NoError(t, os.MkdirAll(sourceDir, 0755))

		files := []string{"file1.mp4", "file2.mp4"}
		for _, name := range files {
			path := filepath.Join(sourceDir, name)
			require.NoError(t, os.WriteFile(path, []byte("test"), 0644))
		}

		matches := []matcher.MatchResult{
			{
				File: scanner.FileInfo{
					Path:      filepath.Join(sourceDir, "file1.mp4"),
					Name:      "file1.mp4",
					Extension: ".mp4",
				},
				ID: "IPX-535",
			},
			{
				File: scanner.FileInfo{
					Path:      filepath.Join(sourceDir, "file2.mp4"),
					Name:      "file2.mp4",
					Extension: ".mp4",
				},
				ID: "ABC-123",
			},
		}

		releaseDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
		movies := map[string]*models.Movie{
			"IPX-535": {ID: "IPX-535", Title: "Movie1", ReleaseDate: &releaseDate},
			"ABC-123": {ID: "ABC-123", Title: "Movie2", ReleaseDate: &releaseDate},
		}

		destDir := filepath.Join(tmpDir, "dryrun-batch-dest")
		results, err := org.OrganizeBatch(matches, movies, destDir, true, false, false)
		require.NoError(t, err)
		require.Len(t, results, 2)

		// Should not have moved files (dry run)
		for i, result := range results {
			assert.False(t, result.Moved, "File %d should not be moved in dry run", i)
			assert.FileExists(t, result.OriginalPath)
		}
	})
}

// TestValidatePlan_EdgeCases tests additional ValidatePlan edge cases
func TestValidatePlan_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.OutputConfig{}
	org := NewOrganizer(afero.NewOsFs(), cfg)

	t.Run("Empty target directory", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: filepath.Join(tmpDir, "test.mp4"),
			TargetDir:  "",
			TargetFile: "test.mp4",
			TargetPath: "/test.mp4",
		}

		issues := org.ValidatePlan(plan)
		assert.NotEmpty(t, issues)
		assert.True(t, containsIssue(issues, "target directory or filename is empty"))
	})

	t.Run("Empty target filename", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: filepath.Join(tmpDir, "test.mp4"),
			TargetDir:  tmpDir,
			TargetFile: "",
			TargetPath: filepath.Join(tmpDir, ""),
		}

		issues := org.ValidatePlan(plan)
		assert.NotEmpty(t, issues)
		assert.True(t, containsIssue(issues, "target directory or filename is empty"))
	})

	t.Run("Double slashes in path", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: filepath.Join(tmpDir, "test.mp4"),
			TargetDir:  tmpDir,
			TargetFile: "test.mp4",
			TargetPath: tmpDir + "//test.mp4",
		}

		issues := org.ValidatePlan(plan)
		assert.NotEmpty(t, issues)
		assert.True(t, containsIssue(issues, "target path contains double slashes"))
	})
}

// TestCleanEmptyDirectories_EdgeCases tests edge cases in directory cleanup
func TestCleanEmptyDirectories_EdgeCases(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.OutputConfig{}
	org := NewOrganizer(afero.NewOsFs(), cfg)

	t.Run("Stop at non-empty directory", func(t *testing.T) {
		// Create nested directories with a file in middle level
		deepDir := filepath.Join(tmpDir, "cleanup1", "b", "c")
		require.NoError(t, os.MkdirAll(deepDir, 0755))

		// Put a file in middle directory
		middleFile := filepath.Join(tmpDir, "cleanup1", "b", "keep.txt")
		require.NoError(t, os.WriteFile(middleFile, []byte("keep"), 0644))

		// Create and remove a file in deep directory
		filePath := filepath.Join(deepDir, "temp.mp4")
		require.NoError(t, os.WriteFile(filePath, []byte("temp"), 0644))
		require.NoError(t, os.Remove(filePath))

		// Clean from deep directory
		err := org.CleanEmptyDirectories(filePath, tmpDir)
		require.NoError(t, err)

		// Deep directory should be removed
		_, err = os.Stat(deepDir)
		assert.True(t, os.IsNotExist(err))

		// Middle directory should still exist (has keep.txt)
		assert.DirExists(t, filepath.Join(tmpDir, "cleanup1", "b"))

		// File should still exist
		assert.FileExists(t, middleFile)
	})

	t.Run("Stop at base directory", func(t *testing.T) {
		// Create nested empty directories
		baseDir := filepath.Join(tmpDir, "cleanup2")
		deepDir := filepath.Join(baseDir, "x", "y", "z")
		require.NoError(t, os.MkdirAll(deepDir, 0755))

		filePath := filepath.Join(deepDir, "file.mp4")

		// Clean from deep directory
		err := org.CleanEmptyDirectories(filePath, baseDir)
		require.NoError(t, err)

		// The base directory should still exist
		assert.DirExists(t, tmpDir)
		// But the cleanup2 directory itself may or may not exist depending on implementation
		// The important thing is the function doesn't error and doesn't go above baseDir
	})

	t.Run("Invalid base directory", func(t *testing.T) {
		filePath := filepath.Join(tmpDir, "cleanup3", "file.mp4")
		baseDir := "/nonexistent/base"

		err := org.CleanEmptyDirectories(filePath, baseDir)
		// Should return error from filepath.Abs
		assert.Error(t, err)
	})

	t.Run("Cleanup with read error", func(t *testing.T) {
		// This is difficult to test without special permissions
		// The code handles os.ReadDir errors by returning them
		t.Skip("Difficult to simulate ReadDir failure reliably")
	})
}

// TestOrganizer_Organize_Integration tests the combined Plan+Execute flow
func TestOrganizer_Organize_Integration(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat: "<ID> - <TITLE>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	t.Run("Organize with max path length error", func(t *testing.T) {
		// Use very short max path length to cause error
		badCfg := &config.OutputConfig{
			FolderFormat:  "<ID> - <TITLE>",
			FileFormat:    "<ID>",
			RenameFile:    true,
			MaxPathLength: 10, // Too short
		}
		badOrg := NewOrganizer(afero.NewOsFs(), badCfg)

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      filepath.Join(tmpDir, "test.mp4"),
				Name:      "test.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		result, err := badOrg.Organize(match, movie, tmpDir, false, false, false)
		assert.Error(t, err)
		assert.Nil(t, result)
		assert.Contains(t, err.Error(), "path validation failed")
	})

	t.Run("Organize with copyOnly", func(t *testing.T) {
		sourceFile := filepath.Join(tmpDir, "copy-organize.mp4")
		require.NoError(t, os.WriteFile(sourceFile, []byte("test"), 0644))

		match := matcher.MatchResult{
			File: scanner.FileInfo{
				Path:      sourceFile,
				Name:      "copy-organize.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		}

		destDir := filepath.Join(tmpDir, "organize-dest")
		result, err := org.Organize(match, movie, destDir, false, false, true)
		require.NoError(t, err)

		// Should copy, not move
		assert.FileExists(t, sourceFile)
		assert.FileExists(t, result.NewPath)
	})
}

// Helper function to check if an issue contains a substring
func containsIssue(issues []string, substring string) bool {
	for _, issue := range issues {
		if strings.Contains(issue, substring) {
			return true
		}
	}
	return false
}
