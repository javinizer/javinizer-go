package organizer

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/spf13/afero"
)

func createTestMovie() *models.Movie {
	releaseDate := time.Date(2020, 9, 13, 0, 0, 0, 0, time.UTC)
	return &models.Movie{
		ID:          "IPX-535",
		ContentID:   "ipx00535",
		Title:       "Beautiful Day",
		ReleaseDate: &releaseDate,
		Runtime:     120,
		Maker:       "IdeaPocket",
		Actresses: []models.Actress{
			{FirstName: "Momo", LastName: "Sakura"},
		},
	}
}

func TestOrganizer_Plan(t *testing.T) {
	cfg := &config.OutputConfig{
		FolderFormat: "<ID> - <TITLE>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      "/source/ipx-535.mp4",
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-535",
	}

	plan, err := org.Plan(match, movie, "/dest", false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Verify plan
	if plan.SourcePath != "/source/ipx-535.mp4" {
		t.Errorf("Expected source path /source/ipx-535.mp4, got %s", plan.SourcePath)
	}

	expectedTargetDir := "/dest/IPX-535 - Beautiful Day"
	if plan.TargetDir != expectedTargetDir {
		t.Errorf("Expected target dir %s, got %s", expectedTargetDir, plan.TargetDir)
	}

	if plan.TargetFile != "IPX-535.mp4" {
		t.Errorf("Expected target file IPX-535.mp4, got %s", plan.TargetFile)
	}

	expectedTargetPath := filepath.Join(expectedTargetDir, "IPX-535.mp4")
	if plan.TargetPath != expectedTargetPath {
		t.Errorf("Expected target path %s, got %s", expectedTargetPath, plan.TargetPath)
	}

	if !plan.WillMove {
		t.Error("Expected WillMove to be true")
	}
}

func TestOrganizer_Execute_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	sourceFile := filepath.Join(tmpDir, "ipx-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-535",
	}

	plan, err := org.Plan(match, movie, tmpDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Execute in dry run mode
	result, err := org.Execute(plan, true)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// File should NOT have moved
	if result.Moved {
		t.Error("Expected Moved to be false in dry run")
	}

	// Source file should still exist
	if _, err := os.Stat(sourceFile); os.IsNotExist(err) {
		t.Error("Source file was moved during dry run")
	}

	// Target should not exist
	if _, err := os.Stat(result.NewPath); !os.IsNotExist(err) {
		t.Error("Target file was created during dry run")
	}
}

func TestOrganizer_Execute_ActualMove(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	sourceFile := filepath.Join(tmpDir, "source", "ipx-535.mp4")
	if err := os.MkdirAll(filepath.Dir(sourceFile), 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}
	if err := os.WriteFile(sourceFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat: "<ID> - <TITLE>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-535",
	}

	destDir := filepath.Join(tmpDir, "dest")
	plan, err := org.Plan(match, movie, destDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Execute actual move
	result, err := org.Execute(plan, false)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	// File should have moved
	if !result.Moved {
		t.Error("Expected Moved to be true")
	}

	// Source file should NOT exist
	if _, err := os.Stat(sourceFile); !os.IsNotExist(err) {
		t.Error("Source file still exists after move")
	}

	// Target should exist
	if _, err := os.Stat(result.NewPath); os.IsNotExist(err) {
		t.Error("Target file does not exist after move")
	}

	// Verify content
	content, err := os.ReadFile(result.NewPath)
	if err != nil {
		t.Fatalf("Failed to read target file: %v", err)
	}
	if string(content) != "test content" {
		t.Errorf("Content mismatch: got %s", string(content))
	}
}

func TestOrganizer_Execute_Conflict(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	sourceFile := filepath.Join(tmpDir, "ipx-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("source"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-535",
	}

	plan, err := org.Plan(match, movie, tmpDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Create conflicting target file
	if err := os.MkdirAll(plan.TargetDir, 0755); err != nil {
		t.Fatalf("Failed to create target dir: %v", err)
	}
	if err := os.WriteFile(plan.TargetPath, []byte("existing"), 0644); err != nil {
		t.Fatalf("Failed to create conflicting file: %v", err)
	}

	// Recreate plan to detect conflict
	plan, err = org.Plan(match, movie, tmpDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Should have conflicts
	if len(plan.Conflicts) == 0 {
		t.Error("Expected conflicts, got none")
	}

	// Execute should fail
	result, err := org.Execute(plan, false)
	if err == nil {
		t.Error("Expected error due to conflict, got nil")
	}

	if result.Error == nil {
		t.Error("Expected result.Error, got nil")
	}
}

func TestOrganizer_Copy(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	sourceFile := filepath.Join(tmpDir, "source", "ipx-535.mp4")
	if err := os.MkdirAll(filepath.Dir(sourceFile), 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}
	if err := os.WriteFile(sourceFile, []byte("test content"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-535",
	}

	destDir := filepath.Join(tmpDir, "dest")
	plan, err := org.Plan(match, movie, destDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Copy instead of move
	result, err := org.Copy(plan, false)
	if err != nil {
		t.Fatalf("Copy failed: %v", err)
	}

	// Source file should still exist
	if _, err := os.Stat(sourceFile); os.IsNotExist(err) {
		t.Error("Source file was deleted after copy")
	}

	// Target should exist
	if _, err := os.Stat(result.NewPath); os.IsNotExist(err) {
		t.Error("Target file does not exist after copy")
	}

	// Verify both files have same content
	sourceContent, _ := os.ReadFile(sourceFile)
	targetContent, _ := os.ReadFile(result.NewPath)
	if string(sourceContent) != string(targetContent) {
		t.Error("Source and target content mismatch after copy")
	}
}

func TestOrganizer_Revert(t *testing.T) {
	tmpDir := t.TempDir()

	// Create and organize a file
	sourceFile := filepath.Join(tmpDir, "source", "ipx-535.mp4")
	if err := os.MkdirAll(filepath.Dir(sourceFile), 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-535",
	}

	destDir := filepath.Join(tmpDir, "dest")
	result, err := org.Organize(match, movie, destDir, false, false, false)
	if err != nil {
		t.Fatalf("Organize failed: %v", err)
	}

	// Verify file was moved
	if _, err := os.Stat(sourceFile); !os.IsNotExist(err) {
		t.Error("Source file still exists after organize")
	}
	if _, err := os.Stat(result.NewPath); os.IsNotExist(err) {
		t.Error("Target file does not exist after organize")
	}

	// Revert the operation
	if err := org.Revert(result); err != nil {
		t.Fatalf("Revert failed: %v", err)
	}

	// File should be back at original location
	if _, err := os.Stat(sourceFile); os.IsNotExist(err) {
		t.Error("Source file does not exist after revert")
	}
	if _, err := os.Stat(result.NewPath); !os.IsNotExist(err) {
		t.Error("Target file still exists after revert")
	}
}

func TestValidatePlan(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.OutputConfig{}
	org := NewOrganizer(afero.NewOsFs(), cfg)

	// Create a valid source file
	sourceFile := filepath.Join(tmpDir, "source.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	t.Run("Valid plan", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: sourceFile,
			TargetDir:  filepath.Join(tmpDir, "target"),
			TargetFile: "target.mp4",
			TargetPath: filepath.Join(tmpDir, "target", "target.mp4"),
			WillMove:   true,
			Conflicts:  []string{},
		}

		issues := org.ValidatePlan(plan)
		if len(issues) != 0 {
			t.Errorf("Expected no issues, got %d: %v", len(issues), issues)
		}
	})

	t.Run("Source does not exist", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: "/nonexistent/file.mp4",
			TargetDir:  tmpDir,
			TargetFile: "target.mp4",
			TargetPath: filepath.Join(tmpDir, "target.mp4"),
		}

		issues := org.ValidatePlan(plan)
		if len(issues) == 0 {
			t.Error("Expected issues for nonexistent source")
		}
	})

	t.Run("Source and target are same", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: sourceFile,
			TargetPath: sourceFile,
			TargetDir:  tmpDir,
			TargetFile: "source.mp4",
		}

		issues := org.ValidatePlan(plan)
		if len(issues) == 0 {
			t.Error("Expected issues for identical source and target")
		}
	})

	t.Run("With conflicts", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: sourceFile,
			TargetPath: filepath.Join(tmpDir, "target.mp4"),
			TargetDir:  tmpDir,
			TargetFile: "target.mp4",
			Conflicts:  []string{"target exists"},
		}

		issues := org.ValidatePlan(plan)
		if len(issues) == 0 {
			t.Error("Expected issues for plan with conflicts")
		}
	})
}

