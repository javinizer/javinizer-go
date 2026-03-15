package commandutil

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/spf13/afero"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/nfo"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/scanner"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// MockDownloaderWithTracking extends MockDownloader with call tracking
type MockDownloaderWithTracking struct {
	*MockDownloader
	Calls []struct {
		Movie     *models.Movie
		DestDir   string
		Multipart *downloader.MultipartInfo
	}
}

func NewMockDownloaderWithTracking(results []downloader.DownloadResult, err error) *MockDownloaderWithTracking {
	return &MockDownloaderWithTracking{
		MockDownloader: NewMockDownloader(results, err),
		Calls: make([]struct {
			Movie     *models.Movie
			DestDir   string
			Multipart *downloader.MultipartInfo
		}, 0),
	}
}

func (m *MockDownloaderWithTracking) DownloadAll(movie *models.Movie, destDir string, multipart *downloader.MultipartInfo) ([]downloader.DownloadResult, error) {
	m.Calls = append(m.Calls, struct {
		Movie     *models.Movie
		DestDir   string
		Multipart *downloader.MultipartInfo
	}{movie, destDir, multipart})
	return m.MockDownloader.DownloadAll(movie, destDir, multipart)
}

// TestGenerateNFOs tests the NFO generation helper function
// Note: This is an integration test that uses real nfo.Generator and organizer.Organizer instances
// because these types are concrete in the production code (not interfaces).
func TestGenerateNFOs(t *testing.T) {
	t.Run("NFO disabled returns 0", func(t *testing.T) {
		movies := map[string]*models.Movie{
			"IPX-123": {ID: "IPX-123", Title: "Test Movie"},
		}
		matches := []matcher.MatchResult{
			{ID: "IPX-123", File: scanner.FileInfo{Name: "ipx-123.mp4", Path: "/tmp/ipx-123.mp4"}},
		}

		nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.DefaultConfig())
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		count, err := GenerateNFOs(movies, matches, nfoGen, org, false, false, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 0, count, "Should return 0 when NFO disabled")
	})

	t.Run("Empty movies map returns 0", func(t *testing.T) {
		movies := map[string]*models.Movie{}
		matches := []matcher.MatchResult{}

		nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.DefaultConfig())
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		count, err := GenerateNFOs(movies, matches, nfoGen, org, true, false, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 0, count, "Should return 0 for empty movies map")
	})

	t.Run("Single file NFO generation in source dir", func(t *testing.T) {
		tmpDir := t.TempDir()
		videoPath := filepath.Join(tmpDir, "ipx-123.mp4")

		// Create a dummy video file
		err := os.WriteFile(videoPath, []byte("fake video"), 0644)
		require.NoError(t, err)

		releaseDate := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:          "IPX-123",
				Title:       "Test Movie",
				Maker:       "Test Studio",
				ReleaseDate: &releaseDate,
			},
		}
		matches := []matcher.MatchResult{
			{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Name: "ipx-123.mp4",
					Path: videoPath,
					Dir:  tmpDir,
				},
			},
		}

		nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.DefaultConfig())
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		count, err := GenerateNFOs(movies, matches, nfoGen, org, true, false, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 1, count, "Should generate 1 NFO")

		// Verify NFO file was created
		nfoPath := filepath.Join(tmpDir, "IPX-123.nfo")
		assert.FileExists(t, nfoPath, "NFO file should be created")
	})

	t.Run("Multi-part with per_file=true generates multiple NFOs", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create dummy video files
		videoPath1 := filepath.Join(tmpDir, "ipx-123-pt1.mp4")
		videoPath2 := filepath.Join(tmpDir, "ipx-123-pt2.mp4")
		err := os.WriteFile(videoPath1, []byte("fake video 1"), 0644)
		require.NoError(t, err)
		err = os.WriteFile(videoPath2, []byte("fake video 2"), 0644)
		require.NoError(t, err)

		releaseDate := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:          "IPX-123",
				Title:       "Test Movie",
				Maker:       "Test Studio",
				ReleaseDate: &releaseDate,
			},
		}
		matches := []matcher.MatchResult{
			{
				ID:          "IPX-123",
				IsMultiPart: true,
				PartNumber:  1,
				PartSuffix:  "-pt1",
				File: scanner.FileInfo{
					Name: "ipx-123-pt1.mp4",
					Path: videoPath1,
					Dir:  tmpDir,
				},
			},
			{
				ID:          "IPX-123",
				IsMultiPart: true,
				PartNumber:  2,
				PartSuffix:  "-pt2",
				File: scanner.FileInfo{
					Name: "ipx-123-pt2.mp4",
					Path: videoPath2,
					Dir:  tmpDir,
				},
			},
		}

		// Create NFO generator with PerFile enabled
		nfoCfg := nfo.DefaultConfig()
		nfoCfg.PerFile = true
		nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfoCfg)
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		// per_file = true should generate one NFO per part
		count, err := GenerateNFOs(movies, matches, nfoGen, org, true, false, true, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 2, count, "Should generate 2 NFOs (one per part)")

		// Verify NFO files were created with part suffixes
		nfoPath1 := filepath.Join(tmpDir, "IPX-123-pt1.nfo")
		nfoPath2 := filepath.Join(tmpDir, "IPX-123-pt2.nfo")
		assert.FileExists(t, nfoPath1, "Part 1 NFO should be created")
		assert.FileExists(t, nfoPath2, "Part 2 NFO should be created")
	})

	t.Run("Multi-part with per_file=false generates single NFO", func(t *testing.T) {
		tmpDir := t.TempDir()

		// Create dummy video files
		videoPath1 := filepath.Join(tmpDir, "ipx-123-pt1.mp4")
		videoPath2 := filepath.Join(tmpDir, "ipx-123-pt2.mp4")
		err := os.WriteFile(videoPath1, []byte("fake video 1"), 0644)
		require.NoError(t, err)
		err = os.WriteFile(videoPath2, []byte("fake video 2"), 0644)
		require.NoError(t, err)

		releaseDate := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:          "IPX-123",
				Title:       "Test Movie",
				Maker:       "Test Studio",
				ReleaseDate: &releaseDate,
			},
		}
		matches := []matcher.MatchResult{
			{
				ID:          "IPX-123",
				IsMultiPart: true,
				PartNumber:  1,
				PartSuffix:  "-pt1",
				File: scanner.FileInfo{
					Name: "ipx-123-pt1.mp4",
					Path: videoPath1,
					Dir:  tmpDir,
				},
			},
			{
				ID:          "IPX-123",
				IsMultiPart: true,
				PartNumber:  2,
				PartSuffix:  "-pt2",
				File: scanner.FileInfo{
					Name: "ipx-123-pt2.mp4",
					Path: videoPath2,
					Dir:  tmpDir,
				},
			},
		}

		nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.DefaultConfig())
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		// per_file = false should generate single shared NFO
		count, err := GenerateNFOs(movies, matches, nfoGen, org, true, false, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 1, count, "Should generate 1 shared NFO")

		// Verify only one NFO file was created (without part suffix)
		nfoPath := filepath.Join(tmpDir, "IPX-123.nfo")
		assert.FileExists(t, nfoPath, "Shared NFO should be created")
	})

	t.Run("Dry run mode does not create files", func(t *testing.T) {
		tmpDir := t.TempDir()
		videoPath := filepath.Join(tmpDir, "ipx-123.mp4")

		err := os.WriteFile(videoPath, []byte("fake video"), 0644)
		require.NoError(t, err)

		releaseDate := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:          "IPX-123",
				Title:       "Test Movie",
				Maker:       "Test Studio",
				ReleaseDate: &releaseDate,
			},
		}
		matches := []matcher.MatchResult{
			{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Name: "ipx-123.mp4",
					Path: videoPath,
					Dir:  tmpDir,
				},
			},
		}

		nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.DefaultConfig())
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		// Dry run should count but not create
		count, err := GenerateNFOs(movies, matches, nfoGen, org, true, false, false, "", false, true)

		assert.NoError(t, err)
		assert.Equal(t, 1, count, "Should return count even in dry run")

		// Verify NO NFO file was actually created
		nfoPath := filepath.Join(tmpDir, "IPX-123.nfo")
		assert.NoFileExists(t, nfoPath, "NFO should not be created in dry run mode")
	})

	t.Run("Move to folder mode uses organizer Plan", func(t *testing.T) {
		tmpDir := t.TempDir()
		videoPath := filepath.Join(tmpDir, "source", "ipx-123.mp4")

		// Create source structure
		err := os.MkdirAll(filepath.Dir(videoPath), 0755)
		require.NoError(t, err)
		err = os.WriteFile(videoPath, []byte("fake video"), 0644)
		require.NoError(t, err)

		releaseDate := time.Date(2023, 1, 1, 0, 0, 0, 0, time.UTC)
		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:          "IPX-123",
				Title:       "Test Movie",
				Maker:       "Test Studio",
				ReleaseDate: &releaseDate,
			},
		}
		matches := []matcher.MatchResult{
			{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Name: "ipx-123.mp4",
					Path: videoPath,
					Dir:  filepath.Dir(videoPath),
				},
			},
		}

		nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.DefaultConfig())
		outputCfg := &config.OutputConfig{
			FolderFormat: "<ID>",
		}
		org := organizer.NewOrganizer(afero.NewOsFs(), outputCfg)
		destPath := filepath.Join(tmpDir, "dest")

		// moveToFolder=true should use organizer.Plan to determine output dir
		count, err := GenerateNFOs(movies, matches, nfoGen, org, true, true, false, destPath, false, false)

		assert.NoError(t, err)
		assert.Equal(t, 1, count, "Should generate NFO in organized folder")

		// Verify NFO was created in the organized destination (not source)
		nfoPath := filepath.Join(destPath, "IPX-123", "IPX-123.nfo")
		assert.FileExists(t, nfoPath, "NFO should be in organized folder")
	})

	t.Run("No matches for movie skips generation", func(t *testing.T) {
		movies := map[string]*models.Movie{
			"IPX-123": {ID: "IPX-123", Title: "Test Movie"},
			"IPX-456": {ID: "IPX-456", Title: "Another Movie"},
		}
		// Only one match for IPX-456, none for IPX-123
		tmpDir := t.TempDir()
		videoPath := filepath.Join(tmpDir, "ipx-456.mp4")
		err := os.WriteFile(videoPath, []byte("fake video"), 0644)
		require.NoError(t, err)

		matches := []matcher.MatchResult{
			{
				ID: "IPX-456",
				File: scanner.FileInfo{
					Name: "ipx-456.mp4",
					Path: videoPath,
					Dir:  tmpDir,
				},
			},
		}

		nfoGen := nfo.NewGenerator(afero.NewOsFs(), nfo.DefaultConfig())
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		// Should generate NFO for IPX-456 only, skip IPX-123 (no matches)
		count, err := GenerateNFOs(movies, matches, nfoGen, org, true, false, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 1, count, "Should generate NFO for movie with matches, skip others")

		// Verify only IPX-456 NFO was created
		nfoPath456 := filepath.Join(tmpDir, "IPX-456.nfo")
		assert.FileExists(t, nfoPath456, "IPX-456 NFO should be created")
	})
}

