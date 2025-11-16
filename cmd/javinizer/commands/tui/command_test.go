package tui_test

import (
	"path/filepath"
	"testing"

	tuicmd "github.com/javinizer/javinizer-go/cmd/javinizer/commands/tui"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/stretchr/testify/assert"
)

// Access buildFileTree via reflection or testing export
// Since buildFileTree is not exported, we need to test it indirectly
// Or we can use a test-specific export

func TestBuildFileTree_EmptyFiles(t *testing.T) {
	basePath := t.TempDir()
	files := []scanner.FileInfo{}
	matchMap := make(map[string]matcher.MatchResult)

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.Empty(t, result)
}

func TestBuildFileTree_SingleFile(t *testing.T) {
	basePath := t.TempDir()
	files := []scanner.FileInfo{
		{
			Path: filepath.Join(basePath, "movie.mp4"),
			Name: "movie.mp4",
			Size: 1024,
		},
	}
	matchMap := make(map[string]matcher.MatchResult)

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.NotEmpty(t, result)
	// Should have at least the file
	fileCount := 0
	for _, item := range result {
		if !item.IsDir {
			fileCount++
			assert.Equal(t, "movie.mp4", item.Name)
			assert.Equal(t, int64(1024), item.Size)
		}
	}
	assert.Equal(t, 1, fileCount)
}

func TestBuildFileTree_WithMatches(t *testing.T) {
	basePath := "/test/path"
	files := []scanner.FileInfo{
		{
			Path: "/test/path/IPX-535.mp4",
			Name: "IPX-535.mp4",
			Size: 2048,
		},
	}
	matchMap := map[string]matcher.MatchResult{
		"/test/path/IPX-535.mp4": {
			ID: "IPX-535",
			File: scanner.FileInfo{
				Path: "/test/path/IPX-535.mp4",
				Name: "IPX-535.mp4",
			},
		},
	}

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.NotEmpty(t, result)
	// Find the file item
	var fileItem *struct {
		Matched bool
		ID      string
	}
	for i := range result {
		if !result[i].IsDir && result[i].Name == "IPX-535.mp4" {
			fileItem = &struct {
				Matched bool
				ID      string
			}{
				Matched: result[i].Matched,
				ID:      result[i].ID,
			}
			break
		}
	}

	assert.NotNil(t, fileItem)
	assert.True(t, fileItem.Matched)
	assert.Equal(t, "IPX-535", fileItem.ID)
}

func TestBuildFileTree_NestedDirectories(t *testing.T) {
	basePath := "/test/path"
	files := []scanner.FileInfo{
		{
			Path: "/test/path/subdir1/movie1.mp4",
			Name: "movie1.mp4",
			Size: 1024,
		},
		{
			Path: "/test/path/subdir1/movie2.mp4",
			Name: "movie2.mp4",
			Size: 2048,
		},
		{
			Path: "/test/path/subdir2/movie3.mp4",
			Name: "movie3.mp4",
			Size: 3072,
		},
	}
	matchMap := make(map[string]matcher.MatchResult)

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.NotEmpty(t, result)

	// Should have directories and files
	dirCount := 0
	fileCount := 0
	for _, item := range result {
		if item.IsDir {
			dirCount++
		} else {
			fileCount++
		}
	}

	assert.Equal(t, 2, dirCount)  // subdir1, subdir2
	assert.Equal(t, 3, fileCount) // 3 movies
}

func TestBuildFileTree_MultipleFilesInSameDir(t *testing.T) {
	basePath := "/test/path"
	files := []scanner.FileInfo{
		{
			Path: "/test/path/movie1.mp4",
			Name: "movie1.mp4",
			Size: 1024,
		},
		{
			Path: "/test/path/movie2.mp4",
			Name: "movie2.mp4",
			Size: 2048,
		},
		{
			Path: "/test/path/movie3.mp4",
			Name: "movie3.mp4",
			Size: 3072,
		},
	}
	matchMap := make(map[string]matcher.MatchResult)

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.NotEmpty(t, result)

	// Count files
	fileCount := 0
	for _, item := range result {
		if !item.IsDir {
			fileCount++
		}
	}

	assert.Equal(t, 3, fileCount)
}

