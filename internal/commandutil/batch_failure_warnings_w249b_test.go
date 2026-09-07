package commandutil

// PR #249 codex follow-up (F4) — the CLI post-apply hook must surface an
// Err!=nil lane's OrganizeResult.Warnings on EVERY consumer: the console
// print, one eventlog entry per warning, and the failure event's context.
// The organizer now binds the force-overwrite displacement crumb at the
// destruction (F2) so failed organize results carry warnings the pre-F4 hook
// dropped behind its `afr.Err == nil` gate.

import (
	"bytes"
	"context"
	"fmt"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCliBatchPostApply_ApplyErrorWithWarnings_SurfacesEverywhere pins the
// failure-lane warning surface: a failed apply whose OrganizeResult carries
// the displacement crumb emits the failure event (with the warnings folded
// into its context), one warning event per entry, and the console print.
func TestCliBatchPostApply_ApplyErrorWithWarnings_SurfacesEverywhere(t *testing.T) {
	var (
		emitter   = &recordingEmitter{}
		buf       bytes.Buffer
		skipCount = &atomic.Int64{}
	)
	hook := cliBatchPostApply(emitter, &buf, "job-1", false, false, skipCount, &sync.Mutex{})

	warning := "overwrite authorized: replaced existing destination /dest/GOOD-700/GOOD-700.mp4"
	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: filepath.Join("src", "GOOD-700.mp4"), Movie: &models.Movie{ID: "GOOD-700"}},
		&worker.ApplyFileResult{
			Err: fmt.Errorf("organization failed: failed to create hard link: cross-device"),
			Result: &workflow.ApplyResult{
				OrganizeResult: &organizer.OrganizeResult{
					NewPath:  filepath.Join("dest", "GOOD-700", "GOOD-700.mp4"),
					Warnings: []string{warning},
				},
			},
		},
	)

	events := emitter.snapshot()
	require.Len(t, events, 2, "failure event + one warning event per crumb entry")
	assert.Equal(t, models.SeverityError, events[0].Severity)
	assert.Equal(t, "file_move", events[0].Source)
	assert.Contains(t, events[0].Message, "Organize failed for GOOD-700")
	assert.Equal(t, models.SeverityWarn, events[1].Severity)
	assert.Equal(t, "file_move", events[1].Source)
	assert.Equal(t, events[1].Message, "Organize warning for GOOD-700: "+warning)
	assert.Contains(t, buf.String(), "⚠️", "the failed lane's crumb prints to the console too")
	assert.Contains(t, buf.String(), warning)
	assert.Zero(t, skipCount.Load(), "a failed lane never claims the duplicate-skip accounting")
}

// TestCliBatchPostApply_ApplyErrorNilResult_StaysQuiet pins the failure lane
// that carries NO result: exactly the failure event, no prints, unchanged
// from the pre-F4 single-surface discipline.
func TestCliBatchPostApply_ApplyErrorNilResult_StaysQuiet(t *testing.T) {
	var (
		emitter = &recordingEmitter{}
		buf     bytes.Buffer
	)
	hook := cliBatchPostApply(emitter, &buf, "job-1", false, false, &atomic.Int64{}, &sync.Mutex{})

	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: "x", Movie: &models.Movie{ID: "GOOD-700"}},
		&worker.ApplyFileResult{Err: fmt.Errorf("disk full"), Result: &workflow.ApplyResult{}},
	)

	events := emitter.snapshot()
	require.Len(t, events, 1, "a failure without warnings keeps failures' single surface")
	assert.Equal(t, models.SeverityError, events[0].Severity)
	assert.Empty(t, buf.String())
}

// TestCliBatchPostApply_UpdateModeApplyErrorWithWarnings_SurfacesEverywhere
// pins the update taxonomy on the failure lane: same crumb surfacing with
// the nfo_gen source + "Update warning" vocabulary.
func TestCliBatchPostApply_UpdateModeApplyErrorWithWarnings_SurfacesEverywhere(t *testing.T) {
	var (
		emitter = &recordingEmitter{}
		buf     bytes.Buffer
	)
	hook := cliBatchPostApply(emitter, &buf, "job-1", false, true, &atomic.Int64{}, &sync.Mutex{})

	warning := "overwrite authorized: replaced existing destination /dest/GOOD-700/GOOD-700.mp4"
	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: "x", Movie: &models.Movie{ID: "GOOD-700"}},
		&worker.ApplyFileResult{
			Err:    fmt.Errorf("apply timed out"),
			Result: &workflow.ApplyResult{OrganizeResult: &organizer.OrganizeResult{Warnings: []string{warning}}},
		},
	)

	events := emitter.snapshot()
	require.Len(t, events, 2)
	assert.Equal(t, "nfo_gen", events[0].Source, "update mode keeps the nfo_gen taxonomy")
	assert.Contains(t, events[0].Message, "Update failed for GOOD-700")
	assert.Equal(t, "nfo_gen", events[1].Source)
	assert.Equal(t, events[1].Message, "Update warning for GOOD-700: "+warning)
	assert.Contains(t, buf.String(), warning)
}
