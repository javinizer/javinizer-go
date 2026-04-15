package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scanner"
)

func TestPlan_AppendsPartSuffix(t *testing.T) {
	cfg := &config.OutputConfig{
		FolderFormat:    "<ID> [<STUDIO>] - <TITLE>",
		FileFormat:      "<ID><PARTSUFFIX>", // Use <PARTSUFFIX> placeholder for multi-part support
		RenameFile:      true,
		MoveToFolder:    true,
		SubfolderFormat: []string{},
		MaxTitleLength:  0,
		MaxPathLength:   260,
	}
	o := NewOrganizer(afero.NewOsFs(), cfg, nil)

	movie := &models.Movie{
		ID:          "IPX-535",
		Maker:       "IdeaPocket",
		Title:       "Beautiful Day",
		ReleaseYear: 2020,
	}

	tests := []struct {
		name         string
		partSuffix   string
		partNumber   int
		expectedFile string
	}{
		{
			name:         "Part with -pt1",
			partSuffix:   "-pt1",
			partNumber:   1,
			expectedFile: "IPX-535-pt1.mp4",
		},
		{
			name:         "Part with -A",
			partSuffix:   "-A",
			partNumber:   1,
			expectedFile: "IPX-535-A.mp4",
		},
		{
			name:         "Part with -part2",
			partSuffix:   "-part2",
			partNumber:   2,
			expectedFile: "IPX-535-part2.mp4",
		},
		{
			name:         "No part suffix",
			partSuffix:   "",
			partNumber:   0,
			expectedFile: "IPX-535.mp4",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match := matcher.MatchResult{
				ID:          "IPX-535",
				IsMultiPart: tt.partNumber > 0,
				PartNumber:  tt.partNumber,
				PartSuffix:  tt.partSuffix,
				File: scanner.FileInfo{
					Path:      "/src/IPX-535.mp4",
					Name:      "IPX-535.mp4",
					Extension: ".mp4",
				},
			}

			plan, err := o.Plan(match, movie, "/dest", false)
			if err != nil {
				t.Fatalf("Plan failed: %v", err)
			}

			if plan.TargetFile != tt.expectedFile {
				t.Errorf("TargetFile: got %q, want %q", plan.TargetFile, tt.expectedFile)
			}

			expectedDir := "IPX-535 [IdeaPocket] - Beautiful Day"
			if !filepath.IsAbs(plan.TargetDir) {
				t.Errorf("TargetDir should be absolute, got %q", plan.TargetDir)
			}
			if filepath.Base(plan.TargetDir) != expectedDir {
				t.Errorf("TargetDir basename: got %q, want %q", filepath.Base(plan.TargetDir), expectedDir)
			}
		})
	}
}

func TestOrganizeBatch_GroupsAndSortsParts(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &config.OutputConfig{
		FolderFormat:    "<ID>",
		FileFormat:      "<ID><PARTSUFFIX>", // Use <PARTSUFFIX> placeholder for multi-part support
		RenameFile:      true,
		MoveToFolder:    true,
		SubfolderFormat: []string{},
	}
	o := NewOrganizer(afero.NewOsFs(), cfg, nil)

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test Movie",
	}

	// Create matches in non-sorted order
	matches := []matcher.MatchResult{
		{
			ID:          "IPX-535",
			IsMultiPart: true,
			PartNumber:  2,
			PartSuffix:  "-pt2",
			File: scanner.FileInfo{
				Path:      filepath.Join(tmpDir, "IPX-535-pt2.mp4"),
				Name:      "IPX-535-pt2.mp4",
				Extension: ".mp4",
			},
		},
		{
			ID:          "IPX-535",
			IsMultiPart: true,
			PartNumber:  1,
			PartSuffix:  "-pt1",
			File: scanner.FileInfo{
				Path:      filepath.Join(tmpDir, "IPX-535-pt1.mp4"),
				Name:      "IPX-535-pt1.mp4",
				Extension: ".mp4",
			},
		},
	}

	// Create the source files
	for _, match := range matches {
		if err := os.WriteFile(match.File.Path, []byte("fake video"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	movies := map[string]*models.Movie{
		"IPX-535": movie,
	}

	destDir := filepath.Join(tmpDir, "dest")

	// Run OrganizeBatch in dry-run mode
	results, err := o.OrganizeBatch(matches, movies, destDir, true, false, false)
	if err != nil {
		t.Fatalf("OrganizeBatch failed: %v", err)
	}

	// Verify results are in correct order (part 1 before part 2)
	if len(results) != 2 {
		t.Fatalf("Expected 2 results, got %d", len(results))
	}

	// Results should be sorted by part number
	if filepath.Base(results[0].NewPath) != "IPX-535-pt1.mp4" {
		t.Errorf("First result should be pt1, got %q", filepath.Base(results[0].NewPath))
	}
	if filepath.Base(results[1].NewPath) != "IPX-535-pt2.mp4" {
		t.Errorf("Second result should be pt2, got %q", filepath.Base(results[1].NewPath))
	}

	// Both parts should go to the same folder
	dir0 := filepath.Dir(results[0].NewPath)
	dir1 := filepath.Dir(results[1].NewPath)
	if dir0 != dir1 {
		t.Errorf("Parts should be in the same folder: got %q and %q", dir0, dir1)
	}
}
