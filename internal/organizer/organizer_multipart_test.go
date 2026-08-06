package organizer

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

func TestPlan_AppendsPartSuffix(t *testing.T) {
	cfg := &Config{
		FolderFormat:    "<ID> [<STUDIO>] - <TITLE>",
		FileFormat:      "<ID><PARTSUFFIX>", // Use <PARTSUFFIX> placeholder for multi-part support
		RenameFile:      true,
		OperationMode:   operationmode.OperationModeOrganize,
		SubfolderFormat: []string{},
		MaxTitleLength:  0,
		MaxPathLength:   260,
	}
	o := NewOrganizer(afero.NewOsFs(), cfg, nil, nil)

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
			match := models.FileMatchInfo{
				MovieID:     "IPX-535",
				IsMultiPart: tt.partNumber > 0,
				PartNumber:  tt.partNumber,
				PartSuffix:  tt.partSuffix,
				Path:        "/src/IPX-535.mp4", Name: "IPX-535.mp4", Extension: ".mp4",
			}

			plan, err := o.plan(match, movie, "/dest", false, "")
			if err != nil {
				t.Fatalf("Plan failed: %v", err)
			}

			if plan.TargetFile != tt.expectedFile {
				t.Errorf("TargetFile: got %q, want %q", plan.TargetFile, tt.expectedFile)
			}

			expectedDir := "IPX-535 [IdeaPocket] - Beautiful Day"
			if plan.TargetDir == "" {
				t.Errorf("TargetDir should not be empty")
			}
			if filepath.Base(plan.TargetDir) != expectedDir {
				t.Errorf("TargetDir basename: got %q, want %q", filepath.Base(plan.TargetDir), expectedDir)
			}
		})
	}
}

