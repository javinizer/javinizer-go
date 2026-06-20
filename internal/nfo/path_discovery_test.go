package nfo

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

func TestResolveNFOPath(t *testing.T) {
	movie := &models.Movie{ID: "ABC-123"}

	testCases := []struct {
		name                string
		baseDir             string
		movie               *models.Movie
		nfoFilenameTemplate string
		groupActress        bool
		perFile             bool
		isMultiPart         bool
		partSuffix          string
		videoFilePath       string
		wantNFOPath         string
		wantLegacyCount     int
	}{
		{
			name:                "default template",
			baseDir:             "/movies",
			movie:               movie,
			nfoFilenameTemplate: "<ID>.nfo",
			wantNFOPath:         "/movies/ABC-123.nfo",
			wantLegacyCount:     0,
		},
		{
			name:                "custom template produces legacy path",
			baseDir:             "/movies",
			movie:               movie,
			nfoFilenameTemplate: "[<ID>] <Title>.nfo",
			wantNFOPath:         "/movies/[ABC-123].nfo",
			wantLegacyCount:     1,
		},
		{
			name:                "multi-part with perFile",
			baseDir:             "/movies",
			movie:               movie,
			nfoFilenameTemplate: "<ID>.nfo",
			perFile:             true,
			isMultiPart:         true,
			partSuffix:          "-pt1",
			videoFilePath:       "/movies/ABC-123-pt1.mp4",
			wantNFOPath:         "/movies/ABC-123-pt1.nfo",
			wantLegacyCount:     1,
		},
		{
			name:                "multi-part without perFile",
			baseDir:             "/movies",
			movie:               movie,
			nfoFilenameTemplate: "<ID>.nfo",
			isMultiPart:         true,
			partSuffix:          "-pt1",
			videoFilePath:       "/movies/ABC-123-pt1.mp4",
			wantNFOPath:         "/movies/ABC-123.nfo",
			wantLegacyCount:     0,
		},
		{
			name:                "video-name legacy path for non-default filename",
			baseDir:             "/movies",
			movie:               movie,
			nfoFilenameTemplate: "[<ID>] <Title>.nfo",
			perFile:             true,
			isMultiPart:         true,
			partSuffix:          "-pt1",
			videoFilePath:       "/movies/ABC-123-pt1.mp4",
			wantNFOPath:         "/movies/[ABC-123]-pt1.nfo",
			wantLegacyCount:     2,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			nfoPath, legacyPaths := ResolveNFOPath(tc.baseDir, tc.movie, tc.nfoFilenameTemplate, tc.groupActress, "", "", false, tc.perFile, tc.isMultiPart, tc.partSuffix, tc.videoFilePath)
			if filepath.ToSlash(nfoPath) != tc.wantNFOPath {
				t.Errorf("ResolveNFOPath nfoPath = %q, want %q", filepath.ToSlash(nfoPath), tc.wantNFOPath)
			}
			if len(legacyPaths) != tc.wantLegacyCount {
				t.Errorf("ResolveNFOPath legacyPaths count = %d, want %d (got %v)", len(legacyPaths), tc.wantLegacyCount, legacyPaths)
			}
		})
	}
}

func TestFindNFOFile(t *testing.T) {
	movie := &models.Movie{ID: "ABC-123"}

	testCases := []struct {
		name                string
		setupFS             func(fs afero.Fs)
		baseDir             string
		nfoFilenameTemplate string
		perFile             bool
		isMultiPart         bool
		partSuffix          string
		videoFilePath       string
		wantPath            string
	}{
		{
			name: "primary path found",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/movies", 0755)
				_ = afero.WriteFile(fs, "/movies/ABC-123.nfo", []byte("<test/>"), 0644)
			},
			baseDir:             "/movies",
			nfoFilenameTemplate: "<ID>.nfo",
			wantPath:            "/movies/ABC-123.nfo",
		},
		{
			name: "legacy path found",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/movies", 0755)
				_ = afero.WriteFile(fs, "/movies/ABC-123.nfo", []byte("<legacy/>"), 0644)
			},
			baseDir:             "/movies",
			nfoFilenameTemplate: "[<ID>] <Title>.nfo",
			wantPath:            "/movies/ABC-123.nfo",
		},
		{
			name: "nothing found",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/movies", 0755)
			},
			baseDir:             "/movies",
			nfoFilenameTemplate: "<ID>.nfo",
			wantPath:            "",
		},
		{
			name: "video-name legacy path found",
			setupFS: func(fs afero.Fs) {
				_ = fs.MkdirAll("/movies", 0755)
				_ = afero.WriteFile(fs, "/movies/ABC-123-pt1.nfo", []byte("<video-nfo/>"), 0644)
			},
			baseDir:             "/movies",
			nfoFilenameTemplate: "<ID>.nfo",
			perFile:             true,
			isMultiPart:         true,
			partSuffix:          "-pt1",
			videoFilePath:       "/movies/ABC-123-pt1.mp4",
			wantPath:            "/movies/ABC-123-pt1.nfo",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			if tc.setupFS != nil {
				tc.setupFS(fs)
			}

			origOSStat := osStat
			osStat = func(name string) (os.FileInfo, error) { return fs.Stat(name) }
			defer func() { osStat = origOSStat }()

			got := FindNFOFile(tc.baseDir, movie, tc.nfoFilenameTemplate, false, "", "", false, tc.perFile, tc.isMultiPart, tc.partSuffix, tc.videoFilePath)
			if filepath.ToSlash(got) != tc.wantPath {
				t.Errorf("FindNFOFile = %q, want %q", filepath.ToSlash(got), tc.wantPath)
			}
		})
	}
}
