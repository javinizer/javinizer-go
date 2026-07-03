package batch

import (
	"encoding/json"
	"math/rand"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestBroadcastProgress_NilRuntime(t *testing.T) {
	// Should not panic with nil runtime
	broadcastProgress(nil, nil)
}

func TestBroadcastProgress_RuntimeWithNilHub(t *testing.T) {
	runtime := core.NewRuntimeState()
	// Should not panic - runtime has no WebSocketHub
	broadcastProgress(runtime, nil)
}

func TestOrganizeProgressPercent(t *testing.T) {
	// Guards the 0-100 scale the frontend OrganizeStatusCard renders. A regression
	// to 0-1 (or removing the *100) would make the bar show 0%-1% for the whole
	// run and snap to 100% only on the terminal broadcast — the original bug.
	tests := []struct {
		name      string
		processed int
		total     int
		want      float64
	}{
		{"empty", 0, 0, -1},
		{"zero total", 3, 0, -1},
		{"negative total", 1, -1, -1},
		{"none processed", 0, 5, 0},
		{"midway", 3, 5, 60},
		{"all processed", 5, 5, 100},
		{"over-count clamped", 6, 5, 100},
		{"single file done", 1, 1, 100},
		{"negative processed clamped to 0", -2, 5, 0},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.want, organizeProgressPercent(tc.processed, tc.total))
		})
	}
}

func TestBuildOrganizeProgressMessage(t *testing.T) {
	// Guards the wire-format contract the frontend depends on: Status must be
	// non-terminal ProgressStatusPending (so handleWebSocketMessage only updates
	// the bar, not the organized/failed/completed branches), Progress must be on
	// the 0-100 scale, JobID must be set, and the message must name the action
	// verb and processed/total counts. A regression to a terminal status, a
	// dropped/0-1 Progress, a swapped processed/total, or the wrong isUpdate verb
	// would NOT be caught by TestOrganizeProgressPercent (pure math) — this is
	// the only test that asserts the assembled message.
	t.Run("organize path", func(t *testing.T) {
		msg, ok := buildOrganizeProgressMessage("job-7", false, 3, 5)
		require.True(t, ok, "ok must be true for a real progress update")
		require.NotNil(t, msg)
		assert.Equal(t, "job-7", msg.JobID, "JobID must be set from the job")
		assert.Equal(t, websocket.ProgressStatusPending, msg.Status, "Status must be non-terminal pending")
		assert.Equal(t, 60.0, msg.Progress, "Progress must be on the 0-100 scale")
		assert.Equal(t, "Organized 3 of 5 files", msg.Message, "Message must name the organize verb and counts")
	})

	t.Run("update path verb", func(t *testing.T) {
		msg, ok := buildOrganizeProgressMessage("job-9", true, 5, 5)
		require.True(t, ok)
		require.NotNil(t, msg)
		assert.Equal(t, 100.0, msg.Progress)
		assert.Equal(t, "Updated 5 of 5 files", msg.Message, "isUpdate must select the Updated verb")
		assert.Equal(t, websocket.ProgressStatusPending, msg.Status, "update progress is also non-terminal")
	})

	t.Run("total<=0 skips", func(t *testing.T) {
		msg, ok := buildOrganizeProgressMessage("job-x", false, 0, 0)
		assert.False(t, ok, "total<=0 must signal skip")
		assert.Nil(t, msg)
	})
}

func TestMakeOrganizeProgressBroadcaster_MessageContent(t *testing.T) {
	// MAJOR-T1 guard: drives the FULL broadcaster closure (build → high-water
	// check → sink) with a recording sink and asserts the exact message that
	// would reach the WS hub — JobID from the job, non-terminal Status, 0-100
	// Progress, and the organize verb + counts. A wired-but-broken closure
	// (no-op body, wrong job reference, terminal status, swapped counts, wrong
	// verb) would fail here. This closes the gap left by the resolver-wiring
	// tests, which only assert the hook is non-nil.
	var got *websocket.ProgressMessage
	sink := func(m *websocket.ProgressMessage) { got = m }
	bcast := makeOrganizeProgressBroadcaster(&stubControlledJob{}, false, sink)
	bcast(3, 5)
	require.NotNil(t, got, "a real progress update must reach the sink")
	assert.Equal(t, "stub-job", got.JobID, "JobID must come from the job")
	assert.Equal(t, websocket.ProgressStatusPending, got.Status, "Status must be non-terminal pending")
	assert.Equal(t, 60.0, got.Progress, "Progress must be on the 0-100 scale")
	assert.Equal(t, "Organized 3 of 5 files", got.Message, "Message must name the organize verb and counts")
}

