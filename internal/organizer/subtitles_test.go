package organizer

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/scanner"
)

func TestSubtitleHandler_FindSubtitles(t *testing.T) {
	cfg := &config.OutputConfig{
		SubtitleExtensions: []string{".srt", ".ass", ".ssa", ".smi", ".vtt"},
	}

	handler := NewSubtitleHandler(cfg)

	// Create temporary directory structure
	tmpDir := t.TempDir()

	// Create video file
	videoPath := filepath.Join(tmpDir, "IPX-535.mp4")
	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatalf("Failed to create video file: %v", err)
	}

	videoFile := scanner.FileInfo{
		Name:      "IPX-535.mp4",
		Path:      videoPath,
		Extension: ".mp4",
		Size:      1000,
	}

	// Create subtitle files
	subtitleFiles := []string{
		"IPX-535.srt",           // Exact match
		"IPX-535.eng.srt",       // English language code
		"IPX-535.japanese.srt",  // Full language name
		"IPX-535.chi.ass",       // Chinese with different extension
		"ABCD-123.srt",          // Different video - should not match
		"not-a-subtitle.txt",    // Wrong extension
	}

	for _, filename := range subtitleFiles {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte("fake subtitle"), 0644); err != nil {
			t.Fatalf("Failed to create subtitle file %s: %v", filename, err)
		}
	}

	// Test subtitle detection
	matches := handler.FindSubtitles(videoFile)

	// Should find 4 matching subtitle files for IPX-535
	if len(matches) != 4 {
		t.Errorf("Expected 4 subtitle matches, got %d", len(matches))
		for _, match := range matches {
			t.Logf("Found: %s (language: %s)", match.OriginalPath, match.Language)
		}
	}

	// Verify language detection
	languages := make(map[string]string)
	for _, match := range matches {
		filename := filepath.Base(match.OriginalPath)
		languages[filename] = match.Language
	}

	expectedLanguages := map[string]string{
		"IPX-535.srt":          "",         // No language code
		"IPX-535.eng.srt":      "english",  // Language code detected
		"IPX-535.japanese.srt": "japanese", // Full language name detected
		"IPX-535.chi.ass":      "chinese",  // Chinese with different extension
	}

	for filename, expectedLang := range expectedLanguages {
		if actualLang, exists := languages[filename]; !exists {
			t.Errorf("Expected subtitle file %s not found", filename)
		} else if actualLang != expectedLang {
			t.Errorf("Expected language %s for %s, got %s", expectedLang, filename, actualLang)
		}
	}
}

func TestSubtitleHandler_FindSubtitles_CaseInsensitive(t *testing.T) {
	cfg := &config.OutputConfig{
		SubtitleExtensions: []string{".srt", ".ass"},
	}

	handler := NewSubtitleHandler(cfg)

	// Create temporary directory structure
	tmpDir := t.TempDir()

	// Create video file with uppercase name
	videoPath := filepath.Join(tmpDir, "IPX-535.mp4")
	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatalf("Failed to create video file: %v", err)
	}

	videoFile := scanner.FileInfo{
		Name:      "IPX-535.mp4",
		Path:      videoPath,
		Extension: ".mp4",
		Size:      1000,
	}

	// Create subtitle files with different cases (common on Windows)
	subtitleFiles := []string{
		"ipx-535.srt",           // Lowercase - should still match
		"IPX-535.eng.srt",       // Exact case with language code
		"ipx-535.japanese.srt",  // Lowercase with full language name
		"Ipx-535.chi.ass",       // Mixed case
	}

	for _, filename := range subtitleFiles {
		path := filepath.Join(tmpDir, filename)
		if err := os.WriteFile(path, []byte("fake subtitle"), 0644); err != nil {
			t.Fatalf("Failed to create subtitle file %s: %v", filename, err)
		}
	}

	// Test subtitle detection with case-insensitive matching
	matches := handler.FindSubtitles(videoFile)

	// Should find all 4 subtitle files regardless of case
	if len(matches) != 4 {
		t.Errorf("Expected 4 subtitle matches, got %d", len(matches))
		for _, match := range matches {
			t.Logf("Found: %s (language: %s)", match.OriginalPath, match.Language)
		}
	}

	// Verify all files were found and language codes were detected
	foundFiles := make(map[string]string) // filename -> detected language
	for _, match := range matches {
		filename := filepath.Base(match.OriginalPath)
		foundFiles[filename] = match.Language
	}

	expectedLanguages := map[string]string{
		"ipx-535.srt":          "",         // No language code
		"IPX-535.eng.srt":      "english",  // Language code detected (mixed case)
		"ipx-535.japanese.srt": "japanese", // Full language name (lowercase)
		"Ipx-535.chi.ass":      "chinese",  // Chinese with mixed case
	}

	for expectedFile, expectedLang := range expectedLanguages {
		actualLang, found := foundFiles[expectedFile]
		if !found {
			t.Errorf("Expected subtitle file not found: %s", expectedFile)
		} else if actualLang != expectedLang {
			t.Errorf("File %s: expected language %q, got %q", expectedFile, expectedLang, actualLang)
		}
	}
}

