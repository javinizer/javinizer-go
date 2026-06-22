package tui

import (
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/javinizer/javinizer-go/internal/models"
)

// BuildMatchMapFromScanResult converts ScanResult into the matchMap and fileRefs
// that the TUI uses internally.
func BuildMatchMapFromScanResult(result *ScanResult) (map[string]models.FileMatchInfo, []models.FileMatchInfo) {
	matchMap := make(map[string]models.FileMatchInfo, len(result.Files))
	fileRefs := make([]models.FileMatchInfo, len(result.Files))
	for i, fmi := range result.Files {
		matchMap[fmi.Path] = fmi
		fileRefs[i] = models.FileMatchInfo{
			Path: fmi.Path,
			Name: fmi.Name,
			Size: fmi.Size,
		}
	}
	return matchMap, fileRefs
}

// BuildFileTree constructs a tree structure of files and directories.
// It groups files by their parent directory, builds the directory hierarchy,
// and returns a sorted slice of FileItems suitable for tree display.
func BuildFileTree(basePath string, files []models.FileMatchInfo, matchMap map[string]models.FileMatchInfo) []fileItem {
	absBasePath, err := filepath.Abs(filepath.FromSlash(basePath))
	if err != nil {
		absBasePath = filepath.FromSlash(basePath)
	}

	dirFiles := groupFilesByDir(files)
	allDirs := enumerateAncestorDirs(dirFiles, absBasePath)

	return buildFileItems(absBasePath, dirFiles, allDirs, matchMap)
}

// groupFilesByDir groups files by their parent directory, normalizing paths
// to absolute form relative to absBasePath.
func groupFilesByDir(files []models.FileMatchInfo) map[string][]models.FileMatchInfo {
	dirFiles := make(map[string][]models.FileMatchInfo)
	for _, file := range files {
		normalizedPath := filepath.FromSlash(file.Path)
		absPath, err := filepath.Abs(normalizedPath)
		if err != nil {
			absPath = normalizedPath
		}
		dir := filepath.Dir(absPath)
		dirFiles[dir] = append(dirFiles[dir], file)
	}
	return dirFiles
}

// enumerateAncestorDirs walks up from each directory to the base path,
// collecting all ancestor directories that should appear in the tree.
func enumerateAncestorDirs(dirFiles map[string][]models.FileMatchInfo, absBasePath string) []string {
	allDirs := make(map[string]bool)
	for dir := range dirFiles {
		current := dir
		for current != absBasePath && current != "." && current != string(os.PathSeparator) && strings.HasPrefix(current, absBasePath) {
			allDirs[current] = true
			parent := filepath.Dir(current)
			if parent == current {
				break
			}
			current = parent
		}
	}

	dirs := make([]string, 0, len(allDirs))
	for dir := range allDirs {
		dirs = append(dirs, dir)
	}
	sort.Strings(dirs)
	return dirs
}

// buildFileItems constructs the ordered list of fileItem entries from grouped
// directory data. It processes subdirectories first (sorted), then the base
// directory files, applying depth calculation and match lookup.
func buildFileItems(absBasePath string, dirFiles map[string][]models.FileMatchInfo, dirs []string, matchMap map[string]models.FileMatchInfo) []fileItem {
	// Calculate relative depth from the base path
	getDepth := func(path string) int {
		rel, err := filepath.Rel(absBasePath, path)
		if err != nil || rel == "." {
			return 0
		}
		return strings.Count(rel, string(filepath.Separator)) + 1
	}

	result := []fileItem{}

	// Process each subdirectory
	for _, dir := range dirs {
		depth := max(getDepth(dir)-1, 0)

		// Add directory item
		result = append(result, fileItem{
			Path:     dir,
			Name:     filepath.Base(dir),
			Size:     0,
			IsDir:    true,
			Selected: false,
			Matched:  false,
			Depth:    depth,
			Parent:   filepath.Dir(dir),
		})

		// Add files in this directory
		if fileList, ok := dirFiles[dir]; ok {
			sort.Slice(fileList, func(i, j int) bool {
				return fileList[i].Name < fileList[j].Name
			})

			for _, file := range fileList {
				item := fileItem{
					Path:     file.Path,
					Name:     file.Name,
					Size:     file.Size,
					IsDir:    false,
					Selected: false,
					Matched:  false,
					Depth:    depth + 1,
					Parent:   dir,
				}

				lookupPath := file.Path
				normalizedLookup := filepath.FromSlash(file.Path)
				if match, found := matchMap[lookupPath]; found {
					item.Matched = true
					item.ID = match.MovieID
				} else if match, found := matchMap[normalizedLookup]; found {
					item.Matched = true
					item.ID = match.MovieID
				}

				result = append(result, item)
			}
		}
	}

	// Add files in the base directory itself
	if baseFiles, ok := dirFiles[absBasePath]; ok {
		sort.Slice(baseFiles, func(i, j int) bool {
			return baseFiles[i].Name < baseFiles[j].Name
		})

		for _, file := range baseFiles {
			item := fileItem{
				Path:     file.Path,
				Name:     file.Name,
				Size:     file.Size,
				IsDir:    false,
				Selected: false,
				Matched:  false,
				Depth:    0,
				Parent:   absBasePath,
			}

			lookupPath := file.Path
			normalizedLookup := filepath.FromSlash(file.Path)
			if match, found := matchMap[lookupPath]; found {
				item.Matched = true
				item.ID = match.MovieID
			} else if match, found := matchMap[normalizedLookup]; found {
				item.Matched = true
				item.ID = match.MovieID
			}

			result = append(result, item)
		}
	}

	return result
}