func TestMakeOrganizeProgressBroadcaster_UpdateVerb(t *testing.T) {
	// The isUpdate=true path must select the "Updated" verb end-to-end through
	// the closure (buildOrganizeProgressMessage alone is tested separately).
	var got *websocket.ProgressMessage
	sink := func(m *websocket.ProgressMessage) { got = m }
	bcast := makeOrganizeProgressBroadcaster(&stubControlledJob{}, true, sink)
	bcast(5, 5)
	require.NotNil(t, got)
	assert.Equal(t, "Updated 5 of 5 files", got.Message)
	assert.Equal(t, 100.0, got.Progress)
	assert.Equal(t, websocket.ProgressStatusPending, got.Status)
}

func TestMakeOrganizeProgressBroadcaster_DropsRegressions(t *testing.T) {
	// MAJOR-C1/M2 guard: the broadcaster must drop non-increasing processed
	// counts so a bar regression never reaches the sink. Drives the closure with
	// out-of-order delivery: 3 (accept, 60%), 2 (drop), 5 (accept, 100%), 4
	// (drop), 5 again (drop — not strictly higher). Asserts the SINK receives
	// exactly [60%, 100%] — proving the high-water check is wired inside the
	// closure (removing it would let 40%/80% through and fail this).
	var recorded []float64
	sink := func(m *websocket.ProgressMessage) { recorded = append(recorded, m.Progress) }
	bcast := makeOrganizeProgressBroadcaster(&stubControlledJob{}, false, sink)
	bcast(3, 5) // 60% accept
	bcast(2, 5) // 40% drop
	bcast(5, 5) // 100% accept
	bcast(4, 5) // 80% drop
	bcast(5, 5) // equal drop
	assert.Equal(t, []float64{60.0, 100.0}, recorded, "only strictly-increasing counts reach the sink")
}

func TestMakeOrganizeProgressBroadcaster_ConcurrentMonotonicDelivery(t *testing.T) {
	// MAJOR-C1 guard: under concurrency the broadcaster must deliver STRICTLY
	// increasing progress to the sink — the mutex serializes the high-water check
	// and the sink call, so a higher count can never be emitted before a lower
	// one. (An atomic-only filter would NOT guarantee this: the filter can win
	// while a concurrent higher count is already in flight to the sink.) Launch
	// goroutines with processed 1..n in scrambled order; assert recorded
	// progresses are strictly increasing and end at 100%. A broken impl that
	// dropped the mutex (e.g. reverted to a racy filter) would let a regression
	// through and fail the strictly-increasing assertion. Run under -race.
	var mu sync.Mutex
	var recorded []float64
	sink := func(m *websocket.ProgressMessage) {
		mu.Lock()
		recorded = append(recorded, m.Progress)
		mu.Unlock()
	}
	bcast := makeOrganizeProgressBroadcaster(&stubControlledJob{}, false, sink)
	const n = 50
	order := rand.Perm(n) // 0..n-1, scrambled dispatch order
	var wg sync.WaitGroup
	for _, v := range order {
		wg.Add(1)
		go func(processed int) {
			defer wg.Done()
			bcast(processed, n) // processed is 1..n
		}(v + 1)
	}
	wg.Wait()
	require.NotEmpty(t, recorded, "at least the max (n) must always be emitted")
	for i := 1; i < len(recorded); i++ {
		assert.Greater(t, recorded[i], recorded[i-1],
			"delivered progress must be strictly increasing (no regression under concurrency)")
	}
	assert.Equal(t, 100.0, recorded[len(recorded)-1], "final delivery must be 100%% (the max count)")
}

func TestMakeOrganizeProgressBroadcaster_TotalZeroIsNoOp(t *testing.T) {
	// total<=0 must early-return before the sink, so nothing is recorded and the
	// job is never dereferenced for its ID beyond buildOrganizeProgressMessage's
	// guard (which returns first on pct<0). Uses a sink that would fail the test
	// if reached.
	sink := func(*websocket.ProgressMessage) {
		t.Fatal("sink must not be called for total<=0")
	}
	bcast := makeOrganizeProgressBroadcaster(&stubControlledJob{}, false, sink)
	bcast(0, 0) // total<=0: buildOrganizeProgressMessage returns (nil,false) → early return
}

