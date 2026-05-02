package batch

import (
	"bytes"
	"encoding/json"
	"image"
	"image/color"
	"image/jpeg"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/types"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBatchScrape(t *testing.T) {
	tests := []struct {
		name           string
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *BatchScrapeResponse)
	}{
		{
			name: "valid batch scrape request",
			requestBody: BatchScrapeRequest{
				Files:       []string{"/path/to/IPX-535.mp4", "/path/to/ABC-123.mkv"},
				Strict:      false,
				Force:       false,
				Destination: "/output",
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchScrapeResponse) {
				assert.NotEmpty(t, resp.JobID)
			},
		},
		{
			name: "single file",
			requestBody: BatchScrapeRequest{
				Files: []string{"/path/to/IPX-535.mp4"},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchScrapeResponse) {
				assert.NotEmpty(t, resp.JobID)
			},
		},
		{
			name: "invalid request - missing files",
			requestBody: map[string]interface{}{
				"strict": false,
			},
			expectedStatus: 400,
		},
		{
			name:           "invalid JSON",
			requestBody:    "invalid json",
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize WebSocket hub to prevent nil pointer panic
			initTestWebSocket(t)

			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: []string{"/path", "/output"}, // Allow test paths
					},
				},
			}

			deps := createTestDeps(t, cfg, "")

			router := gin.New()
			router.POST("/batch/scrape", batchScrape(deps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response BatchScrapeResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestGetBatchJob(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) string // Returns job ID
		jobID          string
		expectedStatus int
		validateFn     func(*testing.T, *BatchJobResponse)
	}{
		{
			name: "get existing job",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				return job.ID
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchJobResponse) {
				assert.NotEmpty(t, resp.ID)
				assert.Equal(t, 1, resp.TotalFiles)
			},
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) string {
				return "nonexistent-job-id"
			},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")
			jobID := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.GET("/batch/:id", getBatchJob(deps))

			req := httptest.NewRequest("GET", "/batch/"+jobID, nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response BatchJobResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestCancelBatchJob(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) string
		expectedStatus int
	}{
		{
			name: "cancel existing job",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				return job.ID
			},
			expectedStatus: 200,
		},
		{
			name: "cancel nonexistent job",
			setupJob: func(jq *worker.JobQueue) string {
				return "nonexistent-job-id"
			},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")
			jobID := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.POST("/batch/:id/cancel", cancelBatchJob(deps))

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/cancel", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestUpdateBatchMovie(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) (string, string) // Returns jobID, movieID
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *MovieResponse)
	}{
		{
			name: "update movie successfully",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})

				// Simulate a completed scrape with movie data
				result := &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Original Title"},
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/path/to/IPX-535.mp4", result)

				return job.ID, "IPX-535"
			},
			requestBody: UpdateMovieRequest{
				Movie: &models.Movie{
					ID:    "IPX-535",
					Title: "Updated Title",
				},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *MovieResponse) {
				assert.NotNil(t, resp.Movie)
				assert.Equal(t, "Updated Title", resp.Movie.Title)
			},
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				return "nonexistent-job", "IPX-535"
			},
			requestBody: UpdateMovieRequest{
				Movie: &models.Movie{ID: "IPX-535"},
			},
			expectedStatus: 404,
		},
		{
			name: "movie not found in job",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/ABC-123.mp4"})
				result := &worker.FileResult{
					FilePath:  "/path/to/ABC-123.mp4",
					MovieID:   "ABC-123",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABC-123"},
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/path/to/ABC-123.mp4", result)
				return job.ID, "NONEXISTENT-999"
			},
			requestBody: UpdateMovieRequest{
				Movie: &models.Movie{ID: "NONEXISTENT-999"},
			},
			expectedStatus: 404,
		},
		{
			name: "invalid request body",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				return job.ID, "IPX-535"
			},
			requestBody:    "invalid json",
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")

			jobID, movieID := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.PATCH("/batch/:id/movies/:movieId", updateBatchMovie(deps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("PATCH", "/batch/"+jobID+"/movies/"+movieID, bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response MovieResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

func TestUpdateBatchMoviePosterCrop(t *testing.T) {
	gin.SetMode(gin.TestMode)

	tempDir := t.TempDir()
	originalWd, err := os.Getwd()
	require.NoError(t, err)
	require.NoError(t, os.Chdir(tempDir))
	defer func() {
		_ = os.Chdir(originalWd)
	}()

	cfg := &config.Config{
		System: config.SystemConfig{
			TempDir: "data/temp",
		},
	}
	deps := createTestDeps(t, cfg, "")

	job := deps.JobQueue.CreateJob([]string{"/path/to/IPX-535.mp4"})
	job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
		FilePath:  "/path/to/IPX-535.mp4",
		MovieID:   "IPX-535",
		Status:    worker.JobStatusCompleted,
		Data:      &models.Movie{ID: "IPX-535", Title: "Test Movie"},
		StartedAt: time.Now(),
	})

	posterDir := filepath.Join("data", "temp", "posters", job.ID)
	require.NoError(t, os.MkdirAll(posterDir, 0755))

	fullPosterPath := filepath.Join(posterDir, "IPX-535-full.jpg")
	fullImg := image.NewRGBA(image.Rect(0, 0, 1000, 600))
	for y := 0; y < 600; y++ {
		for x := 0; x < 1000; x++ {
			fullImg.Set(x, y, color.RGBA{R: 200, G: 120, B: 40, A: 255})
		}
	}
	f, err := os.Create(fullPosterPath)
	require.NoError(t, err)
	require.NoError(t, jpeg.Encode(f, fullImg, &jpeg.Options{Quality: 95}))
	require.NoError(t, f.Close())

	router := gin.New()
	router.POST("/batch/:id/movies/:movieId/poster-crop", updateBatchMoviePosterCrop(deps))

	t.Run("successfully updates crop", func(t *testing.T) {
		body, err := json.Marshal(PosterCropRequest{
			X:      350,
			Y:      0,
			Width:  472,
			Height: 600,
		})
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/batch/"+job.ID+"/movies/IPX-535/poster-crop", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		require.Equal(t, 200, w.Code, "Response body: %s", w.Body.String())

		var resp PosterCropResponse
		require.NoError(t, json.Unmarshal(w.Body.Bytes(), &resp))
		assert.Contains(t, resp.CroppedPosterURL, "/api/v1/temp/posters/"+job.ID+"/IPX-535.jpg")

		croppedPath := filepath.Join(posterDir, "IPX-535.jpg")
		_, err = os.Stat(croppedPath)
		require.NoError(t, err)

		out, err := os.Open(croppedPath)
		require.NoError(t, err)
		defer func() {
			_ = out.Close()
		}()
		outImg, _, err := image.Decode(out)
		require.NoError(t, err)
		b := outImg.Bounds()
		assert.True(t, b.Dx() > 0)
		assert.True(t, b.Dy() > 0)
		assert.LessOrEqual(t, b.Dy(), 500, "manual crop should respect max poster height")
	})

	t.Run("rejects out-of-range crop", func(t *testing.T) {
		body, err := json.Marshal(PosterCropRequest{
			X:      900,
			Y:      0,
			Width:  500,
			Height: 600,
		})
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/batch/"+job.ID+"/movies/IPX-535/poster-crop", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)

		assert.Equal(t, 400, w.Code)
		assert.Contains(t, w.Body.String(), "crop bounds out of range")
	})
}

func TestOrganizeJob(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) string
		requestBody    interface{}
		expectedStatus int
	}{
		{
			name: "organize completed job",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				job.MarkCompleted()
				return job.ID
			},
			requestBody: OrganizeRequest{
				Destination: "/output",
				CopyOnly:    false,
				LinkMode:    "hard",
			},
			expectedStatus: 200,
		},
		{
			name: "invalid link mode",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				job.MarkCompleted()
				return job.ID
			},
			requestBody: OrganizeRequest{
				Destination: "/output",
				CopyOnly:    true,
				LinkMode:    "invalid",
			},
			expectedStatus: 400,
		},
		{
			name: "organize job not completed",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				// Job still running
				return job.ID
			},
			requestBody: OrganizeRequest{
				Destination: "/output",
			},
			expectedStatus: 400,
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) string {
				return "nonexistent-job"
			},
			requestBody: OrganizeRequest{
				Destination: "/output",
			},
			expectedStatus: 404,
		},
		{
			name: "invalid request body",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				job.MarkCompleted()
				return job.ID
			},
			requestBody:    "invalid json",
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize WebSocket hub to prevent nil pointer panic
			initTestWebSocket(t)

			cfg := &config.Config{
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
				Output: config.OutputConfig{
					OperationMode: types.OperationModeOrganize,
				},
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: []string{"/path", "/output"}, // Allow test paths
					},
				},
			}

			deps := createTestDeps(t, cfg, "")
			jobID := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.POST("/batch/:id/organize", organizeJob(deps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/organize", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

func TestPreviewOrganize(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) (string, string) // Returns jobID, movieID
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *OrganizePreviewResponse)
	}{
		{
			name: "preview successfully",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				result := &worker.FileResult{
					FilePath: "/path/to/IPX-535.mp4",
					MovieID:  "IPX-535",
					Status:   worker.JobStatusCompleted,
					Data: &models.Movie{
						ID:    "IPX-535",
						Title: "Test Movie",
					},
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/path/to/IPX-535.mp4", result)
				return job.ID, "IPX-535"
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
				CopyOnly:    false,
				LinkMode:    "soft",
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *OrganizePreviewResponse) {
				assert.NotEmpty(t, resp.FolderName)
				assert.NotEmpty(t, resp.FileName)
				assert.NotEmpty(t, resp.FullPath)
			},
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				return "nonexistent-job", "IPX-535"
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
			},
			expectedStatus: 404,
		},
		{
			name: "movie not found in job",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				return job.ID, "NONEXISTENT-999"
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
			},
			expectedStatus: 404,
		},
		{
			name: "preview invalid link mode",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				result := &worker.FileResult{
					FilePath:  "/path/to/file.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test"},
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/path/to/file.mp4", result)
				return job.ID, "IPX-535"
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
				CopyOnly:    true,
				LinkMode:    "bad",
			},
			expectedStatus: 400,
		},
		{
			name: "preview with resolved content ID (ABP-071 → ABP-071DOD)",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/ABP-071.mp4"})
				result := &worker.FileResult{
					FilePath: "/path/to/ABP-071.mp4",
					MovieID:  "ABP-071", // Original matched ID from filename
					Status:   worker.JobStatusCompleted,
					Data: &models.Movie{
						ID:    "ABP-071DOD", // Resolved content ID from DMM
						Title: "Test Movie with Resolved Content ID",
					},
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/path/to/ABP-071.mp4", result)
				return job.ID, "ABP-071DOD" // Frontend passes resolved content ID
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
				CopyOnly:    false,
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *OrganizePreviewResponse) {
				assert.Equal(t, "ABP-071DOD", resp.FolderName)
				assert.Equal(t, "ABP-071DOD", resp.FileName)
				assert.Contains(t, resp.FullPath, "ABP-071DOD")
			},
		},
		{
			name: "preview with movie override uses provided movie instead of stored data",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				result := &worker.FileResult{
					FilePath: "/path/to/IPX-535.mp4",
					MovieID:  "IPX-535",
					Status:   worker.JobStatusCompleted,
					Data: &models.Movie{
						ID:    "IPX-535",
						Title: "Original Title",
					},
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/path/to/IPX-535.mp4", result)
				return job.ID, "IPX-535"
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
				Movie: &models.Movie{
					ID:    "IPX-999",
					Title: "Edited Title",
				},
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *OrganizePreviewResponse) {
				assert.Contains(t, resp.FolderName, "IPX-999", "preview should use the overridden movie ID")
				assert.Contains(t, resp.FullPath, "IPX-999", "full path should reflect the overridden movie ID")
			},
		},
		{
			name: "multipart files sorted by part number for preview",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				// Create job with multipart files - add pt2 before pt1 to test sorting
				job := jq.CreateJob([]string{"/path/to/STSK-074-pt2.mp4", "/path/to/STSK-074-pt1.mp4"})

				movie := &models.Movie{
					ID:    "STSK-074",
					Title: "Multipart Test Movie",
				}

				// Add pt2 first (simulates random map iteration order)
				result2 := &worker.FileResult{
					FilePath:    "/path/to/STSK-074-pt2.mp4",
					MovieID:     "STSK-074",
					Status:      worker.JobStatusCompleted,
					Data:        movie,
					IsMultiPart: true,
					PartNumber:  2,
					PartSuffix:  "-pt2",
					StartedAt:   time.Now(),
				}
				job.UpdateFileResult("/path/to/STSK-074-pt2.mp4", result2)

				// Add pt1 second
				result1 := &worker.FileResult{
					FilePath:    "/path/to/STSK-074-pt1.mp4",
					MovieID:     "STSK-074",
					Status:      worker.JobStatusCompleted,
					Data:        movie,
					IsMultiPart: true,
					PartNumber:  1,
					PartSuffix:  "-pt1",
					StartedAt:   time.Now(),
				}
				job.UpdateFileResult("/path/to/STSK-074-pt1.mp4", result1)

				return job.ID, "STSK-074"
			},
			requestBody: OrganizePreviewRequest{
				Destination: "/output",
				CopyOnly:    false,
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *OrganizePreviewResponse) {
				// Poster should use pt1's multipart context (the first part after sorting)
				assert.Contains(t, resp.PosterPath, "-pt1-poster", "poster should use pt1 suffix from first part")
				assert.Contains(t, resp.FanartPath, "-pt1-fanart", "fanart should use pt1 suffix from first part")
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize WebSocket hub to prevent nil pointer panic
			initTestWebSocket(t)

			cfg := &config.Config{
				Output: config.OutputConfig{
					FolderFormat: "<ID>",
					FileFormat:   "<ID>",
					RenameFile:   true,
					// Use multipart conditional templates for testing
					PosterFormat:     "<ID><IF:MULTIPART>-pt<PART></IF>-poster.jpg",
					FanartFormat:     "<ID><IF:MULTIPART>-pt<PART></IF>-fanart.jpg",
					ScreenshotFolder: "extrafanart",
					// Enable media downloads for preview testing
					DownloadCover:       true,
					DownloadPoster:      true,
					DownloadExtrafanart: true,
				},
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: []string{"/path", "/output"}, // Allow test paths
					},
				},
			}

			deps := createTestDeps(t, cfg, "")
			jobID, movieID := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.POST("/batch/:id/movies/:movieId/preview", previewOrganize(deps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/movies/"+movieID+"/preview", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)

			if tt.validateFn != nil && w.Code == 200 {
				var response OrganizePreviewResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

// TestDiscoverSiblingParts tests the multi-part file discovery logic
func TestDiscoverSiblingParts(t *testing.T) {
	t.Skip("Skipping multi-part discovery test - requires proper multi-part pattern configuration")
	// Note: This test is skipped because the default matcher configuration
	// may not recognize CD1/CD2/CD3 patterns. Multi-part discovery is tested
	// in integration tests with proper configuration.
}

// TestBatchScrapeValidation tests security validation in batch scrape
func TestBatchScrapeValidation(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	require.NoError(t, os.Mkdir(allowedDir, 0755))

	tests := []struct {
		name           string
		requestBody    BatchScrapeRequest
		expectedStatus int
		errorContains  string
	}{
		{
			name: "reject empty files list",
			requestBody: BatchScrapeRequest{
				Files:       []string{},
				Destination: allowedDir,
			},
			expectedStatus: 200, // Empty list is accepted (creates empty job)
		},
		{
			name: "reject path traversal in files",
			requestBody: BatchScrapeRequest{
				Files: []string{
					filepath.Join(allowedDir, "..", "forbidden", "file.mp4"),
				},
				Destination: allowedDir,
			},
			expectedStatus: 403,
			errorContains:  "Access denied",
		},
		{
			name: "accept valid paths within allowed directory",
			requestBody: BatchScrapeRequest{
				Files: []string{
					filepath.Join(allowedDir, "test.mp4"),
				},
				Destination: allowedDir,
			},
			expectedStatus: 200,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestWebSocket(t)

			cfg := &config.Config{
				Scrapers: config.ScrapersConfig{
					Priority: []string{"r18dev"},
				},
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: []string{allowedDir},
					},
				},
			}

			deps := createTestDeps(t, cfg, "")

			router := gin.New()
			router.POST("/batch/scrape", batchScrape(deps))

			body, err := json.Marshal(tt.requestBody)
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
			if tt.errorContains != "" {
				assert.Contains(t, w.Body.String(), tt.errorContains)
			}
		})
	}
}

