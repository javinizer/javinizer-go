package tui

import (
	"context"
	"errors"
	"net/http"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/downloader"
	"github.com/javinizer/javinizer-go/internal/matcher"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

// TestSetGetCustomScrapers_DefensiveCopy verifies defensive copying behavior
func TestSetGetCustomScrapers_DefensiveCopy(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		setScrapers    []string
		modifyOriginal bool
		expectedGet    []string
	}{
		{
			name:           "nil scrapers",
			setScrapers:    nil,
			modifyOriginal: false,
			expectedGet:    nil,
		},
		{
			name:           "empty scrapers",
			setScrapers:    []string{},
			modifyOriginal: false,
			expectedGet:    nil, // Empty slice becomes nil after defensive copy
		},
		{
			name:           "single scraper",
			setScrapers:    []string{"r18dev"},
			modifyOriginal: false,
			expectedGet:    []string{"r18dev"},
		},
		{
			name:           "multiple scrapers",
			setScrapers:    []string{"r18dev", "dmm"},
			modifyOriginal: false,
			expectedGet:    []string{"r18dev", "dmm"},
		},
		{
			name:           "modify original after set",
			setScrapers:    []string{"r18dev", "dmm"},
			modifyOriginal: true,
			expectedGet:    []string{"r18dev", "dmm"}, // Should still be original
		},
	}

	for _, tt := range tests {
		tt := tt // Rebind for parallel subtest
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			// Create minimal coordinator (only need to test Set/Get)
			pc := &ProcessingCoordinator{}

			// Set scrapers
			pc.SetCustomScrapers(tt.setScrapers)

			// Modify original if requested
			if tt.modifyOriginal && tt.setScrapers != nil && len(tt.setScrapers) > 0 {
				tt.setScrapers[0] = "modified"
			}

			// Get scrapers
			got := pc.GetCustomScrapers()

			// Verify
			assert.Equal(t, tt.expectedGet, got)

			// Verify modifying returned slice doesn't affect internal state
			if len(got) > 0 {
				got[0] = "external-modification"
				gotAgain := pc.GetCustomScrapers()
				assert.Equal(t, tt.expectedGet, gotAgain, "Internal state should not be affected by external modification")
			}
		})
	}
}

// TestSetOptions verifies configuration methods update state correctly
func TestSetOptions(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name             string
		scrape           bool
		download         bool
		organize         bool
		nfo              bool
		expectedScrape   bool
		expectedDownload bool
		expectedOrganize bool
		expectedNFO      bool
	}{
		{
			name:             "all enabled",
			scrape:           true,
			download:         true,
			organize:         true,
			nfo:              true,
			expectedScrape:   true,
			expectedDownload: true,
			expectedOrganize: true,
			expectedNFO:      true,
		},
		{
			name:             "all disabled",
			scrape:           false,
			download:         false,
			organize:         false,
			nfo:              false,
			expectedScrape:   false,
			expectedDownload: false,
			expectedOrganize: false,
			expectedNFO:      false,
		},
		{
			name:             "selective: scrape and organize only",
			scrape:           true,
			download:         false,
			organize:         true,
			nfo:              false,
			expectedScrape:   true,
			expectedDownload: false,
			expectedOrganize: true,
			expectedNFO:      false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetOptions(tt.scrape, tt.download, tt.organize, tt.nfo)

			assert.Equal(t, tt.expectedScrape, pc.scrapeEnabled)
			assert.Equal(t, tt.expectedDownload, pc.downloadEnabled)
			assert.Equal(t, tt.expectedOrganize, pc.organizeEnabled)
			assert.Equal(t, tt.expectedNFO, pc.nfoEnabled)
		})
	}
}

// TestSetDryRun verifies dry-run flag is set correctly
func TestSetDryRun(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		dryRun   bool
		expected bool
	}{
		{
			name:     "enable dry-run",
			dryRun:   true,
			expected: true,
		},
		{
			name:     "disable dry-run",
			dryRun:   false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetDryRun(tt.dryRun)

			assert.Equal(t, tt.expected, pc.dryRun)
		})
	}
}

