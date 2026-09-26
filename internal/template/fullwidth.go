package template

import (
	"path/filepath"
	"strings"
	"unicode/utf8"
)

// foldFullwidthASCII folds fullwidth ASCII-range codepoints (Ａ-Ｚ, ａ-ｚ,
// ０-９, and the rest of U+FF01..U+FF5E, plus the ideographic space U+3000)
// to their halfwidth counterparts, mirroring the scanner's, matcher's,
// worker's, and organizer's same-named folds (internal/scanner/fullwidth.go,
// internal/matcher/fullwidth.go, internal/worker/fullwidth.go,
// internal/organizer/strategy_inplace.go): all of those are package-private,
// so the template engine's filename tags carry the identical fold locally
// instead of importing it. Kana, kanji, and all other runes are untouched,
// and ASCII-only input is returned unchanged.
func foldFullwidthASCII(s string) string {
	if !containsFullwidthASCII(s) {
		return s
	}
	return strings.Map(func(r rune) rune {
		switch {
		case r >= 0xFF01 && r <= 0xFF5E:
			return r - 0xFEE0
		case r == 0x3000:
			return ' '
		default:
			return r
		}
	}, s)
}

func containsFullwidthASCII(s string) bool {
	for _, r := range s {
		if (r >= 0xFF01 && r <= 0xFF5E) || r == 0x3000 {
			return true
		}
	}
	return false
}

// splitRawExtension splits name into its stem and raw extension using the
// fullwidth-aware fold — the same split the organizer carries for its rename
// targets (internal/organizer/strategy_inplace.go splitRawExtension,
// mirrored locally because that helper is package-private). The extension
// is the suffix starting at the last dot of the folded spelling, and
// because foldFullwidthASCII maps rune-for-rune, the raw extension carries
// the same rune count as the folded one — so the raw split trims that many
// runes. It exists because round 40a publishes the raw on-disk basename
// (RCT-156-HD．ｍｋｖ) in Movie.OriginalFileName: filepath.Ext is ASCII-only,
// so the <FILENAME> tag would keep the fullwidth extension in the stem and
// the organizer would append the folded .mkv on top
// (RCT-156-HD．ｍｋｖ.mkv). ASCII-only input reduces exactly to
// strings.TrimSuffix(name, filepath.Ext(name)).
func splitRawExtension(name string) (stem, ext string) {
	foldedExt := filepath.Ext(foldFullwidthASCII(name))
	if foldedExt == "" {
		return name, ""
	}
	// foldFullwidthASCII maps rune-for-rune, so the raw extension carries
	// the same rune count as the folded one — trim exactly that many runes.
	n := utf8.RuneCountInString(foldedExt)
	runes := []rune(name)
	return string(runes[:len(runes)-n]), string(runes[len(runes)-n:])
}

// foldNameExtension returns a filename with its extension spelling folded
// to halfwidth ASCII — mirroring the scanner's and worker's same-named
// helpers (internal/scanner/fullwidth.go, internal/worker/fullwidth.go,
// both package-private): RCT-156-HD．ｍｋｖ emits RCT-156-HD.mkv, so the
// <FILENAME_EXT> tag yields a name whose extension the ASCII-only world
// (filepath.Ext, video extension filters) recognizes. Only the extension's
// spelling folds: the stem keeps its raw form, and the raw on-disk spelling
// stays in Movie.OriginalFileName (round 40a) — the fold happens at tag
// resolution, not in the metadata field.
func foldNameExtension(name string) string {
	folded := foldFullwidthASCII(name)
	ext := filepath.Ext(folded)
	if ext == "" || folded == name {
		return name
	}
	// foldFullwidthASCII maps rune-to-rune, so the raw spelling of ext is
	// exactly the last len([]rune(ext)) runes of name; replacing that tail
	// with ext folds the extension without touching the stem.
	runes := []rune(name)
	return string(runes[:len(runes)-len([]rune(ext))]) + ext
}
