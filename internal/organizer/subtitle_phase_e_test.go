package organizer

import (
	"context"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// subtitleEntryFixture builds an organizer with subtitle handling enabled and
// a source video + subtitle pair.
func subtitleEntryFixture(t *testing.T) (*Organizer, afero.Fs) {
	t.Helper()
	fs := afero.NewMemMapFs()
	cfg := &Config{
		FolderFormat:       "<ID>",
		FileFormat:         "<ID>",
		RenameFile:         true,
		OperationMode:      operationmode.OperationModeOrganize,
		MoveSubtitles:      true,
		SubtitleExtensions: []string{".srt"},
	}
	org := NewOrganizer(fs, cfg, nil, nil)
	require.NoError(t, fs.MkdirAll("/in", 0755))
	require.NoError(t, afero.WriteFile(fs, "/in/ABC-123.mkv", []byte("video"), 0644))
	require.NoError(t, afero.WriteFile(fs, "/in/ABC-123.srt", []byte("sub-source"), 0644))
	return org, fs
}

func subtitleEntryCmd(moveFiles, force bool) OrganizeCmd {
	return OrganizeCmd{
		Match:       models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/ABC-123.mkv", Name: "ABC-123.mkv", Extension: ".mkv"},
		Movie:       &models.Movie{ID: "ABC-123"},
		DestDir:     "/dest",
		MoveFiles:   moveFiles,
		LinkMode:    LinkModeNone,
		ForceUpdate: force,
	}
}

func TestOrganize_Subtitles_ModeAwareResultsAndSkipOnExists(t *testing.T) {
	entryPoints := []struct {
		name      string
		moveFiles bool
		wantMoved bool
		wantCopy  bool
	}{
		{"move entry point", true, true, false},
		{"copy entry point", false, false, true},
	}
	for _, ep := range entryPoints {
		t.Run(ep.name+" install", func(t *testing.T) {
			org, fs := subtitleEntryFixture(t)
			result, err := org.Organize(context.Background(), subtitleEntryCmd(ep.moveFiles, false))
			require.NoError(t, err)
			require.Len(t, result.Subtitles, 1)
			sr := result.Subtitles[0]
			assert.Equal(t, ep.wantMoved, sr.Moved)
			assert.Equal(t, ep.wantCopy, sr.Copied)
			assert.False(t, sr.Skipped)
			destSub := "/dest/ABC-123/ABC-123.srt"
			content, readErr := afero.ReadFile(fs, destSub)
			require.NoError(t, readErr)
			assert.Equal(t, []byte("sub-source"), content)
			_, srcErr := fs.Stat("/in/ABC-123.srt")
			if ep.moveFiles {
				assert.Error(t, srcErr, "move install relocates the source subtitle")
			} else {
				assert.NoError(t, srcErr, "copy install retains the source subtitle")
			}
		})

		t.Run(ep.name+" skip-on-exists is race-hardened", func(t *testing.T) {
			org, fs := subtitleEntryFixture(t)
			require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0755))
			require.NoError(t, afero.WriteFile(fs, "/dest/ABC-123/ABC-123.srt", []byte("foreign"), 0644))
			result, err := org.Organize(context.Background(), subtitleEntryCmd(ep.moveFiles, false))
			require.NoError(t, err, "subtitle skip-on-exists never fails the organize")
			require.Len(t, result.Subtitles, 1)
			sr := result.Subtitles[0]
			assert.True(t, sr.Skipped, "occupied subtitle destination skips through the guarded primitive")
			assert.False(t, sr.Moved)
			assert.False(t, sr.Copied)
			content, readErr := afero.ReadFile(fs, "/dest/ABC-123/ABC-123.srt")
			require.NoError(t, readErr)
			assert.Equal(t, []byte("foreign"), content, "the foreign subtitle is byte-preserved")
			srcContent, readErr := afero.ReadFile(fs, "/in/ABC-123.srt")
			require.NoError(t, readErr)
			assert.Equal(t, []byte("sub-source"), srcContent, "the skipped source subtitle is retained at its origin")
		})

		t.Run(ep.name+" authorization never reaches subtitle destinations", func(t *testing.T) {
			org, fs := subtitleEntryFixture(t)
			require.NoError(t, fs.MkdirAll("/dest/ABC-123", 0755))
			require.NoError(t, afero.WriteFile(fs, "/dest/ABC-123/ABC-123.srt", []byte("foreign"), 0644))
			require.NoError(t, afero.WriteFile(fs, "/dest/ABC-123/ABC-123.mkv", []byte("foreign-video"), 0644))
			result, err := org.Organize(context.Background(), subtitleEntryCmd(ep.moveFiles, true))
			require.NoError(t, err, "authorization replaces the video but not the subtitle")
			require.Len(t, result.Subtitles, 1)
			assert.True(t, result.Subtitles[0].Skipped, "ForceUpdate must not authorize-over a subtitle destination")
			content, readErr := afero.ReadFile(fs, "/dest/ABC-123/ABC-123.srt")
			require.NoError(t, readErr)
			assert.Equal(t, []byte("foreign"), content)
		})
	}
}

func TestOrganize_Subtitles_PlanOnlyInstallIsPlanned(t *testing.T) {
	org, _ := subtitleEntryFixture(t)
	plan, err := org.plan(
		models.FileMatchInfo{MovieID: "ABC-123", Path: "/in/ABC-123.mkv", Name: "ABC-123.mkv", Extension: ".mkv"},
		&models.Movie{ID: "ABC-123"}, "/dest", false, "")
	require.NoError(t, err)
	result := &OrganizeResult{}
	org.handleSubtitles(plan, result, subtitleInstall{})
	require.Len(t, result.Subtitles, 1)
	assert.True(t, result.Subtitles[0].Planned)
	assert.False(t, result.Subtitles[0].Moved)
	assert.False(t, result.Subtitles[0].Copied)
}
