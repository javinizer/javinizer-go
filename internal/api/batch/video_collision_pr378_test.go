package batch

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/applyplan"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
	"github.com/javinizer/javinizer-go/internal/videocollision"
	"github.com/javinizer/javinizer-go/internal/websocket"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

func TestPR378CollisionOverrideResolution(t *testing.T) {
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	snap := core.NewSnapshotForTesting(core.NewAPIRuntime(nil), core.APIConfig{})
	dir := t.TempDir()
	decision := videocollision.Decision{Source: filepath.Join(dir, "source.mp4"), Destination: filepath.Join(dir, "target.mp4"), Policy: videocollision.Skip}
	require.Equal(t, filepath.Clean(decision.Source), decision.Source)
	skip := videocollision.Skip
	for _, tc := range []struct {
		name    string
		req     contracts.OrganizeRequest
		wantErr string
	}{
		{"nested policy", contracts.OrganizeRequest{Overrides: &contracts.ReviewApplyOverrides{VideoCollisionPolicy: &skip}}, ""},
		{"matching policies", contracts.OrganizeRequest{VideoCollisionPolicy: skip, Overrides: &contracts.ReviewApplyOverrides{VideoCollisionPolicy: &skip}}, ""},
		{"contradictory policy", contracts.OrganizeRequest{VideoCollisionPolicy: videocollision.Fail, Overrides: &contracts.ReviewApplyOverrides{VideoCollisionPolicy: &skip}}, "contradictory video_collision_policy"},
		{"nested decisions", contracts.OrganizeRequest{Overrides: &contracts.ReviewApplyOverrides{VideoCollisionDecisions: map[string]videocollision.Decision{decision.Source: decision}}}, ""},
		{"duplicate decisions", contracts.OrganizeRequest{VideoCollisionDecisions: map[string]videocollision.Decision{decision.Source: decision}, Overrides: &contracts.ReviewApplyOverrides{VideoCollisionDecisions: map[string]videocollision.Decision{decision.Source: decision}}}, "must be supplied once"},
		{"invalid policy", contracts.OrganizeRequest{VideoCollisionPolicy: "unsafe"}, "invalid video_collision_policy"},
		{"invalid decision policy", contracts.OrganizeRequest{VideoCollisionDecisions: map[string]videocollision.Decision{decision.Source: {Source: decision.Source, Destination: decision.Destination, Policy: "unsafe"}}}, "invalid video_collision_policy"},
		{"mismatched source", contracts.OrganizeRequest{VideoCollisionDecisions: map[string]videocollision.Decision{filepath.Join(dir, "other.mp4"): decision}}, "invalid video collision decision path"},
		{"empty destination", contracts.OrganizeRequest{VideoCollisionDecisions: map[string]videocollision.Decision{decision.Source: {Source: decision.Source, Policy: skip}}}, "invalid video collision decision path"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			tc.req.OperationMode = string(operationmode.OperationModeInPlace)
			cfg, err := resolveOrganizeApplyConfig(snap, factory, &stubControlledJob{}, tc.req)
			if tc.wantErr != "" {
				require.ErrorContains(t, err, tc.wantErr)
				require.Nil(t, cfg.OnPhaseComplete)
				return
			}
			require.NoError(t, err)
			if tc.req.Overrides.VideoCollisionPolicy != nil {
				require.Equal(t, skip, cfg.OrganizeOptions.VideoCollisionPolicy)
			} else {
				require.Equal(t, decision, cfg.OrganizeOptions.VideoCollisionDecisions[decision.Source])
			}
			require.Equal(t, "stub-job", cfg.OrganizeOptions.VideoCollisionScope)
		})
	}
	require.NoError(t, validateVideoCollisionOverrides(nil))
	base := applyplan.Default(applyplan.VideoOperationOrganize, "/destination")
	effective, err := effectiveFromOverrides(base, &contracts.ReviewApplyOverrides{VideoCollisionPolicy: &skip})
	require.NoError(t, err)
	require.Equal(t, skip, effective.Plan.VideoCollisionPolicy)
	require.Equal(t, videocollision.Fail, base.VideoCollisionPolicy)
}

