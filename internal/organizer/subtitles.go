package organizer

import (
	"fmt"
	"path/filepath"
	"strings"

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
	// The stem must derive from the folded-extension Name so a fullwidth
	// spelling does not survive into the sidecar base name (the Path keeps
	// the raw on-disk spelling).
	videoNameWithoutExt := strings.TrimSuffix(videoFile.Name, videoFile.Extension)

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
		// Use case-insensitive matching for Windows compatibility.
		// The split is fold-aware (splitRawExtension): filepath.Ext is
		// ASCII-only, so a fullwidth dot would leave the raw suffix on the
		// stem and misfire the association and language strip below.
		subtitleNameWithoutExt, _ := splitRawExtension(subtitleName)

		if !subtitleStemAssociates(subtitleNameWithoutExt, videoNameWithoutExt) {
			continue
		}

		// Extract language code from filename
		language := sh.extractLanguageCode(subtitleName, videoNameWithoutExt)

		matches = append(matches, subtitleMatch{
			// OriginalPath keeps the raw on-disk spelling: the move — and the
			// source removal it performs — operates on the real file, never a
			// folded rewriting of its name (round-30 contract: raw for I/O).
			OriginalPath: subtitlePath,
			Language:     language,
			// The extension classifies the sidecar and names its destination,
			// so it is carried folded (round-30 contract: folded for
			// classification): a fullwidth-source subtitle
			// (ＲＣＴ－１５６－ＨＤ．ｓｒｔ) lands on a playable ASCII .srt
			// target under the round-32a/36b folded sidecar stem.
			Extension: filepath.Ext(foldFullwidthASCII(subtitleName)),
		})
	}

	return matches
}

// subtitleStemAssociates reports whether a subtitle stem associates with the
// video stem in either of its two spellings. The raw comparison runs first
// so fullwidth-written subtitles beside fullwidth-written videos keep
// matching exactly where they did before folding existed; the folded
// comparison runs as well because Name deliberately retains the raw
// fullwidth stem while the scanner admits fullwidth spellings (round-30/32
// contract: Name folds only the extension; Path stays fully raw) — an ASCII
// subtitle beside a fullwidth video (ＲＣＴ－１５６－ＨＤ．ｍｋｖ +
// RCT-156-HD.srt) associates through the folded spelling, mirroring the
// dual-form matching the scanner applies to filters (internal/scanner
// scanner.go filterMatchesName).
func subtitleStemAssociates(subtitleStem, videoStem string) bool {
	cand, base := strings.ToLower(subtitleStem), strings.ToLower(videoStem)
	if subtitleStemMatches(cand, base) {
		return true
	}
	foldedCand := strings.ToLower(foldFullwidthASCII(subtitleStem))
	foldedBase := strings.ToLower(foldFullwidthASCII(videoStem))
	if foldedCand == cand && foldedBase == base {
		return false // nothing folded — the raw attempt already covered it
	}
	return subtitleStemMatches(foldedCand, foldedBase)
}

// subtitleStemMatches reports whether the (already lowercased) candidate
// stem is the video stem itself or extends it with a separator (., -, _):
// "ipx-535" matches "ipx-535" and "ipx-535.en" but not "ipx-535trailer".
func subtitleStemMatches(cand, base string) bool {
	return cand == base ||
		(strings.HasPrefix(cand, base) && len(cand) > len(base) &&
			strings.ContainsRune("._-", rune(cand[len(base)])))
}

// isSubtitleFile checks if a filename has a subtitle extension. The
// extension is derived from the FOLDED spelling: filepath.Ext is ASCII-only
// and does not recognize the fullwidth dot, so a sidecar admitted beside a
// fullwidth video (ＲＣＴ－１５６－ＨＤ．ｓｒｔ) was rejected before the
// round-36b folded-stem comparison ever ran. Mirrors the round-30 contract:
// folded for classification — the raw name still performs all file I/O.
func (sh *subtitleHandler) isSubtitleFile(filename string) bool {
	ext := strings.ToLower(filepath.Ext(foldFullwidthASCII(filename)))
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
	// Fold-aware split (splitRawExtension): filepath.Ext is ASCII-only, so a
	// fullwidth dot would leave the raw suffix on the stem and report the
	// extension itself as the subtitle's language.
	subtitleNameWithoutExt, _ := splitRawExtension(subtitleName)

	// The raw prefix strip runs first so fullwidth-written subtitles beside
	// fullwidth-written videos keep extracting language exactly where they
	// did before folding existed; the folded strip then covers the mixed
	// spellings the dual-form association admits (an ASCII subtitle beside
	// a fullwidth video), mirroring the raw-then-folded order of
	// subtitleStemAssociates.
	if lang := languageAfterVideoStem(subtitleNameWithoutExt, videoNameWithoutExt); lang != "" {
		return lang
	}
	foldedSubtitle := foldFullwidthASCII(subtitleNameWithoutExt)
	foldedVideo := foldFullwidthASCII(videoNameWithoutExt)
	if foldedSubtitle == subtitleNameWithoutExt && foldedVideo == videoNameWithoutExt {
		return "" // No language code detected
	}
	return languageAfterVideoStem(foldedSubtitle, foldedVideo)
}

// languageAfterVideoStem strips the video stem prefix (case-insensitively)
// from the subtitle stem and normalizes the remainder into a language name:
// "" when the subtitle carries nothing beyond the stem or is not prefixed
// by it at all, and otherwise the language the suffix spells —
// languageAfterVideoStem("IPX-535.eng", "IPX-535") -> "english". The
// suffix is folded to its ASCII spelling along the way, so a fullwidth
// spelling of the same tag extracts the same language:
// languageAfterVideoStem("ＲＣＴ－１５６－ＨＤ．ｅｎｇ", "ＲＣＴ－１５６－ＨＤ")
// -> "english" too.
func languageAfterVideoStem(subtitleStem, videoStem string) string {
	// Remove the video name prefix to get the language part (case-insensitive)
	if len(subtitleStem) >= len(videoStem) &&
		strings.EqualFold(subtitleStem[:len(videoStem)], videoStem) {
		remaining := subtitleStem[len(videoStem):]

		// Fold the suffix before the separator strip: a fully fullwidth
		// sidecar (ＲＣＴ－１５６－ＨＤ．ｅｎｇ．ｓｒｔ) strips to the suffix
		// ．ｅｎｇ, which is nonempty — so extractLanguageCode's raw early
		// return fired before the round-37 folded retry could map it, and
		// the fullwidth spelling leaked into the destination
		// (RCT-156.．ｅｎｇ.srt). Folding first turns ．ｅｎｇ into .eng, which
		// the separator strip and the code table below recognize as
		// english. The language field is classification data that names the
		// destination, so it is carried folded (round-30 contract: folded
		// for classification); ASCII-only input is returned unchanged.
		remaining = foldFullwidthASCII(remaining)

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