// TestDownloadMediaFiles tests the media download helper function
// Note: This is an integration test that uses MockDownloader but real organizer.Organizer
// because organizer is a concrete type in the production code.
func TestDownloadMediaFiles(t *testing.T) {
	t.Run("Empty movies map returns 0", func(t *testing.T) {
		movies := map[string]*models.Movie{}
		matches := []matcher.MatchResult{}

		mockDownloader := NewMockDownloader(nil, nil)
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		count, err := DownloadMediaFiles(movies, matches, mockDownloader, org, true, true, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 0, count, "Should return 0 for empty movies map")
	})

	t.Run("No matches for movie skips download", func(t *testing.T) {
		movies := map[string]*models.Movie{
			"IPX-123": {ID: "IPX-123", Title: "Test Movie"},
		}
		matches := []matcher.MatchResult{} // No matches

		mockDownloader := NewMockDownloader(nil, nil)
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		count, err := DownloadMediaFiles(movies, matches, mockDownloader, org, true, true, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 0, count, "Should skip movies with no matches")
	})

	t.Run("Single file download success", func(t *testing.T) {
		tmpDir := t.TempDir()

		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:       "IPX-123",
				Title:    "Test Movie",
				CoverURL: "http://example.com/cover.jpg",
				Screenshots: []string{
					"http://example.com/ss1.jpg",
					"http://example.com/ss2.jpg",
				},
			},
		}
		matches := []matcher.MatchResult{
			{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Name: "ipx-123.mp4",
					Path: filepath.Join(tmpDir, "ipx-123.mp4"),
					Dir:  tmpDir,
				},
			},
		}

		// Mock downloader returns 3 successful downloads (1 cover + 2 screenshots)
		downloadResults := []downloader.DownloadResult{
			{LocalPath: filepath.Join(tmpDir, "cover.jpg"), Size: 1024, Downloaded: true},
			{LocalPath: filepath.Join(tmpDir, "ss1.jpg"), Size: 512, Downloaded: true},
			{LocalPath: filepath.Join(tmpDir, "ss2.jpg"), Size: 512, Downloaded: true},
		}
		mockDownloader := NewMockDownloader(downloadResults, nil)
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		count, err := DownloadMediaFiles(movies, matches, mockDownloader, org, true, true, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 3, count, "Should return total downloaded count")
	})

	t.Run("Dry run mode returns 0 count", func(t *testing.T) {
		tmpDir := t.TempDir()

		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:       "IPX-123",
				Title:    "Test Movie",
				CoverURL: "http://example.com/cover.jpg",
				Screenshots: []string{
					"http://example.com/ss1.jpg",
				},
			},
		}
		matches := []matcher.MatchResult{
			{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Name: "ipx-123.mp4",
					Path: filepath.Join(tmpDir, "ipx-123.mp4"),
					Dir:  tmpDir,
				},
			},
		}

		mockDownloader := NewMockDownloader(nil, nil)
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		// Dry run should show intent but return 0
		count, err := DownloadMediaFiles(movies, matches, mockDownloader, org, true, true, false, "", false, true)

		assert.NoError(t, err)
		assert.Equal(t, 0, count, "Dry run should return 0 count")
	})

	t.Run("Multi-part uses lowest part number", func(t *testing.T) {
		tmpDir := t.TempDir()

		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:       "IPX-123",
				Title:    "Test Movie",
				CoverURL: "http://example.com/cover.jpg",
			},
		}
		matches := []matcher.MatchResult{
			{
				ID:          "IPX-123",
				IsMultiPart: true,
				PartNumber:  1,
				File: scanner.FileInfo{
					Name: "ipx-123-pt1.mp4",
					Path: filepath.Join(tmpDir, "ipx-123-pt1.mp4"),
					Dir:  tmpDir,
				},
			},
			{
				ID:          "IPX-123",
				IsMultiPart: true,
				PartNumber:  2,
				File: scanner.FileInfo{
					Name: "ipx-123-pt2.mp4",
					Path: filepath.Join(tmpDir, "ipx-123-pt2.mp4"),
					Dir:  tmpDir,
				},
			},
		}

		downloadResults := []downloader.DownloadResult{
			{LocalPath: filepath.Join(tmpDir, "cover.jpg"), Size: 1024, Downloaded: true},
		}
		mockDownloader := NewMockDownloaderWithTracking(downloadResults, nil)
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		count, err := DownloadMediaFiles(movies, matches, mockDownloader, org, true, false, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 1, count, "Should download for multi-part movie")

		// Verify DownloadAll was called with multipart info
		assert.Equal(t, 1, len(mockDownloader.Calls), "Should have called DownloadAll once")
		assert.NotNil(t, mockDownloader.Calls[0].Multipart, "Should have multipart info for multi-part file")
		assert.Equal(t, 1, mockDownloader.Calls[0].Multipart.PartNumber, "Should use lowest part number (1)")
	})

	t.Run("Mixed results counts only downloaded", func(t *testing.T) {
		tmpDir := t.TempDir()

		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:       "IPX-123",
				Title:    "Test Movie",
				CoverURL: "http://example.com/cover.jpg",
				Screenshots: []string{
					"http://example.com/ss1.jpg",
					"http://example.com/ss2.jpg",
				},
			},
		}
		matches := []matcher.MatchResult{
			{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Name: "ipx-123.mp4",
					Path: filepath.Join(tmpDir, "ipx-123.mp4"),
					Dir:  tmpDir,
				},
			},
		}

		// Mock: 1 downloaded, 1 skipped, 1 failed
		failedError := assert.AnError
		downloadResults := []downloader.DownloadResult{
			{LocalPath: filepath.Join(tmpDir, "cover.jpg"), Size: 1024, Downloaded: true, Error: nil},     // Downloaded
			{LocalPath: filepath.Join(tmpDir, "ss1.jpg"), Size: 0, Downloaded: false, Error: nil},         // Skipped (already exists)
			{LocalPath: filepath.Join(tmpDir, "ss2.jpg"), Size: 0, Downloaded: false, Error: failedError}, // Failed (error)
		}
		mockDownloader := NewMockDownloader(downloadResults, nil)
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		count, err := DownloadMediaFiles(movies, matches, mockDownloader, org, true, true, false, "", false, false)

		assert.NoError(t, err)
		// Count should only include downloaded files (not skipped or failed)
		assert.Equal(t, 1, count, "Should count only successfully downloaded files")
	})

	t.Run("Download to source directory", func(t *testing.T) {
		tmpDir := t.TempDir()

		movies := map[string]*models.Movie{
			"IPX-123": {
				ID:       "IPX-123",
				Title:    "Test Movie",
				CoverURL: "http://example.com/cover.jpg",
			},
		}
		matches := []matcher.MatchResult{
			{
				ID: "IPX-123",
				File: scanner.FileInfo{
					Name: "ipx-123.mp4",
					Path: filepath.Join(tmpDir, "ipx-123.mp4"),
					Dir:  tmpDir,
				},
			},
		}

		downloadResults := []downloader.DownloadResult{
			{LocalPath: filepath.Join(tmpDir, "cover.jpg"), Size: 1024, Downloaded: true},
		}
		mockDownloader := NewMockDownloaderWithTracking(downloadResults, nil)
		org := organizer.NewOrganizer(afero.NewOsFs(), &config.OutputConfig{})

		// moveToFolder=false should download to source dir
		count, err := DownloadMediaFiles(movies, matches, mockDownloader, org, true, false, false, "", false, false)

		assert.NoError(t, err)
		assert.Equal(t, 1, count, "Should download to source directory")

		// Verify download was called with source directory
		assert.Equal(t, 1, len(mockDownloader.Calls), "Should have called DownloadAll")
		assert.Equal(t, tmpDir, mockDownloader.Calls[0].DestDir, "Should download to source dir")
	})
}