func TestSubtitleHandler_MoveSubtitles(t *testing.T) {
	cfg := &config.OutputConfig{
		SubtitleExtensions: []string{".srt", ".ass"},
	}

	handler := NewSubtitleHandler(cfg)

	// Create temporary directory structure
	sourceDir := t.TempDir()
	targetDir := filepath.Join(sourceDir, "organized")

	// Create video file
	videoPath := filepath.Join(sourceDir, "IPX-535.mp4")
	if err := os.WriteFile(videoPath, []byte("fake video"), 0644); err != nil {
		t.Fatalf("Failed to create video file: %v", err)
	}

	// Create subtitle files
	subtitles := []SubtitleMatch{
		{
			OriginalPath: filepath.Join(sourceDir, "IPX-535.srt"),
			Language:     "",
			Extension:    ".srt",
		},
		{
			OriginalPath: filepath.Join(sourceDir, "IPX-535.eng.srt"),
			Language:     "english",
			Extension:    ".srt",
		},
		{
			OriginalPath: filepath.Join(sourceDir, "IPX-535.jpn.ass"),
			Language:     "japanese",
			Extension:    ".ass",
		},
	}

	// Create the actual subtitle files
	for _, subtitle := range subtitles {
		if err := os.WriteFile(subtitle.OriginalPath, []byte("fake subtitle"), 0644); err != nil {
			t.Fatalf("Failed to create subtitle file %s: %v", subtitle.OriginalPath, err)
		}
	}

	// Test dry run
	err := handler.MoveSubtitles(subtitles, targetDir, "IPX-535.mp4", true)
	if err != nil {
		t.Errorf("MoveSubtitles dry run failed: %v", err)
	}

	// Files should still be in original location for dry run
	for _, subtitle := range subtitles {
		if _, err := os.Stat(subtitle.OriginalPath); os.IsNotExist(err) {
			t.Errorf("Subtitle file was moved during dry run: %s", subtitle.OriginalPath)
		}
	}

	// Test actual move
	err = handler.MoveSubtitles(subtitles, targetDir, "IPX-535.mp4", false)
	if err != nil {
		t.Errorf("MoveSubtitles actual move failed: %v", err)
	}

	// Verify files were moved
	expectedFiles := []string{
		"IPX-535.srt",     // No language code
		"IPX-535.eng.srt", // English
		"IPX-535.jpn.ass", // Japanese (code conversion)
	}

	for _, expectedFile := range expectedFiles {
		expectedPath := filepath.Join(targetDir, expectedFile)
		if _, err := os.Stat(expectedPath); os.IsNotExist(err) {
			t.Errorf("Expected subtitle file not found: %s", expectedPath)
		}
	}

	// Verify original files are gone
	for _, subtitle := range subtitles {
		if _, err := os.Stat(subtitle.OriginalPath); !os.IsNotExist(err) {
			t.Errorf("Original subtitle file still exists: %s", subtitle.OriginalPath)
		}
	}
}

