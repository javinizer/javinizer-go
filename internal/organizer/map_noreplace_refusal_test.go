package organizer

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// mapNoReplaceRefusal translations (#224): occupancy → the existing conflict
// wording; unsupported-volume → distinct infrastructure error; anything else
// passes through untouched.
func TestMapNoReplaceRefusal(t *testing.T) {
	collision := mapNoReplaceRefusal(fmt.Errorf("wrapped: %w", fsutil.ErrPublishCollision), "/dst/x.mp4")
	require.Error(t, collision)
	assert.Contains(t, collision.Error(), "refusing to overwrite")
	assert.Contains(t, collision.Error(), "/dst/x.mp4")

	unsupported := mapNoReplaceRefusal(fmt.Errorf("wrapped: %w", fsutil.ErrPublishNoReplaceUnsupported), "/dst/x.mp4")
	require.Error(t, unsupported)
	assert.Contains(t, unsupported.Error(), "cannot express an atomic no-clobber write")
	assert.NotContains(t, unsupported.Error(), "refusing to overwrite")

	plain := errors.New("io failure")
	assert.ErrorIs(t, mapNoReplaceRefusal(plain, "/dst/x.mp4"), plain)

	// Completed-first: a post-publish cleanup refusal never lands on the
	// refusal labels even when it transitively carries them.
	completed := fmt.Errorf("%w: cleanup: %w", fsutil.ErrPublishCompleted, fmt.Errorf("vacate: %w", fsutil.ErrPublishNoReplaceUnsupported))
	done := mapNoReplaceRefusal(completed, "/dst/x.mp4")
	require.Error(t, done)
	// The completed case yields its OWN classification line as the message;
	// preserved refusal classes may appear inside the joined diagnostics.
	assert.True(t, strings.HasPrefix(done.Error(), "published to destination but source cleanup refused"))
	assert.NotContains(t, done.Error(), "refusing to overwrite")
}

// Subtitle refusal→skip: occupancy AND unsupported-volume publish refusals map
// to the skip leg (never an error, never an overwrite).
func TestHandleSubtitles_PublishRefusalMapsToSkip(t *testing.T) {
	for name, ferr := range map[string]error{
		"collision":   fmt.Errorf("publish: %w", fsutil.ErrPublishCollision),
		"unsupported": fmt.Errorf("publish: %w", fsutil.ErrPublishNoReplaceUnsupported),
		"realfailure": errors.New("disk read died"),
		"completed":   fmt.Errorf("%w: cleanup: %w", fsutil.ErrPublishCompleted, fsutil.ErrPublishCollision),
	} {
		t.Run(name, func(t *testing.T) {
			fs := afero.NewMemMapFs()
			require.NoError(t, fs.MkdirAll("/in", 0o755))
			require.NoError(t, afero.WriteFile(fs, "/in/ABC-123.mp4", []byte("v"), 0o644))
			require.NoError(t, afero.WriteFile(fs, "/in/ABC-123.srt", []byte("srt"), 0o644))

			o := NewOrganizer(fs, &Config{
				MoveSubtitles:      true,
				SubtitleExtensions: []string{".srt"},
			}, nil, nil)

			plan := &OrganizePlan{
				Match: models.FileMatchInfo{
					MovieID: "ABC-123",
					Path:    "/in/ABC-123.mp4", Name: "ABC-123.mp4", Extension: ".mp4",
				},
				SourcePath: "/in/ABC-123.mp4",
				TargetDir:  "/out",
				TargetFile: "ABC-123.mp4",
				TargetPath: "/out/ABC-123.mp4",
				WillMove:   true,
			}
			result := &OrganizeResult{}

			o.handleSubtitles(plan, result, func(afero.Fs, string, string) error { return ferr })

			require.Len(t, result.Subtitles, 1)
			sr := result.Subtitles[0].SubtitleMove
			if name == "realfailure" {
				// A genuine failure surfaces, no skip, no move.
				assert.Contains(t, result.Subtitles[0].Error.Error(), "failed to handle subtitle")
			} else if name == "completed" {
				// Post-publish ambiguity: bytes delivered BUT the source was
				// retained — recording a clean move would let revert journaling
				// (MoveBack of new→original) overwrite the retained source's
				// newer contents. It must surface as an error slot (#224 codex P2).
				assert.False(t, result.Subtitles[0].Moved)
				assert.False(t, result.Subtitles[0].Skipped)
				require.Error(t, result.Subtitles[0].Error)
				assert.Contains(t, result.Subtitles[0].Error.Error(), "both copies retained")
			} else {
				assert.True(t, result.Subtitles[0].Skipped)
				assert.False(t, sr.Moved)
				assert.Nil(t, result.Subtitles[0].Error)
			}
		})
	}
}
