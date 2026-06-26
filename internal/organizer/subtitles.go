package organizer

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// Language code mappings (ISO 639)
var (
	// langNameByCode maps language codes to full names
	langNameByCode = map[string]string{
		"eng": "english",
		"en":  "english",
		"jpn": "japanese",
		"ja":  "japanese",
		"chi": "chinese",
		"zh":  "chinese",
		"kor": "korean",
		"ko":  "korean",
		"fre": "french",
		"fr":  "french",
		"ger": "german",
		"de":  "german",
		"spa": "spanish",
		"es":  "spanish",
		"ita": "italian",
		"it":  "italian",
		"por": "portuguese",
		"pt":  "portuguese",
		"rus": "russian",
		"ru":  "russian",
		"ara": "arabic",
		"ar":  "arabic",
	}

	// langCodeByName maps full language names to codes
	langCodeByName = func() map[string]string {
		m := make(map[string]string, len(langNameByCode))
		for code, name := range langNameByCode {
			// Only add 3-letter codes to avoid duplicates
			if len(code) == 3 {
				m[name] = code
			}
		}
		return m
	}()
)

// subtitleHandler manages subtitle file operations
type subtitleHandler struct {
	fs         afero.Fs
	extensions []string
}

// newSubtitleHandler creates a new subtitle handler
func newSubtitleHandler(fs afero.Fs, subtitleExtensions []string) *subtitleHandler {
	return &subtitleHandler{
		fs:         fs,
		extensions: subtitleExtensions,
	}
}

// subtitleMatch represents a matched subtitle file
type subtitleMatch struct {
	OriginalPath string
	NewPath      string
	Language     string // ISO 639 language code if detectable
	Extension    string
}

// FindSubtitles searches for subtitle files associated with a video file
func (sh *subtitleHandler) FindSubtitles(videoFile models.FileMatchInfo) []subtitleMatch {
	if len(sh.extensions) == 0 {
		return nil
	}

	videoDir := filepath.Dir(videoFile.Path)
	videoName := filepath.Base(videoFile.Path)
	videoNameWithoutExt := strings.TrimSuffix(videoName, videoFile.Extension)

	matches := make([]subtitleMatch, 0)

	// Search for subtitle files in the same directory
	files, err := afero.ReadDir(sh.fs, videoDir)
	if err != nil {
		// Surface the failure rather than collapsing it into "no subtitles found",
		// which would hide permission or missing-directory errors from the organize
		// flow. We still return the empty set (no subtitles to attach) but log the
		// cause so it is diagnosable.
		logging.Errorf("Failed to read subtitle directory %s: %v", videoDir, err)
		return matches
	}

	for _, file := range files {
		if file.IsDir() {
			continue
		}

		subtitlePath := filepath.Join(videoDir, file.Name())
		subtitleName := file.Name()

		// Check if this is a subtitle file
		if !sh.isSubtitleFile(subtitleName) {
			continue
		}

		// Check if subtitle filename matches the video filename
		// Require exact match or separator after video name to avoid false matches
		// (e.g., "IPX-535.mp4" should not match "IPX-535-trailer.srt")
		// Use case-insensitive matching for Windows compatibility
		subtitleNameWithoutExt := strings.TrimSuffix(subtitleName, filepath.Ext(subtitleName))
		base := strings.ToLower(videoNameWithoutExt)
		cand := strings.ToLower(subtitleNameWithoutExt)

		// Exact match or has separator (., -, _) after base name
		isMatch := cand == base ||
			(strings.HasPrefix(cand, base) && len(cand) > len(base) &&
				strings.ContainsRune("._-", rune(cand[len(base)])))

		if !isMatch {
			continue
		}

		// Extract language code from filename
		language := sh.extractLanguageCode(subtitleName, videoNameWithoutExt)

		matches = append(matches, subtitleMatch{
			OriginalPath: subtitlePath,
			Language:     language,
			Extension:    filepath.Ext(subtitleName),
		})
	}

	return matches
}

// MoveSubtitles moves subtitle files to the target directory with the video file
func (sh *subtitleHandler) MoveSubtitles(subtitles []subtitleMatch, targetDir, videoFileName string, dryRun bool) error {
	if len(subtitles) == 0 {
		return nil
	}

	// Create target directory if it doesn't exist
	if !dryRun {
		if err := sh.fs.MkdirAll(targetDir, config.DirPerm); err != nil {
			return fmt.Errorf("failed to create target directory: %w", err)
		}
	}

	videoNameWithoutExt := strings.TrimSuffix(videoFileName, filepath.Ext(videoFileName))

	for _, subtitle := range subtitles {
		// Generate new subtitle filename
		newFileName := sh.generateSubtitleFileName(videoNameWithoutExt, subtitle.Language, subtitle.Extension)
		newPath := filepath.Join(targetDir, newFileName)

		if dryRun {
			logging.Debugf("Would move subtitle: %s -> %s", subtitle.OriginalPath, newPath)
			continue
		}

		if _, err := sh.fs.Stat(newPath); err == nil {
			logging.Infof("Subtitle already exists, skipping: %s", newPath)
			continue
		}

		if err := fsutil.MoveFileFs(sh.fs, subtitle.OriginalPath, newPath); err != nil {
			return fmt.Errorf("failed to move subtitle %s to %s: %w", subtitle.OriginalPath, newPath, err)
		}

		logging.Infof("Moved subtitle: %s -> %s", subtitle.OriginalPath, newPath)
	}

	return nil
}

// isSubtitleFile checks if a filename has a subtitle extension
func (sh *subtitleHandler) isSubtitleFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(filename))
	for _, allowedExt := range sh.extensions {
		if ext == strings.ToLower(allowedExt) {
			return true
		}
	}
	return false
}

// ExtractLanguageCode extracts language code from subtitle filename
// Examples: "IPX-535.eng.srt" -> "eng", "IPX-535.english.srt" -> "english"
func (sh *subtitleHandler) extractLanguageCode(subtitleName, videoNameWithoutExt string) string {
	subtitleNameWithoutExt := strings.TrimSuffix(subtitleName, filepath.Ext(subtitleName))

	// Remove the video name prefix to get the language part (case-insensitive)
	if len(subtitleNameWithoutExt) >= len(videoNameWithoutExt) &&
		strings.EqualFold(subtitleNameWithoutExt[:len(videoNameWithoutExt)], videoNameWithoutExt) {
		remaining := subtitleNameWithoutExt[len(videoNameWithoutExt):]

		// Remove leading dots, dashes, or underscores
		remaining = strings.TrimLeft(remaining, "._-")

		// Common language code patterns
		remaining = strings.ToLower(remaining)

		// Check for exact match with language code
		if lang, exists := langNameByCode[remaining]; exists {
			return lang
		}

		// Check for full language name match (exact)
		for _, name := range langNameByCode {
			if remaining == name {
				return name
			}
		}

		// Return remaining part as language name if no exact match found
		if remaining != "" {
			return remaining
		}
	}

	return "" // No language code detected
}

// GenerateSubtitleFileName generates the new filename for a subtitle file
// Examples: "IPX-535.eng.srt", "IPX-535.srt", "IPX-535.english.srt"
func (sh *subtitleHandler) generateSubtitleFileName(videoNameWithoutExt, language, extension string) string {
	if language == "" {
		// No language code detected
		return videoNameWithoutExt + extension
	}

	// Check if language is already a code or full name
	code := language
	if langCode, exists := langCodeByName[strings.ToLower(language)]; exists {
		code = langCode
	}

	return fmt.Sprintf("%s.%s%s", videoNameWithoutExt, code, extension)
}