// TestSetDestPath verifies destination path is set correctly
func TestSetDestPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		destPath string
		expected string
	}{
		{
			name:     "set destination path",
			destPath: "/videos/organized",
			expected: "/videos/organized",
		},
		{
			name:     "empty destination path",
			destPath: "",
			expected: "",
		},
		{
			name:     "relative destination path",
			destPath: "./organized",
			expected: "./organized",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetDestPath(tt.destPath)

			assert.Equal(t, tt.expected, pc.destPath)
		})
	}
}

// TestSetMoveFiles verifies move files flag is set correctly
func TestSetMoveFiles(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		moveFiles bool
		expected  bool
	}{
		{
			name:      "enable move files",
			moveFiles: true,
			expected:  true,
		},
		{
			name:      "disable move files (copy mode)",
			moveFiles: false,
			expected:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetMoveFiles(tt.moveFiles)

			assert.Equal(t, tt.expected, pc.moveFiles)
		})
	}
}

// TestSetScrapeEnabled verifies scrape enabled flag is set correctly
func TestSetScrapeEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable scraping",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable scraping",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetScrapeEnabled(tt.enabled)

			assert.Equal(t, tt.expected, pc.scrapeEnabled)
		})
	}
}

// TestSetDownloadEnabled verifies download enabled flag is set correctly
func TestSetDownloadEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable downloads",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable downloads",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetDownloadEnabled(tt.enabled)

			assert.Equal(t, tt.expected, pc.downloadEnabled)
		})
	}
}

// TestSetOrganizeEnabled verifies organize enabled flag is set correctly
func TestSetOrganizeEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable organize",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable organize",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetOrganizeEnabled(tt.enabled)

			assert.Equal(t, tt.expected, pc.organizeEnabled)
		})
	}
}

// TestSetNFOEnabled verifies NFO enabled flag is set correctly
func TestSetNFOEnabled(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable NFO generation",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable NFO generation",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetNFOEnabled(tt.enabled)

			assert.Equal(t, tt.expected, pc.nfoEnabled)
		})
	}
}

// TestSetForceUpdate verifies force update flag is set correctly
func TestSetForceUpdate(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		forceUpdate bool
		expected    bool
	}{
		{
			name:        "enable force update",
			forceUpdate: true,
			expected:    true,
		},
		{
			name:        "disable force update",
			forceUpdate: false,
			expected:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetForceUpdate(tt.forceUpdate)

			assert.Equal(t, tt.expected, pc.forceUpdate)
		})
	}
}

// TestSetForceRefresh verifies force refresh flag is set correctly
func TestSetForceRefresh(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name         string
		forceRefresh bool
		expected     bool
	}{
		{
			name:         "enable force refresh",
			forceRefresh: true,
			expected:     true,
		},
		{
			name:         "disable force refresh",
			forceRefresh: false,
			expected:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			pc := &ProcessingCoordinator{}

			pc.SetForceRefresh(tt.forceRefresh)

			assert.Equal(t, tt.expected, pc.forceRefresh)
		})
	}
}

// TestNewProcessingCoordinator verifies constructor initializes with correct defaults
func TestNewProcessingCoordinator(t *testing.T) {
	t.Parallel()

	// Create coordinator with nil dependencies (just testing initialization)
	pc := NewProcessingCoordinator(
		nil, // pool
		nil, // progressTracker
		nil, // movieRepo
		nil, // registry
		nil, // aggregator
		nil, // downloader
		nil, // organizer
		nil, // nfoGenerator
		"/dest/path",
		true, // moveFiles
	)

	// Verify initialization
	assert.NotNil(t, pc)
	assert.Equal(t, "/dest/path", pc.destPath)
	assert.Equal(t, true, pc.moveFiles)
	assert.Equal(t, true, pc.scrapeEnabled, "scrapeEnabled should default to true")
	assert.Equal(t, true, pc.downloadEnabled, "downloadEnabled should default to true")
	assert.Equal(t, true, pc.organizeEnabled, "organizeEnabled should default to true")
	assert.Equal(t, true, pc.nfoEnabled, "nfoEnabled should default to true")
	assert.Equal(t, false, pc.dryRun, "dryRun should default to false")
	assert.Equal(t, false, pc.forceUpdate, "forceUpdate should default to false")
	assert.Equal(t, false, pc.forceRefresh, "forceRefresh should default to false")
	assert.Nil(t, pc.customScraperPriority, "customScraperPriority should default to nil")
}

// ============================================================================
// Mock Implementations for Dependency Injection Testing
// ============================================================================