func TestPR378CollisionReviewEndpointErrors(t *testing.T) {
	dir := t.TempDir()
	source := filepath.Join(dir, "unknown.mp4")
	decisions := map[string]videocollision.Decision{source: {Source: source, Destination: filepath.Join(dir, "target.mp4"), Policy: videocollision.Skip}}
	unknownBody, err := json.Marshal(contracts.OrganizeRequest{
		OperationMode:           string(operationmode.OperationModeInPlace),
		VideoCollisionDecisions: decisions,
	})
	require.NoError(t, err)
	var decoded contracts.OrganizeRequest
	require.NoError(t, json.Unmarshal(unknownBody, &decoded))
	require.Equal(t, decisions, decoded.VideoCollisionDecisions)
	for _, tc := range []struct {
		name, body, suffix, wantError string
		badFactory                    bool
		wantStatus                    int
	}{
		{"malformed", "{", "", "unexpected EOF", false, http.StatusBadRequest},
		{"missing job", "{}", "missing", jobNotFoundMessage, false, http.StatusNotFound},
		{"factory unavailable", "{}", "", factoryUnavailableMessage, true, http.StatusServiceUnavailable},
		{"bad config", `{"video_collision_policy":"unsafe"}`, "", "invalid video_collision_policy", false, http.StatusBadRequest},
		{"unknown decision", string(unknownBody), "", "video decision source is not in this job: " + source, false, http.StatusBadRequest},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := config.DefaultConfig(nil, nil)
			if tc.badFactory {
				cfg = badRegexConfig()
			}
			deps := createTestDeps(t, cfg, "")
			job := deps.JobStore.CreateJobBatch(nil)
			setJobStatus(job, models.JobStatusCompleted)
			router := gin.New()
			router.POST("/batch/:id/video-collisions", videoCollisionReview(testkit.GetTestRuntime(deps)))
			id := job.GetID()
			if tc.suffix != "" {
				id = tc.suffix
			}
			body := tc.body
			if tc.name == "bad config" {
				body = strings.TrimSuffix(body, "}") + `,"operation_mode":"in-place"}`
			}
			req := httptest.NewRequest(http.MethodPost, "/batch/"+id+"/video-collisions", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, req)
			require.Equal(t, tc.wantStatus, response.Code, response.Body.String())
			var out contracts.ErrorResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &out))
			require.Contains(t, out.Error, tc.wantError)
			require.Zero(t, job.GetStatus().ApplyGeneration)
			require.Empty(t, job.GetStatus().Results)
		})
	}
}

type pr378ReviewJob struct {
	worker.BatchJobInterface
	status *worker.BatchJobStatus
}

func (j pr378ReviewJob) GetStatus() *worker.BatchJobStatus { return j.status }

func TestPR378CollisionReviewSelection(t *testing.T) {
	deps, job, _, req, item := issue277GenerationFixture(t)
	rt := testkit.GetTestRuntime(deps)
	snap := rt.Snapshot()
	cfg, err := resolveOrganizeApplyConfig(snap, snap.BatchJobFactory(), mustPR378Job(t, deps, job.GetID()), req)
	require.NoError(t, err)
	status := job.GetStatus()
	original := status.Results[item.Source]
	status.Results["/nil"] = nil
	status.Results["/no-movie"] = &resultstore.MovieResult{}
	status.Results["/pending"] = &resultstore.MovieResult{Movie: original.Movie, Status: models.JobStatusPending}
	status.Results["/excluded"] = original
	status.Results["/not-retried"] = original
	status.Excluded = map[string]bool{"/excluded": true}
	cfg.RetryFilePaths = []string{item.Source, "/pending"}
	wrapped := pr378ReviewJob{BatchJobInterface: mustPR378Job(t, deps, job.GetID()), status: status}
	report, err := reviewVideoCollisions(t.Context(), snap, wrapped, cfg)
	require.NoError(t, err)
	require.Len(t, report.Items, 1)
	require.Equal(t, item.Source, report.Items[0].Source)
	require.Equal(t, 1, report.Unresolved)
	cfg.OrganizeOptions.Skip = true
	report, err = reviewVideoCollisions(t.Context(), nil, nil, cfg)
	require.NoError(t, err)
	require.Empty(t, report.Items)
	cfg.OrganizeOptions.Skip = false
	badDeps := createTestDeps(t, badRegexConfig(), "")
	report, err = reviewVideoCollisions(t.Context(), testkit.GetTestRuntime(badDeps).Snapshot(), wrapped, cfg)
	require.ErrorContains(t, err, "workflow factory not available")
	require.Nil(t, report)
	pr378AssertVideos(t, item)
}

