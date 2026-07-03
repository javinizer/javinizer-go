package batch

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestMultipartPreviewEndToEnd(t *testing.T) {
	// Initialize WebSocket hub
	initTestWebSocket(t)

	// Create config with multipart templates
	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			MediaFormat: config.OutputMediaFormatConfig{
				PosterFormat:     "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
				FanartFormat:     "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg",
				ScreenshotFolder: "extrafanart",
			},
			Download: config.OutputDownloadConfig{
				DownloadCover:       true,
				DownloadPoster:      true,
				DownloadExtrafanart: true,
			},

			// Enable media downloads for preview testing

		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/path", "/output"},
			},
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Create job with multipart files
	job := deps.JobStore.CreateJobBatch([]string{
		"/path/to/STSK-074-pt1.mp4",
		"/path/to/STSK-074-pt2.mp4",
	})

	movie := &models.Movie{
		ID:    "STSK-074",
		Title: "Multipart Test Movie",
	}

	// Simulate what RunBatchScrapeOnce does - add pt1 first
	result1 := &worker.MovieResult{
		ResultID:      "STSK-074-pt1",
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/STSK-074-pt1.mp4", MovieID: "STSK-074", IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/path/to/STSK-074-pt1.mp4", result1)

	result2 := &worker.MovieResult{
		ResultID:      "STSK-074-pt2",
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/STSK-074-pt2.mp4", MovieID: "STSK-074", IsMultiPart: true, PartNumber: 2, PartSuffix: "-pt2"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/path/to/STSK-074-pt2.mp4", result2)

	// Verify what's in the job
	status := job.GetStatus()
	t.Logf("Job has %d results", len(status.Results))
	for path, res := range status.Results {
		t.Logf("  %s: IsMultiPart=%v, PartNumber=%d, PartSuffix=%q",
			path, res.FileMatchInfo.IsMultiPart, res.FileMatchInfo.PartNumber, res.FileMatchInfo.PartSuffix)
	}

	// Now call the preview endpoint
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	reqBody := contracts.OrganizePreviewRequest{
		Destination: "/output",
		CopyOnly:    false,
	}
	body, err := json.Marshal(reqBody)
	require.NoError(t, err)

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/STSK-074-pt1/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())

	assert.Equal(t, 200, w.Code)

	var response contracts.OrganizePreviewResponse
	err = json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	t.Logf("PosterPath: %s", response.PosterPath)
	t.Logf("FanartPath: %s", response.FanartPath)

	// These are the key assertions - poster and fanart should have -pt1 suffix
	assert.Contains(t, response.PosterPath, "-pt1-poster", "poster should have pt1 suffix")
	assert.Contains(t, response.FanartPath, "-pt1-fanart", "fanart should have pt1 suffix")
}