func TestSubtitleHandler_extractLanguageCode(t *testing.T) {
	cfg := &config.OutputConfig{
		SubtitleExtensions: []string{".srt"},
	}

	handler := NewSubtitleHandler(cfg)

	tests := []struct {
		name                 string
		subtitleName         string
		videoNameWithoutExt  string
		expectedLanguage     string
	}{
		{
			name:                "No language code",
			subtitleName:        "IPX-535.srt",
			videoNameWithoutExt: "IPX-535",
			expectedLanguage:    "",
		},
		{
			name:                "English language code",
			subtitleName:        "IPX-535.eng.srt",
			videoNameWithoutExt: "IPX-535",
			expectedLanguage:    "english",
		},
		{
			name:                "Japanese language code",
			subtitleName:        "IPX-535.jpn.srt",
			videoNameWithoutExt: "IPX-535",
			expectedLanguage:    "japanese",
		},
		{
			name:                "Full language name",
			subtitleName:        "IPX-535.english.srt",
			videoNameWithoutExt: "IPX-535",
			expectedLanguage:    "english",
		},
		{
			name:                "Chinese language code",
			subtitleName:        "IPX-535.chi.srt",
			videoNameWithoutExt: "IPX-535",
			expectedLanguage:    "chinese",
		},
		{
			name:                "No match - different video",
			subtitleName:        "ABCD-123.eng.srt",
			videoNameWithoutExt: "IPX-535",
			expectedLanguage:    "",
		},
		{
			name:                "Dash separator",
			subtitleName:        "IPX-535-eng.srt",
			videoNameWithoutExt: "IPX-535",
			expectedLanguage:    "english",
		},
		{
			name:                "Underscore separator",
			subtitleName:        "IPX-535_eng.srt",
			videoNameWithoutExt: "IPX-535",
			expectedLanguage:    "english",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.extractLanguageCode(tt.subtitleName, tt.videoNameWithoutExt)
			if result != tt.expectedLanguage {
				t.Errorf("extractLanguageCode() = %q, want %q", result, tt.expectedLanguage)
			}
		})
	}
}

func TestSubtitleHandler_generateSubtitleFileName(t *testing.T) {
	cfg := &config.OutputConfig{
		SubtitleExtensions: []string{".srt", ".ass"},
	}

	handler := NewSubtitleHandler(cfg)

	tests := []struct {
		name                 string
		videoNameWithoutExt  string
		language             string
		extension            string
		expectedFilename     string
	}{
		{
			name:                "No language",
			videoNameWithoutExt: "IPX-535",
			language:            "",
			extension:           ".srt",
			expectedFilename:    "IPX-535.srt",
		},
		{
			name:                "English language code",
			videoNameWithoutExt: "IPX-535",
			language:            "eng",
			extension:           ".srt",
			expectedFilename:    "IPX-535.eng.srt",
		},
		{
			name:                "English full name",
			videoNameWithoutExt: "IPX-535",
			language:            "english",
			extension:           ".srt",
			expectedFilename:    "IPX-535.eng.srt",
		},
		{
			name:                "Japanese language code",
			videoNameWithoutExt: "IPX-535",
			language:            "jpn",
			extension:           ".ass",
			expectedFilename:    "IPX-535.jpn.ass",
		},
		{
			name:                "Chinese full name",
			videoNameWithoutExt: "IPX-535",
			language:            "chinese",
			extension:           ".srt",
			expectedFilename:    "IPX-535.chi.srt",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := handler.generateSubtitleFileName(tt.videoNameWithoutExt, tt.language, tt.extension)
			if result != tt.expectedFilename {
				t.Errorf("generateSubtitleFileName() = %q, want %q", result, tt.expectedFilename)
			}
		})
	}
}