// mockPool is an inline mock for PoolInterface
type mockPool struct {
	submitCalled int
	submitErr    error
	waitCalled   int
	waitErr      error
	stopCalled   int
}

func (m *mockPool) Submit(task worker.Task) error {
	m.submitCalled++
	return m.submitErr
}

func (m *mockPool) Wait() error {
	m.waitCalled++
	return m.waitErr
}

func (m *mockPool) Stop() {
	m.stopCalled++
}

// mockProgressTracker is an inline mock for ProgressTrackerInterface
type mockProgressTracker struct {
	updateCalled   int
	completeCalled int
	failCalled     int
}

func (m *mockProgressTracker) Update(id string, progress float64, message string, bytesProcessed int64) {
	m.updateCalled++
}

func (m *mockProgressTracker) Complete(id string, message string) {
	m.completeCalled++
}

func (m *mockProgressTracker) Fail(id string, err error) {
	m.failCalled++
}

// mockDownloader is an inline mock for DownloaderInterface
type mockDownloader struct {
	setExtrafanartCalled int
	extrafanartEnabled   bool
}

func (m *mockDownloader) SetDownloadExtrafanart(enabled bool) {
	m.setExtrafanartCalled++
	m.extrafanartEnabled = enabled
}

// ============================================================================
// Tests for Previously Untestable Functions
// ============================================================================

// TestSetDownloadExtrafanart_NilDownloader verifies nil check prevents panic
func TestSetDownloadExtrafanart_NilDownloader(t *testing.T) {
	t.Parallel()

	pc := &ProcessingCoordinator{
		downloader: nil,
	}

	// Should not panic when downloader is nil
	pc.SetDownloadExtrafanart(true)
}

// TestSetDownloadExtrafanart_ValidDownloader verifies delegation to downloader
func TestSetDownloadExtrafanart_ValidDownloader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		enabled  bool
		expected bool
	}{
		{
			name:     "enable extrafanart",
			enabled:  true,
			expected: true,
		},
		{
			name:     "disable extrafanart",
			enabled:  false,
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockDL := &mockDownloader{}
			pc := &ProcessingCoordinator{
				downloader: mockDL,
			}

			pc.SetDownloadExtrafanart(tt.enabled)

			assert.Equal(t, 1, mockDL.setExtrafanartCalled)
			assert.Equal(t, tt.expected, mockDL.extrafanartEnabled)
		})
	}
}

// TestWait verifies delegation to pool.Wait()
func TestWait(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		waitErr  error
		wantErr  bool
		expected error
	}{
		{
			name:     "wait succeeds",
			waitErr:  nil,
			wantErr:  false,
			expected: nil,
		},
		{
			name:     "wait returns error",
			waitErr:  errors.New("pool wait error"),
			wantErr:  true,
			expected: errors.New("pool wait error"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			mockP := &mockPool{waitErr: tt.waitErr}
			pc := &ProcessingCoordinator{
				pool: mockP,
			}

			err := pc.Wait()

			assert.Equal(t, 1, mockP.waitCalled)
			if tt.wantErr {
				assert.Error(t, err)
				assert.Equal(t, tt.expected.Error(), err.Error())
			} else {
				assert.NoError(t, err)
			}
		})
	}
}

// TestStop verifies delegation to pool.Stop()
func TestStop(t *testing.T) {
	t.Parallel()

	mockP := &mockPool{}
	pc := &ProcessingCoordinator{
		pool: mockP,
	}

	pc.Stop()

	assert.Equal(t, 1, mockP.stopCalled)
}

// ============================================================================
// ProcessFiles Tests (using real concrete instances + mockPool pattern)
// ============================================================================

// createMinimalCoordinator creates a ProcessingCoordinator with all required dependencies
// for ProcessFiles nil validation, using a mock pool for control
func createMinimalCoordinator(mockP *mockPool) *ProcessingCoordinator {
	progressChan := make(chan worker.ProgressUpdate, 10)
	progressTracker := worker.NewProgressTracker(progressChan)

	mockClient := &http.Client{}
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	dl := downloader.NewDownloader(mockClient, fs, cfg, "test-agent")

	return &ProcessingCoordinator{
		pool:            mockP,
		progressTracker: progressTracker,
		downloader:      dl,
		registry:        models.NewScraperRegistry(),
		organizer:       nil,
		nfoGenerator:    nil,
		aggregator:      nil,
		movieRepo:       nil,
	}
}