func TestMultipartPreviewLetterPatternDiscoveryFlow(t *testing.T) {
	// Test the FULL flow: discovery -> fileMatchInfo -> preview
	// This verifies that letter-pattern multipart metadata is correctly preserved

	initTestWebSocket(t)

	mediaDir := t.TempDir()
	outputDir := t.TempDir()

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID><IF:MULTIPART>-pt<PART></IF>",
			},
			Operation: config.OutputOperationConfig{
				RenameFile: true,
			},
			MediaFormat: config.OutputMediaFormatConfig{
				PosterFormat:     "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
				ScreenshotFolder: "extrafanart",
			},
			Download: config.OutputDownloadConfig{
				DownloadCover:  true,
				DownloadPoster: true,
			},
		},
		Matching: config.MatchingConfig{
			Extensions:   []string{".mp4"},
			RegexPattern: `(?i)([a-z]{2,10}-?\d{2,5}[a-z]?)`,
			RegexEnabled: true,
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{mediaDir, outputDir},
			},
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Create real test files with letter-pattern suffixes
	partA := filepath.Join(mediaDir, "cemd-349-a.mp4")
	partB := filepath.Join(mediaDir, "cemd-349-b.mp4")
	require.NoError(t, os.WriteFile(partA, []byte("a"), 0o644))
	require.NoError(t, os.WriteFile(partB, []byte("b"), 0o644))

	// Test files with letter-pattern suffixes
	files := []string{partA, partB}

	// Run discovery to get metadata
	apiCfg := core.ConfigFromAppConfig(cfg)
	allFiles, fileMatchInfo := discoverSiblingPartsWithMetadata(context.Background(), files, testkit.GetTestRuntime(deps).Snapshot(), apiCfg.SecurityConfig(), apiCfg.ScannerConfig())

	t.Logf("Discovered %d files", len(allFiles))
	for path, info := range fileMatchInfo {
		t.Logf("  %s: MovieID=%s, IsMultiPart=%v, PartNumber=%d, PartSuffix=%s",
			path, info.MovieID, info.IsMultiPart, info.PartNumber, info.PartSuffix)
	}

	// Verify discovery correctly identified multipart
	require.Len(t, allFiles, 2, "should have 2 files")
	require.Len(t, fileMatchInfo, 2, "should have metadata for 2 files")

	// Verify letter-pattern files are marked as multipart
	for _, path := range files {
		info, ok := fileMatchInfo[path]
		require.True(t, ok, "should have metadata for %s", path)
		assert.True(t, info.IsMultiPart, "%s should be marked as multipart", path)
		assert.NotZero(t, info.PartNumber, "%s should have part number", path)
	}

	// Create job and populate fileMatchInfo (simulating what lifecycle.go does)
	job := deps.JobStore.CreateJobBatch(allFiles)
	job.ResultsWriter().SetFileMatchInfoMap(fileMatchInfo)

	// Register a mock scraper that returns test data
	mockResult := &models.ScraperResult{
		Source: "mock",
		ID:     "CEMD-349",
		Title:  "Test Movie",
	}
	mockScraper := testkit.NewMockScraperWithResults("mock", true, mockResult, nil)
	deps.CoreDeps.GetRegistry().RegisterInstance(mockScraper)

	// Execute the real scrape workflow for each file (exercises actual production code)
	for i, filePath := range files {
		fc, _ := workflow.NewFactoryConfigFromRepos(cfg, deps.CoreDeps.ScraperRegistry, deps.CoreDeps.DB.Repositories())
		factory, err := workflow.NewWorkflowFactory(fc)
		require.NoError(t, err, "NewWorkflowFactory should succeed")
		wf, err := factory.NewWorkflow("")
		require.NoError(t, err, "NewWorkflow should succeed")
		_ = i // index not needed anymore

		result, _, scrapeErr := wf.Scrape(context.Background(), scrape.ScrapeCmd{
			MovieID:          "CEMD-349",
			SelectedScrapers: []string{"mock"},
		}, nil)
		require.NoError(t, scrapeErr, "scrape should succeed for %s", filePath)
		require.NotNil(t, result, "scrape should return a result for %s", filePath)

		// Store result in the job (simulating what BatchJob.StartScrape does)
		if result != nil && result.Movie != nil {
			now := time.Now()
			fmi := fileMatchInfo[filePath]
			fmi.MovieID = result.Movie.ID
			setJobResult(job, filePath, &worker.MovieResult{
				ResultID:      fmt.Sprintf("CEMD-349-pt%d", i+1),
				FileMatchInfo: fmi,
				Status:        models.JobStatusCompleted,
				Movie:         result.Movie,
				StartedAt:     now,
				EndedAt:       &now,
			})
			if result.FieldSources != nil {
				job.ResultsWriter().SetProvenance(filePath, &worker.ProvenanceData{
					FieldSources: result.FieldSources,
				})
			}
		}
	}

	// Verify multipart metadata was applied
	for path, res := range job.ResultsWriter().CloneResults() {
		t.Logf("After metadata apply - %s: IsMultiPart=%v, PartNumber=%d",
			path, res.FileMatchInfo.IsMultiPart, res.FileMatchInfo.PartNumber)
		assert.True(t, res.FileMatchInfo.IsMultiPart, "%s should have IsMultiPart=true", path)
	}

	// Test preview endpoint
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	reqBody := contracts.OrganizePreviewRequest{Destination: outputDir}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/CEMD-349-pt1/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())

	assert.Equal(t, 200, w.Code)

	var response contracts.OrganizePreviewResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	// Verify preview shows correct multipart output
	assert.Contains(t, response.PosterPath, "CEMD-349-pt1-poster", "poster should have pt1 suffix")
	assert.Len(t, response.VideoFiles, 2, "should have 2 video files")
	assert.Contains(t, response.VideoFiles[0], "CEMD-349-pt1.mp4", "first video should be pt1")
	assert.Contains(t, response.VideoFiles[1], "CEMD-349-pt2.mp4", "second video should be pt2")
}