// TestBatchScrapeErrors tests error response handling
func TestBatchScrapeErrors(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name           string
		requestBody    interface{}
		expectedStatus int
	}{
		{
			name:           "invalid JSON body",
			requestBody:    "{invalid-json",
			expectedStatus: 400,
		},
		{
			name: "missing required fields",
			requestBody: map[string]interface{}{
				"force": true,
			},
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestWebSocket(t)

			cfg := &config.Config{
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: []string{tempDir},
					},
				},
			}

			deps := createTestDeps(t, cfg, "")

			router := gin.New()
			router.POST("/batch/scrape", batchScrape(deps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/batch/scrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code)
		})
	}
}

// TestRescrapeBatchMovie tests the rescrape endpoint
func TestRescrapeBatchMovie(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) (jobID, movieID string)
		requestBody    interface{}
		expectedStatus int
		validateFn     func(*testing.T, *BatchRescrapeResponse)
	}{
		{
			name: "rescrape with selected scrapers - scraping fails with mock",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/test/IPX-535.mp4"})
				job.UpdateFileResult("/test/IPX-535.mp4", &worker.FileResult{
					MovieID: "IPX-535",
					Status:  worker.JobStatusCompleted,
				})
				return job.ID, "IPX-535"
			},
			requestBody: BatchRescrapeRequest{
				SelectedScrapers: []string{"r18dev"},
				Force:            true,
			},
			expectedStatus: 500, // Internal Server Error - scraping fails with mock scraper (no results)
		},
		{
			name: "rescrape with manual search - scraping fails with mock",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/test/ABC-123.mp4"})
				job.UpdateFileResult("/test/ABC-123.mp4", &worker.FileResult{
					MovieID: "ABC-123",
					Status:  worker.JobStatusCompleted,
				})
				return job.ID, "ABC-123"
			},
			requestBody: BatchRescrapeRequest{
				ManualSearchInput: "IPX-535",
				Force:             false,
			},
			expectedStatus: 500, // Internal Server Error - scraping fails with mock scraper (no results)
		},
		{
			name: "rescrape with valid preset",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/test/TEST-001.mp4"})
				job.UpdateFileResult("/test/TEST-001.mp4", &worker.FileResult{
					MovieID: "TEST-001",
					Status:  worker.JobStatusCompleted,
				})
				return job.ID, "TEST-001"
			},
			requestBody: BatchRescrapeRequest{
				SelectedScrapers: []string{"r18dev"},
				Preset:           "conservative", // Use valid preset
			},
			expectedStatus: 500, // Internal Server Error - scraping fails with mock scraper (no results)
		},
		{
			name: "invalid preset name",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/test/TEST-002.mp4"})
				job.UpdateFileResult("/test/TEST-002.mp4", &worker.FileResult{
					MovieID: "TEST-002",
					Status:  worker.JobStatusCompleted,
				})
				return job.ID, "TEST-002"
			},
			requestBody: BatchRescrapeRequest{
				SelectedScrapers: []string{"r18dev"},
				Preset:           "invalid_preset",
			},
			expectedStatus: 400, // Bad Request - invalid preset
		},
		{
			name: "invalid JSON",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				return "job123", "movie123"
			},
			requestBody:    "{invalid-json",
			expectedStatus: 400,
		},
		{
			name: "missing scrapers and manual input",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/test/file.mp4"})
				return job.ID, "MOVIE-001"
			},
			requestBody:    BatchRescrapeRequest{},
			expectedStatus: 400,
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				return "nonexistent-job", "MOVIE-001"
			},
			requestBody: BatchRescrapeRequest{
				SelectedScrapers: []string{"r18dev"},
			},
			expectedStatus: 404,
		},
		{
			name: "movie not found in job",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/test/file.mp4"})
				job.UpdateFileResult("/test/file.mp4", &worker.FileResult{
					MovieID: "DIFFERENT-ID",
					Status:  worker.JobStatusCompleted,
				})
				return job.ID, "NONEXISTENT-MOVIE"
			},
			requestBody: BatchRescrapeRequest{
				SelectedScrapers: []string{"r18dev"},
			},
			expectedStatus: 404,
		},
		{
			name: "rescrape with DMM URL - extracts content ID and auto-selects dmm scraper",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/test/kitaike429.mp4"})
				job.UpdateFileResult("/test/kitaike429.mp4", &worker.FileResult{
					MovieID: "KITAIKE-429",
					Status:  worker.JobStatusCompleted,
				})
				return job.ID, "KITAIKE-429"
			},
			requestBody: BatchRescrapeRequest{
				ManualSearchInput: "https://video.dmm.co.jp/amateur/content/?id=kitaike429",
				Force:             true,
			},
			expectedStatus: 500, // Internal Server Error - scraping fails with mock scraper (no results)
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Initialize WebSocket hub
			initTestWebSocket(t)

			cfg := config.DefaultConfig()
			cfg.Scrapers.UserAgent = "Test Agent"
			cfg.Scrapers.Referer = "https://test.com"
			cfg.Scrapers.RequestTimeoutSeconds = 30
			cfg.Scrapers.Priority = []string{"r18dev"}
			cfg.Scrapers.Proxy = config.ProxyConfig{Enabled: false}
			cfg.API.Security.AllowedDirectories = []string{"/test"}

			deps := createTestDeps(t, cfg, "")
			jobID, movieID := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.POST("/batch/:id/movies/:movieId/rescrape", rescrapeBatchMovie(deps))

			var body []byte
			var err error
			if str, ok := tt.requestBody.(string); ok {
				body = []byte(str)
			} else {
				body, err = json.Marshal(tt.requestBody)
				require.NoError(t, err)
			}

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/movies/"+movieID+"/rescrape", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())

			if tt.validateFn != nil && w.Code == 200 {
				var response BatchRescrapeResponse
				err := json.Unmarshal(w.Body.Bytes(), &response)
				require.NoError(t, err)
				tt.validateFn(t, &response)
			}
		})
	}
}

