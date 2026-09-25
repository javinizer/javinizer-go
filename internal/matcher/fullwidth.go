package matcher

import "strings"

// foldFullwidthASCII folds fullwidth ASCII-range codepoints (Ａ-Ｚ, ａ-ｚ,
// ０-９, －, and the rest of U+FF01..U+FF5E, plus the ideographic space
// U+3000) to their halfwidth counterparts so fullwidth filename spellings
// (common in JP-sourced names) behave exactly like their ASCII spellings in
// every matcher tier. Kana, kanji, and all other runes are untouched, and
// ASCII-only input is returned unchanged.
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
