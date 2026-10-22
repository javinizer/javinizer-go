package actress

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

func TestImportActressesOptionsAndOutcomes(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "overwrite"}[overwrite], func(t *testing.T) {
			repo := mocks.NewMockActressRepositoryInterface(t)
			outcomes := []string{"created", "updated", "unchanged", "protected", "conflicted"}
			for i, outcome := range outcomes {
				id := uint(i + 1)
				repo.EXPECT().ImportUpsertWithOptions(mock.Anything, mock.MatchedBy(func(a *models.Actress) bool {
					return a.DMMID == int(id) && a.FirstName == "Name" && (a.ID == id && overwrite || a.ID == 0 && !overwrite)
				}), database.ActressImportOptions{OverwriteNames: overwrite}).RunAndReturn(func(_ context.Context, _ *models.Actress, _ database.ActressImportOptions) (*database.ActressImportResult, error) {
					var err error
					if outcome == "conflicted" {
						err = errors.New("identity conflict")
					}
					return &database.ActressImportResult{Outcome: outcome}, err
				}).Once()
			}
			payload := actressesImportRequest{OverwriteNames: overwrite}
			for i := range outcomes {
				payload.Actresses = append(payload.Actresses, actressImportItem{ID: uint(i + 1), DMMID: i + 1, FirstName: "Name"})
			}
			data, err := json.Marshal(payload)
			require.NoError(t, err)
			router := gin.New()
			router.POST("/actresses/import", importActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
			request := httptest.NewRequest(http.MethodPost, "/actresses/import", bytes.NewReader(data))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			var summary importSummaryResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &summary))
			assert.Equal(t, importSummaryResponse{Imported: 2, Skipped: 2, Errors: 1, Created: 1, Updated: 1, Unchanged: 1, Protected: 1, Conflicted: 1}, summary)
		})
	}
}

func TestImportActressesAllowsSparseDMMIdentity(t *testing.T) {
	repo := mocks.NewMockActressRepositoryInterface(t)
	repo.EXPECT().ImportUpsertWithOptions(mock.Anything, mock.MatchedBy(func(a *models.Actress) bool { return a.DMMID == 123 && a.FirstName == "" }), database.ActressImportOptions{OverwriteNames: true}).Return(&database.ActressImportResult{Outcome: "unchanged"}, nil).Once()
	router := gin.New()
	router.POST("/actresses/import", importActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
	request := httptest.NewRequest(http.MethodPost, "/actresses/import", bytes.NewBufferString(`{"overwrite_names":true,"actresses":[{"dmm_id":123},{"dmm_id":-1},{}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var summary importSummaryResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &summary))
	assert.Equal(t, importSummaryResponse{Skipped: 1, Errors: 2, Unchanged: 1}, summary)
}

func TestImportActressesOverwriteNamesIntegration(t *testing.T) {
	for _, overwrite := range []bool{false, true} {
		t.Run(map[bool]string{false: "default", true: "explicit"}[overwrite], func(t *testing.T) {
			repo := newMockActressRepo(t)
			imported := &models.Actress{DMMID: 123, FirstName: "Original", Origin: database.ActressOriginImport, Verified: true}
			require.NoError(t, repo.GetDB().Create(imported).Error)
			manual := &models.Actress{DMMID: 456, FirstName: "Manual", Origin: database.ActressOriginUser, Verified: true}
			require.NoError(t, repo.GetDB().Create(manual).Error)
			payload := actressesImportRequest{OverwriteNames: overwrite, Actresses: []actressImportItem{{ID: imported.ID, DMMID: 123, FirstName: "Replacement"}, {ID: manual.ID, DMMID: 456, FirstName: "DoNotReplace"}, {ID: 99999, FirstName: "Unknown"}}}
			data, err := json.Marshal(payload)
			require.NoError(t, err)
			router := gin.New()
			router.POST("/actresses/import", importActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
			request := httptest.NewRequest(http.MethodPost, "/actresses/import", bytes.NewReader(data))
			request.Header.Set("Content-Type", "application/json")
			response := httptest.NewRecorder()
			router.ServeHTTP(response, request)
			require.Equal(t, http.StatusOK, response.Code)
			var summary importSummaryResponse
			require.NoError(t, json.Unmarshal(response.Body.Bytes(), &summary))
			if overwrite {
				require.Equal(t, 1, summary.Errors)
				require.Equal(t, 1, summary.Conflicted)
			} else {
				require.Equal(t, 1, summary.Created)
				require.Zero(t, summary.Errors)
			}
			got, err := repo.FindByID(t.Context(), imported.ID)
			require.NoError(t, err)
			if overwrite {
				require.Equal(t, "Replacement", got.FirstName)
				require.Equal(t, 1, summary.Updated)
				require.Equal(t, 1, summary.Protected)
			} else {
				require.Equal(t, "Original", got.FirstName)
				require.Equal(t, 2, summary.Protected)
			}
			got, err = repo.FindByID(t.Context(), manual.ID)
			require.NoError(t, err)
			require.Equal(t, "Manual", got.FirstName)
			_, err = repo.FindByID(t.Context(), 99999)
			require.Error(t, err) // Default ignores the supplied local ID; opt-in rejects unknown IDs.
		})
	}
}

func TestImportActressesDefaultKeepsLegacySparseValidation(t *testing.T) {
	repo := mocks.NewMockActressRepositoryInterface(t)
	router := gin.New()
	router.POST("/actresses/import", importActresses(ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}))
	request := httptest.NewRequest(http.MethodPost, "/actresses/import", bytes.NewBufferString(`{"actresses":[{"id":42},{"dmm_id":123},{"last_name":"Only"}]}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	router.ServeHTTP(response, request)
	require.Equal(t, http.StatusOK, response.Code)
	var summary importSummaryResponse
	require.NoError(t, json.Unmarshal(response.Body.Bytes(), &summary))
	require.Equal(t, 3, summary.Errors)
}
