package batch

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/applyplan"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

func TestPreviewApplyParity_AllOperations(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	deps := createTestDeps(t, cfg, "")
	snapshot := core.NewSnapshotForTesting(testkit.GetTestRuntime(deps), core.APIConfig{AllowedDirectories: []string{"/source"}})
	factory := worker.NewBatchJobFactory(nil, nil, nil, nil, worker.BatchJobConfig{}, nil)

	tests := []struct {
		name          string
		operation     applyplan.VideoOperation
		destination   string
		overrides     *contracts.ReviewApplyOverrides
		wantSkipApply bool
	}{
		{"organize same source remains organize", applyplan.VideoOperationOrganize, "/source", &contracts.ReviewApplyOverrides{Destination: stringPtr("/source"), SkipNFO: boolPtr(true), SkipDownload: boolPtr(true)}, false},
		{"rename in place", applyplan.VideoOperationRenameInPlace, "", &contracts.ReviewApplyOverrides{SkipNFO: boolPtr(true), SkipDownload: boolPtr(true)}, false},
		{"rename file", applyplan.VideoOperationRenameFile, "", &contracts.ReviewApplyOverrides{SkipNFO: boolPtr(true), SkipDownload: boolPtr(true)}, false},
		{"leave in place", applyplan.VideoOperationLeaveInPlace, "", &contracts.ReviewApplyOverrides{SkipNFO: boolPtr(false), SkipDownload: boolPtr(true)}, true},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			base := applyplan.Default(tc.operation, tc.destination)
			effectivePreview, err := effectiveFromOverrides(base, tc.overrides)
			require.NoError(t, err)
			projection, err := applyplan.Project(effectivePreview.Plan)
			require.NoError(t, err)

			job := &stubControlledJob{status: statusWithPlan(t, base)}
			if tc.operation == applyplan.VideoOperationLeaveInPlace {
				applyCfg, applyErr := resolveUpdateApplyConfig(snapshot, factory, job, contracts.UpdateRequest{Overrides: tc.overrides})
				require.NoError(t, applyErr)
				assert.Equal(t, tc.wantSkipApply, applyCfg.OrganizeOptions.Skip)
				assert.Equal(t, !projection.SkipNFO, applyCfg.GenerateNFO)
				assert.Equal(t, !projection.SkipDownload, applyCfg.Download)
				assert.Empty(t, applyCfg.Destination)
				return
			}

			applyCfg, applyErr := resolveOrganizeApplyConfig(snapshot, factory, job, contracts.OrganizeRequest{Overrides: tc.overrides})
			require.NoError(t, applyErr)
			assert.Equal(t, tc.wantSkipApply, applyCfg.OrganizeOptions.Skip)
			assert.Equal(t, projection.OperationMode, applyCfg.OperationModeOverride)
			assert.Equal(t, projection.Destination, applyCfg.Destination)
			assert.Equal(t, !projection.SkipNFO, applyCfg.GenerateNFO)
			assert.Equal(t, !projection.SkipDownload, applyCfg.Download)
		})
	}
}

func TestPreviewApplyParity_MergeOverrideRepresentations(t *testing.T) {
	base := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
	tests := []struct {
		name      string
		overrides *contracts.ReviewApplyOverrides
		want      applyplan.MergeOverride
	}{
		{"none", &contracts.ReviewApplyOverrides{}, applyplan.MergeOverrideNone},
		{"force overwrite", &contracts.ReviewApplyOverrides{ForceOverwrite: boolPtr(true)}, applyplan.MergeOverrideForceOverwrite},
		{"preserve NFO", &contracts.ReviewApplyOverrides{PreserveNFO: boolPtr(true)}, applyplan.MergeOverridePreserveNFO},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			effective, err := effectiveFromOverrides(base, tc.overrides)
			require.NoError(t, err)
			assert.Equal(t, tc.want, effective.MergeOverride)
		})
	}
}

