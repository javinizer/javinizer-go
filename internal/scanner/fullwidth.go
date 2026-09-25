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
// extension lands in FileMatchInfo.Extension while Path and Name keep
// their raw spelling — the matcher folds the name itself and its custom
// regex sees the raw name first.
func fileExtension(path string) string {
	return filepath.Ext(foldFullwidthASCII(path))
}