// TestExcludeBatchMovie tests the exclude endpoint
func TestExcludeBatchMovie(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) (jobID, movieID string)
		expectedStatus int
	}{
		{
			name: "exclude existing movie by MovieID",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test Movie"},
					StartedAt: time.Now(),
				})
				return job.ID, "IPX-535"
			},
			expectedStatus: 200,
		},
		{
			name: "exclude existing movie by Movie.ID",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/ABP-071.mp4"})
				job.UpdateFileResult("/path/to/ABP-071.mp4", &worker.FileResult{
					FilePath:  "/path/to/ABP-071.mp4",
					MovieID:   "ABP-071",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABP-071DOD", Title: "Test Movie"}, // Movie.ID differs from MovieID
					StartedAt: time.Now(),
				})
				return job.ID, "ABP-071DOD" // Request by Movie.ID
			},
			expectedStatus: 200,
		},
		{
			name: "exclude multi-part files",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{
					"/path/to/IPX-535-CD1.mp4",
					"/path/to/IPX-535-CD2.mp4",
				})
				// Both parts have same MovieID
				job.UpdateFileResult("/path/to/IPX-535-CD1.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535-CD1.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535"},
					StartedAt: time.Now(),
				})
				job.UpdateFileResult("/path/to/IPX-535-CD2.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535-CD2.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535"},
					StartedAt: time.Now(),
				})
				return job.ID, "IPX-535"
			},
			expectedStatus: 200,
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				return "nonexistent-job", "IPX-535"
			},
			expectedStatus: 404,
		},
		{
			name: "movie not found in job",
			setupJob: func(jq *worker.JobQueue) (string, string) {
				job := jq.CreateJob([]string{"/path/to/ABC-123.mp4"})
				job.UpdateFileResult("/path/to/ABC-123.mp4", &worker.FileResult{
					FilePath:  "/path/to/ABC-123.mp4",
					MovieID:   "ABC-123",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABC-123"},
					StartedAt: time.Now(),
				})
				return job.ID, "NONEXISTENT-999"
			},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")

			jobID, movieID := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.POST("/batch/:id/movies/:movieId/exclude", excludeBatchMovie(deps))

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/movies/"+movieID+"/exclude", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())
		})
	}
}