func TestPreviewHTTPApplyParity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	root := t.TempDir()
	outputDir := filepath.Join(root, "output")
	require.NoError(t, os.MkdirAll(outputDir, 0o755))
	cfg := config.DefaultConfig(nil, nil)
	cfg.API.Security.AllowedDirectories = []string{root}
	cfg.Output.Template.FolderFormat = "<ID>"
	cfg.Output.Template.FileFormat = "<ID>"
	deps := createTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	router := gin.New()
	router.POST("/batch/:id/results/:resultId/preview", previewOrganize(rt))
	router.POST("/batch/:id/update", updateBatchJob(rt))
	router.POST("/batch/:id/organize", organizeJob(rt))
	factory := rt.Snapshot().BatchJobFactory()

	tests := []struct {
		name        string
		operation   applyplan.VideoOperation
		destination string
		overrides   *contracts.ReviewApplyOverrides
	}{
		{"organize", applyplan.VideoOperationOrganize, outputDir, &contracts.ReviewApplyOverrides{Destination: stringPtr(outputDir), SkipNFO: boolPtr(true), SkipDownload: boolPtr(true)}},
		{"rename in place", applyplan.VideoOperationRenameInPlace, "", &contracts.ReviewApplyOverrides{SkipNFO: boolPtr(true), SkipDownload: boolPtr(true)}},
		{"rename file", applyplan.VideoOperationRenameFile, "", &contracts.ReviewApplyOverrides{SkipNFO: boolPtr(true), SkipDownload: boolPtr(true)}},
		{"leave in place", applyplan.VideoOperationLeaveInPlace, "", &contracts.ReviewApplyOverrides{SkipNFO: boolPtr(false), SkipDownload: boolPtr(true), PreserveNFO: boolPtr(true)}},
	}
	for index, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			movieID := fmt.Sprintf("TEST-%03d", index+1)
			inputDir := filepath.Join(root, fmt.Sprintf("input-%d", index+1))
			require.NoError(t, os.MkdirAll(inputDir, 0o755))
			source := filepath.Join(inputDir, "raw.mp4")
			require.NoError(t, os.WriteFile(source, []byte("video"), 0o644))
			plan := applyplan.Default(tc.operation, tc.destination)
			job := createJobWithWF(deps, cfg, []string{source}, plan)
			setJobResult(job, source, &resultstore.MovieResult{ResultID: movieID + "-result", FileMatchInfo: models.FileMatchInfo{Path: source, MovieID: movieID}, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: movieID, Title: "Test"}, StartedAt: time.Now()})
			controlled, ok := deps.JobStore.GetBatchJob(job.GetID())
			require.True(t, ok)

			previewRequest := contracts.OrganizePreviewRequest{Destination: tc.destination, Overrides: tc.overrides}
			if tc.overrides.SkipNFO != nil {
				previewRequest.SkipNFO = *tc.overrides.SkipNFO
			}
			if tc.overrides.SkipDownload != nil {
				previewRequest.SkipDownload = *tc.overrides.SkipDownload
			}
			payload, err := json.Marshal(previewRequest)
			require.NoError(t, err)
			req := httptest.NewRequest(http.MethodPost, "/batch/"+job.GetID()+"/results/"+movieID+"-result/preview", bytes.NewReader(payload))
			req.Header.Set("Content-Type", "application/json")
			w := httptest.NewRecorder()
			router.ServeHTTP(w, req)
			require.Equal(t, http.StatusOK, w.Code, w.Body.String())
			var preview contracts.OrganizePreviewResponse
			require.NoError(t, json.Unmarshal(w.Body.Bytes(), &preview))
			require.NotNil(t, preview.EffectiveApply)

			want, err := effectiveFromOverrides(plan, tc.overrides)
			require.NoError(t, err)
			assert.Equal(t, want, preview.EffectiveApply)
			var applyPath string
			var applyRequest any
			if tc.operation == applyplan.VideoOperationLeaveInPlace {
				assert.Contains(t, preview.VideoFiles, source)
				request := parityUpdateRequest(tc.overrides)
				applyCfg, applyErr := resolveUpdateApplyConfig(rt.Snapshot(), factory, controlled, request)
				require.NoError(t, applyErr)
				assert.Equal(t, want.MergeOverride == applyplan.MergeOverridePreserveNFO, applyCfg.MergeOptions.PreserveNFO)
				applyPath = "/batch/" + job.GetID() + "/update"
				applyRequest = request
			} else {
				request := parityOrganizeRequest(tc.destination, tc.overrides)
				_, applyErr := resolveOrganizeApplyConfig(rt.Snapshot(), factory, controlled, request)
				require.NoError(t, applyErr)
				applyPath = "/batch/" + job.GetID() + "/organize"
				applyRequest = request
			}
			setJobStatus(job, models.JobStatusCompleted)
			applyPayload, err := json.Marshal(applyRequest)
			require.NoError(t, err)
			applyHTTPReq := httptest.NewRequest(http.MethodPost, applyPath, bytes.NewReader(applyPayload))
			applyHTTPReq.Header.Set("Content-Type", "application/json")
			applyW := httptest.NewRecorder()
			router.ServeHTTP(applyW, applyHTTPReq)
			require.Equal(t, http.StatusOK, applyW.Code, applyW.Body.String())
			require.NotEmpty(t, preview.VideoFiles)
			expectedPath := preview.VideoFiles[0]
			if tc.operation == applyplan.VideoOperationLeaveInPlace {
				expectedPath = source
			}
			require.Eventually(t, func() bool {
				if _, statErr := os.Stat(expectedPath); statErr != nil {
					return false
				}
				if tc.operation != applyplan.VideoOperationLeaveInPlace {
					return true
				}
				nfos, _ := filepath.Glob(filepath.Join(inputDir, "*.nfo"))
				return len(nfos) > 0
			}, 5*time.Second, 10*time.Millisecond)
			require.Eventually(t, func() bool {
				status := job.GetStatus().Status
				return status == models.JobStatusCompleted || status == models.JobStatusOrganized || status == models.JobStatusFailed
			}, 5*time.Second, 10*time.Millisecond)
			require.NotEqual(t, models.JobStatusFailed, job.GetStatus().Status)
		})
	}

	invalidPlan := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
	invalidPath := filepath.Join(root, "INVALID.mp4")
	invalidJob := createJobWithWF(deps, cfg, []string{invalidPath}, invalidPlan)
	setJobResult(invalidJob, invalidPath, &resultstore.MovieResult{ResultID: "invalid-result", FileMatchInfo: models.FileMatchInfo{Path: invalidPath, MovieID: "INVALID"}, Status: models.JobStatusCompleted, Movie: &models.Movie{ID: "INVALID"}, StartedAt: time.Now()})
	invalidControlled, ok := deps.JobStore.GetBatchJob(invalidJob.GetID())
	require.True(t, ok)
	invalid := []*contracts.ReviewApplyOverrides{
		{SkipDownload: boolPtr(true), OverwriteExistingMedia: boolPtr(true)},
		{SkipNFO: boolPtr(true), SkipDownload: boolPtr(true)},
		{ForceOverwrite: boolPtr(true), PreserveNFO: boolPtr(true)},
		{OperationMode: stringPtr("organize")},
	}
	for _, overrides := range invalid {
		payload, err := json.Marshal(contracts.OrganizePreviewRequest{Overrides: overrides})
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPost, "/batch/"+invalidJob.GetID()+"/results/invalid-result/preview", bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		w := httptest.NewRecorder()
		router.ServeHTTP(w, req)
		assert.Equal(t, http.StatusBadRequest, w.Code, w.Body.String())
		updateRequest := parityUpdateRequest(overrides)
		_, applyErr := resolveUpdateApplyConfig(rt.Snapshot(), factory, invalidControlled, updateRequest)
		assert.Error(t, applyErr)
		applyPayload, err := json.Marshal(updateRequest)
		require.NoError(t, err)
		applyReq := httptest.NewRequest(http.MethodPost, "/batch/"+invalidJob.GetID()+"/update", bytes.NewReader(applyPayload))
		applyReq.Header.Set("Content-Type", "application/json")
		applyW := httptest.NewRecorder()
		router.ServeHTTP(applyW, applyReq)
		assert.Equal(t, http.StatusBadRequest, applyW.Code, applyW.Body.String())
	}
}
func parityUpdateRequest(overrides *contracts.ReviewApplyOverrides) contracts.UpdateRequest {
	request := contracts.UpdateRequest{Overrides: overrides}
	if overrides == nil {
		return request
	}
	if overrides.ForceOverwrite != nil {
		request.ForceOverwrite = *overrides.ForceOverwrite
	}
	if overrides.OverwriteExistingMedia != nil {
		request.OverwriteExistingMedia = *overrides.OverwriteExistingMedia
	}
	if overrides.PreserveNFO != nil {
		request.PreserveNFO = *overrides.PreserveNFO
	}
	if overrides.Preset != nil {
		request.Preset = *overrides.Preset
	}
	if overrides.ScalarStrategy != nil {
		request.ScalarStrategy = *overrides.ScalarStrategy
	}
	if overrides.ArrayStrategy != nil {
		request.ArrayStrategy = *overrides.ArrayStrategy
	}
	if overrides.SkipNFO != nil {
		request.SkipNFO = *overrides.SkipNFO
	}
	if overrides.SkipDownload != nil {
		request.SkipDownload = *overrides.SkipDownload
	}
	return request
}