func TestResolveOrganizeApplyConfig_WiresProgressHooks(t *testing.T) {
	// MAJOR-T2 guard: resolveOrganizeApplyConfig must set BOTH OnFileProgress
	// (incremental bar) and OnPhaseComplete (terminal 100%) on the returned
	// ApplyPhaseConfig. If a future refactor drops the OnFileProgress assignment
	// on this branch, the bar reverts to jumping 0→100 — the original bug — with
	// no other test failing. Uses in-place mode to skip the organize-mode
	// destination/dir-access checks (which would need a real filesystem), so the
	// resolver runs with a nil-deps runtime + a stub job + a real factory whose
	// NewApplyConfig only assembles the struct.
	rt := core.NewAPIRuntime(nil) // nil deps: in-place path never dereferences deps during resolution
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	applyOpts, err := resolveOrganizeApplyConfig(core.NewSnapshotForTesting(rt, core.APIConfig{}), factory, job, contracts.OrganizeRequest{
		Destination:   "", // in-place needs no destination
		OperationMode: string(operationmode.OperationModeInPlace),
	})
	require.NoError(t, err)
	assert.NotNil(t, applyOpts.OnFileProgress, "OnFileProgress must be wired so the bar advances per file")
	assert.NotNil(t, applyOpts.OnPhaseComplete, "OnPhaseComplete must be wired for the terminal 100% signal")
	assert.NotNil(t, applyOpts.PostApplyFunc, "PostApplyFunc must be wired for per-file event emission")

	// Invoking the hooks must not panic with the nil-hub runtime (broadcastProgress
	// no-ops on a nil hub) — proves the wired closures are well-formed end-to-end.
	assert.NotPanics(t, func() {
		applyOpts.OnFileProgress(1, 2)
		applyOpts.OnPhaseComplete(1, 0)
	})
}

func TestResolveUpdateApplyConfig_WiresProgressHooks(t *testing.T) {
	// MAJOR-T2 guard for the update branch: resolveUpdateApplyConfig must set
	// BOTH OnFileProgress and OnPhaseComplete. The update path has no
	// destination/dir-access checks, so it runs cleanly with a nil-deps runtime.
	rt := core.NewAPIRuntime(nil)
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	job := &stubControlledJob{}

	applyOpts, err := resolveUpdateApplyConfig(core.NewSnapshotForTesting(rt, core.APIConfig{}), factory, job, contracts.UpdateRequest{})
	require.NoError(t, err)
	assert.NotNil(t, applyOpts.OnFileProgress, "OnFileProgress must be wired on the update path too")
	assert.NotNil(t, applyOpts.OnPhaseComplete, "OnPhaseComplete must be wired on the update path too")
	assert.NotNil(t, applyOpts.PostApplyFunc, "PostApplyFunc must be wired for per-file event emission")

	assert.NotPanics(t, func() {
		applyOpts.OnFileProgress(1, 1)
		applyOpts.OnPhaseComplete(1, 0)
	})
}

func TestMakeOrganizeCompleteBroadcaster_MessageContent(t *testing.T) {
	// arch MINOR-2 enabled sink injection on the complete broadcaster too. Guard
	// its wire format through the closure: status is the terminal
	// organization_completed/update_completed, progress is 100, JobID from the
	// job, and the message names the action + counts. A regression to a
	// non-terminal status, wrong progress, or wrong isUpdate status string fails.
	t.Run("organize completion", func(t *testing.T) {
		var got *websocket.ProgressMessage
		sink := func(m *websocket.ProgressMessage) { got = m }
		bcast := makeOrganizeCompleteBroadcaster(&stubControlledJob{}, false, sink)
		bcast(7, 2)
		require.NotNil(t, got)
		assert.Equal(t, "stub-job", got.JobID)
		assert.Equal(t, websocket.ProgressStatusOrganizeCompleted, got.Status)
		assert.Equal(t, 100.0, got.Progress)
		assert.Equal(t, "Organized 7 files, 2 failed", got.Message)
	})
	t.Run("update completion", func(t *testing.T) {
		var got *websocket.ProgressMessage
		sink := func(m *websocket.ProgressMessage) { got = m }
		bcast := makeOrganizeCompleteBroadcaster(&stubControlledJob{}, true, sink)
		bcast(3, 0)
		require.NotNil(t, got)
		assert.Equal(t, websocket.ProgressStatusUpdateCompleted, got.Status)
		assert.Equal(t, "Updated 3 files, 0 failed", got.Message)
	})
}