func TestMultipartPreviewLetterPatternFiles(t *testing.T) {
	// Test case: Letter-pattern multipart files (cemd-349-a.mp4, cemd-349-b.mp4)
	// These should NOT cause conflicts because each part gets a unique filename

	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID><IF:MULTIPART>-pt<PART></IF>", // Uses IsMultiPart conditional
			},
			Operation: config.OutputOperationConfig{
				RenameFile: true,
			},
			MediaFormat: config.OutputMediaFormatConfig{
				PosterFormat:     "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
				FanartFormat:     "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg",
				ScreenshotFolder: "extrafanart",
			},
			Download: config.OutputDownloadConfig{
				DownloadCover:  true,
				DownloadPoster: true,
			},
		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/path", "/output"},
			},
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Create job with letter-pattern multipart files
	job := deps.JobStore.CreateJobBatch([]string{
		"/path/to/cemd-349-a.mp4",
		"/path/to/cemd-349-b.mp4",
	})

	movie := &models.Movie{
		ID:    "CEMD-349",
		Title: "Test Movie",
	}

	// Simulate discovery phase results with IsMultiPart=true for letter patterns
	result1 := &worker.MovieResult{
		ResultID:      "CEMD-349-pt1",
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/cemd-349-a.mp4", MovieID: "CEMD-349", IsMultiPart: true, PartNumber: 1, PartSuffix: "-A", Extension: ".mp4"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/path/to/cemd-349-a.mp4", result1)

	result2 := &worker.MovieResult{
		ResultID:      "CEMD-349-pt2",
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/cemd-349-b.mp4", MovieID: "CEMD-349", IsMultiPart: true, PartNumber: 2, PartSuffix: "-B", Extension: ".mp4"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/path/to/cemd-349-b.mp4", result2)

	// Verify job has the correct multipart metadata
	status := job.GetStatus()
	for path, res := range status.Results {
		t.Logf("  %s: IsMultiPart=%v, PartNumber=%d, PartSuffix=%q",
			path, res.FileMatchInfo.IsMultiPart, res.FileMatchInfo.PartNumber, res.FileMatchInfo.PartSuffix)
		assert.True(t, res.FileMatchInfo.IsMultiPart, "file should be marked as multipart")
	}

	// Test preview for first file
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	reqBody := contracts.OrganizePreviewRequest{Destination: "/output"}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/CEMD-349-pt1/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	t.Logf("Response status: %d", w.Code)
	t.Logf("Response body: %s", w.Body.String())

	assert.Equal(t, 200, w.Code)

	var response contracts.OrganizePreviewResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	t.Logf("PosterPath: %s", response.PosterPath)

	// Poster should have -pt1 suffix (part number from discovery phase)
	assert.Contains(t, response.PosterPath, "-pt1-poster", "poster should have pt1 suffix from PartNumber")

	// Verify the file paths in response have unique part suffixes (no conflicts)
	assert.Contains(t, response.FullPath, "CEMD-349-pt1.mp4", "full path should have pt1 suffix")
}

func TestMultipartPreviewSingleFile(t *testing.T) {
	// Test case: User submits only ONE multipart file (e.g., just pt1)
	// The poster should still use the multipart template

	initTestWebSocket(t)

	cfg := &config.Config{
		Output: config.OutputConfig{
			Template: config.OutputTemplateConfig{
				FolderFormat: "<ID>",
				FileFormat:   "<ID>",
			},
			MediaFormat: config.OutputMediaFormatConfig{
				PosterFormat:     "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
				FanartFormat:     "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg",
				ScreenshotFolder: "extrafanart",
			},
			Download: config.OutputDownloadConfig{
				DownloadCover:       true,
				DownloadPoster:      true,
				DownloadExtrafanart: true,
			},

			// Enable media downloads for preview testing

		},
		API: config.APIConfig{
			Security: config.SecurityConfig{
				AllowedDirectories: []string{"/path", "/output"},
			},
		},
	}

	deps := createTestDeps(t, cfg, "")

	// Create job with ONLY ONE multipart file
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/STSK-074-pt1.mp4"})

	movie := &models.Movie{
		ID:    "STSK-074",
		Title: "Multipart Test Movie",
	}

	result := &worker.MovieResult{
		ResultID:      "STSK-074-pt1",
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/STSK-074-pt1.mp4", MovieID: "STSK-074", IsMultiPart: true, PartNumber: 1, PartSuffix: "-pt1"},
		Status:        models.JobStatusCompleted,
		Movie:         movie,
		StartedAt:     time.Now(),
	}
	setJobResult(job, "/path/to/STSK-074-pt1.mp4", result)

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(testkit.GetTestRuntime(deps)))

	reqBody := contracts.OrganizePreviewRequest{Destination: "/output"}
	body, _ := json.Marshal(reqBody)

	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/STSK-074-pt1/preview", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()

	router.ServeHTTP(w, req)

	t.Logf("Response: %s", w.Body.String())

	assert.Equal(t, 200, w.Code)

	var response contracts.OrganizePreviewResponse
	err := json.Unmarshal(w.Body.Bytes(), &response)
	require.NoError(t, err)

	t.Logf("PosterPath: %s", response.PosterPath)
	t.Logf("FanartPath: %s", response.FanartPath)

	// Even with single file, if it's multipart, poster should have -pt1
	assert.Contains(t, response.PosterPath, "-pt1-poster", "poster should have pt1 suffix")
	assert.Contains(t, response.FanartPath, "-pt1-fanart", "fanart should have pt1 suffix")
}