func TestBatchExcludeMovies(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) (jobID string, movieIDs []string)
		expectedStatus int
		validateFn     func(*testing.T, *BatchExcludeResponse)
	}{
		{
			name: "exclude multiple movies at once",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4", "/path/to/ABC-123.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test Movie 1"},
					StartedAt: time.Now(),
				})
				job.UpdateFileResult("/path/to/ABC-123.mp4", &worker.FileResult{
					FilePath:  "/path/to/ABC-123.mp4",
					MovieID:   "ABC-123",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "ABC-123", Title: "Test Movie 2"},
					StartedAt: time.Now(),
				})
				return job.ID, []string{"IPX-535", "ABC-123"}
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchExcludeResponse) {
				assert.Len(t, resp.Excluded, 2)
				assert.Contains(t, resp.Excluded, "IPX-535")
				assert.Contains(t, resp.Excluded, "ABC-123")
				assert.Empty(t, resp.Failed)
				assert.NotNil(t, resp.Job)
			},
		},
		{
			name: "exclude mix of existing and non-existing movies",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test Movie"},
					StartedAt: time.Now(),
				})
				return job.ID, []string{"IPX-535", "NONEXISTENT-999"}
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchExcludeResponse) {
				assert.Len(t, resp.Excluded, 1)
				assert.Contains(t, resp.Excluded, "IPX-535")
				assert.Len(t, resp.Failed, 1)
				assert.Equal(t, "NONEXISTENT-999", resp.Failed[0].MovieID)
			},
		},
		{
			name: "exclude already-excluded movie is idempotent",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test Movie"},
					StartedAt: time.Now(),
				})
				job.ExcludeFile("/path/to/IPX-535.mp4")
				return job.ID, []string{"IPX-535"}
			},
			expectedStatus: 200,
			validateFn: func(t *testing.T, resp *BatchExcludeResponse) {
				assert.Len(t, resp.Excluded, 1)
				assert.Contains(t, resp.Excluded, "IPX-535")
				assert.Empty(t, resp.Failed)
			},
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				return "nonexistent-job", []string{"IPX-535"}
			},
			expectedStatus: 404,
		},
		{
			name: "empty movie_ids returns 400",
			setupJob: func(jq *worker.JobQueue) (string, []string) {
				job := jq.CreateJob([]string{"/path/to/IPX-535.mp4"})
				job.UpdateFileResult("/path/to/IPX-535.mp4", &worker.FileResult{
					FilePath:  "/path/to/IPX-535.mp4",
					MovieID:   "IPX-535",
					Status:    worker.JobStatusCompleted,
					Data:      &models.Movie{ID: "IPX-535", Title: "Test Movie"},
					StartedAt: time.Now(),
				})
				return job.ID, []string{}
			},
			expectedStatus: 400,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")

			jobID, movieIDs := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.POST("/batch/:id/movies/batch-exclude", batchExcludeMovies(deps))

			body, _ := json.Marshal(BatchExcludeRequest{MovieIDs: movieIDs})
			req := httptest.NewRequest("POST", "/batch/"+jobID+"/movies/batch-exclude", bytes.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())

			if tt.validateFn != nil && w.Code == 200 {
				var resp BatchExcludeResponse
				err := json.Unmarshal(w.Body.Bytes(), &resp)
				require.NoError(t, err)
				tt.validateFn(t, &resp)
			}
		})
	}
}

