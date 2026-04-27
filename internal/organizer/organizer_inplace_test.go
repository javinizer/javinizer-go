package organizer

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
)

func TestPlan_InPlaceDetection(t *testing.T) {
	tmpDir := t.TempDir()

	// Create matcher
	matcherCfg := &config.MatchingConfig{
		RegexEnabled: false,
	}
	m, err := matcher.NewMatcher(matcherCfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	tests := []struct {
		name                string
		renameFolderInPlace bool
		moveToFolder        bool
		sourceFolder        string
		sourceFile          string
		destDir             string
		expectedInPlace     bool
		expectedReason      string
		expectedTargetDir   string
		addMixedVideo       bool
	}{
		{
			name:                "In-place enabled, dedicated folder, needs rename",
			renameFolderInPlace: true,
			moveToFolder:        true,
			sourceFolder:        "old_folder_name",
			sourceFile:          "IPX-535.mp4",
			destDir:             tmpDir,
			expectedInPlace:     true,
			expectedReason:      "",
		},
		{
			name:                "In-place disabled",
			renameFolderInPlace: false,
			moveToFolder:        true,
			sourceFolder:        "old_folder_name",
			sourceFile:          "IPX-535.mp4",
			destDir:             tmpDir,
			expectedInPlace:     false,
			expectedReason:      "organize mode - always move to destination",
		},
		{
			name:                "Folder already has correct name",
			renameFolderInPlace: true,
			moveToFolder:        true,
			sourceFolder:        "IPX-535 [IdeaPocket] - Beautiful Day",
			sourceFile:          "IPX-535.mp4",
			destDir:             tmpDir,
			expectedInPlace:     false,
			expectedReason:      "folder already has correct name",
		},
		{
			name:                "Mixed IDs in folder",
			renameFolderInPlace: true,
			moveToFolder:        true,
			sourceFolder:        "mixed_folder",
			sourceFile:          "IPX-535.mp4",
			destDir:             tmpDir,
			expectedInPlace:     false,
			expectedReason:      "folder contains mixed IDs",
			addMixedVideo:       true,
		},
		{
			name:                "Folder already correct, MoveToFolder=false, stays in source",
			renameFolderInPlace: true,
			moveToFolder:        false,
			sourceFolder:        "IPX-535 [IdeaPocket] - Beautiful Day",
			sourceFile:          "IPX-535.mp4",
			destDir:             filepath.Join(tmpDir, "dest"),
			expectedInPlace:     false,
			expectedReason:      "folder already has correct name",
		},
		{
			name:                "Not dedicated folder, MoveToFolder=false, stays in source",
			renameFolderInPlace: true,
			moveToFolder:        false,
			sourceFolder:        "mixed_folder_no_move",
			sourceFile:          "IPX-535.mp4",
			destDir:             filepath.Join(tmpDir, "dest"),
			expectedInPlace:     false,
			expectedReason:      "folder contains mixed IDs",
			addMixedVideo:       true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orgCfg := &config.OutputConfig{
				RenameFolderInPlace: tt.renameFolderInPlace,
				MoveToFolder:        tt.moveToFolder,
				FolderFormat:        "<ID> [<STUDIO>] - <TITLE>",
				FileFormat:          "<ID>",
			}
			o := NewOrganizer(afero.NewOsFs(), orgCfg, nil)
			o.SetMatcher(m)

			sourceDir := filepath.Join(tmpDir, tt.sourceFolder)
			if err := os.MkdirAll(sourceDir, 0755); err != nil {
				t.Fatalf("Failed to create source directory: %v", err)
			}

			sourcePath := filepath.Join(sourceDir, tt.sourceFile)
			if err := os.WriteFile(sourcePath, []byte("test video"), 0644); err != nil {
				t.Fatalf("Failed to create source file: %v", err)
			}

			if tt.addMixedVideo {
				otherFile := filepath.Join(sourceDir, "ABC-123.mp4")
				if err := os.WriteFile(otherFile, []byte("other video"), 0644); err != nil {
					t.Fatalf("Failed to create other file: %v", err)
				}
			}

			match := matcher.MatchResult{
				ID: "IPX-535",
				File: scanner.FileInfo{
					Path:      sourcePath,
					Name:      tt.sourceFile,
					Extension: ".mp4",
				},
			}

			movie := &models.Movie{
				ID:    "IPX-535",
				Maker: "IdeaPocket",
				Title: "Beautiful Day",
			}

			plan, err := o.Plan(match, movie, tt.destDir, false)
			if err != nil {
				t.Fatalf("Plan failed: %v", err)
			}

			if plan.InPlace != tt.expectedInPlace {
				t.Errorf("Expected InPlace=%v, got %v", tt.expectedInPlace, plan.InPlace)
			}

			if tt.expectedReason != "" && plan.SkipInPlaceReason != tt.expectedReason {
				t.Errorf("Expected SkipInPlaceReason=%q, got %q", tt.expectedReason, plan.SkipInPlaceReason)
			}

			if plan.InPlace {
				if plan.OldDir == "" {
					t.Error("Expected OldDir to be set for in-place rename")
				}
				if plan.OldDir != sourceDir {
					t.Errorf("Expected OldDir=%q, got %q", sourceDir, plan.OldDir)
				}
			}

			if tt.moveToFolder == false && !tt.expectedInPlace {
				if plan.TargetDir != sourceDir {
					t.Errorf("Expected TargetDir=%q (sourceDir), got %q", sourceDir, plan.TargetDir)
				}
			}
		})
	}
}

