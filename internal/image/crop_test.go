package image

import (
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// createTestImage creates a test image with the given dimensions and color
func createTestImage(t *testing.T, path string, width, height int, col color.Color) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, col)
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("Failed to create test image: %v", err)
	}
	defer file.Close()

	if err := jpeg.Encode(file, img, &jpeg.Options{Quality: 95}); err != nil {
		t.Fatalf("Failed to encode test image: %v", err)
	}
}

// getImageDimensions returns the width and height of an image file
func getImageDimensions(t *testing.T, path string) (int, int) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("Failed to open image: %v", err)
	}
	defer file.Close()

	img, _, err := image.Decode(file)
	if err != nil {
		t.Fatalf("Failed to decode image: %v", err)
	}

	bounds := img.Bounds()
	return bounds.Dx(), bounds.Dy()
}

func TestCropPosterFromCover(t *testing.T) {
	// Create temporary directory for test images
	tempDir := t.TempDir()

	tests := []struct {
		name           string
		coverWidth     int
		coverHeight    int
		expectedAspect string // "landscape", "portrait", or "square"
		checkResize    bool
	}{
		{
			name:           "landscape image (typical JAV cover)",
			coverWidth:     1500,
			coverHeight:    1000,
			expectedAspect: "landscape",
			checkResize:    false, // Won't trigger resize (height = 1000, cropped height ≈ 1000)
		},
		{
			name:           "wide landscape image",
			coverWidth:     2000,
			coverHeight:    1000,
			expectedAspect: "landscape",
			checkResize:    false,
		},
		{
			name:           "square image",
			coverWidth:     1000,
			coverHeight:    1000,
			expectedAspect: "square",
			checkResize:    false,
		},
		{
			name:           "portrait image",
			coverWidth:     600,
			coverHeight:    900,
			expectedAspect: "portrait",
			checkResize:    false,
		},
		{
			name:           "tall landscape triggering resize",
			coverWidth:     1800,
			coverHeight:    1200,
			expectedAspect: "landscape",
			checkResize:    true, // Height = 1200 > MaxPosterHeight (500)
		},
		{
			name:           "small landscape",
			coverWidth:     300,
			coverHeight:    200,
			expectedAspect: "landscape",
			checkResize:    false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Create test cover image
			coverPath := filepath.Join(tempDir, tt.name+"_cover.jpg")
			posterPath := filepath.Join(tempDir, tt.name+"_poster.jpg")

			// Use different colors for different test cases for visual debugging
			testColor := color.RGBA{R: 100, G: 150, B: 200, A: 255}
			createTestImage(t, coverPath, tt.coverWidth, tt.coverHeight, testColor)

			// Perform the crop
			err := CropPosterFromCover(coverPath, posterPath)
			if err != nil {
				t.Fatalf("CropPosterFromCover() failed: %v", err)
			}

			// Verify the poster was created
			if _, err := os.Stat(posterPath); os.IsNotExist(err) {
				t.Fatal("Poster file was not created")
			}

			// Get poster dimensions
			posterWidth, posterHeight := getImageDimensions(t, posterPath)

			// Verify poster dimensions are reasonable
			if posterWidth <= 0 || posterHeight <= 0 {
				t.Errorf("Invalid poster dimensions: %dx%d", posterWidth, posterHeight)
			}

			// Verify resize behavior
			if tt.checkResize {
				if posterHeight > MaxPosterHeight {
					t.Errorf("Poster height %d exceeds MaxPosterHeight %d", posterHeight, MaxPosterHeight)
				}
			}

			// Verify aspect ratio based on original image type
			aspectRatio := float64(tt.coverWidth) / float64(tt.coverHeight)
			posterAspect := float64(posterWidth) / float64(posterHeight)

			if aspectRatio > LandscapeAspectRatioThreshold {
				// Landscape: Right-side crop keeps 47.2% of width, full height
				// Expected cropped aspect = (width * 0.472) / height
				expectedCroppedAspect := (float64(tt.coverWidth) * 0.472) / float64(tt.coverHeight)

				// If resized, aspect ratio should be maintained from cropped dimensions
				// Allow 10% tolerance for rounding and JPEG compression
				if posterAspect < expectedCroppedAspect*0.90 || posterAspect > expectedCroppedAspect*1.10 {
					t.Errorf("Landscape poster aspect ratio %.2f not close to expected cropped aspect %.2f (input: %dx%d, output: %dx%d)",
						posterAspect, expectedCroppedAspect, tt.coverWidth, tt.coverHeight, posterWidth, posterHeight)
				}
			} else {
				// Square/Portrait: Should be 2:3 aspect ratio
				targetAspect := 2.0 / 3.0
				// Allow 5% tolerance for rounding
				if posterAspect < targetAspect*0.95 || posterAspect > targetAspect*1.05 {
					t.Errorf("Square/portrait poster aspect ratio %.2f not close to target 2:3 (%.2f)", posterAspect, targetAspect)
				}
			}
		})
	}
}