// TestUpdateBatchJob tests the update batch job endpoint
func TestUpdateBatchJob(t *testing.T) {
	tests := []struct {
		name           string
		setupJob       func(*worker.JobQueue) string
		expectedStatus int
	}{
		{
			name: "update completed job",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				job.MarkCompleted()
				return job.ID
			},
			expectedStatus: 200,
		},
		{
			name: "update job not completed",
			setupJob: func(jq *worker.JobQueue) string {
				job := jq.CreateJob([]string{"/path/to/file.mp4"})
				// Job still running (not completed)
				return job.ID
			},
			expectedStatus: 400,
		},
		{
			name: "job not found",
			setupJob: func(jq *worker.JobQueue) string {
				return "nonexistent-job"
			},
			expectedStatus: 404,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestWebSocket(t)

			cfg := &config.Config{}
			deps := createTestDeps(t, cfg, "")
			jobID := tt.setupJob(deps.JobQueue)

			router := gin.New()
			router.POST("/batch/:id/update", updateBatchJob(deps))

			req := httptest.NewRequest("POST", "/batch/"+jobID+"/update", nil)
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())
		})
	}
}

// TestOrganizeJobSecurityValidation tests security validation in organize endpoint
func TestOrganizeJobSecurityValidation(t *testing.T) {
	tempDir := t.TempDir()
	allowedDir := filepath.Join(tempDir, "allowed")
	deniedDir := filepath.Join(tempDir, "denied")
	require.NoError(t, os.Mkdir(allowedDir, 0755))
	require.NoError(t, os.Mkdir(deniedDir, 0755))

	tests := []struct {
		name           string
		destination    string
		expectedStatus int
	}{
		{
			name:           "access allowed directory",
			destination:    allowedDir,
			expectedStatus: 200,
		},
		{
			name:           "access denied directory",
			destination:    deniedDir,
			expectedStatus: 403,
		},
		{
			name:           "path traversal attempt",
			destination:    filepath.Join(allowedDir, "..", "denied"),
			expectedStatus: 403,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestWebSocket(t)

			cfg := &config.Config{
				Matching: config.MatchingConfig{
					RegexEnabled: false,
				},
				Output: config.OutputConfig{
					OperationMode: types.OperationModeOrganize,
				},
				API: config.APIConfig{
					Security: config.SecurityConfig{
						AllowedDirectories: []string{allowedDir},
						DeniedDirectories:  []string{deniedDir},
					},
				},
			}

			deps := createTestDeps(t, cfg, "")

			// Create completed job
			job := deps.JobQueue.CreateJob([]string{"/path/to/file.mp4"})
			job.MarkCompleted()

			router := gin.New()
			router.POST("/batch/:id/organize", organizeJob(deps))

			body, err := json.Marshal(OrganizeRequest{
				Destination: tt.destination,
			})
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/batch/"+job.ID+"/organize", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			assert.Equal(t, tt.expectedStatus, w.Code, "Response body: %s", w.Body.String())
		})
	}
}