func TestResolveOrganizeApplyConfig_OnFileProgressBroadcastsToHub(t *testing.T) {
	// tests MINOR-1 definitive closure: prove the resolver wires a REAL
	// broadcasting sink (newOrganizeBroadcastSink → broadcastProgress → hub), not
	// a no-op, by driving the resolved OnFileProgress through a running WS hub to
	// a real client connection and asserting the delivered JSON. A no-op sink, a
	// sink wired to the wrong runtime, or a broken broadcastProgress path all fail
	// here (the client would receive nothing and ReadMessage would time out). The
	// organize path is exercised; the update path shares the same sink helper.
	rt := core.NewAPIRuntime(nil) // nil deps: in-place resolution never dereferences deps
	rt.Runtime = core.NewRuntimeState()
	hub := rt.Runtime.ResetWebSocketHub() // starts a running hub
	t.Cleanup(func() {
		rt.Runtime.Shutdown()
	})
	serverConn, clientConn, srv := newTestWSConn(t)
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		srv.Close()
	})
	registerClientOnHub(t, hub, serverConn, clientConn)

	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	applyOpts, err := resolveOrganizeApplyConfig(core.NewSnapshotForTesting(rt, core.APIConfig{}), factory, &stubControlledJob{}, contracts.OrganizeRequest{
		OperationMode: string(operationmode.OperationModeInPlace),
	})
	require.NoError(t, err)
	require.NotNil(t, applyOpts.OnFileProgress, "OnFileProgress must be wired")

	// Drive the resolved hook: 3 of 5 files → 60% pending.
	applyOpts.OnFileProgress(3, 5)

	// The client must receive the broadcast progress message.
	require.NoError(t, clientConn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, data, err := clientConn.ReadMessage()
	require.NoError(t, err, "client must receive the OnFileProgress broadcast (a no-op sink would time out here)")
	var msg websocket.ProgressMessage
	require.NoError(t, json.Unmarshal(data, &msg))
	assert.Equal(t, "stub-job", msg.JobID, "JobID must be the job's id, proving the sink is wired to the right job/runtime")
	assert.Equal(t, websocket.ProgressStatusPending, msg.Status, "intermediate progress is non-terminal pending")
	assert.Equal(t, 60.0, msg.Progress, "progress must reach the hub on the 0-100 scale")
	assert.Contains(t, msg.Message, "3 of 5", "message must carry the processed/total counts")
}

func TestResolveUpdateApplyConfig_OnFileProgressBroadcastsToHub(t *testing.T) {
	// tests MINOR-2 closure: the update resolver (resolveUpdateApplyConfig)
	// wires the SAME real broadcasting sink as the organize resolver, not a
	// no-op. Mirrors TestResolveOrganizeApplyConfig_OnFileProgressBroadcastsToHub
	// but drives resolveUpdateApplyConfig. A regression that wired the update
	// branch's OnFileProgress to a no-op sink (or the wrong runtime) would leave
	// the client receiving nothing and ReadMessage would time out here. The
	// update path differs from organize only in the terminal status string
	// (update_completed vs organization_completed), which is unit-tested
	// separately in TestMakeOrganizeCompleteBroadcaster_MessageContent; this
	// test pins the hub-forwarding seam for the update resolver specifically.
	rt := core.NewAPIRuntime(nil) // nil deps: update resolution never dereferences deps for the hook
	rt.Runtime = core.NewRuntimeState()
	hub := rt.Runtime.ResetWebSocketHub() // starts a running hub
	t.Cleanup(func() {
		rt.Runtime.Shutdown()
	})
	serverConn, clientConn, srv := newTestWSConn(t)
	t.Cleanup(func() {
		_ = clientConn.Close()
		_ = serverConn.Close()
		srv.Close()
	})
	registerClientOnHub(t, hub, serverConn, clientConn)

	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	applyOpts, err := resolveUpdateApplyConfig(core.NewSnapshotForTesting(rt, core.APIConfig{}), factory, &stubControlledJob{}, contracts.UpdateRequest{})
	require.NoError(t, err)
	require.NotNil(t, applyOpts.OnFileProgress, "OnFileProgress must be wired on the update path")

	// Drive the resolved hook: 2 of 5 files → 40% pending.
	applyOpts.OnFileProgress(2, 5)

	// The client must receive the broadcast progress message.
	require.NoError(t, clientConn.SetReadDeadline(time.Now().Add(2*time.Second)))
	_, data, err := clientConn.ReadMessage()
	require.NoError(t, err, "client must receive the update-path OnFileProgress broadcast (a no-op sink would time out here)")
	var msg websocket.ProgressMessage
	require.NoError(t, json.Unmarshal(data, &msg))
	assert.Equal(t, "stub-job", msg.JobID, "JobID must be the job's id, proving the update sink is wired to the right job/runtime")
	assert.Equal(t, websocket.ProgressStatusPending, msg.Status, "intermediate progress is non-terminal pending")
	assert.Equal(t, 40.0, msg.Progress, "progress must reach the hub on the 0-100 scale")
	assert.Contains(t, msg.Message, "2 of 5", "message must carry the processed/total counts")
}