func mustPR378Job(t *testing.T, deps *core.APIDeps, id string) worker.BatchJobInterface {
	t.Helper()
	job, ok := deps.GetJobStore().GetBatchJob(id)
	require.True(t, ok)
	return job
}

func pr378AssertVideos(t *testing.T, item videocollision.Collision) {
	t.Helper()
	for path, want := range map[string]string{item.Source: "incoming", item.Destination: "foreign"} {
		body, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, want, string(body))
	}
}

func TestPR378CollisionReviewStaleDecision(t *testing.T) {
	deps, job, router, req, item := issue277GenerationFixture(t)
	req.VideoCollisionDecisions = map[string]videocollision.Decision{item.Source: {Source: item.Source, Destination: item.Destination + ".old", Policy: videocollision.Skip}}
	response := doJSON(t, router, http.MethodPost, "/batch/"+job.GetID()+"/organize", req)
	require.Equal(t, http.StatusConflict, response.Code, response.Body.String())
	var out contracts.ErrorResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &out))
	require.Equal(t, "VIDEO_COLLISION_STALE", out.Code)
	require.NotNil(t, out.VideoCollisionReview)
	require.Equal(t, 1, out.VideoCollisionReview.Unresolved)
	require.Equal(t, videoCollisionKindStale, out.VideoCollisionReview.Items[0].Kind)
	require.Zero(t, mustPR378Job(t, deps, job.GetID()).GetStatus().ApplyGeneration)
	pr378AssertVideos(t, item)
}

func TestPR378CollisionReviewAuthorityAndPreviewErrors(t *testing.T) {
	for _, mode := range []string{"authority", "preview", "empty preview"} {
		t.Run(mode, func(t *testing.T) {
			deps, job, router, req, item := issue277GenerationFixture(t)
			original := job.GetStatus().Results[item.Source]
			want := ""
			switch mode {
			case "authority":
				status := job.GetStatus()
				status.Results[item.Source].ResultID = "missing-result"
				deps.JobStore = &excludeEdgeStore{JobStoreInterface: deps.JobStore, job: pr378ReviewJob{BatchJobInterface: mustPR378Job(t, deps, job.GetID()), status: status}}
				want = "Result missing-result not found"
			case "preview":
				require.NoError(t, os.Remove(item.Source))
				require.NoError(t, os.Mkdir(item.Source, 0755))
				want = "video review requires a regular file: " + item.Source
			case "empty preview":
				original.FileMatchInfo.Path = ""
				setJobResult(job, item.Source, original)
				req.OperationMode = string(operationmode.OperationModeInPlace)
				want = "cannot resolve planned video destination"
			}
			response := doJSON(t, router, http.MethodPost, "/batch/"+job.GetID()+"/organize", req)
			require.Equal(t, http.StatusBadRequest, response.Code, response.Body.String())
			var out contracts.ErrorResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &out))
			require.Contains(t, out.Error, want)
			require.Zero(t, job.GetStatus().ApplyGeneration)
			body, err := os.ReadFile(item.Destination)
			require.NoError(t, err)
			require.Equal(t, "foreign", string(body))
		})
	}
}