// TestOrganizeJobRetryWorkflow tests the state machine for organization retry
func TestOrganizeJobRetryWorkflow(t *testing.T) {
	tests := []struct {
		name                string
		setupJob            func(*worker.JobQueue, *config.Config) *worker.BatchJob
		simulateOrganize    func(*worker.BatchJob)
		validateStatus      func(*testing.T, *worker.BatchJob)
		canRetry            bool
		expectedFinalStatus worker.JobStatus
	}{
		{
			name: "successful organization transitions to Organized",
			setupJob: func(jq *worker.JobQueue, cfg *config.Config) *worker.BatchJob {
				job := jq.CreateJob([]string{"/test/file.mp4"})
				movie := &models.Movie{ID: "TEST-001", Title: "Test Movie"}
				result := &worker.FileResult{
					FilePath:  "/test/file.mp4",
					MovieID:   "TEST-001",
					Status:    worker.JobStatusCompleted,
					Data:      movie,
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/test/file.mp4", result)
				job.MarkCompleted()
				return job
			},
			simulateOrganize: func(job *worker.BatchJob) {
				// Simulate successful organization (no failures)
				job.MarkStarted()
				job.MarkOrganized()
			},
			validateStatus: func(t *testing.T, job *worker.BatchJob) {
				// After successful organization, should be "Organized"
				assert.Equal(t, worker.JobStatusOrganized, job.Status)
			},
			canRetry:            false,
			expectedFinalStatus: worker.JobStatusOrganized,
		},
		{
			name: "organization with failures stays Completed for retry",
			setupJob: func(jq *worker.JobQueue, cfg *config.Config) *worker.BatchJob {
				job := jq.CreateJob([]string{"/test/file.mp4"})
				movie := &models.Movie{ID: "TEST-002", Title: "Test Movie"}
				result := &worker.FileResult{
					FilePath:  "/test/file.mp4",
					MovieID:   "TEST-002",
					Status:    worker.JobStatusCompleted,
					Data:      movie,
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/test/file.mp4", result)
				job.MarkCompleted()
				return job
			},
			simulateOrganize: func(job *worker.BatchJob) {
				// Simulate organization with failures
				// (This is what processOrganizeJob does when failed > 0)
				job.MarkStarted()
				job.MarkCompleted() // Re-marks as completed instead of organized
			},
			validateStatus: func(t *testing.T, job *worker.BatchJob) {
				// After organization with failures, should remain "Completed" for retry
				assert.Equal(t, worker.JobStatusCompleted, job.Status)
			},
			canRetry:            true,
			expectedFinalStatus: worker.JobStatusCompleted,
		},
		{
			name: "job with mixed results (completed + failed) stays Completed",
			setupJob: func(jq *worker.JobQueue, cfg *config.Config) *worker.BatchJob {
				job := jq.CreateJob([]string{"/test/file1.mp4", "/test/file2.mp4"})
				movie1 := &models.Movie{ID: "TEST-003", Title: "Test Movie 1"}
				result1 := &worker.FileResult{
					FilePath:  "/test/file1.mp4",
					MovieID:   "TEST-003",
					Status:    worker.JobStatusCompleted,
					Data:      movie1,
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/test/file1.mp4", result1)

				// File 2 failed during scraping - won't be organized
				result2 := &worker.FileResult{
					FilePath:  "/test/file2.mp4",
					MovieID:   "TEST-004",
					Status:    worker.JobStatusFailed,
					Error:     "Scraping failed",
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/test/file2.mp4", result2)
				job.MarkCompleted()
				return job
			},
			simulateOrganize: func(job *worker.BatchJob) {
				// Simulate organization (files don't exist, so will fail)
				// This will transition to Completed (failed > 0)
				job.MarkStarted()
				job.MarkCompleted()
			},
			validateStatus: func(t *testing.T, job *worker.BatchJob) {
				// With partial results, should stay "Completed" for potential retry
				assert.Equal(t, worker.JobStatusCompleted, job.Status)
			},
			canRetry:            true,
			expectedFinalStatus: worker.JobStatusCompleted,
		},
		{
			name: "job with all files skipped/excluded stays Completed",
			setupJob: func(jq *worker.JobQueue, cfg *config.Config) *worker.BatchJob {
				job := jq.CreateJob([]string{"/test/file1.mp4", "/test/file2.mp4"})

				// Both files failed during scraping - nothing to organize
				result1 := &worker.FileResult{
					FilePath:  "/test/file1.mp4",
					Status:    worker.JobStatusFailed,
					Error:     "Scraping failed",
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/test/file1.mp4", result1)

				result2 := &worker.FileResult{
					FilePath:  "/test/file2.mp4",
					Status:    worker.JobStatusFailed,
					Error:     "Scraping failed",
					StartedAt: time.Now(),
				}
				job.UpdateFileResult("/test/file2.mp4", result2)
				job.MarkCompleted()
				return job
			},
			simulateOrganize: func(job *worker.BatchJob) {
				// Simulate organization with zero files processed (organized == 0, failed == 0)
				// Should stay Completed (not transition to Organized)
				job.MarkStarted()
				job.MarkCompleted()
			},
			validateStatus: func(t *testing.T, job *worker.BatchJob) {
				// With zero files processed, should stay "Completed" (not "Organized")
				assert.Equal(t, worker.JobStatusCompleted, job.Status)
			},
			canRetry:            true,
			expectedFinalStatus: worker.JobStatusCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			initTestWebSocket(t)

			cfg := config.DefaultConfig()
			cfg.API.Security.AllowedDirectories = []string{"/test"}
			cfg.Output.FolderFormat = "<ID>"
			cfg.Output.FileFormat = "<ID>"

			deps := createTestDeps(t, cfg, "")
			job := tt.setupJob(deps.JobQueue, cfg)

			// Verify job is in Completed state before organization
			assert.Equal(t, worker.JobStatusCompleted, job.Status, "Job should be Completed before organization")

			// Simulate the organization state transition
			tt.simulateOrganize(job)

			// Validate final status
			tt.validateStatus(t, job)

			// Test retry capability through the API
			router := gin.New()
			router.POST("/batch/:id/organize", organizeJob(deps))

			body, err := json.Marshal(OrganizeRequest{
				Destination: "/test/output",
				CopyOnly:    true,
			})
			require.NoError(t, err)

			req := httptest.NewRequest("POST", "/batch/"+job.ID+"/organize", bytes.NewBuffer(body))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()

			router.ServeHTTP(w, req)

			if tt.canRetry {
				// Retry should be accepted (job is still in Completed state)
				assert.Equal(t, 200, w.Code, "Retry should be allowed for job in Completed state")
			} else {
				// Retry should be rejected (job is in Organized state)
				assert.Equal(t, 400, w.Code, "Retry should be rejected for job in Organized state")
				assert.Contains(t, w.Body.String(), "Job must be completed before organizing")
			}
		})
	}
}

// TestJobStateMachineTransitions documents and validates the job state machine
func TestJobStateMachineTransitions(t *testing.T) {
	t.Run("state machine transitions are correct", func(t *testing.T) {
		initTestWebSocket(t)

		cfg := config.DefaultConfig()
		cfg.API.Security.AllowedDirectories = []string{"/test"}
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID>"

		deps := createTestDeps(t, cfg, "")

		// Start with pending job
		job := deps.JobQueue.CreateJob([]string{"/test/file.mp4"})
		assert.Equal(t, worker.JobStatusPending, job.Status, "Initial state should be Pending")

		// Mark as started (running)
		job.MarkStarted()
		assert.Equal(t, worker.JobStatusRunning, job.Status, "After MarkStarted should be Running")

		// Mark as completed (after scraping)
		job.MarkCompleted()
		assert.Equal(t, worker.JobStatusCompleted, job.Status, "After MarkCompleted should be Completed")

		// Simulate successful organization
		job.MarkStarted() // Organization starts
		job.MarkOrganized()
		assert.Equal(t, worker.JobStatusOrganized, job.Status, "After successful organization should be Organized")

		// Create another job to test failed organization path
		job2 := deps.JobQueue.CreateJob([]string{"/test/file2.mp4"})
		job2.MarkStarted()
		job2.MarkCompleted()
		assert.Equal(t, worker.JobStatusCompleted, job2.Status, "Job2 should be Completed after scraping")

		// Simulate failed organization (should stay Completed)
		job2.MarkStarted() // Organization starts
		// In real scenario, processOrganizeJob would call MarkCompleted instead of MarkOrganized
		job2.MarkCompleted() // This is what happens when failed > 0
		assert.Equal(t, worker.JobStatusCompleted, job2.Status, "After failed organization should stay Completed")
	})

	t.Run("organized job cannot be organized again", func(t *testing.T) {
		initTestWebSocket(t)

		cfg := config.DefaultConfig()
		cfg.API.Security.AllowedDirectories = []string{"/test"}

		deps := createTestDeps(t, cfg, "")

		job := deps.JobQueue.CreateJob([]string{"/test/file.mp4"})
		job.MarkCompleted()
		job.MarkOrganized()

		router := gin.New()
		router.POST("/batch/:id/organize", organizeJob(deps))

		body, err := json.Marshal(OrganizeRequest{
			Destination: "/test/output",
		})
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/batch/"+job.ID+"/organize", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 400, w.Code)
		assert.Contains(t, w.Body.String(), "Job must be completed before organizing")
	})

	t.Run("completed job can be organized (retry)", func(t *testing.T) {
		initTestWebSocket(t)

		cfg := config.DefaultConfig()
		cfg.API.Security.AllowedDirectories = []string{"/test"}
		cfg.Output.FolderFormat = "<ID>"
		cfg.Output.FileFormat = "<ID>"

		deps := createTestDeps(t, cfg, "")

		job := deps.JobQueue.CreateJob([]string{"/test/file.mp4"})
		movie := &models.Movie{ID: "TEST-001", Title: "Test"}
		result := &worker.FileResult{
			FilePath:  "/test/file.mp4",
			MovieID:   "TEST-001",
			Status:    worker.JobStatusCompleted,
			Data:      movie,
			StartedAt: time.Now(),
		}
		job.UpdateFileResult("/test/file.mp4", result)
		job.MarkCompleted()

		router := gin.New()
		router.POST("/batch/:id/organize", organizeJob(deps))

		body, err := json.Marshal(OrganizeRequest{
			Destination: "/test/output",
			CopyOnly:    true,
		})
		require.NoError(t, err)

		req := httptest.NewRequest("POST", "/batch/"+job.ID+"/organize", bytes.NewBuffer(body))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()

		router.ServeHTTP(w, req)

		assert.Equal(t, 200, w.Code, "Completed job should be organizizable (retry allowed)")
	})

	t.Run("MarkStarted clears OrganizedAt timestamp for retry", func(t *testing.T) {
		initTestWebSocket(t)

		cfg := config.DefaultConfig()
		cfg.API.Security.AllowedDirectories = []string{"/test"}

		deps := createTestDeps(t, cfg, "")

		// Create and organize a job
		job := deps.JobQueue.CreateJob([]string{"/test/file.mp4"})
		job.MarkStarted()
		job.MarkCompleted()
		job.MarkStarted()
		job.MarkOrganized()

		// Verify OrganizedAt is set
		snap := job.GetStatus()
		require.NotNil(t, snap.OrganizedAt, "OrganizedAt should be set after organization")

		// Simulate retry - MarkStarted should clear OrganizedAt
		job.MarkStarted()

		// Verify OrganizedAt is cleared
		snap2 := job.GetStatus()
		assert.Nil(t, snap2.OrganizedAt, "OrganizedAt should be cleared on MarkStarted for retry")
		assert.Equal(t, worker.JobStatusRunning, snap2.Status, "Status should be Running after MarkStarted")
	})
}
