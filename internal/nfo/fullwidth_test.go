package nfo

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
)

// The fold mirrored from the matcher/scanner: ASCII-only input is returned
// unchanged, fullwidth ASCII-range codepoints fold to halfwidth, kana and
// kanji survive, and the ideographic space folds to an ASCII space.
func TestFoldFullwidthASCII(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		want string
	}{
		{"ASCII-only input unchanged", "RCT-156-HD", "RCT-156-HD"},
		{"fullwidth letters and digits fold", "ＲＣＴ-156-ＨＤ", "RCT-156-HD"},
		{"fullwidth extension folds", "．ｍｋｖ", ".mkv"},
		{"kana survives while the ideographic space folds", "ＲＣＴ　１５６　アイ", "RCT 156 アイ"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := foldFullwidthASCII(tc.in); got != tc.want {
				t.Errorf("foldFullwidthASCII(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// A fullwidth-spelled extension (．ｍｋｖ) must strip like an ASCII one
// (.mkv): filepath.Ext is ASCII-only, so without the fold the stem would
// keep the ．ｍｋｖ tail and every sidecar probe would miss the conventional
// RCT-156-HD.nfo. The stem itself keeps its raw spelling.
func TestVideoStem(t *testing.T) {
	testCases := []struct {
		name string
		in   string
		want string
	}{
		{"ASCII extension", "/movies/RCT-156-HD.mkv", "RCT-156-HD"},
		{"fullwidth extension folds before stripping", "/movies/RCT-156-HD．ｍｋｖ", "RCT-156-HD"},
		{"raw fullwidth stem survives the strip", "/movies/ＲＣＴ-156-HD．ｍｋｖ", "ＲＣＴ-156-HD"},
		{"only the final extension strips", "/movies/RCT-156-HD.v2．ｍｋｖ", "RCT-156-HD.v2"},
		{"fullwidth name without extension", "/movies/ＲＣＴ-156", "ＲＣＴ-156"},
		{"ASCII name without extension", "/movies/RCT-156-HD", "RCT-156-HD"},
		{"fullwidth slash is not a path separator", "/movies/IPX-535／sample．ｍｋｖ", "IPX-535／sample"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			if got := videoStem(tc.in); got != tc.want {
				t.Errorf("videoStem(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

// Pure-ASCII video names keep exactly one sidecar probe so their discovery
// behavior is unchanged; fullwidth-bearing video names additionally probe
// the fullwidth-spelled twin.
func TestNFOSidecarFilenames(t *testing.T) {
	if got, want := nfoSidecarFilenames("/movies/RCT-156-HD.mkv"), []string{"RCT-156-HD.nfo"}; !reflect.DeepEqual(got, want) {
		t.Errorf("nfoSidecarFilenames(ASCII video) = %v, want %v", got, want)
	}
	want := []string{"RCT-156-HD.nfo", "RCT-156-HD．ｎｆｏ"}
	if got := nfoSidecarFilenames("/movies/RCT-156-HD．ｍｋｖ"); !reflect.DeepEqual(got, want) {
		t.Errorf("nfoSidecarFilenames(fullwidth video) = %v, want %v", got, want)
	}
}

// The PerFile legacy probe must derive the stem with fullwidth-aware
// extension stripping: for ABC-123-pt1．ｍｋｖ the legacy list carries
// ABC-123-pt1.nfo (plus its fullwidth twin), not the bogus
// ABC-123-pt1．ｍｋｖ.nfo that silently skips the user's stored metadata.
func TestResolveNFOPath_FullwidthVideoPerFileLegacyProbe(t *testing.T) {
	movie := &models.Movie{ID: "ABC-123", Title: "Test Title"}
	cfg := NFONameConfig{FilenameTemplate: "[<ID>] <Title>.nfo", PerFile: true, IsMultiPart: true, PartSuffix: "-pt1"}

	nfoPath, legacyPaths := resolveNFOPath("/movies", movie, cfg, "/movies/ABC-123-pt1．ｍｋｖ", nil)

	if want := "/movies/[ABC-123] Test Title-pt1.nfo"; filepath.ToSlash(nfoPath) != want {
		t.Errorf("nfoPath = %q, want %q", filepath.ToSlash(nfoPath), want)
	}
	wantLegacy := []string{
		"/movies/ABC-123.nfo",
		"/movies/ABC-123-pt1.nfo",
		"/movies/ABC-123-pt1．ｎｆｏ",
	}
	gotLegacy := make([]string, len(legacyPaths))
	for i, p := range legacyPaths {
		gotLegacy[i] = filepath.ToSlash(p)
	}
	if !reflect.DeepEqual(gotLegacy, wantLegacy) {
		t.Errorf("legacyPaths = %v, want %v", gotLegacy, wantLegacy)
	}
}

// End-to-end discovery through the PerFile legacy probe: a
// fullwidth-extension video with a conventional ASCII sidecar is found.
func TestFindNFOFile_FullwidthVideoPerFileLegacyProbe(t *testing.T) {
	fs := afero.NewMemMapFs()
	_ = fs.MkdirAll("/movies", 0755)
	_ = afero.WriteFile(fs, "/movies/ABC-123-pt1.nfo", []byte("<video-nfo/>"), 0644)

	movie := &models.Movie{ID: "ABC-123", Title: "Test Title"}
	cfg := NFONameConfig{FilenameTemplate: "[<ID>] <Title>.nfo", PerFile: true, IsMultiPart: true, PartSuffix: "-pt1"}

	got := findNFOFile(fs, "/movies", movie, cfg, "/movies/ABC-123-pt1．ｍｋｖ", nil)
	if want := "/movies/ABC-123-pt1.nfo"; filepath.ToSlash(got) != want {
		t.Errorf("findNFOFile = %q, want %q", filepath.ToSlash(got), want)
	}
}

// The finding's scenario: the sidecar fallback probe must strip a
// fullwidth-spelled extension, so RCT-156-HD．ｍｋｖ discovers the
// conventional RCT-156-HD.nfo instead of probing RCT-156-HD．ｍｋｖ.nfo.
func TestFindNFOFile_FullwidthVideoSidecarFallback(t *testing.T) {
	fs := afero.NewMemMapFs()
	_ = fs.MkdirAll("/movies", 0755)
	_ = afero.WriteFile(fs, "/movies/RCT-156-HD.nfo", []byte("<sidecar/>"), 0644)

	// The templated and ID-based paths deliberately miss (the movie ID
	// differs from the video stem) so discovery reaches the sidecar fallback.
	movie := &models.Movie{ID: "RCT-156"}
	cfg := NFONameConfig{FilenameTemplate: "[<ID>] <TITLE>.nfo"}

	got := findNFOFile(fs, "/movies", movie, cfg, "/movies/RCT-156-HD．ｍｋｖ", nil)
	if want := "/movies/RCT-156-HD.nfo"; filepath.ToSlash(got) != want {
		t.Errorf("findNFOFile = %q, want %q (fullwidth extension must not defeat the sidecar probe)", filepath.ToSlash(got), want)
	}
}

// Both spellings of the sidecar filename are probed when the video
// filename carries fullwidth ASCII: the ASCII-spelled .nfo and the
// fullwidth-spelled ．ｎｆｏ twin.
func TestFindNFOFile_FullwidthSpelledSidecarTwin(t *testing.T) {
	testCases := []struct {
		name        string
		existingNFO string
	}{
		{"ASCII-spelled sidecar found", "/movies/RCT-156-HD.nfo"},
		{"fullwidth-spelled sidecar found", "/movies/RCT-156-HD．ｎｆｏ"},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			_ = fs.MkdirAll("/movies", 0755)
			_ = afero.WriteFile(fs, tc.existingNFO, []byte("<sidecar/>"), 0644)

			movie := &models.Movie{ID: "RCT-156"}
			cfg := NFONameConfig{FilenameTemplate: "[<ID>] <TITLE>.nfo"}

			got := findNFOFile(fs, "/movies", movie, cfg, "/movies/RCT-156-HD．ｍｋｖ", nil)
			if filepath.ToSlash(got) != tc.existingNFO {
				t.Errorf("findNFOFile = %q, want %q", filepath.ToSlash(got), tc.existingNFO)
			}
		})
	}
}

// A fullwidth-bearing video with no sidecar in either spelling discovers
// nothing (the fallback probes exhaust).
func TestFindNFOFile_FullwidthVideoWithoutSidecar(t *testing.T) {
	fs := afero.NewMemMapFs()
	_ = fs.MkdirAll("/movies", 0755)

	movie := &models.Movie{ID: "RCT-156"}
	cfg := NFONameConfig{FilenameTemplate: "[<ID>] <TITLE>.nfo"}

	if got := findNFOFile(fs, "/movies", movie, cfg, "/movies/RCT-156-HD．ｍｋｖ", nil); got != "" {
		t.Errorf("findNFOFile = %q, want empty", got)
	}
}

// A sidecar that equals the (already missed) templated nfoPath is not
// probed again, and discovery still returns nothing.
func TestFindNFOFile_SidecarEqualsNFOPathIsSkipped(t *testing.T) {
	fs := afero.NewMemMapFs()
	_ = fs.MkdirAll("/movies", 0755)

	movie := &models.Movie{ID: "ABC-123"}
	cfg := NFONameConfig{FilenameTemplate: "<ID>.nfo"}

	// The video stem equals the movie ID, so the sidecar probe target
	// equals nfoPath, which already missed.
	if got := findNFOFile(fs, "/movies", movie, cfg, "/movies/ABC-123.mp4", nil); got != "" {
		t.Errorf("findNFOFile = %q, want empty", got)
	}
}