func TestOrganizeBatch_GroupsAndSortsParts(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := &Config{
		FolderFormat:    "<ID>",
		FileFormat:      "<ID><PARTSUFFIX>", // Use <PARTSUFFIX> placeholder for multi-part support
		RenameFile:      true,
		OperationMode:   operationmode.OperationModeOrganize,
		SubfolderFormat: []string{},
	}
	o := NewOrganizer(afero.NewOsFs(), cfg, nil, nil)

	movie := &models.Movie{
		ID:    "IPX-535",
		Title: "Test Movie",
	}

	// Create matches in non-sorted order
	matches := []models.FileMatchInfo{
		{
			MovieID:     "IPX-535",
			IsMultiPart: true,
			PartNumber:  2,
			PartSuffix:  "-pt2",
			Path:        filepath.Join(tmpDir, "IPX-535-pt2.mp4"), Name: "IPX-535-pt2.mp4", Extension: ".mp4",
		},
		{
			MovieID:     "IPX-535",
			IsMultiPart: true,
			PartNumber:  1,
			PartSuffix:  "-pt1",
			Path:        filepath.Join(tmpDir, "IPX-535-pt1.mp4"), Name: "IPX-535-pt1.mp4", Extension: ".mp4",
		},
	}

	// Create the source files
	for _, match := range matches {
		if err := os.WriteFile(match.Path, []byte("fake video"), 0644); err != nil {
			t.Fatalf("Failed to create test file: %v", err)
		}
	}

	movies := map[string]*models.Movie{
		"IPX-535": movie,
	}

	destDir := filepath.Join(tmpDir, "dest")

	// Run OrganizeBatch in dry-run mode
	results, err := organizeBatchViaOrganizeSimple(o, matches, movies, destDir, true, false, false)
	if err != nil {
		t.Fatalf("organizeBatchViaOrganizeSimple failed: %v", err)
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

// TestPlan_LetterQualityTagNoCollision_EndToEnd verifies the reported regression:
// SVFLA-001a-4k.mp4 + SVFLA-001b-4k.mp4 must organize to distinct filenames via the
// real matcher -> ValidateMultipartInDirectory -> organizer.plan pipeline, using the
// default conditional template <ID><IF:MULTIPART>-pt<PART></IF>.
func TestPlan_LetterQualityTagNoCollision_EndToEnd(t *testing.T) {
	cfg := &Config{
		FolderFormat:    "<ID>",
		FileFormat:      "<ID><IF:MULTIPART>-pt<PART></IF>",
		RenameFile:      true,
		OperationMode:   operationmode.OperationModeOrganize,
		SubfolderFormat: []string{},
		MaxPathLength:   260,
	}
	o := NewOrganizer(afero.NewOsFs(), cfg, nil, nil)

	m, err := matcher.NewMatcher(&matcher.Config{RegexEnabled: false})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	files := []models.FileMatchInfo{
		{Name: "SVFLA-001a-4k.mp4", Extension: ".mp4", Path: "/media/JAV/SVFLA-001a-4k.mp4"},
		{Name: "SVFLA-001b-4k.mp4", Extension: ".mp4", Path: "/media/JAV/SVFLA-001b-4k.mp4"},
	}

	results := matcher.ValidateMultipartInDirectory(m.Match(files))

	movie := &models.Movie{ID: "SVFLA-001", Title: "Test"}
	seen := map[string]bool{}
	for _, r := range results {
		fmi := models.FileMatchInfo{
			MovieID: r.ID, IsMultiPart: r.IsMultiPart, PartNumber: r.PartNumber, PartSuffix: r.PartSuffix,
			Path: r.File.Path, Name: r.File.Name, Extension: r.File.Extension,
		}
		plan, err := o.plan(fmi, movie, "/dest", false, "")
		if err != nil {
			t.Fatalf("plan %s: %v", r.File.Name, err)
		}
		if seen[plan.TargetFile] {
			t.Fatalf("filename collision: %q produced more than once", plan.TargetFile)
		}
		seen[plan.TargetFile] = true
	}

	want := map[string]bool{"SVFLA-001-pt1.mp4": true, "SVFLA-001-pt2.mp4": true}
	for f := range want {
		if !seen[f] {
			t.Errorf("missing expected TargetFile %q; got %v", f, seen)
		}
	}
	for f := range seen {
		if !want[f] {
			t.Errorf("unexpected TargetFile %q", f)
		}
	}
}

// TestPlan_LetterSamePartDifferentQuality_NoPtCollision verifies that two encodes of the
// SAME part (same letter, different quality tag) are NOT confirmed multipart, so they do
// not both render the colliding -pt1 name. They stay single-part; any duplicate-target
// collision is surfaced by the existing plan.Conflicts mechanism, not invented here.
func TestPlan_LetterSamePartDifferentQuality_NoPtCollision(t *testing.T) {
	cfg := &Config{
		FolderFormat:    "<ID>",
		FileFormat:      "<ID><IF:MULTIPART>-pt<PART></IF>",
		RenameFile:      true,
		OperationMode:   operationmode.OperationModeOrganize,
		SubfolderFormat: []string{},
		MaxPathLength:   260,
	}
	o := NewOrganizer(afero.NewOsFs(), cfg, nil, nil)

	m, err := matcher.NewMatcher(&matcher.Config{RegexEnabled: false})
	if err != nil {
		t.Fatalf("NewMatcher: %v", err)
	}

	files := []models.FileMatchInfo{
		{Name: "IPX-535a-4k.mp4", Extension: ".mp4", Path: "/media/JAV/IPX-535a-4k.mp4"},
		{Name: "IPX-535a-1080p.mp4", Extension: ".mp4", Path: "/media/JAV/IPX-535a-1080p.mp4"},
	}

	results := matcher.ValidateMultipartInDirectory(m.Match(files))

	movie := &models.Movie{ID: "IPX-535", Title: "Test"}
	targets := map[string]int{}
	for _, r := range results {
		if r.IsMultiPart {
			t.Errorf("%s: same-part-different-quality must stay single-part, got IsMultiPart=true", r.File.Name)
		}
		fmi := models.FileMatchInfo{
			MovieID: r.ID, IsMultiPart: r.IsMultiPart, PartNumber: r.PartNumber, PartSuffix: r.PartSuffix,
			Path: r.File.Path, Name: r.File.Name, Extension: r.File.Extension,
		}
		plan, err := o.plan(fmi, movie, "/dest", false, "")
		if err != nil {
			t.Fatalf("plan %s: %v", r.File.Name, err)
		}
		if strings.Contains(plan.TargetFile, "-pt") {
			t.Errorf("same-part-different-quality must not render a -pt name, got %q", plan.TargetFile)
		}
		targets[plan.TargetFile]++
	}

	// Both encodes are the same part: they share the single-part name <ID>.<ext>.
	// The duplicate-target collision is surfaced by execute-time conflict detection
	// (see TestOrganizerWithAfero_MoveCollision), not invented by the matcher.
	if len(targets) != 1 {
		t.Errorf("expected both encodes to share the single-part name, got %v", targets)
	}
	if _, ok := targets["IPX-535.mp4"]; !ok {
		t.Errorf("expected single-part name IPX-535.mp4, got %v", targets)
	}
}

// TestPlan_LetterSamePart_EncodeNoDataLoss_Execute verifies at EXECUTE time (not just
// plan) that two encodes of the same part (a-4k + a-1080p) do not cause data loss:
// the first moves to the single-part name, the second is skipped via the existing
// target-exists conflict detection rather than overwriting the first.
func TestPlan_LetterSamePart_EncodeNoDataLoss_Execute(t *testing.T) {
	fs := afero.NewMemMapFs()
	srcDir := "/src"
	require.NoError(t, fs.MkdirAll(srcDir, 0755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(srcDir, "IPX-535a-4k.mp4"), []byte("encode-4k"), 0644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(srcDir, "IPX-535a-1080p.mp4"), []byte("encode-1080p"), 0644))

	cfg := &Config{
		FolderFormat:    "<ID>",
		FileFormat:      "<ID><IF:MULTIPART>-pt<PART></IF>",
		RenameFile:      true,
		OperationMode:   operationmode.OperationModeOrganize,
		SubfolderFormat: []string{},
		MaxPathLength:   260,
	}
	org := NewOrganizer(fs, cfg, nil, nil)

	m, err := matcher.NewMatcher(&matcher.Config{RegexEnabled: false})
	require.NoError(t, err)

	files := []models.FileMatchInfo{
		{Name: "IPX-535a-4k.mp4", Extension: ".mp4", Path: filepath.Join(srcDir, "IPX-535a-4k.mp4")},
		{Name: "IPX-535a-1080p.mp4", Extension: ".mp4", Path: filepath.Join(srcDir, "IPX-535a-1080p.mp4")},
	}
	results := matcher.ValidateMultipartInDirectory(m.Match(files))
	for _, r := range results {
		require.False(t, r.IsMultiPart, "same-part encodes must stay single-part")
	}

	movie := &models.Movie{ID: "IPX-535", Title: "Test"}
	destDir := "/dest"
	var firstResult, secondResult *OrganizeResult
	for i, r := range results {
		fmi := models.FileMatchInfo{
			MovieID: r.ID, IsMultiPart: r.IsMultiPart, PartNumber: r.PartNumber, PartSuffix: r.PartSuffix,
			Path: r.File.Path, Name: r.File.Name, Extension: r.File.Extension,
		}
		res, err := org.Organize(context.Background(), OrganizeCmd{
			Match:     fmi,
			Movie:     movie,
			DestDir:   destDir,
			MoveFiles: true,
		})
		if i == 0 {
			firstResult = res
			require.NoError(t, err, "first encode should move")
			require.True(t, res.Moved, "first encode should move to IPX-535.mp4")
		} else {
			secondResult = res
			// Second encode targets the same name; existing conflict detection must reject it
			// (Organize returns nil + error on conflict) rather than overwriting the first.
			require.Error(t, err, "second encode must error on duplicate target, not overwrite")
			if res != nil {
				require.False(t, res.Moved, "second encode must not overwrite the first (no data loss)")
			}
		}
	}

	// Verify the first encode's content survived (no overwrite).
	content, err := afero.ReadFile(fs, filepath.Join(destDir, "IPX-535", "IPX-535.mp4"))
	require.NoError(t, err)
	require.Equal(t, []byte("encode-4k"), content, "first encode content must be preserved")
	_ = firstResult
	_ = secondResult
}
