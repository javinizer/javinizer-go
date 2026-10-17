package organizer

// codex P2 (PR #257, round 37): a video the scanner admits with a fullwidth
// spelling (ＲＣＴ－１５６－ＨＤ．ｍｋｖ) can sit beside an equally
// fullwidth-written sidecar (ＲＣＴ－１５６－ＨＤ．ｓｒｔ), but isSubtitleFile
// derived the subtitle extension with filepath.Ext — an ASCII-only scan that
// does not recognize the fullwidth dot — so the sidecar never even reached
// the round-36b folded-stem comparison and move_subtitles left it behind.
// The admission check (and the stem/language splits downstream of it) now
// folds the fullwidth spelling first, mirroring the round-30 contract:
// folded for classification, raw path/name for I/O — the match carries the
// RAW subtitle path so the move and its source removal operate on the real
// on-disk file, while the destination keeps the folded extension spelling
// (a playable ASCII .srt target under the round-32a/36b folded sidecar
// stem).

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

// TestSubtitleHandler_FindSubtitles_FullwidthDotSubtitle pins the finding at
// the discovery level: the fullwidth-dot sidecar is admitted through the
// folded extension, keeps its raw on-disk path, and neither the fullwidth
// suffix nor a fullwidth-dot non-subtitle leaks into the match set.
func TestSubtitleHandler_FindSubtitles_FullwidthDotSubtitle(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ", []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｓｒｔ", []byte("subs"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｔｘｔ", []byte("note"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５７．ｓｒｔ", []byte("subs"), 0o644))

	handler := newSubtitleHandler(fs, []string{".srt"})
	matches := handler.FindSubtitles(fullwidthStemMatch("/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ"))

	require.Len(t, matches, 1, "the fullwidth-dot sidecar is admitted through the folded extension; the ．ｔｘｔ non-subtitle and the unrelated ＲＣＴ－１５７．ｓｒｔ are not")

	m := matches[0]
	assert.Equal(t, filepath.ToSlash("/source/ＲＣＴ－１５６－ＨＤ．ｓｒｔ"), filepath.ToSlash(m.OriginalPath),
		"the match carries the raw on-disk spelling — file I/O never sees a folded rewriting")
	assert.Equal(t, "", m.Language, "the fullwidth suffix must not leak into the language field")
	assert.Equal(t, ".srt", m.Extension, "the extension is carried in its folded (classification) spelling")
}

// TestOrganize_FullwidthVideoFullwidthDotSubtitle_MovedWithVideo pins the
// finding end to end: the fullwidth-written sidecar is discovered AND moved
// with the fullwidth video — the source removal takes the real raw filename,
// and the destination lands on the playable ASCII .srt target the
// round-32a/36b folded sidecar stem derives.
func TestOrganize_FullwidthVideoFullwidthDotSubtitle_MovedWithVideo(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, fs.MkdirAll("/source", 0o755))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｍｋｖ", []byte("video"), 0o644))
	require.NoError(t, afero.WriteFile(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｓｒｔ", []byte("subs"), 0o644))

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
	assert.True(t, exists, "the fullwidth subtitle moves with the video onto a playable ASCII .srt target")

	exists, err = afero.Exists(fs, "/source/ＲＣＴ－１５６－ＨＤ．ｓｒｔ")
	require.NoError(t, err)
	assert.False(t, exists, "the raw-spelled source subtitle is removed by the move, not left behind")

	require.Len(t, result.Subtitles, 1, "move_subtitles records the discovered sidecar")
	assert.True(t, result.Subtitles[0].Moved, "the subtitle install records a move")
	assert.Equal(t, filepath.ToSlash("/source/ＲＣＴ－１５６－ＨＤ．ｓｒｔ"), filepath.ToSlash(result.Subtitles[0].OriginalPath),
		"the source removal names the real raw on-disk filename")
	assert.Equal(t, filepath.ToSlash("/dest/RCT-156/RCT-156.srt"), filepath.ToSlash(result.Subtitles[0].NewPath),
		"the destination keeps the folded extension spelling under the round-32a/36b folded sidecar stem")
}
