package batch

import (
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// These tests pin the WS-protocol enrichment (Part A) and the verbose organize
// per-file start message (Part B) that together make the Home "Current Activity"
// bar monotonic + verbose. The iter-6 MAJOR regression (revert 30e6e53f) pegged
// the organize bar at 100% after the first file because the frontend inferred
// totals from message counts; the backend now stamps AUTHORITATIVE
// total_files/completed/failed on every ProgressMessage (stampJobCounts) and
// emits a non-terminal "Organizing <file>" pending message per file.

func TestStampJobCounts_FromGetStatus(t *testing.T) {
	// The helper reads authoritative TotalFiles/Completed/Failed from
	// job.GetStatus() (a lock-protected snapshot) and stamps them on the message.
	// Frontend consumers depend on these instead of inferring totals from message
	// counts — the root cause of the iter-6 MAJOR.
	job := &stubControlledJob{status: &worker.BatchJobStatus{}}
	job.status.TotalFiles = 10
	job.status.Completed = 3
	job.status.Failed = 1

	msg := stampJobCounts(&websocket.ProgressMessage{JobID: "stub-job"}, job)
	require.NotNil(t, msg)
	assert.Equal(t, 10, msg.TotalFiles, "TotalFiles must come from job.GetStatus()")
	assert.Equal(t, 3, msg.Completed, "Completed must come from job.GetStatus()")
	assert.Equal(t, 1, msg.Failed, "Failed must come from job.GetStatus()")
}

func TestStampJobCounts_NilSafe(t *testing.T) {
	// nil message and nil job must not panic; a nil status snapshot leaves zeros.
	assert.NotPanics(t, func() {
		assert.Nil(t, stampJobCounts(nil, &stubControlledJob{}))
		assert.NotNil(t, stampJobCounts(&websocket.ProgressMessage{JobID: "j"}, nil))
	})
	msg := stampJobCounts(&websocket.ProgressMessage{JobID: "j"}, &stubControlledJob{}) // status nil
	assert.Equal(t, 0, msg.TotalFiles)
	assert.Equal(t, 0, msg.Completed)
	assert.Equal(t, 0, msg.Failed)
}

func TestMakeOrganizeFileStartBroadcaster_MessageContent(t *testing.T) {
	// Verbose organize progress (Part B): the per-file start broadcaster emits a
	// NON-terminal 'pending' message with Progress 0 and the file basename in the
	// message, so the Home "Current Activity" card shows which file is being
	// organized. Progress MUST be 0 (never 100): the in-flight pending row enters
	// messagesByFile and counts in computeJobProgress's activeProgress
	// (contributing 0), then is OVERWRITTEN by the terminal organized/failed
	// message on completion (dedup-latest by file_path) — the certified
	// double-count-safe pattern. A Progress:100 here would falsely peg the bar.
	t.Run("organize verb + basename", func(t *testing.T) {
		var got *websocket.ProgressMessage
		sink := func(m *websocket.ProgressMessage) { got = m }
		bcast := makeOrganizeFileStartBroadcaster(&stubControlledJob{}, false, sink)
		bcast("/movies/movie-abc-123.mp4")
		require.NotNil(t, got)
		assert.Equal(t, "stub-job", got.JobID)
		assert.Equal(t, "/movies/movie-abc-123.mp4", got.FilePath, "FilePath must be the source path")
		assert.Equal(t, websocket.ProgressStatusPending, got.Status, "must be non-terminal pending")
		assert.Equal(t, 0.0, got.Progress, "must be Progress:0 (NEVER 100)")
		assert.Contains(t, got.Message, "movie-abc-123.mp4", "message must contain the basename")
		assert.Contains(t, got.Message, "Organizing", "organize mode must use the Organizing verb")
	})

	t.Run("update verb", func(t *testing.T) {
		var got *websocket.ProgressMessage
		sink := func(m *websocket.ProgressMessage) { got = m }
		bcast := makeOrganizeFileStartBroadcaster(&stubControlledJob{}, true, sink)
		bcast("/movies/movie-xyz-7.mp4")
		require.NotNil(t, got)
		assert.Equal(t, websocket.ProgressStatusPending, got.Status)
		assert.Equal(t, 0.0, got.Progress)
		assert.Contains(t, got.Message, "movie-xyz-7.mp4")
		assert.Contains(t, got.Message, "Updating", "update mode must use the Updating verb")
	})
}

func TestMakeOrganizeFileStartBroadcaster_StampsJobCounts(t *testing.T) {
	// The start broadcaster must also stamp authoritative job-level counts so the
	// Home page (which scans wsState.messages backwards for the latest
	// total_files-bearing message) can derive totals from ANY latest message.
	var got *websocket.ProgressMessage
	sink := func(m *websocket.ProgressMessage) { got = m }
	job := &stubControlledJob{status: &worker.BatchJobStatus{}}
	job.status.TotalFiles = 10
	job.status.Completed = 2
	job.status.Failed = 0
	bcast := makeOrganizeFileStartBroadcaster(job, false, sink)
	bcast("/movies/f3.mp4")
	require.NotNil(t, got)
	assert.Equal(t, 10, got.TotalFiles)
	assert.Equal(t, 2, got.Completed)
	assert.Equal(t, 0, got.Failed)
}

func TestMakeOrganizeProgressBroadcaster_StampsJobCounts(t *testing.T) {
	// The aggregate progress broadcaster must stamp authoritative counts too —
	// its job-level overall-% message (no FilePath) is the primary carrier of
	// total_files for organize, since per-file terminal messages alone would let
	// the Home page infer totals from message counts (the MAJOR).
	var got *websocket.ProgressMessage
	sink := func(m *websocket.ProgressMessage) { got = m }
	job := &stubControlledJob{status: &worker.BatchJobStatus{}}
	job.status.TotalFiles = 10
	job.status.Completed = 4
	job.status.Failed = 1
	bcast := makeOrganizeProgressBroadcaster(job, false, sink)
	bcast(5, 10)
	require.NotNil(t, got)
	assert.Equal(t, 10, got.TotalFiles)
	assert.Equal(t, 4, got.Completed)
	assert.Equal(t, 1, got.Failed)
}

func TestResolveOrganizeApplyConfig_WiresOnFileOrganizeStart(t *testing.T) {
	// Part B wiring guard: resolveOrganizeApplyConfig must wire OnFileOrganizeStart
	// so the per-file "Organizing <file>" message is broadcast. A future refactor
	// that drops the assignment would silently revert organize to non-verbose
	// (only the aggregate "Organized N of M") with no other test failing.
	rt := core.NewAPIRuntime(nil) // nil deps: in-place resolution never dereferences deps
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	applyOpts, err := resolveOrganizeApplyConfig(rt, factory, job, contracts.OrganizeRequest{
		Destination:   "", // in-place needs no destination
		OperationMode: string(operationmode.OperationModeInPlace),
	})
	require.NoError(t, err)
	require.NotNil(t, applyOpts.OnFileOrganizeStart, "OnFileOrganizeStart must be wired for verbose organize")

	// Invoking the hook must not panic with the nil-hub runtime (broadcastProgress
	// no-ops on a nil hub) and must produce a well-formed pending message.
	assert.NotPanics(t, func() {
		applyOpts.OnFileOrganizeStart("/src/movie-1.mp4")
	})
}

func TestResolveUpdateApplyConfig_WiresOnFileOrganizeStart(t *testing.T) {
	// Part B wiring guard for the update branch.
	rt := core.NewAPIRuntime(nil) // nil deps: update resolution never dereferences deps
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	applyOpts, err := resolveUpdateApplyConfig(rt, factory, job, contracts.UpdateRequest{})
	require.NoError(t, err)
	require.NotNil(t, applyOpts.OnFileOrganizeStart, "OnFileOrganizeStart must be wired on the update path too")

	assert.NotPanics(t, func() {
		applyOpts.OnFileOrganizeStart("/src/movie-2.mp4")
	})
}