func TestPR378CollisionReviewMissingSourceRetry(t *testing.T) {
	deps, job, _, req, item := issue277GenerationFixture(t)
	require.NoError(t, os.Remove(item.Source))
	snap := testkit.GetTestRuntime(deps).Snapshot()
	controller := mustPR378Job(t, deps, job.GetID())
	cfg, err := resolveOrganizeApplyConfig(snap, snap.BatchJobFactory(), controller, req)
	require.NoError(t, err)
	report, err := reviewVideoCollisions(t.Context(), snap, controller, cfg)
	require.NoError(t, err)
	require.Empty(t, report.Items)
	cfg.OrganizeOptions.VideoCollisionPolicy = videocollision.Skip
	report, err = reviewVideoCollisions(t.Context(), snap, controller, cfg)
	require.NoError(t, err)
	require.Equal(t, 1, report.SameFile)
	require.Zero(t, report.Skipped)
	require.Equal(t, videoCollisionKindSameFile, report.Items[0].Kind)
	body, err := os.ReadFile(item.Destination)
	require.NoError(t, err)
	require.Equal(t, "foreign", string(body))
}

func TestPR378SkippedCallbackGenerationOwnership(t *testing.T) {
	rt := core.NewAPIRuntime(&core.APIDeps{})
	rt.Runtime = core.NewRuntimeState()
	hub := rt.Runtime.ResetWebSocketHub()
	connections, err := newTestWSConn()
	require.NoError(t, err)
	t.Cleanup(connections.Close)
	done, err := registerClientOnHub(hub, connections.serverConn, connections.clientConn)
	require.NoError(t, err)
	t.Cleanup(func() {
		rt.Runtime.Shutdown()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Error("websocket pump did not stop")
		}
	})
	job := &stubControlledJob{status: &worker.BatchJobStatus{ApplyGeneration: 99, TotalFiles: 3, Completed: 2, Failed: 1}}
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)
	snap := core.NewSnapshotForTesting(rt, core.APIConfig{})
	configs := make([]worker.ApplyPhaseConfig, 2)
	for i := range configs {
		configs[i], err = resolveOrganizeApplyConfig(snap, factory, job, contracts.OrganizeRequest{OperationMode: string(operationmode.OperationModeInPlace)})
		require.NoError(t, err)
		atomic.StoreUint64(configs[i].ApplyGenerationRef, uint64(i+7))
	}
	for i, cfg := range configs {
		require.NotNil(t, cfg.OnFileSkipped)
		cfg.OnFileSkipped("/skipped.mp4")
		cfg.OnPhaseComplete(2, 1)
		for _, status := range []websocket.ProgressStatus{"skipped", websocket.ProgressStatusOrganizeCompleted} {
			require.NoError(t, connections.clientConn.SetReadDeadline(time.Now().Add(2*time.Second)))
			data, err := readUntilMessage(connections.clientConn, "stub-job")
			require.NoError(t, err)
			var msg websocket.ProgressMessage
			require.NoError(t, json.Unmarshal(data, &msg))
			require.Equal(t, status, msg.Status)
			require.Equal(t, uint64(i+7), msg.ApplyGeneration)
			require.Equal(t, 3, msg.TotalFiles)
			if status == "skipped" {
				require.Equal(t, "/skipped.mp4", msg.FilePath)
			} else {
				require.Equal(t, "BATCH_COMPLETED_WITH_SKIPS", msg.MessageCode)
				require.Equal(t, "Applied 1 files, 1 skipped, 1 failed", msg.Message)
				require.Equal(t, float64(1), msg.MessageArgs["skipped"])
			}
		}
	}
}