func TestOrganizer_OrganizeBatch(t *testing.T) {
	tmpDir := t.TempDir()

	// Create multiple source files
	sourceDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	files := []string{"ipx-535.mp4", "abc-123.mp4", "xyz-999.mp4"}
	for _, name := range files {
		path := filepath.Join(sourceDir, name)
		if err := os.WriteFile(path, []byte("test"), 0644); err != nil {
			t.Fatalf("Failed to create file %s: %v", name, err)
		}
	}

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	// Create matches
	matches := []matcher.MatchResult{
		{
			File: scanner.FileInfo{
				Path:      filepath.Join(sourceDir, "ipx-535.mp4"),
				Name:      "ipx-535.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		},
		{
			File: scanner.FileInfo{
				Path:      filepath.Join(sourceDir, "abc-123.mp4"),
				Name:      "abc-123.mp4",
				Extension: ".mp4",
			},
			ID: "ABC-123",
		},
	}

	// Create movie data
	releaseDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	movies := map[string]*models.Movie{
		"IPX-535": {ID: "IPX-535", Title: "Movie1", ReleaseDate: &releaseDate},
		"ABC-123": {ID: "ABC-123", Title: "Movie2", ReleaseDate: &releaseDate},
	}

	destDir := filepath.Join(tmpDir, "dest")

	// Organize batch
	results, err := org.OrganizeBatch(matches, movies, destDir, false, false, false)
	if err != nil {
		t.Fatalf("OrganizeBatch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// Verify all files were moved
	for _, result := range results {
		if result.Error != nil {
			t.Errorf("Organize error: %v", result.Error)
		}
		if !result.Moved {
			t.Errorf("File %s was not moved", result.OriginalPath)
		}
	}
}

func TestCleanEmptyDirectories(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.OutputConfig{}
	org := NewOrganizer(afero.NewOsFs(), cfg)

	// Create nested empty directories with a file
	deepDir := filepath.Join(tmpDir, "a", "b", "c", "d")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}

	// Create a file in the deepest directory
	filePath := filepath.Join(deepDir, "test.mp4")
	if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file: %v", err)
	}

	// Remove the file (simulating a file move)
	if err := os.Remove(filePath); err != nil {
		t.Fatalf("Failed to remove file: %v", err)
	}

	// Clean from file path
	if err := org.CleanEmptyDirectories(filePath, tmpDir); err != nil {
		t.Fatalf("CleanEmptyDirectories failed: %v", err)
	}

	// All nested directories should be removed
	if _, err := os.Stat(filepath.Join(tmpDir, "a")); !os.IsNotExist(err) {
		t.Error("Empty directory 'a' was not removed")
	}
}

func TestCleanEmptyDirectories_WithHiddenFiles(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.OutputConfig{}
	org := NewOrganizer(afero.NewOsFs(), cfg)

	// Create nested directories
	deepDir := filepath.Join(tmpDir, "a", "b", "c")
	if err := os.MkdirAll(deepDir, 0755); err != nil {
		t.Fatalf("Failed to create directories: %v", err)
	}

	// Create a hidden file in middle directory (should prevent deletion)
	hiddenFile := filepath.Join(tmpDir, "a", "b", ".hidden")
	if err := os.WriteFile(hiddenFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create hidden file: %v", err)
	}

	// Try to clean from deepest directory
	if err := org.CleanEmptyDirectories(filepath.Join(deepDir, "file.mp4"), tmpDir); err != nil {
		t.Fatalf("CleanEmptyDirectories failed: %v", err)
	}

	// Directory 'c' should be removed (empty)
	if _, err := os.Stat(deepDir); !os.IsNotExist(err) {
		t.Error("Empty directory 'c' was not removed")
	}

	// Directory 'b' should still exist (has hidden file)
	if _, err := os.Stat(filepath.Join(tmpDir, "a", "b")); os.IsNotExist(err) {
		t.Error("Directory 'b' with hidden file was incorrectly removed")
	}
}

func TestOrganizer_Copy_SourceDoesNotExist(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	// Create plan with nonexistent source
	plan := &OrganizePlan{
		SourcePath: filepath.Join(tmpDir, "nonexistent.mp4"),
		TargetDir:  filepath.Join(tmpDir, "target"),
		TargetFile: "IPX-535.mp4",
		TargetPath: filepath.Join(tmpDir, "target", "IPX-535.mp4"),
		WillMove:   true,
		Conflicts:  []string{},
		Movie:      movie,
	}

	// Copy should fail
	result, err := org.Copy(plan, false)
	if err == nil {
		t.Error("Expected error for nonexistent source file")
	}
	if result.Error == nil {
		t.Error("Expected result.Error for nonexistent source file")
	}
}

func TestOrganizer_Plan_WithSubfolderFormat(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat:    "<ID> - <TITLE>",
		FileFormat:      "<ID>",
		RenameFile:      true,
		SubfolderFormat: []string{"<STUDIO>", "<YEAR>"},
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	sourceFile := filepath.Join(tmpDir, "ipx-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
		},
		ID: "IPX-535",
	}

	plan, err := org.Plan(match, movie, tmpDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Verify subfolder hierarchy is included in target dir
	expectedTargetDir := filepath.Join(tmpDir, "IdeaPocket", "2020", "IPX-535 - Beautiful Day")
	if plan.TargetDir != expectedTargetDir {
		t.Errorf("Expected target dir %s, got %s", expectedTargetDir, plan.TargetDir)
	}
}

func TestOrganizer_Execute_InPlaceRename_DirectoryAlreadyExists(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source directory with file
	sourceDir := filepath.Join(tmpDir, "old-folder")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourceFile := filepath.Join(sourceDir, "ipx-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create target directory with same name (conflict)
	targetDir := filepath.Join(tmpDir, "IPX-535 - Beautiful Day")
	if err := os.MkdirAll(targetDir, 0755); err != nil {
		t.Fatalf("Failed to create target directory: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat: "<ID> - <TITLE>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	// Create plan with in-place rename
	plan := &OrganizePlan{
		SourcePath: sourceFile,
		TargetDir:  targetDir,
		TargetFile: "IPX-535.mp4",
		TargetPath: filepath.Join(targetDir, "IPX-535.mp4"),
		WillMove:   true,
		Conflicts:  []string{},
		InPlace:    true,
		OldDir:     sourceDir,
		Movie:      movie,
	}

	// Execute should fail because target directory already exists
	result, err := org.Execute(plan, false)
	if err == nil {
		t.Error("Expected error when target directory already exists in in-place rename")
	}
	if result.Error == nil {
		t.Error("Expected result.Error when target directory already exists")
	}
}

func TestOrganizer_OrganizeBatch_PartialFailure(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source directory
	sourceDir := filepath.Join(tmpDir, "source")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	// Create first file (will succeed) - use ABC so it sorts first
	file1 := filepath.Join(sourceDir, "abc-123.mp4")
	if err := os.WriteFile(file1, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create file1: %v", err)
	}

	// file2 doesn't exist (will fail) - use XYZ so it sorts last
	file2 := filepath.Join(sourceDir, "xyz-999.mp4")

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	matches := []matcher.MatchResult{
		{
			File: scanner.FileInfo{
				Path:      file1,
				Name:      "abc-123.mp4",
				Extension: ".mp4",
			},
			ID: "ABC-123",
		},
		{
			File: scanner.FileInfo{
				Path:      file2,
				Name:      "xyz-999.mp4",
				Extension: ".mp4",
			},
			ID: "XYZ-999",
		},
	}

	releaseDate := time.Date(2020, 1, 1, 0, 0, 0, 0, time.UTC)
	movies := map[string]*models.Movie{
		"ABC-123": {ID: "ABC-123", Title: "Movie1", ReleaseDate: &releaseDate},
		"XYZ-999": {ID: "XYZ-999", Title: "Movie2", ReleaseDate: &releaseDate},
	}

	destDir := filepath.Join(tmpDir, "dest")

	// Organize batch (should continue despite one failure)
	results, err := org.OrganizeBatch(matches, movies, destDir, false, false, false)
	if err != nil {
		t.Fatalf("OrganizeBatch failed: %v", err)
	}

	if len(results) != 2 {
		t.Errorf("Expected 2 results, got %d", len(results))
	}

	// First file (ABC-123) should succeed
	if results[0].Error != nil {
		t.Errorf("Expected first file (ABC-123) to succeed, got error: %v", results[0].Error)
	}

	// Second file (XYZ-999) should fail (doesn't exist)
	if results[1].Error == nil {
		t.Error("Expected second file (XYZ-999) to fail (doesn't exist)")
	}
}

func TestOrganizer_OrganizeBatch_MissingMovieData(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	sourceFile := filepath.Join(tmpDir, "ipx-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	matches := []matcher.MatchResult{
		{
			File: scanner.FileInfo{
				Path:      sourceFile,
				Name:      "ipx-535.mp4",
				Extension: ".mp4",
			},
			ID: "IPX-535",
		},
	}

	// Empty movie map (no data for this ID)
	movies := map[string]*models.Movie{}

	destDir := filepath.Join(tmpDir, "dest")

	results, err := org.OrganizeBatch(matches, movies, destDir, false, false, false)
	if err != nil {
		t.Fatalf("OrganizeBatch failed: %v", err)
	}

	if len(results) != 1 {
		t.Errorf("Expected 1 result, got %d", len(results))
	}

	// Should have error for missing movie data
	if results[0].Error == nil {
		t.Error("Expected error for missing movie data")
	}
}