func TestCropPosterFromCover_ErrorCases(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name          string
		setupFunc     func() (coverPath, posterPath string)
		expectedError string
	}{
		{
			name: "nonexistent cover file",
			setupFunc: func() (string, string) {
				return filepath.Join(tempDir, "nonexistent.jpg"),
					filepath.Join(tempDir, "poster.jpg")
			},
			expectedError: "failed to open cover image",
		},
		{
			name: "invalid cover image",
			setupFunc: func() (string, string) {
				coverPath := filepath.Join(tempDir, "invalid.jpg")
				// Create a file with invalid image data
				if err := os.WriteFile(coverPath, []byte("not an image"), 0644); err != nil {
					t.Fatalf("Failed to create invalid image file: %v", err)
				}
				return coverPath, filepath.Join(tempDir, "poster.jpg")
			},
			expectedError: "failed to decode cover image",
		},
		{
			name: "invalid output directory",
			setupFunc: func() (string, string) {
				coverPath := filepath.Join(tempDir, "valid_cover.jpg")
				createTestImage(t, coverPath, 800, 600, color.RGBA{R: 100, G: 100, B: 100, A: 255})
				// Use a path that doesn't exist
				return coverPath, filepath.Join(tempDir, "nonexistent_dir", "poster.jpg")
			},
			expectedError: "failed to create poster file",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coverPath, posterPath := tt.setupFunc()

			err := CropPosterFromCover(coverPath, posterPath)

			if err == nil {
				t.Fatal("Expected error, got nil")
			}

			if tt.expectedError != "" {
				if len(err.Error()) < len(tt.expectedError) || err.Error()[:len(tt.expectedError)] != tt.expectedError {
					t.Errorf("Expected error to start with %q, got %q", tt.expectedError, err.Error())
				}
			}
		})
	}
}

func TestCropPosterFromCover_AspectRatioEdgeCases(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name        string
		width       int
		height      int
		description string
	}{
		{
			name:        "exactly at threshold (1.2)",
			width:       1200,
			height:      1000,
			description: "Should be treated as landscape (aspect ratio = 1.2)",
		},
		{
			name:        "just below threshold",
			width:       1190,
			height:      1000,
			description: "Should be treated as square/portrait (aspect ratio < 1.2)",
		},
		{
			name:        "just above threshold",
			width:       1210,
			height:      1000,
			description: "Should be treated as landscape (aspect ratio > 1.2)",
		},
		{
			name:        "very wide landscape",
			width:       3000,
			height:      1000,
			description: "Should use right-side crop",
		},
		{
			name:        "very tall portrait",
			width:       600,
			height:      1800,
			description: "Should use center crop with 2:3 ratio",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coverPath := filepath.Join(tempDir, tt.name+"_cover.jpg")
			posterPath := filepath.Join(tempDir, tt.name+"_poster.jpg")

			createTestImage(t, coverPath, tt.width, tt.height, color.RGBA{R: 150, G: 150, B: 150, A: 255})

			err := CropPosterFromCover(coverPath, posterPath)
			if err != nil {
				t.Fatalf("CropPosterFromCover() failed: %v", err)
			}

			// Verify poster was created and has valid dimensions
			posterWidth, posterHeight := getImageDimensions(t, posterPath)
			if posterWidth <= 0 || posterHeight <= 0 {
				t.Errorf("Invalid poster dimensions: %dx%d", posterWidth, posterHeight)
			}

			t.Logf("%s: Input %dx%d → Output %dx%d (aspect: %.2f → %.2f)",
				tt.description,
				tt.width, tt.height,
				posterWidth, posterHeight,
				float64(tt.width)/float64(tt.height),
				float64(posterWidth)/float64(posterHeight))
		})
	}
}

func TestCropPosterFromCover_ResizeLogic(t *testing.T) {
	tempDir := t.TempDir()

	tests := []struct {
		name            string
		coverWidth      int
		coverHeight     int
		expectResize    bool
		maxPosterHeight int
	}{
		{
			name:            "small image - no resize",
			coverWidth:      400,
			coverHeight:     300,
			expectResize:    false,
			maxPosterHeight: MaxPosterHeight,
		},
		{
			name:            "tall image - should resize",
			coverWidth:      1500,
			coverHeight:     1500,
			expectResize:    true,
			maxPosterHeight: MaxPosterHeight,
		},
		{
			name:            "very tall landscape - should resize",
			coverWidth:      2400,
			coverHeight:     1600,
			expectResize:    true,
			maxPosterHeight: MaxPosterHeight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			coverPath := filepath.Join(tempDir, tt.name+"_cover.jpg")
			posterPath := filepath.Join(tempDir, tt.name+"_poster.jpg")

			createTestImage(t, coverPath, tt.coverWidth, tt.coverHeight, color.RGBA{R: 200, G: 100, B: 100, A: 255})

			err := CropPosterFromCover(coverPath, posterPath)
			if err != nil {
				t.Fatalf("CropPosterFromCover() failed: %v", err)
			}

			posterWidth, posterHeight := getImageDimensions(t, posterPath)

			if tt.expectResize {
				if posterHeight > tt.maxPosterHeight {
					t.Errorf("Expected resize: poster height %d should be <= %d", posterHeight, tt.maxPosterHeight)
				}
			}

			t.Logf("%s: Input %dx%d → Output %dx%d (resized: %v)",
				tt.name,
				tt.coverWidth, tt.coverHeight,
				posterWidth, posterHeight,
				posterHeight <= tt.maxPosterHeight && tt.expectResize)
		})
	}
}

func TestConstants(t *testing.T) {
	if MaxPosterHeight != 500 {
		t.Errorf("MaxPosterHeight = %d, want 500", MaxPosterHeight)
	}

	if LandscapeAspectRatioThreshold != 1.2 {
		t.Errorf("LandscapeAspectRatioThreshold = %.1f, want 1.2", LandscapeAspectRatioThreshold)
	}
}