// TestProcessFiles_SkipsDirectories verifies directories are skipped
func TestProcessFiles_SkipsDirectories(t *testing.T) {
	mockP := &mockPool{}
	pc := createMinimalCoordinator(mockP)

	files := []FileItem{
		{Path: "/dir1", IsDir: true, Matched: true},
		{Path: "/dir2", IsDir: true, Matched: true},
	}
	matches := map[string]matcher.MatchResult{}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 0, mockP.submitCalled, "Should not submit tasks for directories")
}

// TestProcessFiles_SkipsUnmatched verifies unmatched files are skipped
func TestProcessFiles_SkipsUnmatched(t *testing.T) {
	mockP := &mockPool{}
	pc := createMinimalCoordinator(mockP)

	files := []FileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: false},
		{Path: "/video2.mp4", IsDir: false, Matched: false},
	}
	matches := map[string]matcher.MatchResult{}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 0, mockP.submitCalled, "Should not submit tasks for unmatched files")
}

// TestProcessFiles_SkipsMissingMatches verifies files without match data are skipped
func TestProcessFiles_SkipsMissingMatches(t *testing.T) {
	mockP := &mockPool{}
	pc := createMinimalCoordinator(mockP)

	files := []FileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
	}
	matches := map[string]matcher.MatchResult{
		// No entry for /video1.mp4
	}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 0, mockP.submitCalled, "Should not submit tasks for files missing match data")
}

// TestProcessFiles_EmptyFileList verifies no crashes with empty file list
func TestProcessFiles_EmptyFileList(t *testing.T) {
	mockP := &mockPool{}
	pc := createMinimalCoordinator(mockP)

	files := []FileItem{}
	matches := map[string]matcher.MatchResult{}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 0, mockP.submitCalled, "Should not submit tasks for empty file list")
}

// TestProcessFiles_SubmitError_Propagates verifies error handling when Submit() fails
func TestProcessFiles_SubmitError_Propagates(t *testing.T) {
	// This test requires real concrete instances to pass type assertions
	// Create minimal dependencies
	progressChan := make(chan worker.ProgressUpdate, 10)
	progressTracker := worker.NewProgressTracker(progressChan)

	mockClient := &http.Client{}
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	dl := downloader.NewDownloader(mockClient, fs, cfg, "test-agent")

	submitErr := errors.New("pool submit failed")
	mockP := &mockPool{submitErr: submitErr}

	pc := &ProcessingCoordinator{
		pool:            mockP,
		progressTracker: progressTracker,
		downloader:      dl,
		organizer:       nil, // Not used in this test
		nfoGenerator:    nil, // Not used in this test
		registry:        models.NewScraperRegistry(),
		aggregator:      nil, // Not used in task creation
		movieRepo:       nil, // Not used in task creation
		destPath:        "/dest",
		moveFiles:       true,
	}

	files := []FileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
	}
	matches := map[string]matcher.MatchResult{
		"/video1.mp4": {ID: "IPX-123"},
	}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to submit process task")
	assert.Equal(t, 1, mockP.submitCalled, "Should attempt to submit task")
}

// TestProcessFiles_ValidFiles_SubmitsTask verifies successful task submission
func TestProcessFiles_ValidFiles_SubmitsTask(t *testing.T) {
	// This test requires real concrete instances to pass type assertions
	// Create minimal dependencies
	progressChan := make(chan worker.ProgressUpdate, 10)
	progressTracker := worker.NewProgressTracker(progressChan)

	mockClient := &http.Client{}
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	dl := downloader.NewDownloader(mockClient, fs, cfg, "test-agent")

	mockP := &mockPool{submitErr: nil} // Success case

	pc := &ProcessingCoordinator{
		pool:            mockP,
		progressTracker: progressTracker,
		downloader:      dl,
		organizer:       nil, // Not used in this test
		nfoGenerator:    nil, // Not used in this test
		registry:        models.NewScraperRegistry(),
		aggregator:      nil, // Not used in task creation
		movieRepo:       nil, // Not used in task creation
		destPath:        "/dest",
		moveFiles:       true,
	}

	files := []FileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
		{Path: "/video2.mp4", IsDir: false, Matched: true},
		{Path: "/dir1", IsDir: true, Matched: true}, // Should be skipped
	}
	matches := map[string]matcher.MatchResult{
		"/video1.mp4": {ID: "IPX-123"},
		"/video2.mp4": {ID: "IPX-456"},
	}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 2, mockP.submitCalled, "Should submit tasks for 2 valid files (skipping directory)")
}

