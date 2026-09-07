package commandutil

// PR #249 codex P2 — canceled-error lane, CLI equivalent of the API resolvers'
// suppression (internal/api/batch/apply_config_builder_canceled_w249_test.go).
// interpretApplyResult (worker/apply_phase.go) records an apply error matching
// errors.Is(err, context.Canceled) as Cancelled, NOT failed, so the CLI hook
// must not persist an "Organize failed"/"Update failed" SeverityError row for
// it — the audit trail would mislabel a cancelled item as a retriable failure.
// Buffered warnings STILL surface (console AND eventlog): they are truthful
// evidence of work done before the cancellation landed.

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

// TestCliBatchPostApply_CanceledApplyError_NoFailureEvent_WarningsStillRecorded
// pins the organize-mode canceled lane: no failure event, the buffered warning
// still gets its own audit row, and the console line still prints — the crumb
// must reach every surface that a real failure would have told.
func TestCliBatchPostApply_CanceledApplyError_NoFailureEvent_WarningsStillRecorded(t *testing.T) {
	var (
		emitter   = &recordingEmitter{}
		buf       bytes.Buffer
		skipCount = &atomic.Int64{}
	)
	hook := cliBatchPostApply(emitter, &buf, "job-cancel-1", false, false, skipCount, &sync.Mutex{})

	warning := "overwrite authorized: replaced existing destination /dest/GOOD-701/GOOD-701.mp4"
	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: filepath.Join("src", "GOOD-701.mp4"), Movie: &models.Movie{ID: "GOOD-701"}},
		&worker.ApplyFileResult{
			Err: fmt.Errorf("organization aborted: %w", context.Canceled),
			Result: &workflow.ApplyResult{OrganizeResult: &organizer.OrganizeResult{
				NewPath:  filepath.Join("dest", "GOOD-701", "GOOD-701.mp4"),
				Warnings: []string{warning},
			}},
		},
	)

	events := emitter.snapshot()
	require.Len(t, events, 1, "exactly the warning event — cancellation suppresses the failure event")
	assert.Equal(t, models.SeverityWarn, events[0].Severity, "canceled applies never produce SeverityError rows")
	assert.Equal(t, "file_move", events[0].Source)
	assert.Contains(t, events[0].Message, warning)
	assert.Zero(t, skipCount.Load(), "a canceled lane never claimed a duplicate skip")
	assert.Contains(t, buf.String(), "⚠️")
	assert.Contains(t, buf.String(), warning, "the console warning surface still fires on the canceled lane")
}

// TestCliBatchPostApply_CanceledApplyError_NoCrumbs_NoEvents pins the
// organize-mode canceled lane with no result payload: the hook persists
// nothing at all — no failure event to mislabel the Cancelled file.
func TestCliBatchPostApply_CanceledApplyError_NoCrumbs_NoEvents(t *testing.T) {
	var (
		emitter = &recordingEmitter{}
		buf     bytes.Buffer
	)
	hook := cliBatchPostApply(emitter, &buf, "job-cancel-2", false, false, &atomic.Int64{}, &sync.Mutex{})

	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: "x", Movie: &models.Movie{ID: "GOOD-702"}},
		&worker.ApplyFileResult{Err: context.Canceled},
	)

	assert.Empty(t, emitter.snapshot(), "a canceled apply with no crumbs persists nothing")
	assert.Empty(t, buf.String())
}

// TestCliBatchPostApply_CanceledApplyError_UpdateMode_NoEvents pins the
// update-mode canceled lane: same taxonomy, nfo_gen vocabulary stays silent —
// the worker's Cancelled status is the only truth. The control invocation
// re-asserts that a real update failure still lands its failure event.
func TestCliBatchPostApply_CanceledApplyError_UpdateMode_NoEvents(t *testing.T) {
	var (
		emitter = &recordingEmitter{}
		buf     bytes.Buffer
	)
	hook := cliBatchPostApply(emitter, &buf, "job-cancel-3", false, true, &atomic.Int64{}, &sync.Mutex{})

	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: "x", Movie: &models.Movie{ID: "GOOD-703"}},
		&worker.ApplyFileResult{Err: fmt.Errorf("update aborted: %w", context.Canceled)},
	)
	assert.Empty(t, emitter.snapshot(), "a canceled update persists no failure row")

	hook(context.Background(),
		&worker.ApplyFileContext{FilePath: "y", Movie: &models.Movie{ID: "GOOD-704"}},
		&worker.ApplyFileResult{Err: fmt.Errorf("update failed: nfo write error")},
	)
	events := emitter.snapshot()
	require.Len(t, events, 1, "real update failures still land the failure event")
	assert.Equal(t, models.SeverityError, events[0].Severity)
	assert.Equal(t, "nfo_gen", events[0].Source)
	assert.Contains(t, events[0].Message, "Update failed for GOOD-704")
}
