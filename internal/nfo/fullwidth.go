package nfo

import (
	"path/filepath"
	"strings"
)

// nfoExtensionFullwidth is the fullwidth ASCII spelling of nfoExtension
// (．ｎｆｏ). A JP-sourced tool may spell the sidecar's extension in fullwidth
// just as it spells the video's own extension (RCT-156-HD．ｍｋｖ), so the
// sidecar probes check both spellings.
const nfoExtensionFullwidth = "．ｎｆｏ"

// foldFullwidthASCII folds fullwidth ASCII-range codepoints (Ａ-Ｚ, ａ-ｚ,
// ０-９, and the rest of U+FF01..U+FF5E, plus the ideographic space
// U+3000) to their halfwidth counterparts, mirroring the matcher's and
// scanner's fold. Kana, kanji, and all other runes are untouched, and
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

// videoStem derives the sidecar stem for a video file path with
// fullwidth-aware extension stripping, mirroring the scanner's extension
// fold: filepath.Ext is ASCII-only, so a fullwidth extension
// (RCT-156-HD．ｍｋｖ) yields an empty extension and a plain TrimSuffix would
// keep the ．ｍｋｖ tail — the sidecar probes would look for
// RCT-156-HD．ｍｋｖ.nfo and silently miss the user's stored RCT-156-HD.nfo
// in preserve/merge workflows. Only the extension detection folds; the stem
// keeps its raw spelling (the scanner's raw-stem contract), and ASCII-only
// names derive the same stem as plain filepath.Ext-based stripping.
func videoStem(videoFilePath string) string {
	base := filepath.Base(videoFilePath)
	if !containsFullwidthASCII(base) {
		return strings.TrimSuffix(base, filepath.Ext(base))
	}
	ext := filepath.Ext(foldFullwidthASCII(base))
	if ext == "" {
		return base
	}
	// foldFullwidthASCII maps rune-to-rune, so the raw spelling of ext is
	// exactly the last len([]rune(ext)) runes of base; stripping that tail
	// removes the fullwidth-spelled extension without touching the stem.
	runes := []rune(base)
	return string(runes[:len(runes)-len([]rune(ext))])
}

// nfoSidecarFilenames lists the conventional video-sidecar NFO filenames
// (<video-stem>.nfo) to probe for a video path, in priority order: the
// ASCII-spelled extension first, then — only when the video filename
// itself carries fullwidth ASCII — its fullwidth twin (<stem>．ｎｆｏ),
// mirroring how the video's own extension may be spelled either way.
// Pure-ASCII video names keep exactly one probe, so their discovery
// behavior is unchanged.
func nfoSidecarFilenames(videoFilePath string) []string {
	stem := videoStem(videoFilePath)
	names := []string{stem + nfoExtension}
	if containsFullwidthASCII(filepath.Base(videoFilePath)) {
		names = append(names, stem+nfoExtensionFullwidth)
	}
	return names
}
