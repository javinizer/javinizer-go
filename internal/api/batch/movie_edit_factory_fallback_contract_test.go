package batch_test

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/batch"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/jobpersist"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/stretchr/testify/require"
)

// SetConfig is the documented manually wired runtime setup boundary. This test
// does not claim production config reload accepts malformed regexes: a genuine
// matcher construction error must not prevent an unrelated metadata edit, nor
// allow the caller's stale derived title to become authoritative.
func TestMovieEditUnavailableFactoryKeepsDurableTitleFallbackAndRecovers(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "owned-review.mp4")
	require.NoError(t, os.WriteFile(file, []byte("unchanged video"), 0600))
	cfg := config.DefaultConfig(nil, nil)
	cfg.System.TempDir = filepath.Join(dir, "work")
	cfg.Matching.RegexEnabled = true
	cfg.Matching.RegexPattern = "(unclosed["
	cfg.Metadata.NFO.Format.DisplayTitle = "[<ID>] <TITLE>"
	deps := testkit.CreateTestDeps(t, cfg, "")
	rt := testkit.GetTestRuntime(deps)
	factory, err := rt.Snapshot().WorkflowFactory()
	require.ErrorContains(t, err, "workflow factory not available")
	require.Nil(t, factory)
	actor := models.Actress{FirstName: "Kept", LastName: "Identity", Verified: true, Origin: "user"}
	require.NoError(t, deps.Repos.ActressRepo.Create(t.Context(), &actor))
	authority, err := deps.Repos.MovieRepo.Upsert(t.Context(), &models.Movie{ContentID: "owned-fallback-content", ID: "FALL-001", Title: "Before", DisplayTitle: "[FALL-001] Before", Actresses: []models.Actress{actor}})
	require.NoError(t, err)
	require.Len(t, authority.Credits, 1)
	crop := &models.CropBounds{X: 0.1, Y: 0.2, Width: 0.5, Height: 0.6, SourceAspect: 1.5}
	authority.Poster.PosterCropBounds = crop
	authority.Poster.PosterCropSourceFull = true
	original := authority.Clone()
	job := deps.JobStore.CreateJobBatch([]string{file})
	job.Controller().SetJobStatus(models.JobStatusCompleted)
	const rid = "owned-fallback-result"
	job.ResultsWriter().UpdateFileResult(file, &resultstore.MovieResult{ResultID: rid, Revision: 1, Status: models.JobStatusCompleted, FileMatchInfo: models.FileMatchInfo{Path: file, MovieID: authority.ID}, Movie: authority, PersistedMovie: true})
	router := gin.New()
	batch.RegisterRoutes(router.Group("/api/v1"), rt)
	endpoint := "/api/v1/batch/" + job.GetID() + "/results/" + rid
	patch := func(title string, revision uint64) contracts.MovieResponse {
		t.Helper()
		current, _, found := job.Results().GetFileResultByResultID(rid)
		require.True(t, found)
		view := contracts.MovieViewFromModel(current.Movie)
		view.Title = title
		view.DisplayTitle = "untrusted stale derived title"
		// Omit the crop key as a normal metadata-only PATCH. Intent belongs to the
		// result/envelope, not to library movie columns.
		view.PosterCropBounds = nil
		payload, err := json.Marshal(contracts.UpdateMovieRequest{Movie: view, ExpectedResultRevision: &revision})
		require.NoError(t, err)
		var wire map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(payload, &wire))
		var movieWire map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(wire["movie"], &movieWire))
		delete(movieWire, "poster_crop_bounds")
		wire["movie"], err = json.Marshal(movieWire)
		require.NoError(t, err)
		payload, err = json.Marshal(wire)
		require.NoError(t, err)
		req := httptest.NewRequest(http.MethodPatch, endpoint, bytes.NewReader(payload))
		req.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, req)
		require.Equal(t, http.StatusOK, response.Code, response.Body.String())
		var result contracts.MovieResponse
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		return result
	}
	verify := func(title, display string, response contracts.MovieResponse) uint64 {
		t.Helper()
		require.Equal(t, title, response.Movie.Title)
		require.Equal(t, display, response.Movie.DisplayTitle)
		require.NotNil(t, response.Revision)
		stored, err := deps.Repos.MovieRepo.FindByContentID(t.Context(), original.ContentID)
		require.NoError(t, err)
		require.Equal(t, title, stored.Title)
		require.Equal(t, display, stored.DisplayTitle)
		require.Equal(t, original.ContentID, stored.ContentID)
		require.Equal(t, original.ID, stored.ID)
		require.Equal(t, original.Actresses, stored.Actresses)
		require.Equal(t, original.Credits, stored.Credits)
		current, path, found := job.Results().GetFileResultByResultID(rid)
		require.True(t, found)
		require.Equal(t, file, path)
		require.Equal(t, rid, current.ResultID)
		require.Equal(t, *response.Revision, current.Revision)
		require.Equal(t, display, current.Movie.DisplayTitle)
		require.Equal(t, crop, current.Movie.Poster.PosterCropBounds)
		require.True(t, current.Movie.Poster.PosterCropSourceFull)
		durable, err := deps.Repos.JobRepo.FindByID(t.Context(), job.GetID())
		require.NoError(t, err)
		var envelope jobpersist.JobResultsEnvelope
		require.NoError(t, durable.ParseResults(&envelope))
		require.Contains(t, envelope.Domain, file)
		expectedJSON, err := json.Marshal(current)
		require.NoError(t, err)
		durableJSON, err := json.Marshal(envelope.Domain[file])
		require.NoError(t, err)
		// PersistedMovie is runtime-only; compare the full serializable result.
		require.JSONEq(t, string(expectedJSON), string(durableJSON), "committed envelope must match published result")
		data, err := os.ReadFile(file)
		require.NoError(t, err)
		require.Equal(t, "unchanged video", string(data))
		return *response.Revision
	}
	logPath := filepath.Join(dir, "fallback.jsonl")
	require.NoError(t, logging.InitLogger(&logging.Config{Level: "warn", Format: "json", Output: logPath}))
	// This test is serial; restore the documented default logger, not an
	// unsupported snapshot of arbitrary prior singleton state.
	t.Cleanup(func() { logging.CloseLogger(); require.NoError(t, logging.InitLogger(nil)) })
	response := patch("Edited while unavailable", 1)
	revision := verify("Edited while unavailable", "Edited while unavailable", response)
	require.Greater(t, revision, uint64(1))
	logging.CloseLogger()
	data, err := os.ReadFile(logPath)
	require.NoError(t, err)
	warnings := 0
	for _, line := range strings.Split(strings.TrimSpace(string(data)), "\n") {
		var event struct {
			Level   string `json:"level"`
			Message string `json:"msg"`
		}
		require.NoError(t, json.Unmarshal([]byte(line), &event))
		if strings.HasPrefix(event.Message, "Failed to create workflow for display-title re-derive on save:") {
			require.Equal(t, "warning", event.Level)
			require.Contains(t, event.Message, "workflow factory not available")
			warnings++
		}
	}
	require.Equal(t, 1, warnings)
	require.NoError(t, logging.InitLogger(nil))
	valid := cfg.Clone()
	valid.Matching.RegexEnabled = false
	rt.SetConfig(valid)
	factory, err = rt.Snapshot().WorkflowFactory()
	require.NoError(t, err)
	require.NotNil(t, factory)
	recovered := patch("Recovered", revision)
	require.Greater(t, verify("Recovered", "[FALL-001] Recovered", recovered), revision)
	require.Equal(t, "(unclosed[", cfg.Matching.RegexPattern)
	require.True(t, cfg.Matching.RegexEnabled, "recovery uses a clone, not caller config mutation")
	require.Equal(t, original, authority, "edits must not mutate the original caller movie")
}