func TestPlan_InPlaceFallbackToMoveToFolder(t *testing.T) {
	tmpDir := t.TempDir()

	matcherCfg := &config.MatchingConfig{RegexEnabled: false}
	m, err := matcher.NewMatcher(matcherCfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	destDir := filepath.Join(tmpDir, "dest")
	if err := os.MkdirAll(destDir, 0755); err != nil {
		t.Fatalf("Failed to create dest dir: %v", err)
	}

	movie := &models.Movie{
		ID:    "IPX-535",
		Maker: "IdeaPocket",
		Title: "Beautiful Day",
	}

	t.Run("Mixed IDs with MoveToFolder=true falls back to destDir", func(t *testing.T) {
		sourceDir := filepath.Join(tmpDir, "mixed-ids")
		if err := os.MkdirAll(sourceDir, 0755); err != nil {
			t.Fatalf("Failed to create source dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "IPX-535.mp4"), []byte("video"), 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "ABC-123.mp4"), []byte("other"), 0644); err != nil {
			t.Fatalf("Failed to create mixed file: %v", err)
		}

		cfg := &config.OutputConfig{
			RenameFolderInPlace: true,
			MoveToFolder:        true,
			FolderFormat:        "<ID> - <TITLE>",
			FileFormat:          "<ID>",
			RenameFile:          true,
		}
		o := NewOrganizer(afero.NewOsFs(), cfg, nil)
		o.SetMatcher(m)

		match := matcher.MatchResult{
			ID: "IPX-535",
			File: scanner.FileInfo{
				Path:      filepath.Join(sourceDir, "IPX-535.mp4"),
				Name:      "IPX-535.mp4",
				Extension: ".mp4",
			},
		}

		plan, err := o.Plan(match, movie, destDir, false)
		if err != nil {
			t.Fatalf("Plan failed: %v", err)
		}
		if plan.InPlace {
			t.Error("Expected InPlace=false for mixed IDs")
		}
		if !strings.HasPrefix(plan.TargetDir, destDir) {
			t.Errorf("Expected TargetDir under destDir=%q, got %q", destDir, plan.TargetDir)
		}
		if !plan.WillMove {
			t.Error("Expected WillMove=true for MoveToFolder fallback")
		}
	})

	t.Run("Already-correct folder with MoveToFolder=true falls back to destDir", func(t *testing.T) {
		sourceDir := filepath.Join(tmpDir, "IPX-535 - Beautiful Day")
		if err := os.MkdirAll(sourceDir, 0755); err != nil {
			t.Fatalf("Failed to create source dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "IPX-535.mp4"), []byte("video"), 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}

		cfg := &config.OutputConfig{
			RenameFolderInPlace: true,
			MoveToFolder:        true,
			FolderFormat:        "<ID> - <TITLE>",
			FileFormat:          "<ID>",
			RenameFile:          true,
		}
		o := NewOrganizer(afero.NewOsFs(), cfg, nil)
		o.SetMatcher(m)

		match := matcher.MatchResult{
			ID: "IPX-535",
			File: scanner.FileInfo{
				Path:      filepath.Join(sourceDir, "IPX-535.mp4"),
				Name:      "IPX-535.mp4",
				Extension: ".mp4",
			},
		}

		plan, err := o.Plan(match, movie, destDir, false)
		if err != nil {
			t.Fatalf("Plan failed: %v", err)
		}
		if plan.InPlace {
			t.Error("Expected InPlace=false when folder already correct")
		}
		if !strings.HasPrefix(plan.TargetDir, destDir) {
			t.Errorf("Expected TargetDir under destDir=%q, got %q", destDir, plan.TargetDir)
		}
	})

	t.Run("Mixed IDs with MoveToFolder=false stays in source", func(t *testing.T) {
		sourceDir := filepath.Join(tmpDir, "mixed-no-move")
		if err := os.MkdirAll(sourceDir, 0755); err != nil {
			t.Fatalf("Failed to create source dir: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "IPX-535.mp4"), []byte("video"), 0644); err != nil {
			t.Fatalf("Failed to create source file: %v", err)
		}
		if err := os.WriteFile(filepath.Join(sourceDir, "ABC-123.mp4"), []byte("other"), 0644); err != nil {
			t.Fatalf("Failed to create mixed file: %v", err)
		}

		cfg := &config.OutputConfig{
			RenameFolderInPlace: true,
			MoveToFolder:        false,
			FolderFormat:        "<ID> - <TITLE>",
			FileFormat:          "<ID>",
			RenameFile:          true,
		}
		o := NewOrganizer(afero.NewOsFs(), cfg, nil)
		o.SetMatcher(m)

		match := matcher.MatchResult{
			ID: "IPX-535",
			File: scanner.FileInfo{
				Path:      filepath.Join(sourceDir, "IPX-535.mp4"),
				Name:      "IPX-535.mp4",
				Extension: ".mp4",
			},
		}

		plan, err := o.Plan(match, movie, destDir, false)
		if err != nil {
			t.Fatalf("Plan failed: %v", err)
		}
		if plan.InPlace {
			t.Error("Expected InPlace=false for mixed IDs")
		}
		if plan.TargetDir != sourceDir {
			t.Errorf("Expected TargetDir=sourceDir=%q, got %q", sourceDir, plan.TargetDir)
		}
		if plan.WillMove {
			t.Error("Expected WillMove=false when MoveToFolder=false and no rename needed")
		}
	})
}

func TestExecute_InPlaceRename(t *testing.T) {
	tmpDir := t.TempDir()

	// Create matcher
	matcherCfg := &config.MatchingConfig{
		RegexEnabled: false,
	}
	m, err := matcher.NewMatcher(matcherCfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	orgCfg := &config.OutputConfig{
		RenameFolderInPlace: true,
		FolderFormat:        "<ID> [<STUDIO>] - <TITLE>",
		FileFormat:          "<ID>",
	}
	o := NewOrganizer(afero.NewOsFs(), orgCfg, nil)
	o.SetMatcher(m)

	// Create source directory and file
	sourceFolder := "old_folder_name"
	sourceDir := filepath.Join(tmpDir, sourceFolder)
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourceFile := "IPX-535.mp4" // Must contain the ID for matcher to detect
	sourcePath := filepath.Join(sourceDir, sourceFile)
	if err := os.WriteFile(sourcePath, []byte("test video"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create match result
	match := matcher.MatchResult{
		ID: "IPX-535",
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      sourceFile,
			Extension: ".mp4",
		},
	}

	// Create movie metadata
	movie := &models.Movie{
		ID:    "IPX-535",
		Maker: "IdeaPocket",
		Title: "Beautiful Day",
	}

	// Plan the organization
	plan, err := o.Plan(match, movie, tmpDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	// Verify it's an in-place rename
	if !plan.InPlace {
		t.Fatal("Expected in-place rename to be enabled")
	}

	// Execute the plan
	result, err := o.Execute(plan, false)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Moved {
		t.Error("Expected file to be moved")
	}

	// Verify old directory no longer exists
	if _, err := os.Stat(sourceDir); !os.IsNotExist(err) {
		t.Error("Old directory should not exist after in-place rename")
	}

	// Verify new directory exists
	expectedDir := filepath.Join(tmpDir, "IPX-535 [IdeaPocket] - Beautiful Day")
	if _, err := os.Stat(expectedDir); os.IsNotExist(err) {
		t.Error("New directory should exist after in-place rename")
	}

	// Verify file was renamed
	expectedFile := filepath.Join(expectedDir, "IPX-535.mp4")
	if _, err := os.Stat(expectedFile); os.IsNotExist(err) {
		t.Error("File should exist at new location")
	}

	// Verify file content
	content, err := os.ReadFile(expectedFile)
	if err != nil {
		t.Fatalf("Failed to read file: %v", err)
	}
	if string(content) != "test video" {
		t.Errorf("File content mismatch: got %q, want %q", string(content), "test video")
	}
}

func TestExecute_InPlaceMultiPart(t *testing.T) {
	tmpDir := t.TempDir()

	// Create matcher
	matcherCfg := &config.MatchingConfig{
		RegexEnabled: false,
	}
	m, err := matcher.NewMatcher(matcherCfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	orgCfg := &config.OutputConfig{
		RenameFolderInPlace: true,
		FolderFormat:        "<ID>",
		FileFormat:          "<ID>",
	}
	o := NewOrganizer(afero.NewOsFs(), orgCfg, nil)
	o.SetMatcher(m)

	// Create source directory with multi-part files
	sourceFolder := "old_folder"
	sourceDir := filepath.Join(tmpDir, sourceFolder)
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	// Create part 1
	part1Path := filepath.Join(sourceDir, "IPX-535-pt1.mp4")
	if err := os.WriteFile(part1Path, []byte("part1"), 0644); err != nil {
		t.Fatalf("Failed to create part1: %v", err)
	}

	// Create part 2
	part2Path := filepath.Join(sourceDir, "IPX-535-pt2.mp4")
	if err := os.WriteFile(part2Path, []byte("part2"), 0644); err != nil {
		t.Fatalf("Failed to create part2: %v", err)
	}

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test Movie",
	}

	// Process both parts
	matches := []matcher.MatchResult{
		{
			ID:          "IPX-535",
			IsMultiPart: true,
			PartNumber:  1,
			PartSuffix:  "-pt1",
			File: scanner.FileInfo{
				Path:      part1Path,
				Name:      "IPX-535-pt1.mp4",
				Extension: ".mp4",
			},
		},
		{
			ID:          "IPX-535",
			IsMultiPart: true,
			PartNumber:  2,
			PartSuffix:  "-pt2",
			File: scanner.FileInfo{
				Path:      part2Path,
				Name:      "IPX-535-pt2.mp4",
				Extension: ".mp4",
			},
		},
	}

	// Plan for first part (should trigger in-place rename)
	plan1, err := o.Plan(matches[0], movie, tmpDir, false)
	if err != nil {
		t.Fatalf("Plan failed for part1: %v", err)
	}

	if !plan1.InPlace {
		t.Fatal("Expected in-place rename for part1")
	}

	// Execute part 1 - this renames the directory
	result1, err := o.Execute(plan1, false)
	if err != nil {
		t.Fatalf("Execute failed for part1: %v", err)
	}

	if !result1.Moved {
		t.Error("Expected part1 to be moved")
	}

	// After directory rename, part2 is now at the new location
	// We need to plan for it from its new location
	newPart2Path := filepath.Join(tmpDir, "IPX-535", "IPX-535-pt2.mp4")
	matches[1].File.Path = newPart2Path

	// Plan for part 2 - it should only rename the file (not the directory again)
	// Use forceUpdate=true to allow renaming the file even though directory already exists
	plan2, err := o.Plan(matches[1], movie, tmpDir, true)
	if err != nil {
		t.Fatalf("Plan failed for part2: %v", err)
	}

	// Part 2 should not trigger in-place (directory already has correct name)
	if plan2.InPlace {
		t.Error("Part 2 should not trigger in-place rename")
	}

	// The file is already named correctly, so it shouldn't need to move
	if plan2.WillMove {
		// Execute part 2 - should just rename the file
		result2, err := o.Execute(plan2, false)
		if err != nil {
			t.Fatalf("Execute failed for part2: %v", err)
		}

		if !result2.Moved {
			t.Error("Expected part2 to be moved")
		}
	}

	// Verify both parts are in the renamed directory
	expectedDir := filepath.Join(tmpDir, "IPX-535")
	expectedPart1 := filepath.Join(expectedDir, "IPX-535-pt1.mp4")
	expectedPart2 := filepath.Join(expectedDir, "IPX-535-pt2.mp4")

	if _, err := os.Stat(expectedPart1); os.IsNotExist(err) {
		t.Error("Part 1 should exist at new location")
	}

	if _, err := os.Stat(expectedPart2); os.IsNotExist(err) {
		t.Error("Part 2 should exist at new location")
	}
}

func TestExecute_InPlaceWithSubtitles(t *testing.T) {
	tmpDir := t.TempDir()

	// Create matcher
	matcherCfg := &config.MatchingConfig{
		RegexEnabled: false,
	}
	m, err := matcher.NewMatcher(matcherCfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	orgCfg := &config.OutputConfig{
		RenameFolderInPlace: true,
		FolderFormat:        "<ID>",
		FileFormat:          "<ID>",
		MoveSubtitles:       true,
		SubtitleExtensions:  []string{".srt", ".ass"},
	}
	o := NewOrganizer(afero.NewOsFs(), orgCfg, nil)
	o.SetMatcher(m)

	// Create source directory with video and subtitle files
	sourceFolder := "old_folder"
	sourceDir := filepath.Join(tmpDir, sourceFolder)
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	// Create video file
	videoPath := filepath.Join(sourceDir, "IPX-535.mp4")
	if err := os.WriteFile(videoPath, []byte("video"), 0644); err != nil {
		t.Fatalf("Failed to create video file: %v", err)
	}

	// Create subtitle files
	subtitlePath1 := filepath.Join(sourceDir, "IPX-535.srt")
	if err := os.WriteFile(subtitlePath1, []byte("subtitle1"), 0644); err != nil {
		t.Fatalf("Failed to create subtitle1: %v", err)
	}

	subtitlePath2 := filepath.Join(sourceDir, "IPX-535.eng.ass")
	if err := os.WriteFile(subtitlePath2, []byte("subtitle2"), 0644); err != nil {
		t.Fatalf("Failed to create subtitle2: %v", err)
	}

	match := matcher.MatchResult{
		ID: "IPX-535",
		File: scanner.FileInfo{
			Path:      videoPath,
			Name:      "IPX-535.mp4",
			Extension: ".mp4",
		},
	}

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test",
	}

	// Plan and execute
	plan, err := o.Plan(match, movie, tmpDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if !plan.InPlace {
		t.Fatal("Expected in-place rename")
	}

	result, err := o.Execute(plan, false)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if !result.Moved {
		t.Error("Expected file to be moved")
	}

	// Verify subtitles were moved
	if len(result.Subtitles) != 2 {
		t.Errorf("Expected 2 subtitles, got %d", len(result.Subtitles))
	}

	// Verify subtitle files exist in new location
	expectedDir := filepath.Join(tmpDir, "IPX-535")
	expectedSub1 := filepath.Join(expectedDir, "IPX-535.srt")
	expectedSub2 := filepath.Join(expectedDir, "IPX-535.eng.ass")

	if _, err := os.Stat(expectedSub1); os.IsNotExist(err) {
		t.Error("Subtitle 1 should exist at new location")
	}

	if _, err := os.Stat(expectedSub2); os.IsNotExist(err) {
		t.Error("Subtitle 2 should exist at new location")
	}

	// Verify subtitle content
	content1, _ := os.ReadFile(expectedSub1)
	if string(content1) != "subtitle1" {
		t.Errorf("Subtitle 1 content mismatch: got %q", string(content1))
	}

	content2, _ := os.ReadFile(expectedSub2)
	if string(content2) != "subtitle2" {
		t.Errorf("Subtitle 2 content mismatch: got %q", string(content2))
	}
}

func TestExecute_InPlaceDryRun(t *testing.T) {
	tmpDir := t.TempDir()

	// Create matcher
	matcherCfg := &config.MatchingConfig{
		RegexEnabled: false,
	}
	m, err := matcher.NewMatcher(matcherCfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	orgCfg := &config.OutputConfig{
		RenameFolderInPlace: true,
		FolderFormat:        "<ID>",
		FileFormat:          "<ID>",
	}
	o := NewOrganizer(afero.NewOsFs(), orgCfg, nil)
	o.SetMatcher(m)

	// Create source directory and file
	sourceFolder := "old_folder"
	sourceDir := filepath.Join(tmpDir, sourceFolder)
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourcePath := filepath.Join(sourceDir, "IPX-535.mp4")
	if err := os.WriteFile(sourcePath, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	match := matcher.MatchResult{
		ID: "IPX-535",
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-535.mp4",
			Extension: ".mp4",
		},
	}

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test",
	}

	// Plan and execute in dry-run mode
	plan, err := o.Plan(match, movie, tmpDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	result, err := o.Execute(plan, true)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Moved {
		t.Error("File should not be marked as moved in dry-run")
	}

	// Verify original directory still exists
	if _, err := os.Stat(sourceDir); os.IsNotExist(err) {
		t.Error("Original directory should still exist in dry-run")
	}

	// Verify new directory does not exist
	expectedDir := filepath.Join(tmpDir, "IPX-535")
	if _, err := os.Stat(expectedDir); !os.IsNotExist(err) {
		t.Error("New directory should not exist in dry-run")
	}
}

func TestPlan_InPlaceTruncation_UsesSourceParent(t *testing.T) {
	tmpDir := t.TempDir()

	matcherCfg := &config.MatchingConfig{
		RegexEnabled: false,
	}
	m, err := matcher.NewMatcher(matcherCfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	sourceParent := filepath.Join(tmpDir, "source_parent")
	sourceDir := filepath.Join(sourceParent, "old_folder")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourcePath := filepath.Join(sourceDir, "IPX-535.mp4")
	if err := os.WriteFile(sourcePath, []byte("test video"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	destDir := filepath.Join(tmpDir, "dest")

	// Use a MaxPathLength that will trigger truncation but still allow success
	maxPathLen := 150
	orgCfg := &config.OutputConfig{
		RenameFolderInPlace: true,
		MoveToFolder:        false,
		FolderFormat:        "<ID> - <TITLE>",
		FileFormat:          "<ID>",
		MaxPathLength:       maxPathLen,
	}
	o := NewOrganizer(afero.NewOsFs(), orgCfg, nil)
	o.SetMatcher(m)

	match := matcher.MatchResult{
		ID: "IPX-535",
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "IPX-535.mp4",
			Extension: ".mp4",
		},
	}

	// Create a very long title to ensure truncation is needed
	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "This is an extremely long movie title that will absolutely require truncation to fit within the maximum path length constraint of one hundred characters total",
	}

	// Calculate the untruncated path to verify truncation is needed
	untruncatedFolderName := "IPX-535 - " + movie.Title
	untruncatedPath := filepath.Join(sourceParent, untruncatedFolderName, "IPX-535.mp4")

	plan, err := o.Plan(match, movie, destDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if !plan.InPlace {
		t.Errorf("Expected InPlace=true, got false. SkipReason: %s", plan.SkipInPlaceReason)
	}

	if !strings.HasPrefix(plan.TargetDir, sourceParent) {
		t.Errorf("Expected TargetDir to use sourceParent=%q, got %q", sourceParent, plan.TargetDir)
	}

	if strings.HasPrefix(plan.TargetDir, destDir) {
		t.Errorf("TargetDir should NOT use destDir=%q, got %q", destDir, plan.TargetDir)
	}

	// Verify truncation actually happened
	if len(untruncatedPath) <= maxPathLen {
		t.Fatalf("Test setup error: untruncated path (%d chars) should exceed MaxPathLength (%d)", len(untruncatedPath), maxPathLen)
	}

	if len(plan.TargetPath) > maxPathLen {
		t.Errorf("TargetPath length %d exceeds MaxPathLength %d", len(plan.TargetPath), maxPathLen)
	}

	// Verify the path was actually shortened
	if len(plan.TargetPath) >= len(untruncatedPath) {
		t.Errorf("Expected truncation but path wasn't shortened: before=%d, after=%d", len(untruncatedPath), len(plan.TargetPath))
	}
}

func TestExecute_CaseOnlyDirectoryRename(t *testing.T) {
	tmpDir := t.TempDir()

	caseFile := filepath.Join(tmpDir, "case-test")
	if err := os.WriteFile(caseFile, []byte("test"), 0644); err != nil {
		t.Fatalf("Failed to create case test file: %v", err)
	}
	upperFile := filepath.Join(tmpDir, "CASE-TEST")
	lowerStat, lowerErr := os.Stat(caseFile)
	upperStat, upperErr := os.Stat(upperFile)
	if lowerErr != nil || upperErr != nil || !os.SameFile(lowerStat, upperStat) {
		t.Skip("Skipping: filesystem is case-sensitive, test only valid on case-insensitive FS (macOS/Windows)")
	}
	os.Remove(caseFile)

	matcherCfg := &config.MatchingConfig{
		RegexEnabled: false,
	}
	m, err := matcher.NewMatcher(matcherCfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	sourceParent := filepath.Join(tmpDir, "library")
	sourceDir := filepath.Join(sourceParent, "ipx-535 - beautiful day")
	if err := os.MkdirAll(sourceDir, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	sourcePath := filepath.Join(sourceDir, "ipx-535.mp4")
	if err := os.WriteFile(sourcePath, []byte("test video"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	cfg := &config.OutputConfig{
		RenameFolderInPlace: true,
		MoveToFolder:        false,
		FolderFormat:        "<ID> - <TITLE>",
		FileFormat:          "<ID>",
		RenameFile:          true,
	}
	o := NewOrganizer(afero.NewOsFs(), cfg, nil)
	o.SetMatcher(m)

	match := matcher.MatchResult{
		ID: "IPX-535",
		File: scanner.FileInfo{
			Path:      sourcePath,
			Name:      "ipx-535.mp4",
			Extension: ".mp4",
		},
	}

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Beautiful Day",
	}

	plan, err := o.Plan(match, movie, tmpDir, false)
	if err != nil {
		t.Fatalf("Plan failed: %v", err)
	}

	if !plan.InPlace {
		t.Fatalf("Expected InPlace=true, got false. SkipReason: %s", plan.SkipInPlaceReason)
	}

	expectedTargetDir := filepath.Join(sourceParent, "IPX-535 - Beautiful Day")
	if plan.TargetDir != expectedTargetDir {
		t.Errorf("Expected TargetDir=%q, got %q", expectedTargetDir, plan.TargetDir)
	}

	result, err := o.Execute(plan, false)
	if err != nil {
		t.Fatalf("Execute failed: %v", err)
	}

	if result.Error != nil {
		t.Fatalf("Execute returned error: %v", result.Error)
	}

	if !result.InPlaceRenamed {
		t.Error("Expected InPlaceRenamed=true")
	}

	if _, err := os.Stat(result.NewDirectoryPath); err != nil {
		t.Errorf("New directory path does not exist: %s (error: %v)", result.NewDirectoryPath, err)
	}

	expectedTargetPath := filepath.Join(result.NewDirectoryPath, "IPX-535.mp4")
	if _, err := os.Stat(expectedTargetPath); err != nil {
		t.Errorf("Renamed file does not exist at expected path: %s (error: %v)", expectedTargetPath, err)
	}
}
