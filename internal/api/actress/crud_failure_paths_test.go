package actress

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/stretchr/testify/require"
)

type crudFailureRepository struct {
	database.ActressRepositoryInterface
	record    models.Actress
	lookupErr error
	dmmErr    error
	updates   int
	chunks    [][]models.Actress
	listCalls int
	failList  int
}

func (r *crudFailureRepository) FindByID(context.Context, uint) (*models.Actress, error) {
	if r.lookupErr != nil {
		return nil, r.lookupErr
	}
	copy := r.record
	return &copy, nil
}
func (r *crudFailureRepository) FindByDMMID(context.Context, int) (*models.Actress, error) {
	return nil, r.dmmErr
}
func (r *crudFailureRepository) Update(_ context.Context, a *models.Actress) error {
	r.updates++
	r.record = *a
	return nil
}
func (r *crudFailureRepository) List(_ context.Context, limit, offset int) ([]models.Actress, error) {
	r.listCalls++
	if r.listCalls == r.failList {
		return nil, errors.New("stream repository unavailable")
	}
	if r.listCalls > len(r.chunks) {
		return nil, nil
	}
	return r.chunks[r.listCalls-1], nil
}
func crudFailureDeps(repo database.ActressRepositoryInterface) ActressDeps {
	return ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
}

func TestGetActressRepositoryFailure(t *testing.T) {
	repo := &crudFailureRepository{lookupErr: errors.New("lookup unavailable")}
	router := gin.New()
	router.GET("/actresses/:id", getActress(crudFailureDeps(repo)))
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/actresses/1", nil))
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "lookup unavailable")
}

func TestUpdateActressDMMFailurePreservesStoredRecord(t *testing.T) {
	original := models.Actress{ID: 1, DMMID: 10, FirstName: "Original"}
	repo := &crudFailureRepository{record: original, dmmErr: errors.New("DMM lookup unavailable")}
	router := gin.New()
	router.PUT("/actresses/:id", updateActress(crudFailureDeps(repo)))
	req := httptest.NewRequest(http.MethodPut, "/actresses/1", strings.NewReader(`{"first_name":"Changed","dmm_id":20}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Contains(t, rec.Body.String(), "DMM lookup unavailable")
	require.Zero(t, repo.updates)
	require.Equal(t, original, repo.record)
}

func TestUpdateActressDuplicateDMMPreservesBothRecords(t *testing.T) {
	repo := newMockActressRepo(t)
	first := models.Actress{DMMID: 10, FirstName: "Original"}
	second := models.Actress{DMMID: 20, FirstName: "Other"}
	require.NoError(t, repo.Create(t.Context(), &first))
	require.NoError(t, repo.Create(t.Context(), &second))
	router := gin.New()
	router.PUT("/actresses/:id", updateActress(crudFailureDeps(repo)))
	req := httptest.NewRequest(http.MethodPut, "/actresses/"+strconv.FormatUint(uint64(first.ID), 10), strings.NewReader(`{"first_name":"Changed","dmm_id":20}`))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	router.ServeHTTP(rec, req)
	require.Equal(t, http.StatusConflict, rec.Code)
	require.Contains(t, rec.Body.String(), "already used")
	stored, err := repo.FindByID(t.Context(), first.ID)
	require.NoError(t, err)
	require.Equal(t, first.FirstName, stored.FirstName)
	require.Equal(t, first.DMMID, stored.DMMID)
	other, err := repo.FindByID(t.Context(), second.ID)
	require.NoError(t, err)
	require.Equal(t, second.FirstName, other.FirstName)
	require.Equal(t, second.DMMID, other.DMMID)
}

type disconnectedExportWriter struct {
	*httptest.ResponseRecorder
	failAt int
	calls  int
}

func (w *disconnectedExportWriter) Write(p []byte) (int, error) {
	w.calls++
	if w.calls >= w.failAt {
		return 0, errors.New("client disconnected")
	}
	return w.ResponseRecorder.Write(p)
}

func TestExportActressesStopsAfterWriterFailure(t *testing.T) {
	records := []models.Actress{{ID: 1, FirstName: "First"}, {ID: 2, FirstName: "Second"}}
	first, _ := json.Marshal(records[0])
	second, _ := json.Marshal(records[1])
	for _, tc := range []struct {
		name              string
		failAt, listCalls int
		prefix            string
	}{
		{"opening bracket", 1, 0, ""},
		{"first encoded record", 2, 1, "["},
		{"separator", 3, 1, "[" + string(first) + "\n"},
		{"closing bracket", 5, 2, "[" + string(first) + "\n," + string(second) + "\n"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &crudFailureRepository{chunks: [][]models.Actress{records}}
			w := &disconnectedExportWriter{ResponseRecorder: httptest.NewRecorder(), failAt: tc.failAt}
			c, _ := gin.CreateTestContext(w)
			c.Request = httptest.NewRequest(http.MethodGet, "/actresses/export", nil)
			exportActresses(crudFailureDeps(repo))(c)
			require.Equal(t, tc.failAt, w.calls, "streaming must stop at first failed write")
			require.Equal(t, tc.listCalls, repo.listCalls)
			require.Equal(t, tc.prefix, w.Body.String())
			require.False(t, json.Valid(w.Body.Bytes()), "a disconnected export must not masquerade as a complete JSON array")
		})
	}
}

func TestExportActressesStopsOnRepositoryFailure(t *testing.T) {
	for _, afterRecord := range []bool{false, true} {
		name := "before first record"
		failAt := 1
		var chunks [][]models.Actress
		if afterRecord {
			name = "after first record"
			failAt = 2
			chunks = [][]models.Actress{{{ID: 1, FirstName: "First"}}}
		}
		t.Run(name, func(t *testing.T) {
			repo := &crudFailureRepository{chunks: chunks, failList: failAt}
			rec := httptest.NewRecorder()
			c, _ := gin.CreateTestContext(rec)
			c.Request = httptest.NewRequest(http.MethodGet, "/actresses/export", nil)
			exportActresses(crudFailureDeps(repo))(c)
			require.Equal(t, failAt, repo.listCalls)
			require.False(t, json.Valid(rec.Body.Bytes()), "interrupted stream cannot be reported as complete")
			require.True(t, strings.HasPrefix(rec.Body.String(), "["))
			if afterRecord {
				require.Contains(t, rec.Body.String(), "First")
			}
		})
	}
}
