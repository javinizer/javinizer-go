package batch

import (
	"context"
	"encoding/json"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestWireApplyProgressCallbacks_ReturnsConfigGenerationReference(t *testing.T) {
	for _, isUpdate := range []bool{false, true} {
		var cfg worker.ApplyPhaseConfig
		var got *websocket.ProgressMessage
		ref := wireApplyProgressCallbacks(&cfg, &stubControlledJob{}, isUpdate, func(msg *websocket.ProgressMessage) { got = msg })
		require.Same(t, ref, cfg.ApplyGenerationRef)
		assert.Zero(t, atomic.LoadUint64(ref))
		atomic.StoreUint64(ref, 17)
		cfg.OnFileOrganized("/source/movie.mp4")
		require.NotNil(t, got)
		assert.Equal(t, uint64(17), got.ApplyGeneration)
	}
}

func TestResolveApplyConfig_CallbackWiring(t *testing.T) {
	for _, tc := range []struct {
		name, start, done, fileStatus, auditSource string
		complete                                   websocket.ProgressStatus
		update                                     bool
	}{
		{"organize", "Organizing", "Organized", "organized", "file_move", websocket.ProgressStatusOrganizeCompleted, false},
		{"update", "Updating", "Updated", "updated", "nfo_gen", websocket.ProgressStatusUpdateCompleted, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			emitter := &applyConfigEventEmitter{}
			rt := core.NewAPIRuntime(&core.APIDeps{EventEmitter: emitter})
			rt.Runtime = core.NewRuntimeState()
			hub := rt.Runtime.ResetWebSocketHub()
			connections, err := newTestWSConn()
			require.NoError(t, err)
			t.Cleanup(connections.Close)
			var writePumpDone <-chan struct{}
			t.Cleanup(func() {
				rt.Runtime.Shutdown()
				if writePumpDone != nil {
					select {
					case <-writePumpDone:
					case <-time.After(2 * time.Second):
						t.Error("timeout waiting for websocket WritePump shutdown")
					}
				}
			})
			writePumpDone, err = registerClientOnHub(hub, connections.serverConn, connections.clientConn)
			require.NoError(t, err)
			snapshot := core.NewSnapshotForTesting(rt, core.APIConfig{})
			factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
			job := &stubControlledJob{status: &worker.BatchJobStatus{}}
			job.status.TotalFiles = 5
			job.status.Completed = 2
			job.status.Failed = 1
			job.status.ApplyGeneration = 99
			resolve := func() worker.ApplyPhaseConfig {
				var cfg worker.ApplyPhaseConfig
				var err error
				if tc.update {
					cfg, err = resolveUpdateApplyConfig(snapshot, factory, job, contracts.UpdateRequest{})
				} else {
					cfg, err = resolveOrganizeApplyConfig(snapshot, factory, job, contracts.OrganizeRequest{OperationMode: string(operationmode.OperationModeInPlace)})
				}
				require.NoError(t, err)
				return cfg
			}
			first, second := resolve(), resolve()
			require.NotNil(t, first.ApplyGenerationRef)
			require.NotNil(t, second.ApplyGenerationRef)
			require.NotSame(t, first.ApplyGenerationRef, second.ApplyGenerationRef)
			assert.Zero(t, atomic.LoadUint64(first.ApplyGenerationRef))
			assert.Zero(t, atomic.LoadUint64(second.ApplyGenerationRef))
			atomic.StoreUint64(first.ApplyGenerationRef, 7)
			atomic.StoreUint64(second.ApplyGenerationRef, 11)

			// Exercise both configs after both have been built. All callbacks and audit
			// must retain their own live reference, not job status or the newest config.
			for i, cfg := range []worker.ApplyPhaseConfig{first, second} {
				generation := []uint64{7, 11}[i]
				require.NotNil(t, cfg.OnFileOrganizeStart)
				require.NotNil(t, cfg.OnFileProgress)
				require.NotNil(t, cfg.OnFileOrganized)
				require.NotNil(t, cfg.OnFileFailed)
				require.NotNil(t, cfg.OnPhaseComplete)
				require.NotNil(t, cfg.PostApplyFunc)
				cfg.OnFileOrganizeStart("/source/movie.mp4")
				cfg.OnFileProgress(3, 5)
				cfg.OnFileOrganized("/source/movie.mp4")
				cfg.OnFileFailed("/source/bad.mp4", "failed")
				cfg.OnPhaseComplete(2, 1)
				for _, want := range []struct {
					status             websocket.ProgressStatus
					progress           float64
					file, message, err string
				}{
					{websocket.ProgressStatusPending, 0, "/source/movie.mp4", tc.start + " movie.mp4", ""},
					{websocket.ProgressStatusPending, 60, "", tc.done + " 3 of 5 files", ""},
					{websocket.ProgressStatus(tc.fileStatus), 100, "/source/movie.mp4", "", ""},
					{websocket.ProgressStatus("failed"), 100, "/source/bad.mp4", "", "failed"},
					{tc.complete, 100, "", tc.done + " 2 files, 1 failed", ""},
				} {
					require.NoError(t, connections.clientConn.SetReadDeadline(time.Now().Add(2*time.Second)))
					data, err := readUntilMessage(connections.clientConn, "stub-job")
					require.NoError(t, err)
					var msg websocket.ProgressMessage
					require.NoError(t, json.Unmarshal(data, &msg))
					assert.Equal(t, want.status, msg.Status)
					assert.Equal(t, want.progress, msg.Progress)
					assert.Equal(t, want.file, msg.FilePath)
					assert.Equal(t, want.message, msg.Message)
					assert.Equal(t, want.err, msg.Error)
					assert.Equal(t, generation, msg.ApplyGeneration)
					assert.Equal(t, 5, msg.TotalFiles)
					assert.Equal(t, 2, msg.Completed)
					assert.Equal(t, 1, msg.Failed)
				}
				cfg.PostApplyFunc(context.Background(), &worker.ApplyFileContext{FilePath: "/source/movie.mp4", Movie: &models.Movie{ID: "MOV-1"}}, &worker.ApplyFileResult{Err: errors.New("audit failure")})
				require.Len(t, emitter.calls, i+1)
				assert.Equal(t, tc.auditSource, emitter.calls[i].source)
				assert.Equal(t, generation, emitter.calls[i].context["apply_generation"])
			}
		})
	}
}
