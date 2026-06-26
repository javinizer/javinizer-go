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
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// failingActressRepo implements ActressRepositoryInterface with configurable errors.
type failingActressRepo struct {
	database.ActressRepositoryInterface
	createErr            error
	updateErr            error
	findByIDErr          error
	deleteErr            error
	searchErr            error
	countErr             error
	countSearchErr       error
	listSortedErr        error
	searchPagedSortedErr error
	previewMergeErr      error
	mergeErr             error
	findByIDResult       *models.Actress
}

func (r *failingActressRepo) Create(_ context.Context, _ *models.Actress) error {
	if r.createErr != nil {
		return r.createErr
	}
	return nil
}

func (r *failingActressRepo) Update(_ context.Context, _ *models.Actress) error {
	if r.updateErr != nil {
		return r.updateErr
	}
	return nil
}

func (r *failingActressRepo) FindByID(_ context.Context, _ uint) (*models.Actress, error) {
	if r.findByIDErr != nil {
		return nil, r.findByIDErr
	}
	if r.findByIDResult != nil {
		return r.findByIDResult, nil
	}
	return &models.Actress{DMMID: 1, FirstName: "Fallback"}, nil
}

func (r *failingActressRepo) Delete(_ context.Context, _ uint) error {
	if r.deleteErr != nil {
		return r.deleteErr
	}
	return nil
}

func (r *failingActressRepo) Search(_ context.Context, _ string) ([]models.Actress, error) {
	if r.searchErr != nil {
		return nil, r.searchErr
	}
	return nil, nil
}

func (r *failingActressRepo) Count(_ context.Context) (int64, error) {
	if r.countErr != nil {
		return 0, r.countErr
	}
	return 0, nil
}

func (r *failingActressRepo) CountSearch(_ context.Context, _ string) (int64, error) {
	if r.countSearchErr != nil {
		return 0, r.countSearchErr
	}
	return 0, nil
}

func (r *failingActressRepo) ListSorted(_ context.Context, _, _ int, _, _ string) ([]models.Actress, error) {
	if r.listSortedErr != nil {
		return nil, r.listSortedErr
	}
	return nil, nil
}

func (r *failingActressRepo) SearchPagedSorted(_ context.Context, _ string, _, _ int, _, _ string) ([]models.Actress, error) {
	if r.searchPagedSortedErr != nil {
		return nil, r.searchPagedSortedErr
	}
	return nil, nil
}

func (r *failingActressRepo) PreviewMerge(_ context.Context, _, _ uint) (*database.ActressMergePreview, error) {
	if r.previewMergeErr != nil {
		return nil, r.previewMergeErr
	}
	return &database.ActressMergePreview{
		Target:         models.Actress{ID: 1, FirstName: "Target"},
		Source:         models.Actress{ID: 2, FirstName: "Source"},
		ProposedMerged: models.Actress{ID: 1, FirstName: "Merged"},
	}, nil
}

func (r *failingActressRepo) Merge(_ context.Context, _, _ uint, _ map[string]string) (*database.ActressMergeResult, error) {
	if r.mergeErr != nil {
		return nil, r.mergeErr
	}
	return &database.ActressMergeResult{
		MergedActress: models.Actress{ID: 1, FirstName: "Merged"},
	}, nil
}

func makeDeps(repo *failingActressRepo) ActressDeps {
	return ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
}

// --- createActress ---

