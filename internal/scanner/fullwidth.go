package scanner

import (
	"path/filepath"
	"strings"
)

// foldFullwidthASCII folds fullwidth ASCII-range codepoints (Ａ-Ｚ, ａ-ｚ,
// ０-９, and the rest of U+FF01..U+FF5E, plus the ideographic space
// U+3000) to their halfwidth counterparts, mirroring the matcher's fold:
// a JP-sourced filename with a fullwidth extension (RCT-156-HD-2．ｍｋｖ)
// derives the same .mkv extension as its ASCII spelling. Kana, kanji,
// and all other runes are untouched, and ASCII-only input is returned
// unchanged.
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
// folded to halfwidth first: filepath.Ext is ASCII-only, so a fullwidth
// extension (．ｍｋｖ) would otherwise yield an empty Extension and the
// video filter would reject the file as a non-video. The folded ASCII
// extension is a literal suffix of the Name foldNameExtension emits, and
// the matcher folds the name itself (its custom regex sees the raw stem
// first).
func fileExtension(path string) string {
	return filepath.Ext(foldFullwidthASCII(path))
}

// foldNameExtension returns a file's basename with its extension spelling
// folded to halfwidth ASCII: RCT-156-HD-2．ｍｋｖ emits RCT-156-HD-2.mkv, so
// the extension the scanner derives (fileExtension) is a literal suffix of
// FileMatchInfo.Name and downstream base-name derivation
// (strings.TrimSuffix(Name, Extension)) cannot retain a ．ｍｋｖ remnant in
// sidecar names. Only the extension's spelling folds: the stem keeps its
// raw form — the matcher's custom-regex tier runs against the raw name
// first, and fullwidth-written patterns must keep matching scanner-fed
// files. Path deliberately keeps the raw on-disk spelling (see
// models.FileMatchInfo): it is the source of truth for file I/O.
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
