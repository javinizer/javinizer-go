package worker

import (
	"path/filepath"
	"strings"
)

// foldFullwidthASCII folds fullwidth ASCII-range codepoints (Ａ-Ｚ, ａ-ｚ,
// ０-９, and the rest of U+FF01..U+FF5E, plus the ideographic space U+3000)
// to their halfwidth counterparts, mirroring the scanner's, matcher's, and
// organizer's same-named folds (internal/scanner/fullwidth.go,
// internal/matcher/fullwidth.go, internal/organizer/strategy_inplace.go):
// all of those are package-private, so the worker's fallback FileMatchInfo
// constructors carry the identical fold locally instead of importing it.
// Kana, kanji, and all other runes are untouched, and ASCII-only input is
// returned unchanged.
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

// fileExtension derives a file's extension with fullwidth ASCII spellings
// folded to halfwidth first — the same derivation the scanner used to admit
// the file (internal/scanner/fullwidth.go fileExtension, mirrored locally
// because that helper is package-private): filepath.Ext is ASCII-only, so a
// fullwidth extension (RCT-156-HD．ｍｋｖ) would otherwise yield an empty
// Extension, and the worker's fallback FileMatchInfo constructors
// (scrape-phase map-miss backfill, rescrape tracker-map-miss fallback)
// would hand the organizer a match whose rename target loses the .mkv
// suffix. The folded ASCII extension is a literal suffix of the Name
// foldNameExtension emits.
func fileExtension(path string) string {
	return filepath.Ext(foldFullwidthASCII(path))
}

// foldNameExtension returns a file's basename with its extension spelling
// folded to halfwidth ASCII: RCT-156-HD．ｍｋｖ emits RCT-156-HD.mkv, so the
// extension the fallback derives (fileExtension) is a literal suffix of
// FileMatchInfo.Name and downstream base-name derivation
// (strings.TrimSuffix(Name, Extension)) cannot retain a ．ｍｋｖ remnant.
// Mirrors the scanner's same-named helper (internal/scanner/fullwidth.go,
// package-private) so the worker's fallback constructors satisfy the same
// round-30 contract the scanner's own construction does. Only the
// extension's spelling folds: the stem keeps its raw form. Path
// deliberately keeps the raw on-disk spelling (models.FileMatchInfo): it
// is the source of truth for file I/O.
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
