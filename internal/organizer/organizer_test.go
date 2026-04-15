package organizer

import (
	"os"
	"path/filepath"
	"strings"
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
		MoveToFolder: true,
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
		MoveToFolder: true,
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
		MoveToFolder: true,
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
		MoveToFolder: true,
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
		MoveToFolder: true,
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

func TestOrganizer_CopyWithLinkMode_HardLink(t *testing.T) {
	tmpDir := t.TempDir()

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
		MoveToFolder: true,
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

	result, err := org.CopyWithLinkMode(plan, false, LinkModeHard)
	if err != nil {
		t.Fatalf("CopyWithLinkMode hard failed: %v", err)
	}

	srcInfo, err := os.Stat(sourceFile)
	if err != nil {
		t.Fatalf("Failed to stat source file: %v", err)
	}
	dstInfo, err := os.Stat(result.NewPath)
	if err != nil {
		t.Fatalf("Failed to stat target file: %v", err)
	}
	if !os.SameFile(srcInfo, dstInfo) {
		t.Error("Expected hard link to reference the same file")
	}
}

func TestOrganizer_CopyWithLinkMode_SoftLink(t *testing.T) {
	tmpDir := t.TempDir()

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
		MoveToFolder: true,
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

	result, err := org.CopyWithLinkMode(plan, false, LinkModeSoft)
	if err != nil {
		if os.IsPermission(err) {
			t.Skipf("Skipping symlink test due to permissions: %v", err)
		}
		t.Fatalf("CopyWithLinkMode soft failed: %v", err)
	}

	info, err := os.Lstat(result.NewPath)
	if err != nil {
		t.Fatalf("Failed to lstat target file: %v", err)
	}
	if info.Mode()&os.ModeSymlink == 0 {
		t.Error("Expected target to be a symlink")
	}

	linkedContent, err := os.ReadFile(result.NewPath)
	if err != nil {
		t.Fatalf("Failed to read through symlink: %v", err)
	}
	if string(linkedContent) != "test content" {
		t.Errorf("Symlink content mismatch: got %q", string(linkedContent))
	}
}

func TestOrganizer_CopyWithLinkMode_Copy(t *testing.T) {
	tmpDir := t.TempDir()

	sourceFile := filepath.Join(tmpDir, "source", "ipx-535.mp4")
	if err := os.MkdirAll(filepath.Dir(sourceFile), 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}
	if err := os.WriteFile(sourceFile, []byte("test content for copy"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
		MoveToFolder: true,
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

	result, err := org.CopyWithLinkMode(plan, false, LinkModeNone)
	if err != nil {
		t.Fatalf("CopyWithLinkMode copy failed: %v", err)
	}

	if !result.Moved {
		t.Error("Expected Moved to be true")
	}
	if !result.ShouldGenerateMetadata {
		t.Error("Expected ShouldGenerateMetadata to be true")
	}

	copiedContent, err := os.ReadFile(result.NewPath)
	if err != nil {
		t.Fatalf("Failed to read copied file: %v", err)
	}
	if string(copiedContent) != "test content for copy" {
		t.Errorf("Copied content mismatch: got %q", string(copiedContent))
	}

	originalContent, err := os.ReadFile(sourceFile)
	if err != nil {
		t.Fatalf("Failed to read original file: %v", err)
	}
	if string(originalContent) != "test content for copy" {
		t.Errorf("Original file should still exist after copy: got %q", string(originalContent))
	}
}

func TestOrganizer_CopyWithLinkMode_DryRun(t *testing.T) {
	tmpDir := t.TempDir()

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
		MoveToFolder: true,
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

	result, err := org.CopyWithLinkMode(plan, true, LinkModeHard)
	if err != nil {
		t.Fatalf("CopyWithLinkMode dry-run failed: %v", err)
	}

	if result.Moved {
		t.Error("Expected Moved to be false in dry run")
	}
}

func TestOrganizer_CopyWithLinkMode_Conflicts(t *testing.T) {
	tmpDir := t.TempDir()

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
		MoveToFolder: true,
	}
	org := NewOrganizer(afero.NewOsFs(), cfg)

	plan := &OrganizePlan{
		SourcePath: sourceFile,
		TargetPath: filepath.Join(tmpDir, "dest", "IPX-535.mp4"),
		TargetDir:  filepath.Join(tmpDir, "dest"),
		TargetFile: "IPX-535.mp4",
		WillMove:   true,
		Conflicts:  []string{"target already exists"},
	}

	_, err := org.CopyWithLinkMode(plan, false, LinkModeNone)
	if err == nil {
		t.Fatal("Expected error for conflicts")
	}
	if !strings.Contains(err.Error(), "conflicts detected") {
		t.Errorf("Expected conflict error, got: %v", err)
	}
}

func TestOrganizer_CopyWithLinkMode_NoMove(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
		MoveToFolder: true,
	}
	org := NewOrganizer(afero.NewOsFs(), cfg)

	plan := &OrganizePlan{
		SourcePath: filepath.Join(tmpDir, "source", "ipx-535.mp4"),
		TargetPath: filepath.Join(tmpDir, "source", "ipx-535.mp4"),
		TargetDir:  filepath.Join(tmpDir, "source"),
		TargetFile: "ipx-535.mp4",
		WillMove:   false,
	}

	result, err := org.CopyWithLinkMode(plan, false, LinkModeNone)
	if err != nil {
		t.Fatalf("CopyWithLinkMode no-move failed: %v", err)
	}
	if result.Moved {
		t.Error("Expected Moved to be false when WillMove is false")
	}
}

func TestOrganizer_CopyWithLinkMode_InvalidLinkMode(t *testing.T) {
	tmpDir := t.TempDir()

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
		MoveToFolder: true,
	}
	org := NewOrganizer(afero.NewOsFs(), cfg)

	plan := &OrganizePlan{
		SourcePath: sourceFile,
		TargetPath: filepath.Join(tmpDir, "dest", "IPX-535.mp4"),
		TargetDir:  filepath.Join(tmpDir, "dest"),
		TargetFile: "IPX-535.mp4",
		WillMove:   true,
	}

	_, err := org.CopyWithLinkMode(plan, false, LinkMode("invalid"))
	if err == nil {
		t.Fatal("Expected error for invalid link mode")
	}
	if !strings.Contains(err.Error(), "unsupported link mode") {
		t.Errorf("Expected unsupported link mode error, got: %v", err)
	}
}