func TestPR378MultipartCollisionReports(t *testing.T) {
	for _, scenario := range []string{"foreign sibling", "resident siblings", "duplicate siblings"} {
		t.Run(scenario, func(t *testing.T) {
			root := t.TempDir()
			output := filepath.Join(root, "output")
			cfg := config.DefaultConfig(nil, nil)
			cfg.API.Security.AllowedDirectories = []string{root}
			cfg.Output.Template.FolderFormat = "<ID>"
			cfg.Output.Template.FileFormat = "<ID>"
			cfg.Output.Template.SubfolderFormat = nil
			cfg.Output.Operation.RenameFile = true
			deps := createTestDeps(t, cfg, "")
			rt := testkit.GetTestRuntime(deps)
			movie := &models.Movie{ID: "IPX-277", ContentID: "multipart-pr378", Title: "Synthetic"}
			_, err := deps.Repos.MovieRepo.Upsert(t.Context(), movie)
			require.NoError(t, err)
			movie, err = deps.Repos.MovieRepo.FindByContentID(t.Context(), movie.ContentID)
			require.NoError(t, err)
			sourceDir := filepath.Join(root, "a")
			if scenario == "resident siblings" {
				sourceDir = filepath.Join(output, movie.ID)
			}
			source := filepath.Join(sourceDir, movie.ID+".mp4")
			sibling := filepath.Join(sourceDir, movie.ID+"-cd2.mp4")
			files := map[string]string{source: "primary", sibling: "part two"}
			sources := []string{source}
			if scenario == "duplicate siblings" {
				second := filepath.Join(root, "b", movie.ID+".mp4")
				sources = append(sources, second)
				files[second] = "second primary"
				files[filepath.Join(root, "b", movie.ID+"-cd2.mp4")] = "second part"
			}
			if scenario == "foreign sibling" {
				files[filepath.Join(output, movie.ID, movie.ID+"-cd2.mp4")] = "foreign part"
			}
			for path, body := range files {
				require.NoError(t, os.MkdirAll(filepath.Dir(path), 0755))
				require.NoError(t, os.WriteFile(path, []byte(body), 0644))
			}
			require.NoError(t, os.MkdirAll(output, 0755))
			job := deps.JobStore.CreateJobBatch(sources)
			for _, path := range sources {
				setJobResult(job, path, &resultstore.MovieResult{ResultID: path, Status: models.JobStatusCompleted, Movie: movie, PersistedMovie: true, FileMatchInfo: models.FileMatchInfo{Path: path, Name: filepath.Base(path), Extension: ".mp4", MovieID: movie.ID}})
			}
			setJobStatus(job, models.JobStatusCompleted)
			router := gin.New()
			router.POST("/batch/:id/video-collisions", videoCollisionReview(rt))
			req := contracts.OrganizeRequest{Destination: output, SkipNFO: true, SkipDownload: true}
			url := "/batch/" + job.GetID() + "/video-collisions"
			response := doJSON(t, router, http.MethodPost, url, req)
			require.Equal(t, http.StatusOK, response.Code, response.Body.String())
			var report videoCollisionReport
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &report))
			switch scenario {
			case "foreign sibling":
				require.Len(t, report.Items, 2)
				require.Equal(t, 1, report.New)
				require.Equal(t, 1, report.Unresolved)
				require.Equal(t, "multipart", report.Items[0].Kind)
				require.Equal(t, source, report.Items[0].SelectedSource)
				require.Empty(t, report.Items[0].ReviewToken)
				req.VideoCollisionPolicy = videocollision.Skip
				response = doJSON(t, router, http.MethodPost, url, req)
				require.Equal(t, http.StatusOK, response.Code, response.Body.String())
				var skipped videoCollisionReport
				require.NoError(t, json.Unmarshal(response.Body.Bytes(), &skipped))
				require.Len(t, skipped.Items, 1)
				require.Equal(t, source, skipped.Items[0].Source)
				require.Equal(t, 1, skipped.Skipped)
				require.Zero(t, skipped.Unresolved)
			case "resident siblings":
				require.Len(t, report.Items, 2)
				require.Equal(t, 2, report.SameFile)
				require.Zero(t, report.New)
				require.Zero(t, report.Unresolved)
				for _, item := range report.Items {
					require.Equal(t, videoCollisionKindSameFile, item.Kind)
					require.Equal(t, item.Source, item.Destination)
				}
			case "duplicate siblings":
				require.Len(t, report.Items, 4)
				require.Equal(t, 2, report.New)
				require.Equal(t, 2, report.Unresolved)
				require.Equal(t, "duplicate", report.Items[2].Kind)
				require.Equal(t, sources[1], report.Items[2].SelectedSource)
				require.Equal(t, "duplicate", report.Items[3].Kind)
				require.Equal(t, sources[1], report.Items[3].Source)
			}
			for path, want := range files {
				body, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, want, string(body))
			}
			require.Zero(t, job.GetStatus().ApplyGeneration)
			require.Zero(t, report.Replaced)
		})
	}
}