func parityOrganizeRequest(destination string, overrides *contracts.ReviewApplyOverrides) contracts.OrganizeRequest {
	request := contracts.OrganizeRequest{Destination: destination, Overrides: overrides}
	if overrides == nil {
		return request
	}
	if overrides.Destination != nil {
		request.Destination = *overrides.Destination
	}
	if overrides.OperationMode != nil {
		request.OperationMode = *overrides.OperationMode
	}
	if overrides.SkipNFO != nil {
		request.SkipNFO = *overrides.SkipNFO
	}
	if overrides.SkipDownload != nil {
		request.SkipDownload = *overrides.SkipDownload
	}
	return request
}
func TestPreviewApplyParity_InvalidOverrides(t *testing.T) {
	base := applyplan.Default(applyplan.VideoOperationLeaveInPlace, "")
	tests := []struct {
		name      string
		overrides *contracts.ReviewApplyOverrides
	}{
		{"media contradiction", &contracts.ReviewApplyOverrides{SkipDownload: boolPtr(true), OverwriteExistingMedia: boolPtr(true)}},
		{"advanced override contradiction", &contracts.ReviewApplyOverrides{ForceOverwrite: boolPtr(true), PreserveNFO: boolPtr(true)}},
		{"preset contradiction", &contracts.ReviewApplyOverrides{Preset: stringPtr("aggressive"), ArrayStrategy: stringPtr("merge")}},
		{"operation contradiction", &contracts.ReviewApplyOverrides{OperationMode: stringPtr("organize")}},
		{"no output", &contracts.ReviewApplyOverrides{SkipNFO: boolPtr(true), SkipDownload: boolPtr(true)}},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			_, err := effectiveFromOverrides(base, tc.overrides)
			assert.Error(t, err)
		})
	}
}