func TestBuildFileTree_DepthCalculation(t *testing.T) {
	basePath := "/test/path"
	files := []scanner.FileInfo{
		{
			Path: "/test/path/movie.mp4",
			Name: "movie.mp4",
			Size: 1024,
		},
		{
			Path: "/test/path/level1/movie.mp4",
			Name: "movie.mp4",
			Size: 2048,
		},
		{
			Path: "/test/path/level1/level2/movie.mp4",
			Name: "movie.mp4",
			Size: 3072,
		},
	}
	matchMap := make(map[string]matcher.MatchResult)

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.NotEmpty(t, result)

	// Check depths
	depthMap := make(map[string]int)
	for _, item := range result {
		depthMap[item.Path] = item.Depth
	}

	// Base level file should have depth 0
	assert.Equal(t, 0, depthMap["/test/path/movie.mp4"])

	// Level1 dir should have depth 0
	assert.Equal(t, 0, depthMap[filepath.Join("/test/path/level1")])

	// Level2 dir should have depth 1
	assert.Equal(t, 1, depthMap[filepath.Join("/test/path/level1/level2")])
}

func TestBuildFileTree_ParentTracking(t *testing.T) {
	basePath := "/test/path"
	files := []scanner.FileInfo{
		{
			Path: "/test/path/subdir/movie.mp4",
			Name: "movie.mp4",
			Size: 1024,
		},
	}
	matchMap := make(map[string]matcher.MatchResult)

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.NotEmpty(t, result)

	// Find the file and check its parent
	for _, item := range result {
		if !item.IsDir && item.Name == "movie.mp4" {
			assert.Equal(t, filepath.Join("/test/path/subdir"), item.Parent)
			break
		}
	}
}

func TestBuildFileTree_SortedOutput(t *testing.T) {
	basePath := "/test/path"
	files := []scanner.FileInfo{
		{
			Path: "/test/path/zebra.mp4",
			Name: "zebra.mp4",
			Size: 1024,
		},
		{
			Path: "/test/path/alpha.mp4",
			Name: "alpha.mp4",
			Size: 2048,
		},
		{
			Path: "/test/path/beta.mp4",
			Name: "beta.mp4",
			Size: 3072,
		},
	}
	matchMap := make(map[string]matcher.MatchResult)

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.NotEmpty(t, result)

	// Extract file names in order
	var fileNames []string
	for _, item := range result {
		if !item.IsDir {
			fileNames = append(fileNames, item.Name)
		}
	}

	// Files should be sorted alphabetically
	assert.Equal(t, []string{"alpha.mp4", "beta.mp4", "zebra.mp4"}, fileNames)
}

func TestBuildFileTree_ComplexStructure(t *testing.T) {
	basePath := "/test/path"
	files := []scanner.FileInfo{
		{
			Path: "/test/path/root1.mp4",
			Name: "root1.mp4",
			Size: 1024,
		},
		{
			Path: "/test/path/dir1/file1.mp4",
			Name: "file1.mp4",
			Size: 2048,
		},
		{
			Path: "/test/path/dir1/file2.mp4",
			Name: "file2.mp4",
			Size: 3072,
		},
		{
			Path: "/test/path/dir2/subdir/file3.mp4",
			Name: "file3.mp4",
			Size: 4096,
		},
	}
	matchMap := map[string]matcher.MatchResult{
		"/test/path/root1.mp4": {
			ID: "ABC-123",
			File: scanner.FileInfo{
				Path: "/test/path/root1.mp4",
				Name: "root1.mp4",
			},
		},
		"/test/path/dir1/file1.mp4": {
			ID: "IPX-535",
			File: scanner.FileInfo{
				Path: "/test/path/dir1/file1.mp4",
				Name: "file1.mp4",
			},
		},
	}

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.NotEmpty(t, result)

	// Count dirs and files
	dirCount := 0
	fileCount := 0
	matchedCount := 0

	for _, item := range result {
		if item.IsDir {
			dirCount++
		} else {
			fileCount++
			if item.Matched {
				matchedCount++
			}
		}
	}

	assert.Equal(t, 3, dirCount)     // dir1, dir2, subdir
	assert.Equal(t, 4, fileCount)    // 4 files
	assert.Equal(t, 2, matchedCount) // 2 matched files
}

func TestBuildFileTree_EmptyMatchMap(t *testing.T) {
	basePath := "/test/path"
	files := []scanner.FileInfo{
		{
			Path: "/test/path/movie.mp4",
			Name: "movie.mp4",
			Size: 1024,
		},
	}
	matchMap := make(map[string]matcher.MatchResult)

	result := tuicmd.BuildFileTree(basePath, files, matchMap)

	assert.NotEmpty(t, result)

	// All files should be unmatched
	for _, item := range result {
		if !item.IsDir {
			assert.False(t, item.Matched)
			assert.Empty(t, item.ID)
		}
	}
}
