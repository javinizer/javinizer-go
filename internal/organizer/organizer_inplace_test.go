package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
)

func TestIsDedicatedFolder(t *testing.T) {
	tmpDir := t.TempDir()

	// Create a matcher
	cfg := &config.MatchingConfig{
		RegexEnabled: false,
	}
	m, err := matcher.NewMatcher(cfg)
	if err != nil {
		t.Fatalf("Failed to create matcher: %v", err)
	}

	orgCfg := &config.OutputConfig{}
	o := NewOrganizer(orgCfg)

	tests := []struct {
		name           string
		files          []string
		id             string
		shouldDedicate bool
	}{
		{
			name:           "Single ID - dedicated",
			files:          []string{"IPX-535.mp4", "IPX-535.nfo", "cover.jpg"},
			id:             "IPX-535",
			shouldDedicate: true,
		},
		{
			name:           "Multi-part same ID - dedicated",
			files:          []string{"IPX-535-pt1.mp4", "IPX-535-pt2.mp4"},
			id:             "IPX-535",
			shouldDedicate: true,
		},
		{
			name:           "Mixed IDs - not dedicated",
			files:          []string{"IPX-535.mp4", "ABC-123.mp4"},
			id:             "IPX-535",
			shouldDedicate: false,
		},
		{
			name:           "No video files - not dedicated",
			files:          []string{"cover.jpg", "metadata.nfo"},
			id:             "IPX-535",
			shouldDedicate: false,
		},
		{
			name:           "Different ID - not dedicated",
			files:          []string{"ABC-123.mp4"},
			id:             "IPX-535",
			shouldDedicate: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test directory
			testDir := filepath.Join(tmpDir, tt.name)
			if err := os.MkdirAll(testDir, 0755); err != nil {
				t.Fatalf("Failed to create test directory: %v", err)
			}

			// Create test files
			for _, file := range tt.files {
				filePath := filepath.Join(testDir, file)
				if err := os.WriteFile(filePath, []byte("test"), 0644); err != nil {
					t.Fatalf("Failed to create test file: %v", err)
				}
			}

			// Test isDedicatedFolder
			isDedicated := o.isDedicatedFolder(testDir, tt.id, m)
			if isDedicated != tt.shouldDedicate {
				t.Errorf("Expected isDedicated=%v, got %v", tt.shouldDedicate, isDedicated)
			}
		})
	}
}

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
		sourceFolder        string
		sourceFile          string
		destDir             string
		expectedInPlace     bool
		expectedReason      string
	}{
		{
			name:                "In-place enabled, dedicated folder, needs rename",
			renameFolderInPlace: true,
			sourceFolder:        "old_folder_name",
			sourceFile:          "IPX-535.mp4",
			destDir:             tmpDir,
			expectedInPlace:     true,
			expectedReason:      "",
		},
		{
			name:                "In-place disabled",
			renameFolderInPlace: false,
			sourceFolder:        "old_folder_name",
			sourceFile:          "IPX-535.mp4",
			destDir:             tmpDir,
			expectedInPlace:     false,
			expectedReason:      "feature disabled in config",
		},
		{
			name:                "Folder already has correct name",
			renameFolderInPlace: true,
			sourceFolder:        "IPX-535 [IdeaPocket] - Beautiful Day",
			sourceFile:          "IPX-535.mp4",
			destDir:             tmpDir,
			expectedInPlace:     false,
			expectedReason:      "folder already has correct name",
		},
		{
			name:                "Mixed IDs in folder",
			renameFolderInPlace: true,
			sourceFolder:        "mixed_folder",
			sourceFile:          "IPX-535.mp4",
			destDir:             tmpDir,
			expectedInPlace:     false,
			expectedReason:      "folder contains mixed IDs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Setup
			orgCfg := &config.OutputConfig{
				RenameFolderInPlace: tt.renameFolderInPlace,
				FolderFormat:        "<ID> [<STUDIO>] - <TITLE>",
				FileFormat:          "<ID>",
			}
			o := NewOrganizer(orgCfg)
			o.SetMatcher(m)

			// Create source directory and file
			sourceDir := filepath.Join(tmpDir, tt.sourceFolder)
			if err := os.MkdirAll(sourceDir, 0755); err != nil {
				t.Fatalf("Failed to create source directory: %v", err)
			}

			sourcePath := filepath.Join(sourceDir, tt.sourceFile)
			if err := os.WriteFile(sourcePath, []byte("test video"), 0644); err != nil {
				t.Fatalf("Failed to create source file: %v", err)
			}

			// For mixed IDs test, add another video file
			if tt.sourceFolder == "mixed_folder" {
				otherFile := filepath.Join(sourceDir, "ABC-123.mp4")
				if err := os.WriteFile(otherFile, []byte("other video"), 0644); err != nil {
					t.Fatalf("Failed to create other file: %v", err)
				}
			}

			// Create match result
			match := matcher.MatchResult{
				ID: "IPX-535",
				File: scanner.FileInfo{
					Path:      sourcePath,
					Name:      tt.sourceFile,
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
			plan, err := o.Plan(match, movie, tt.destDir, false)
			if err != nil {
				t.Fatalf("Plan failed: %v", err)
			}

			// Verify in-place detection
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
		})
	}
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
	o := NewOrganizer(orgCfg)
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
	o := NewOrganizer(orgCfg)
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
	o := NewOrganizer(orgCfg)
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
	o := NewOrganizer(orgCfg)
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
