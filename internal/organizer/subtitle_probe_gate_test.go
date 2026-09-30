package organizer

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// A subtitle destination occupied at PlanSubtitleMoves time is omitted from
// the armed intents. If the occupant then vacates before execution, the
// execute lane must REFUSE the now-vacant slot (fail-closed skip): installing
// there would leave a sidecar no durable recovery record covers.
func TestProbeOmittedEndpointRefusesVacatedInstall(t *testing.T) {
	for _, move := range []bool{true, false} {
		t.Run(map[bool]string{false: "copy", true: "move"}[move], func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "ABC-123.mkv")
			sub := filepath.Join(dir, "ABC-123.srt")
			require.NoError(t, os.WriteFile(src, []byte("video"), 0o600))
			require.NoError(t, os.WriteFile(sub, []byte("subtitle"), 0o600))
			org := NewOrganizer(afero.NewOsFs(), &Config{
				FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
				OperationMode: operationmode.OperationModeOrganize,
				MoveSubtitles: true, SubtitleExtensions: []string{".srt"},
			}, nil, nil)
			dest := filepath.Join(dir, "out")
			subTarget := filepath.Join(dest, "ABC-123", "ABC-123.srt")
			require.NoError(t, os.MkdirAll(filepath.Dir(subTarget), 0o755))
			require.NoError(t, os.WriteFile(subTarget, []byte("foreign"), 0o644))

			plan, planErr := org.PlanOrganize(context.Background(), OrganizeCmd{
				Match: models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: "ABC-123.mkv", Extension: ".mkv"},
				Movie: &models.Movie{ID: "ABC-123"}, DestDir: dest, MoveFiles: move, LinkMode: LinkModeNone,
			})
			require.NoError(t, planErr)
			assert.Empty(t, org.PlanSubtitleMoves(plan), "occupied endpoint omitted from the intent plan")

			require.NoError(t, os.Remove(subTarget), "the occupant vacates before execution")
			result, err := org.ExecuteOrganizePlan(plan, move, LinkModeNone)
			require.NoError(t, err)
			require.Len(t, result.Subtitles, 1)
			sr := result.Subtitles[0]
			assert.True(t, sr.Skipped, "the probe-omitted endpoint refuses the vacated slot")
			assert.False(t, sr.Moved || sr.Copied, "no install the journal never armed")
			require.NoFileExists(t, subTarget, "nothing lands at the unjournaled endpoint")
			require.FileExists(t, sub, "the refused install never consumes the source")
			require.FileExists(t, result.NewPath, "the video leg is unaffected by the subtitle gate")
		})
	}
}

// Enumerated (vacant-at-probe) endpoints install exactly as before — the gate
// only locks out endpoints the probe omitted, never the journaled plan.
func TestProbeVacantEndpointStillInstalls(t *testing.T) {
	for _, move := range []bool{true, false} {
		t.Run(map[bool]string{false: "copy", true: "move"}[move], func(t *testing.T) {
			dir := t.TempDir()
			src := filepath.Join(dir, "ABC-123.mkv")
			sub := filepath.Join(dir, "ABC-123.srt")
			require.NoError(t, os.WriteFile(src, []byte("video"), 0o600))
			require.NoError(t, os.WriteFile(sub, []byte("subtitle"), 0o600))
			org := NewOrganizer(afero.NewOsFs(), &Config{
				FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
				OperationMode: operationmode.OperationModeOrganize,
				MoveSubtitles: true, SubtitleExtensions: []string{".srt"},
			}, nil, nil)
			dest := filepath.Join(dir, "out")
			plan, planErr := org.PlanOrganize(context.Background(), OrganizeCmd{
				Match: models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: "ABC-123.mkv", Extension: ".mkv"},
				Movie: &models.Movie{ID: "ABC-123"}, DestDir: dest, MoveFiles: move, LinkMode: LinkModeNone,
			})
			require.NoError(t, planErr)
			moves := org.PlanSubtitleMoves(plan)
			require.Len(t, moves, 1, "vacant endpoint enumerates into the intent plan")

			result, err := org.ExecuteOrganizePlan(plan, move, LinkModeNone)
			require.NoError(t, err)
			require.Len(t, result.Subtitles, 1)
			assert.Equal(t, move, result.Subtitles[0].Moved)
			assert.Equal(t, !move, result.Subtitles[0].Copied)
			assert.False(t, result.Subtitles[0].Skipped)
			got, readErr := os.ReadFile(moves[0].NewPath)
			require.NoError(t, readErr)
			assert.Equal(t, "subtitle", string(got))
		})
	}
}

// The gate is scoped to plans that were actually probed: an un-probed plan
// (the non-journaling direct flow) keeps its historical install behavior into
// a just-vacated endpoint.
func TestUnprobedPlanKeepsVacatedInstallBehavior(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "ABC-123.mkv")
	sub := filepath.Join(dir, "ABC-123.srt")
	require.NoError(t, os.WriteFile(src, []byte("video"), 0o600))
	require.NoError(t, os.WriteFile(sub, []byte("subtitle"), 0o600))
	org := NewOrganizer(afero.NewOsFs(), &Config{
		FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true,
		OperationMode: operationmode.OperationModeOrganize,
		MoveSubtitles: true, SubtitleExtensions: []string{".srt"},
	}, nil, nil)
	dest := filepath.Join(dir, "out")
	subTarget := filepath.Join(dest, "ABC-123", "ABC-123.srt")
	require.NoError(t, os.MkdirAll(filepath.Dir(subTarget), 0o755))
	require.NoError(t, os.WriteFile(subTarget, []byte("foreign"), 0o644))
	plan, planErr := org.PlanOrganize(context.Background(), OrganizeCmd{
		Match: models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: "ABC-123.mkv", Extension: ".mkv"},
		Movie: &models.Movie{ID: "ABC-123"}, DestDir: dest, MoveFiles: true, LinkMode: LinkModeNone,
	})
	require.NoError(t, planErr)
	require.NoError(t, os.Remove(subTarget))
	result, err := org.ExecuteOrganizePlan(plan, true, LinkModeNone)
	require.NoError(t, err)
	require.Len(t, result.Subtitles, 1)
	assert.True(t, result.Subtitles[0].Moved, "un-probed plans install into vacated endpoints as before")
	got, readErr := os.ReadFile(subTarget)
	require.NoError(t, readErr)
	assert.Equal(t, "subtitle", string(got))
}