func TestOrganizer_CopyWithLinkMode_SourceNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	cfg := &config.OutputConfig{
		FolderFormat: "<ID>",
		FileFormat:   "<ID>",
		RenameFile:   true,
		MoveToFolder: true,
	}
	org := NewOrganizer(afero.NewOsFs(), cfg)

	plan := &OrganizePlan{
		SourcePath: filepath.Join(tmpDir, "nonexistent", "source.mp4"),
		TargetPath: filepath.Join(tmpDir, "dest", "IPX-535.mp4"),
		TargetDir:  filepath.Join(tmpDir, "dest"),
		TargetFile: "IPX-535.mp4",
		WillMove:   true,
	}

	_, err := org.CopyWithLinkMode(plan, false, LinkModeNone)
	if err == nil {
		t.Fatal("Expected error for missing source file")
	}
}

func TestParseLinkMode(t *testing.T) {
	mode, err := ParseLinkMode("hard")
	if err != nil {
		t.Fatalf("ParseLinkMode hard returned error: %v", err)
	}
	if mode != LinkModeHard {
		t.Fatalf("Expected hard, got %q", mode)
	}

	mode, err = ParseLinkMode(" SOFT ")
	if err != nil {
		t.Fatalf("ParseLinkMode soft returned error: %v", err)
	}
	if mode != LinkModeSoft {
		t.Fatalf("Expected soft, got %q", mode)
	}

	mode, err = ParseLinkMode("")
	if err != nil {
		t.Fatalf("ParseLinkMode empty returned error: %v", err)
	}
	if mode != LinkModeNone {
		t.Fatalf("Expected none, got %q", mode)
	}

	if _, err := ParseLinkMode("invalid"); err == nil {
		t.Fatal("Expected invalid link mode to return error")
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
		MoveToFolder: true,
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

	t.Run("WillMove consistent with path equality", func(t *testing.T) {
		// WillMove=true with different paths - valid
		plan1 := &OrganizePlan{
			SourcePath: "/source/file.mp4",
			TargetPath: "/target/file.mp4",
			TargetDir:  "/target",
			TargetFile: "file.mp4",
			WillMove:   true,
		}

		issues1 := org.ValidatePlan(plan1)
		for _, issue := range issues1 {
			if strings.Contains(issue, "WillMove") {
				t.Errorf("Should not report WillMove issue for different paths, got: %s", issue)
			}
		}

		// WillMove=false with same paths - valid (no-op)
		plan2 := &OrganizePlan{
			SourcePath: sourceFile,
			TargetPath: sourceFile,
			TargetDir:  tmpDir,
			TargetFile: "source.mp4",
			WillMove:   false,
		}

		issues2 := org.ValidatePlan(plan2)
		for _, issue := range issues2 {
			if strings.Contains(issue, "identical") {
				t.Errorf("Should not report identical paths as issue for no-op, got: %s", issue)
			}
		}
	})

	t.Run("Source and target same, WillMove=false - no issue", func(t *testing.T) {
		plan := &OrganizePlan{
			SourcePath: sourceFile,
			TargetPath: sourceFile,
			TargetDir:  tmpDir,
			TargetFile: "source.mp4",
			WillMove:   false,
		}

		issues := org.ValidatePlan(plan)
		for _, issue := range issues {
			if strings.Contains(issue, "identical") {
				t.Errorf("Should not report identical paths as issue when WillMove=false, got: %s", issue)
			}
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
		MoveToFolder: true,
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
		MoveToFolder: true,
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
		MoveToFolder:    true,
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
		MoveToFolder: true,
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
		MoveToFolder: true,
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
		MoveToFolder: true,
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

func TestOrganizer_Plan_RenameFolderInPlace_Priority(t *testing.T) {
	tmpDir := t.TempDir()

	sourceDir := filepath.Join(tmpDir, "source", "old-folder")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourceFile := filepath.Join(sourceDir, "ipx-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat:        "<ID> - <TITLE>",
		FileFormat:          "<ID>",
		RenameFile:          true,
		RenameFolderInPlace: true,
		MoveToFolder:        false,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	fileMatcher, err := matcher.NewMatcher(&config.MatchingConfig{})
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	org.SetMatcher(fileMatcher)

	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
			Dir:       sourceDir,
		},
		ID: "IPX-535",
	}

	destDir := filepath.Join(tmpDir, "dest")

	plan, err := org.Plan(match, movie, destDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if !plan.InPlace {
		t.Errorf("Expected InPlace=true when RenameFolderInPlace=true, got false. SkipReason: %s", plan.SkipInPlaceReason)
	}

	expectedOldDir := sourceDir
	if plan.OldDir != expectedOldDir {
		t.Errorf("Expected OldDir=%s, got %s", expectedOldDir, plan.OldDir)
	}

	expectedTargetDir := filepath.Join(tmpDir, "source", "IPX-535 - Beautiful Day")
	if plan.TargetDir != expectedTargetDir {
		t.Errorf("Expected TargetDir=%s, got %s", expectedTargetDir, plan.TargetDir)
	}
}

func TestOrganizer_Plan_BothConfigsTrue_RenamePriority(t *testing.T) {
	tmpDir := t.TempDir()

	sourceDir := filepath.Join(tmpDir, "source", "old-folder")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourceFile := filepath.Join(sourceDir, "ipx-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat:        "<ID> - <TITLE>",
		FileFormat:          "<ID>",
		RenameFile:          true,
		RenameFolderInPlace: true,
		MoveToFolder:        true, // Both configs true - rename should take priority
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	fileMatcher, err := matcher.NewMatcher(&config.MatchingConfig{})
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	org.SetMatcher(fileMatcher)

	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
			Dir:       sourceDir,
		},
		ID: "IPX-535",
	}

	destDir := filepath.Join(tmpDir, "dest")

	plan, err := org.Plan(match, movie, destDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if !plan.InPlace {
		t.Errorf("Expected InPlace=true when both configs true (rename takes priority), got false. SkipReason: %s", plan.SkipInPlaceReason)
	}

	expectedTargetDir := filepath.Join(tmpDir, "source", "IPX-535 - Beautiful Day")
	if plan.TargetDir != expectedTargetDir {
		t.Errorf("Expected TargetDir=%s (in-place), got %s", expectedTargetDir, plan.TargetDir)
	}
}

func TestOrganizer_Plan_BothConfigsFalse_NoFolderChanges(t *testing.T) {
	tmpDir := t.TempDir()

	sourceDir := filepath.Join(tmpDir, "source", "old-folder")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourceFile := filepath.Join(sourceDir, "ipx-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat:        "<ID> - <TITLE>",
		FileFormat:          "<ID>",
		RenameFile:          true,
		RenameFolderInPlace: false,
		MoveToFolder:        false,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	fileMatcher, err := matcher.NewMatcher(&config.MatchingConfig{})
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	org.SetMatcher(fileMatcher)

	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
			Dir:       sourceDir,
		},
		ID: "IPX-535",
	}

	destDir := filepath.Join(tmpDir, "dest")

	plan, err := org.Plan(match, movie, destDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if plan.InPlace {
		t.Error("Expected InPlace=false when both configs are false")
	}

	if plan.TargetDir != sourceDir {
		t.Errorf("Expected TargetDir=%s (no change), got %s", sourceDir, plan.TargetDir)
	}
}

func TestOrganizer_Plan_NoOpHasEmptyConflicts(t *testing.T) {
	tmpDir := t.TempDir()

	sourceDir := filepath.Join(tmpDir, "source", "IPX-535 - Beautiful Day")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourceFile := filepath.Join(sourceDir, "IPX-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat:        "<ID> - <TITLE>",
		FileFormat:          "<ID>",
		RenameFile:          true,
		RenameFolderInPlace: false,
		MoveToFolder:        false,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)
	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
			Dir:       sourceDir,
		},
		ID: "IPX-535",
	}

	destDir := filepath.Join(tmpDir, "dest")

	plan, err := org.Plan(match, movie, destDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if plan.WillMove {
		t.Errorf("Expected WillMove=false for no-op (source == target), got true")
	}

	if len(plan.Conflicts) > 0 {
		t.Errorf("Expected empty Conflicts for no-op plan, got: %v", plan.Conflicts)
	}
}

func TestOrganizer_Plan_TruncationPreservesInPlaceSkip(t *testing.T) {
	tmpDir := t.TempDir()

	sourceDir := filepath.Join(tmpDir, "source", "IPX-535 - Beautiful Day")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourceFile := filepath.Join(sourceDir, "IPX-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat:        "<ID> - <TITLE>",
		FileFormat:          "<ID>",
		RenameFile:          true,
		RenameFolderInPlace: true,
		MoveToFolder:        false,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	fileMatcher, err := matcher.NewMatcher(&config.MatchingConfig{})
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	org.SetMatcher(fileMatcher)

	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "IPX-535.mp4",
			Extension: ".mp4",
			Dir:       sourceDir,
		},
		ID: "IPX-535",
	}

	destDir := filepath.Join(tmpDir, "dest")

	plan, err := org.Plan(match, movie, destDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if plan.InPlace {
		t.Errorf("Expected InPlace=false (folder already correct), got true")
	}

	if plan.TargetDir != sourceDir {
		t.Errorf("Expected TargetDir=%s (stay in source), got %s", sourceDir, plan.TargetDir)
	}

	if strings.HasPrefix(plan.TargetDir, destDir) {
		t.Errorf("TargetDir should not be under destDir when staying in source, got: %s", plan.TargetDir)
	}

	if plan.SkipInPlaceReason != "folder already has correct name" {
		t.Errorf("Expected SkipInPlaceReason='folder already has correct name', got: %s", plan.SkipInPlaceReason)
	}
}

func TestOrganizer_Plan_TruncationPreservesMixedIdSkip(t *testing.T) {
	tmpDir := t.TempDir()

	sourceDir := filepath.Join(tmpDir, "source", "mixed-folder")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourceFile := filepath.Join(sourceDir, "IPX-535.mp4")
	if err := os.WriteFile(sourceFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	otherFile := filepath.Join(sourceDir, "ABC-123.mp4")
	if err := os.WriteFile(otherFile, []byte("other"), 0644); err != nil {
		t.Fatalf("Failed to create other file: %v", err)
	}

	cfg := &config.OutputConfig{
		FolderFormat:        "<ID> - <TITLE>",
		FileFormat:          "<ID>",
		RenameFile:          true,
		RenameFolderInPlace: true,
		MoveToFolder:        false,
	}

	org := NewOrganizer(afero.NewOsFs(), cfg)

	fileMatcher, err := matcher.NewMatcher(&config.MatchingConfig{})
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}
	org.SetMatcher(fileMatcher)

	movie := createTestMovie()

	match := matcher.MatchResult{
		File: scanner.FileInfo{
			Path:      sourceFile,
			Name:      "IPX-535.mp4",
			Extension: ".mp4",
			Dir:       sourceDir,
		},
		ID: "IPX-535",
	}

	destDir := filepath.Join(tmpDir, "dest")

	plan, err := org.Plan(match, movie, destDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if plan.InPlace {
		t.Errorf("Expected InPlace=false (mixed IDs), got true")
	}

	if plan.TargetDir != sourceDir {
		t.Errorf("Expected TargetDir=%s (stay in source for mixed ID), got %s", sourceDir, plan.TargetDir)
	}

	if strings.HasPrefix(plan.TargetDir, destDir) {
		t.Errorf("TargetDir should not be under destDir when mixed ID causes stay-in-source, got: %s", plan.TargetDir)
	}

	if plan.SkipInPlaceReason != "folder contains mixed IDs" {
		t.Errorf("Expected SkipInPlaceReason='folder contains mixed IDs', got: %s", plan.SkipInPlaceReason)
	}
}