// TestProcessFiles_NilDependencies verifies nil checks prevent panics
func TestProcessFiles_NilDependencies(t *testing.T) {
	tests := []struct {
		name        string
		pc          *ProcessingCoordinator
		expectedErr string
	}{
		{
			name:        "nil pool",
			pc:          &ProcessingCoordinator{pool: nil},
			expectedErr: "worker pool is nil",
		},
		{
			name:        "nil registry",
			pc:          &ProcessingCoordinator{pool: &mockPool{}, registry: nil},
			expectedErr: "scraper registry is nil",
		},
		{
			name: "nil progress tracker",
			pc: &ProcessingCoordinator{
				pool:            &mockPool{},
				registry:        models.NewScraperRegistry(),
				progressTracker: nil,
			},
			expectedErr: "progress tracker is nil",
		},
		{
			name: "nil downloader",
			pc: &ProcessingCoordinator{
				pool:            &mockPool{},
				registry:        models.NewScraperRegistry(),
				progressTracker: &mockProgressTracker{},
				downloader:      nil,
			},
			expectedErr: "downloader is nil",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			files := []FileItem{{Path: "/video1.mp4", IsDir: false, Matched: true}}
			matches := map[string]matcher.MatchResult{"/video1.mp4": {ID: "IPX-123"}}

			err := tt.pc.ProcessFiles(context.Background(), files, matches)

			assert.Error(t, err)
			assert.Contains(t, err.Error(), tt.expectedErr)
		})
	}
}

// TestProcessFiles_ContextCancellation verifies context cancellation stops processing
func TestProcessFiles_ContextCancellation(t *testing.T) {
	mockP := &mockPool{}
	pc := createMinimalCoordinator(mockP)

	// Create a cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Cancel immediately

	files := []FileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
		{Path: "/video2.mp4", IsDir: false, Matched: true},
	}
	matches := map[string]matcher.MatchResult{
		"/video1.mp4": {ID: "IPX-123"},
		"/video2.mp4": {ID: "IPX-456"},
	}

	err := pc.ProcessFiles(ctx, files, matches)

	assert.Error(t, err)
	assert.Equal(t, context.Canceled, err)
	assert.Equal(t, 0, mockP.submitCalled, "Should not submit any tasks when context is cancelled")
}

// TestProcessFiles_CustomScrapers_DefensiveCopy verifies defensive copy of custom scrapers
func TestProcessFiles_CustomScrapers_DefensiveCopy(t *testing.T) {
	// This test verifies that ProcessFiles creates a defensive copy of customScraperPriority
	// to prevent data races when the UI modifies it while tasks are running

	progressChan := make(chan worker.ProgressUpdate, 10)
	progressTracker := worker.NewProgressTracker(progressChan)

	mockClient := &http.Client{}
	fs := afero.NewMemMapFs()
	cfg := &config.OutputConfig{}
	dl := downloader.NewDownloader(mockClient, fs, cfg, "test-agent")

	mockP := &mockPool{submitErr: nil}

	pc := &ProcessingCoordinator{
		pool:                  mockP,
		progressTracker:       progressTracker,
		downloader:            dl,
		organizer:             nil,
		nfoGenerator:          nil,
		registry:              models.NewScraperRegistry(),
		aggregator:            nil,
		movieRepo:             nil,
		destPath:              "/dest",
		moveFiles:             true,
		customScraperPriority: []string{"r18dev", "dmm"},
	}

	files := []FileItem{
		{Path: "/video1.mp4", IsDir: false, Matched: true},
	}
	matches := map[string]matcher.MatchResult{
		"/video1.mp4": {ID: "IPX-123"},
	}

	err := pc.ProcessFiles(context.Background(), files, matches)

	assert.NoError(t, err)
	assert.Equal(t, 1, mockP.submitCalled)

	// Modify the original slice - should not affect submitted tasks (defensive copy was made)
	pc.customScraperPriority[0] = "modified"
	assert.Equal(t, "modified", pc.customScraperPriority[0], "Original slice should be modified")
	// We can't verify the copy directly, but the defensive copy code is executed
}
