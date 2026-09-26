package organizer

// codex P2 (PR #257, round 45a): a fully fullwidth sidecar can carry its
// language tag in the same spelling (ＲＣＴ－１５６－ＨＤ．ｅｮｇ．ｓｒｔ). The raw
// languageAfterVideoStem strip returned the nonempty fullwidth suffix
// (．ｅｎｇ) — a fullwidth dot is not in "._-" — so extractLanguageCode's raw
// early return fired before the round-37 folded retry could map it, and the
// fullwidth spelling leaked into the destination (RCT-156.．ｅｎｇ.srt). The
// suffix is now folded before the separator strip, so the raw path itself
// extracts the language the folded spelling names and the destination
// lands on the playable ASCII target (RCT-156.eng.srt).

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/testutil"
)

// TestSubtitleHandler_FindSubtitles_FullwidthLanguageTag pins the finding at
// the discovery level: the fully fullwidth language-tagged sidecar extracts
// the language its folded spelling names — never the fullwidth suffix —
// while the no-language fullwidth sidecar still extracts none.
func TestSubtitleHandler_FindSubtitles_FullwidthLanguageTag(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ", []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｅｎｇ．ｓｒｔ", []byte("subs"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｓｒｔ", []byte("subs"), 0o644))

	handler := newSubtitleHandler(fs, []string{".srt"})
	matches := handler.FindSubtitles(fullwidthStemMatch("/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ"))

	require.Len(t, matches, 2, "the fully fullwidth language-tagged sidecar and the no-language sidecar both associate")

	byName := make(map[string]subtitleMatch, len(matches))
	for _, m := range matches {
		byName[filepath.Base(m.OriginalPath)] = m
	}

	english, ok := byName["ＲＣＴ－１５６－ＨＤ．ｅｎｇ．ｓｒｔ"]
	require.True(t, ok, "the fully fullwidth language-tagged sidecar is discovered")
	assert.Equal(t, filepath.ToSlash("/source/ＲＣＴ－１５６－ＨＤ．ｅｎｇ．ｓｒｔ"), filepath.ToSlash(english.OriginalPath),
		"the match carries the raw on-disk spelling — file I/O never sees a folded rewriting")
	assert.Equal(t, "english", english.Language, "the fullwidth language tag extracts the language its folded spelling names")
	assert.Equal(t, ".srt", english.Extension, "the extension is carried in its folded (classification) spelling")

	plain, ok := byName["ＲＣＴ－１５６－ＨＤ．ｓｒｔ"]
	require.True(t, ok, "the no-language fullwidth sidecar is still discovered")
	assert.Equal(t, "", plain.Language, "the no-language fullwidth case is unchanged")
}

// TestSubtitleHandler_extractLanguageCode_FullwidthLanguageTag pins the
// extraction itself: the fully fullwidth pair spelling that previously
// tripped the raw early return, plus the mixed and ASCII spellings that
// must stay unchanged.
func TestSubtitleHandler_extractLanguageCode_FullwidthLanguageTag(t *testing.T) {
	handler := newSubtitleHandler(afero.NewMemMapFs(), []string{".srt"})

	assert.Equal(t, "english", handler.extractLanguageCode("ＲＣＴ－１５６－ＨＤ．ｅｎｇ．ｓｒｔ", "ＲＣＴ－１５６－ＨＤ"),
		"a fully fullwidth tag folds to its ASCII form before the raw early return accepts it")
	assert.Equal(t, "english", handler.extractLanguageCode("ＲＣＴ－１５６－ＨＤ.eng.srt", "ＲＣＴ－１５６－ＨＤ"),
		"the raw path keeps extracting language for a fullwidth stem beside an ASCII tag")
	assert.Equal(t, "english", handler.extractLanguageCode("RCT-156-HD.eng.srt", "RCT-156-HD"),
		"the ASCII .eng.srt case is unchanged")
	assert.Equal(t, "", handler.extractLanguageCode("ＲＣＴ－１５６－ＨＤ．ｓｒｔ", "ＲＣＴ－１５６－ＨＤ"),
		"the no-language fullwidth case is unchanged")
}

// TestOrganize_FullwidthVideoFullwidthLanguageTagSubtitle_MovedWithVideo
// pins the finding end to end: the fully fullwidth language-tagged sidecar
// moves with the video onto the playable ASCII .eng.srt target — never a
// destination carrying the fullwidth spelling of the tag.
func TestOrganize_FullwidthVideoFullwidthLanguageTagSubtitle_MovedWithVideo(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ", []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｅｎｇ．ｓｒｔ", []byte("subs"), 0o644))

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

	exists, err = afero.Exists(fs, "/dest/RCT-156/RCT-156.eng.srt")
	require.NoError(t, err)
	assert.True(t, exists, "the fullwidth language-tagged subtitle moves onto the playable ASCII .eng.srt target")

	exists, err = afero.Exists(fs, "/dest/RCT-156/RCT-156.．ｅｎｇ.srt")
	require.NoError(t, err)
	assert.False(t, exists, "no destination carries the fullwidth spelling of the language tag")

	exists, err = afero.Exists(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｅｎｇ．ｓｒｔ")
	require.NoError(t, err)
	assert.False(t, exists, "the raw-spelled source subtitle is removed by the move, not left behind")

	require.Len(t, result.Subtitles, 1, "move_subtitles records the discovered sidecar")
	assert.True(t, result.Subtitles[0].Moved, "the subtitle install records a move")
	assert.Equal(t, filepath.ToSlash("/source/ＲＣＴ－１５６－ＨＤ．ｅｎｇ．ｓｒｔ"), filepath.ToSlash(result.Subtitles[0].OriginalPath),
		"the source removal names the real raw on-disk filename")
	assert.Equal(t, filepath.ToSlash("/dest/RCT-156/RCT-156.eng.srt"), filepath.ToSlash(result.Subtitles[0].NewPath),
		"the destination derives from the folded language tag under the folded sidecar stem")
}
