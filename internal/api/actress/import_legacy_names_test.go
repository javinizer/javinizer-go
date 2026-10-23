package actress

import (
	"bytes"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

func TestHTTPImportLegacyOverrideRequestOnly(t *testing.T) {
	repo := newMockActressRepo(t)
	deps := ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
	router := gin.New()
	router.POST("/import", importActresses(deps))
	router.PUT("/actresses/:id", updateActress(deps))
	legacy := models.Actress{DMMID: 9732, FirstName: "Legacy", LastName: "Keep", ThumbURL: "keep.jpg", Origin: database.ActressOriginImport, Verified: true}
	require.NoError(t, repo.GetDB().Create(&legacy).Error)
	call := func(method, path, body string, status int) importSummaryResponse {
		request := httptest.NewRequest(method, path, bytes.NewBufferString(body))
		request.Header.Set("Content-Type", "application/json")
		response := httptest.NewRecorder()
		router.ServeHTTP(response, request)
		require.Equal(t, status, response.Code, response.Body.String())
		var result importSummaryResponse
		require.NoError(t, json.Unmarshal(response.Body.Bytes(), &result))
		return result
	}
	// Forged provenance fields cannot opt in an upgraded row; aliases still replace.
	result := call("POST", "/import", `{"overwrite_names":true,"actresses":[{"dmm_id":9732,"first_name":"Forbidden","aliases":"Replacement","managed":true,"import_names_managed":true,"origin":"import"}]}`, 200)
	require.Equal(t, 1, result.Updated)
	got, err := repo.FindByID(t.Context(), legacy.ID)
	require.NoError(t, err)
	require.Equal(t, "Legacy", got.FirstName)
	require.Equal(t, "Replacement", got.Aliases)
	result = call("POST", "/import", `{"overwrite_names":true,"overwrite_legacy_names":true,"actresses":[{"dmm_id":9732,"first_name":"Allowed","aliases":"Replacement"}]}`, 200)
	require.Equal(t, 1, result.Updated)
	got, err = repo.FindByID(t.Context(), legacy.ID)
	require.NoError(t, err)
	require.Equal(t, "Allowed", got.FirstName)
	require.Equal(t, "Keep", got.LastName)
	require.Equal(t, "keep.jpg", got.ThumbURL)
	result = call("POST", "/import", `{"overwrite_names":true,"actresses":[{"dmm_id":9732,"last_name":"Forbidden","aliases":"Replacement"}]}`, 200)
	require.Equal(t, 1, result.Protected)
	call("PUT", fmt.Sprintf("/actresses/%d", legacy.ID), `{"dmm_id":9732,"first_name":"Manual","last_name":"Keep","thumb_url":"keep.jpg","aliases":"Replacement"}`, 200)
	result = call("POST", "/import", `{"overwrite_names":true,"overwrite_legacy_names":true,"actresses":[{"dmm_id":9732,"first_name":"Forbidden","aliases":"Replacement"}]}`, 200)
	require.Equal(t, 1, result.Protected)
	got, err = repo.FindByID(t.Context(), legacy.ID)
	require.NoError(t, err)
	require.Equal(t, "Manual", got.FirstName)
	require.Equal(t, database.ActressOriginUser, got.Origin)
	call("POST", "/import", `{"overwrite_legacy_names":true,"actresses":[{"dmm_id":98765,"first_name":"Must not create"}]}`, http.StatusBadRequest)
	_, err = repo.FindByDMMID(t.Context(), 98765)
	require.Error(t, err)
}
