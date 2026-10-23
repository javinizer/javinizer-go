package batch

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/google/uuid"
	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestListJobsUseCaseNormalizesNegativePagination(t *testing.T) {
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.On("List", mock.Anything).Return([]models.Job{{ID: "kept"}}, nil).Once()
	out, err := ListJobsUseCase(context.Background(), newTestAPIDeps(t, repo, nil), ListJobsInput{Limit: -1, Offset: -1})
	require.NoError(t, err)
	require.Equal(t, 1, out.Total)
	require.NotNil(t, out.Jobs)
	require.Empty(t, out.Jobs, "negative limit becomes zero without querying operation repositories")
}

func TestListBatchJobsPaginationAndRepositoryFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		name := "empty page beyond total"
		if fail {
			name = "repository error"
		}
		t.Run(name, func(t *testing.T) {
			repo := mocks.NewMockJobRepositoryInterface(t)
			var repoErr error
			if fail {
				repoErr = errors.New("list unavailable")
			}
			repo.On("List", mock.Anything).Return([]models.Job{{ID: "kept"}}, repoErr).Once()
			rt := core.NewAPIRuntime(newTestAPIDeps(t, repo, nil))
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/jobs?limit=999&offset=3", nil)
			listBatchJobs(rt)(c)
			if fail {
				require.Equal(t, http.StatusInternalServerError, rec.Code)
				require.JSONEq(t, `{"error":"failed to list jobs"}`, rec.Body.String())
				require.NotContains(t, rec.Body.String(), "list unavailable")
				return
			}
			require.Equal(t, http.StatusOK, rec.Code)
			var body contracts.BatchJobListResponse
			require.NoError(t, json.Unmarshal(rec.Body.Bytes(), &body))
			require.Equal(t, 1, body.Total)
			require.NotNil(t, body.Jobs)
			require.Empty(t, body.Jobs)
		})
	}
}

func TestParseJobResultsMalformedAndLegacyIdentity(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.Empty(t, parseAndConvertJobResults(&models.Job{ID: "broken", Results: `{`}, fs))
	job := &models.Job{ID: "legacy", Results: `{"/movies/ABC-123.mp4":{"status":"completed"}}`}
	first := parseAndConvertJobResults(job, fs)
	second := parseAndConvertJobResults(job, fs)
	require.Len(t, first, 1)
	require.NotNil(t, first["/movies/ABC-123.mp4"])
	require.Equal(t, uuid.NewSHA1(uuid.NameSpaceURL, []byte("/movies/ABC-123.mp4")).String(), first["/movies/ABC-123.mp4"].ResultID)
	require.Equal(t, first, second, "polling legacy records must preserve stable result identities")
	require.Equal(t, models.JobStatusCompleted, first["/movies/ABC-123.mp4"].Status)
}
