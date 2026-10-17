package worker

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

// --- Local mirror of the scanner's package-private fullwidth fold
// (internal/scanner/fullwidth.go). The worker's fallback FileMatchInfo
// constructors (scrape_phase.go map-miss backfill, rescrape_phase.go
// tracker-map-miss fallback) must admit the same spellings the scanner
// admitted, so the mirror is pinned to the scanner's own behaviors. ---

func TestFoldFullwidthASCII_WorkerMirror(t *testing.T) {
	assert.Equal(t, "RCT-156-HD", foldFullwidthASCII("ＲＣＴ-156-ＨＤ"))
	assert.Equal(t, ".mkv", foldFullwidthASCII("．ｍｋｖ"))
	assert.Equal(t, "RCT-156-HD", foldFullwidthASCII("RCT-156-HD"), "ASCII-only input is returned unchanged")
	assert.Equal(t, "RCT 156 アイ", foldFullwidthASCII("ＲＣＴ　１５６　アイ"), "kana survives while the ideographic space folds")
}

func TestFileExtension_WorkerMirror(t *testing.T) {
	assert.Equal(t, ".mkv", fileExtension("vids/RCT-156-HD．ｍｋｖ"), "fullwidth extension folds to ASCII")
	assert.Equal(t, ".mp4", fileExtension("vids/ABF-346.mp4"), "ASCII path derives its extension unchanged")
	assert.Equal(t, "", fileExtension("vids/no-extension"), "no dot yields no extension")
}

func TestFoldNameExtension_WorkerMirror(t *testing.T) {
	assert.Equal(t, "RCT-156-HD.mkv", foldNameExtension("RCT-156-HD．ｍｋｖ"),
		"extension spelling folds; the stem keeps its raw spelling")
	assert.Equal(t, "ABF-346.mp4", foldNameExtension("ABF-346.mp4"), "ASCII name is returned unchanged")
	assert.Equal(t, "no-extension", foldNameExtension("no-extension"), "no extension yields the name unchanged")
}
