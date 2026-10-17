package organizer

// codex P2 (PR #257): a video admitted with a fullwidth stem
// (ＲＣＴ－１５６－ＨＤ．ｍｋｖ) deliberately keeps that raw stem in Name — the
// round-30/32 contract folds only the extension; Path stays fully raw — but
// the subtitle association comparison lowercased both stems without folding
// them, so an ASCII-spelled subtitle (RCT-156-HD.srt) beside the fullwidth
// video was never discovered and was left behind when the video was
// organized. The association — and the language extraction behind it — now
// compares the folded stem spellings as well as the raw ones, raw first,
// mirroring the dual-form matching the scanner applies to filters
// (internal/scanner scanner.go filterMatchesName).

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
)

// fullwidthStemMatch mirrors scanner-style admission for a fullwidth STEM
// (internal/scanner fullwidth.go foldNameExtension): only the extension
// folds, so Name keeps the raw fullwidth stem with the folded .mkv suffix
// while Path keeps the fully raw on-disk spelling.
func fullwidthStemMatch(path string) models.FileMatchInfo {
	return models.FileMatchInfo{
		MovieID:   "RCT-156",
		Path:      path,
		Name:      "ＲＣＴ－１５６－ＨＤ.mkv",
		Extension: ".mkv",
	}
}

// TestSubtitleHandler_FindSubtitles_FullwidthVideoAsciiSubtitle pins the
// finding itself: an ASCII subtitle beside a fullwidth-stem video associates
// through the folded spelling, with its language tag intact, and unrelated
// subtitles still do not.
func TestSubtitleHandler_FindSubtitles_FullwidthVideoAsciiSubtitle(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ", []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/RCT-156-HD.srt", []byte("subs"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/RCT-156-HD.eng.srt", []byte("subs"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/RCT-156-HD.frenchy.srt", []byte("subs"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/RCT-157.srt", []byte("subs"), 0o644))

	handler := newSubtitleHandler(fs, []string{".srt"})
	matches := handler.FindSubtitles(fullwidthStemMatch("/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ"))

	require.Len(t, matches, 3, "ASCII subtitles beside a fullwidth-stem video associate through the folded spelling")

	byName := make(map[string]subtitleMatch, len(matches))
	for _, m := range matches {
		byName[filepath.Base(m.OriginalPath)] = m
	}

	plain, ok := byName["RCT-156-HD.srt"]
	require.True(t, ok, "the plain ASCII subtitle is discovered beside the fullwidth video")
	assert.Equal(t, "", plain.Language)

	english, ok := byName["RCT-156-HD.eng.srt"]
	require.True(t, ok, "the language-tagged ASCII subtitle is discovered beside the fullwidth video")
	assert.Equal(t, "english", english.Language, "the language tag survives the mixed-spelling association")

	unknown, ok := byName["RCT-156-HD.frenchy.srt"]
	require.True(t, ok, "an unknown language tag still associates")
	assert.Equal(t, "frenchy", unknown.Language, "an unknown language tag passes through the folded strip verbatim")
}

// TestSubtitleHandler_FindSubtitles_FullwidthVideoFullwidthSubtitle pins the
// raw-comparison path: fullwidth-written subtitles beside fullwidth-written
// videos keep associating and keep extracting language where they did before
// the folded comparison existed.
func TestSubtitleHandler_FindSubtitles_FullwidthVideoFullwidthSubtitle(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ", []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ.srt", []byte("subs"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ.eng.srt", []byte("subs"), 0o644))

	handler := newSubtitleHandler(fs, []string{".srt"})
	matches := handler.FindSubtitles(fullwidthStemMatch("/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ"))

	require.Len(t, matches, 2, "fullwidth-written subtitles beside a fullwidth video keep matching through the raw comparison")

	byName := make(map[string]subtitleMatch, len(matches))
	for _, m := range matches {
		byName[filepath.Base(m.OriginalPath)] = m
	}

	plain, ok := byName["ＲＣＴ－１５６－ＨＤ.srt"]
	require.True(t, ok, "the fullwidth-stem subtitle is still discovered through the raw comparison")
	assert.Equal(t, "", plain.Language)

	english, ok := byName["ＲＣＴ－１５６－ＨＤ.eng.srt"]
	require.True(t, ok, "the language-tagged fullwidth subtitle is still discovered")
	assert.Equal(t, "english", english.Language, "the raw comparison still extracts language for fullwidth pairs")
}

// TestOrganize_FullwidthVideoAsciiSubtitle_MovedWithVideo pins the finding
// end to end: the ASCII subtitle beside the fullwidth video is discovered
// and moved with it, under the sidecar name the folded-extension target
// stem (round-32a splitRawExtension contract) derives — never the raw
// fullwidth stem.
func TestOrganize_FullwidthVideoAsciiSubtitle_MovedWithVideo(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ", []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/RCT-156-HD.srt", []byte("subs"), 0o644))

	org := NewOrganizer(fs, &Config{
		FolderFormat:       "<ID>",
		FileFormat:         "<ID>",
		RenameFile:         true,
		OperationMode:      operationmode.OperationModeOrganize,
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
	}, nil, nil)

	result, err := org.Organize(context.Background(), OrganizeCmd{
		Match:         fullwidthStemMatch("/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ"),
		Movie:         testutil.NewMovieBuilder().WithID("RCT-156").Build(),
		DestDir:       "/dest",
		MoveFiles:     true,
		OperationMode: operationmode.OperationModeOrganize,
	})
	require.NoError(t, err)
	require.NotNil(t, result)

	exists, err := afero.Exists(fs, "/dest/RCT-156/RCT-156.mkv")
	require.NoError(t, err)
	assert.True(t, exists, "the video is organized under the canonical name")

	exists, err = afero.Exists(fs, "/dest/RCT-156/RCT-156.srt")
	require.NoError(t, err)
	assert.True(t, exists, "the ASCII subtitle beside the fullwidth video moves with it")

	exists, err = afero.Exists(fs, "/source/RCT-156-HD.srt")
	require.NoError(t, err)
	assert.False(t, exists, "the subtitle is moved, not left behind in the source dir")

	require.Len(t, result.Subtitles, 1)
	assert.True(t, result.Subtitles[0].Moved, "the subtitle install records a move")
	assert.Equal(t, filepath.ToSlash("/source/RCT-156-HD.srt"), filepath.ToSlash(result.Subtitles[0].OriginalPath))
	// The sidecar stem derives from the folded-extension target name
	// (round-32a splitRawExtension contract), never the raw fullwidth stem.
	assert.Equal(t, filepath.ToSlash("/dest/RCT-156/RCT-156.srt"), filepath.ToSlash(result.Subtitles[0].NewPath))
}
