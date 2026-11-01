package movie_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	movieapi "github.com/javinizer/javinizer-go/internal/api/movie"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/workflow"
	"github.com/stretchr/testify/require"
)

// The route accepts a WorkflowInterface adapter, not necessarily the default
// workflow. Typed failures from that boundary must remain meaningful even when
// the HTTP layer has already accepted the request's strategy strings.
type compareFailureContractWorkflow struct {
	t      *testing.T
	calls  int
	cmd    workflow.CompareCmd
	err    error
	cancel context.CancelFunc
	result *workflow.CompareResult
}

func (w *compareFailureContractWorkflow) Compare(ctx context.Context, cmd workflow.CompareCmd) (*workflow.CompareResult, error) {
	require.NoError(w.t, ctx.Err(), "cancellation must occur at the actual adapter boundary")
	w.calls++
	w.cmd = cmd
	if w.cancel != nil {
		w.cancel()
	}
	return w.result, w.err
}
func (w *compareFailureContractWorkflow) Scrape(context.Context, scrape.ScrapeCmd) (*scrape.ScrapeResult, *workflow.OrchestrationMeta, error) {
	w.t.Fatal("unexpected scrape operation")
	return nil, nil, nil
}
func (w *compareFailureContractWorkflow) Apply(context.Context, workflow.ApplyCmd) (*workflow.ApplyResult, error) {
	w.t.Fatal("unexpected apply operation")
	return nil, nil
}
func (w *compareFailureContractWorkflow) Preview(context.Context, workflow.PreviewCmd) (*workflow.PreviewResult, error) {
	w.t.Fatal("unexpected preview operation")
	return nil, nil
}
func (w *compareFailureContractWorkflow) ScanAndMatch(context.Context, workflow.ScanAndMatchCmd) (*workflow.ScanAndMatchResult, error) {
	w.t.Fatal("unexpected scan operation")
	return nil, nil
}

var _ workflow.WorkflowInterface = (*compareFailureContractWorkflow)(nil)

func TestCompareWorkflowFailuresRespectTypedMappingAndCancellationPrecedence(t *testing.T) {
	for _, tc := range []struct {
		name   string
		cause  error
		status int
	}{
		{"adapter rejects preset", workflow.ErrInvalidPreset, http.StatusBadRequest},
		{"adapter merge fails", workflow.ErrMergeFailed, http.StatusInternalServerError},
	} {
		for _, cancelAtBoundary := range []bool{false, true} {
			name := tc.name
			if cancelAtBoundary {
				name += " after request cancellation"
			}
			t.Run(name, func(t *testing.T) {
				root := t.TempDir()
				path := filepath.Join(root, "ABC-001.nfo")
				original := []byte(`<movie><id>ABC-001</id><title>Original NFO</title></movie>`)
				require.NoError(t, os.WriteFile(path, original, 0600))
				// The route forwards the canonical existing path, which may expand
				// Windows short directory names rather than retain TempDir spelling.
				canonicalPath, err := filepath.EvalSymlinks(path)
				require.NoError(t, err)
				parent, cancel := context.WithCancel(t.Context())
				defer cancel()
				adapterErr := fmt.Errorf("comparison adapter: %w", tc.cause)
				adapter := &compareFailureContractWorkflow{t: t, err: adapterErr, result: &workflow.CompareResult{Movie: &models.Movie{ID: "ABC-001", Title: "Partial adapter result"}, NFOExists: true, NFOPath: "ABC-001.nfo"}}
				if cancelAtBoundary {
					adapter.cancel = cancel
				}
				factoryCalls := 0
				deps := movieapi.NewMovieDeps(nil, movieapi.WithAllowedDirs([]string{root}), movieapi.WithWorkflow(func() workflow.WorkflowInterface { factoryCalls++; return adapter }))
				router := gin.New()
				movieapi.RegisterRoutes(router.Group("/api/v1"), deps)
				input := contracts.NFOComparisonRequest{NFOPath: path, SelectedScrapers: []string{"adapter-source"}}
				payload, err := json.Marshal(input)
				require.NoError(t, err)
				request := func(ctx context.Context) *httptest.ResponseRecorder {
					req := httptest.NewRequest(http.MethodPost, "/api/v1/movies/ABC-001/compare-nfo", bytes.NewReader(payload)).WithContext(ctx)
					req.Header.Set("Content-Type", "application/json")
					out := httptest.NewRecorder()
					router.ServeHTTP(out, req)
					return out
				}
				out := request(parent)
				expectedStatus, expectedError := tc.status, adapterErr.Error()
				if cancelAtBoundary {
					expectedStatus, expectedError = http.StatusGatewayTimeout, "compare timed out"
				}
				require.Equal(t, expectedStatus, out.Code)
				var failure map[string]any
				require.NoError(t, json.Unmarshal(out.Body.Bytes(), &failure))
				require.Equal(t, map[string]any{"error": expectedError}, failure, "partial adapter data must not be published")
				require.Equal(t, 1, adapter.calls)
				require.Equal(t, 1, factoryCalls)
				resolved, err := workflow.ResolveSeamStrings(workflow.SeamStringsInput{})
				require.NoError(t, err)
				require.Equal(t, workflow.CompareCmd{MovieID: "ABC-001", NFOPath: canonicalPath, ScalarStrategy: resolved.ScalarStrategy, ArrayStrategy: resolved.ArrayStrategy, SelectedScrapers: input.SelectedScrapers}, adapter.cmd)
				got, err := os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, original, got)

				// A new request through the same registered route can succeed. Error and
				// cancellation state belong to the failed operation, not the route.
				adapter.err, adapter.cancel = nil, nil
				out = request(t.Context())
				require.Equal(t, http.StatusOK, out.Code)
				require.Contains(t, out.Body.String(), "Partial adapter result")
				require.Equal(t, 2, adapter.calls)
				require.Equal(t, 2, factoryCalls)
				got, err = os.ReadFile(path)
				require.NoError(t, err)
				require.Equal(t, original, got)
			})
		}
	}
}