func TestCreateActress_MalformedJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{}
	router := gin.New()
	router.POST("/actresses", createActress(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodPost, "/actresses", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestCreateActress_ValidationError_NegativeDMMID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{}
	router := gin.New()
	router.POST("/actresses", createActress(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{
		"dmm_id":     -1,
		"first_name": "Test",
	})
	req := httptest.NewRequest(http.MethodPost, "/actresses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "dmm_id must be greater than or equal to 0")
}

func TestCreateActress_CreateRepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{createErr: errors.New("db write failed")}
	router := gin.New()
	router.POST("/actresses", createActress(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{
		"dmm_id":     1,
		"first_name": "Test",
	})
	req := httptest.NewRequest(http.MethodPost, "/actresses", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "db write failed")
}

// --- updateActress ---

func TestUpdateActress_InvalidID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{}
	router := gin.New()
	router.PUT("/actresses/:id", updateActress(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{"dmm_id": 1, "first_name": "X"})
	req := httptest.NewRequest(http.MethodPut, "/actresses/abc", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "invalid actress id")
}

func TestUpdateActress_FindByIDInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{findByIDErr: errors.New("connection lost")}
	router := gin.New()
	router.PUT("/actresses/:id", updateActress(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{"dmm_id": 1, "first_name": "X"})
	req := httptest.NewRequest(http.MethodPut, "/actresses/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "connection lost")
}

func TestUpdateActress_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{} // FindByID returns default actress
	router := gin.New()
	router.PUT("/actresses/:id", updateActress(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodPut, "/actresses/1", bytes.NewReader([]byte("{bad")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestUpdateActress_ValidationError_MissingNames(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{}
	router := gin.New()
	router.PUT("/actresses/:id", updateActress(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{"dmm_id": 0})
	req := httptest.NewRequest(http.MethodPut, "/actresses/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
	assert.Contains(t, w.Body.String(), "either first_name or japanese_name is required")
}

func TestUpdateActress_UpdateRepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{updateErr: errors.New("update failed")}
	router := gin.New()
	router.PUT("/actresses/:id", updateActress(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{"dmm_id": 1, "first_name": "X"})
	req := httptest.NewRequest(http.MethodPut, "/actresses/1", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "update failed")
}

// --- deleteActress ---

func TestDeleteActress_FindByIDInternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{findByIDErr: errors.New("db error")}
	router := gin.New()
	router.DELETE("/actresses/:id", deleteActress(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodDelete, "/actresses/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "db error")
}

func TestDeleteActress_DeleteRepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{deleteErr: errors.New("delete constraint")}
	router := gin.New()
	router.DELETE("/actresses/:id", deleteActress(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodDelete, "/actresses/1", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "delete constraint")
}

// --- listActresses ---

func TestListActresses_CountError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{countErr: errors.New("count fail")}
	router := gin.New()
	router.GET("/actresses", listActresses(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodGet, "/actresses", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "count fail")
}

func TestListActresses_ListSortedError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{listSortedErr: errors.New("list fail")}
	router := gin.New()
	router.GET("/actresses", listActresses(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodGet, "/actresses", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "list fail")
}

func TestListActresses_CountSearchError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{countSearchErr: errors.New("countsearch fail")}
	router := gin.New()
	router.GET("/actresses", listActresses(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodGet, "/actresses?q=test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "countsearch fail")
}

func TestListActresses_SearchPagedSortedError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{searchPagedSortedErr: errors.New("searchpaged fail")}
	router := gin.New()
	router.GET("/actresses", listActresses(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodGet, "/actresses?q=test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "searchpaged fail")
}

func TestListActresses_TranslationErrorBestEffort(t *testing.T) {
	gin.SetMode(gin.TestMode)
	// Use a real DB so that list succeeds with data, but inject a translation repo that errors.
	_, repo, _ := setupActressTestDB(t)
	require.NoError(t, repo.Create(context.Background(), &models.Actress{DMMID: 1, FirstName: "Yui"}))

	// Translation repo that returns an error
	errTransRepo := &failingTransRepo{err: errors.New("translation lookup failed")}

	deps := ActressDeps{
		ContentRepos:     database.ContentRepos{ActressRepo: repo},
		TranslationRepos: database.TranslationRepos{ActressTranslationRepo: errTransRepo},
	}
	router := gin.New()
	router.GET("/actresses", listActresses(deps))

	req := httptest.NewRequest(http.MethodGet, "/actresses?include_translations=en", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	// Translation errors are best-effort; should still return 200
	assert.Equal(t, http.StatusOK, w.Code)
}

// failingTransRepo implements ActressTranslationRepositoryInterface with a configurable error.
type failingTransRepo struct {
	database.ActressTranslationRepositoryInterface
	err error
}

func (r *failingTransRepo) FindByActressIDsAndLanguage(_ context.Context, _ []uint, _ string) (map[uint][]models.ActressTranslation, error) {
	return nil, r.err
}

// --- searchActresses ---

func TestSearchActresses_RepoError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{searchErr: errors.New("search fail")}
	router := gin.New()
	router.GET("/actresses/search", searchActresses(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodGet, "/actresses/search?q=test", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
	assert.Contains(t, w.Body.String(), "internal server error")
}

func TestSearchActresses_EmptyQuery(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{}
	router := gin.New()
	router.GET("/actresses/search", searchActresses(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodGet, "/actresses/search?q=", nil)
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

// --- previewActressMerge ---

func TestPreviewActressMerge_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{}
	router := gin.New()
	router.POST("/actresses/merge/preview", previewActressMerge(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodPost, "/actresses/merge/preview", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPreviewActressMerge_SourceNotFound(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{previewMergeErr: database.ErrNotFound}
	router := gin.New()
	router.POST("/actresses/merge/preview", previewActressMerge(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{"target_id": 1, "source_id": 999})
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusNotFound, w.Code)
}

func TestPreviewActressMerge_SameID(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{previewMergeErr: database.ErrActressMergeSameID}
	router := gin.New()
	router.POST("/actresses/merge/preview", previewActressMerge(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{"target_id": 1, "source_id": 1})
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestPreviewActressMerge_InternalError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{previewMergeErr: errors.New("unexpected")}
	router := gin.New()
	router.POST("/actresses/merge/preview", previewActressMerge(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{"target_id": 1, "source_id": 2})
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge/preview", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusInternalServerError, w.Code)
}

// --- mergeActresses ---

func TestMergeActresses_InvalidJSON(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{}
	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(makeDeps(repo)))

	req := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader([]byte("not json")))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMergeActresses_InvalidIDError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{mergeErr: database.ErrActressMergeInvalidID}
	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{
		"target_id":   0,
		"source_id":   1,
		"resolutions": map[string]string{"first_name": "target"},
	})
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMergeActresses_UniqueConstraintError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{mergeErr: database.ErrActressMergeUniqueConstraint}
	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{
		"target_id":   1,
		"source_id":   2,
		"resolutions": map[string]string{"dmm_id": "source"},
	})
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusConflict, w.Code)
}

func TestMergeActresses_InvalidFieldError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{mergeErr: database.ErrActressMergeInvalidField}
	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{
		"target_id":   1,
		"source_id":   2,
		"resolutions": map[string]string{"bogus": "target"},
	})
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}

func TestMergeActresses_InvalidDecisionError(t *testing.T) {
	gin.SetMode(gin.TestMode)
	repo := &failingActressRepo{mergeErr: database.ErrActressMergeInvalidDecision}
	router := gin.New()
	router.POST("/actresses/merge", mergeActresses(makeDeps(repo)))

	body, _ := json.Marshal(map[string]interface{}{
		"target_id":   1,
		"source_id":   2,
		"resolutions": map[string]string{"first_name": "maybe"},
	})
	req := httptest.NewRequest(http.MethodPost, "/actresses/merge", bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)

	assert.Equal(t, http.StatusBadRequest, w.Code)
}
