package actress

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/database"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type finalActressRepo struct {
	database.ActressRepositoryInterface
	findByID                   func(uint) (*models.Actress, error)
	findByDMMID                func(int) (*models.Actress, error)
	list                       func(int, int) ([]models.Actress, error)
	findByJapaneseNameAndDMMID func(string, int) (*models.Actress, error)
	create                     func(*models.Actress) error
	update                     func(*models.Actress) error
}

func (r *finalActressRepo) FindByID(_ context.Context, id uint) (*models.Actress, error) {
	return r.findByID(id)
}
func (r *finalActressRepo) FindByDMMID(_ context.Context, id int) (*models.Actress, error) {
	return r.findByDMMID(id)
}
func (r *finalActressRepo) List(_ context.Context, limit, offset int) ([]models.Actress, error) {
	return r.list(limit, offset)
}
func (r *finalActressRepo) FindByJapaneseNameAndDMMID(_ context.Context, name string, id int) (*models.Actress, error) {
	return r.findByJapaneseNameAndDMMID(name, id)
}
func (r *finalActressRepo) Create(_ context.Context, actress *models.Actress) error {
	return r.create(actress)
}
func (r *finalActressRepo) Update(_ context.Context, actress *models.Actress) error {
	return r.update(actress)
}

func depsWithFinalRepo(repo database.ActressRepositoryInterface) ActressDeps {
	return ActressDeps{ContentRepos: database.ContentRepos{ActressRepo: repo}}
}

func TestGetActressRemainingRepositoryErrors(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		code int
	}{
		{"not found", database.ErrNotFound, http.StatusNotFound},
		{"internal", errors.New("read failed"), http.StatusInternalServerError},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &finalActressRepo{findByID: func(uint) (*models.Actress, error) { return nil, tc.err }}
			router := gin.New()
			router.GET("/:id", getActress(depsWithFinalRepo(repo)))
			w := httptest.NewRecorder()
			router.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/1", nil))
			assert.Equal(t, tc.code, w.Code)
		})
	}
}

func TestUpdateActressDuplicateLookupBranches(t *testing.T) {
	for _, tc := range []struct {
		name string
		find func(int) (*models.Actress, error)
		code int
	}{
		{"lookup error", func(int) (*models.Actress, error) { return nil, errors.New("duplicate lookup failed") }, http.StatusInternalServerError},
		{"conflict", func(int) (*models.Actress, error) { return &models.Actress{ID: 2}, nil }, http.StatusConflict},
	} {
		t.Run(tc.name, func(t *testing.T) {
			repo := &finalActressRepo{
				findByID:    func(uint) (*models.Actress, error) { return &models.Actress{ID: 1}, nil },
				findByDMMID: tc.find,
				update:      func(*models.Actress) error { return nil },
			}
			router := gin.New()
			router.PUT("/:id", updateActress(depsWithFinalRepo(repo)))
			w := httptest.NewRecorder()
			req := httptest.NewRequest(http.MethodPut, "/1", bytes.NewBufferString(`{"dmm_id":7,"first_name":"Name"}`))
			req.Header.Set("Content-Type", "application/json")
			router.ServeHTTP(w, req)
			assert.Equal(t, tc.code, w.Code, w.Body.String())
		})
	}
}

type failAtWriter struct {
	header http.Header
	writes int
	failAt int
}

func (w *failAtWriter) Header() http.Header {
	if w.header == nil {
		w.header = make(http.Header)
	}
	return w.header
}
func (w *failAtWriter) WriteHeader(int) {}
func (w *failAtWriter) Write(p []byte) (int, error) {
	w.writes++
	if w.writes == w.failAt {
		return 0, errors.New("write failed")
	}
	return len(p), nil
}

func invokeExport(t *testing.T, repo database.ActressRepositoryInterface, writer http.ResponseWriter) {
	t.Helper()
	c, _ := gin.CreateTestContext(writer)
	c.Request = httptest.NewRequest(http.MethodGet, "/export", nil)
	exportActresses(depsWithFinalRepo(repo))(c)
}

func TestExportActressesWriteAndRepositoryFailures(t *testing.T) {
	oneThen := func(finalErr error) *finalActressRepo {
		return &finalActressRepo{list: func(_ int, offset int) ([]models.Actress, error) {
			if offset == 0 {
				return []models.Actress{{ID: 1, FirstName: "A"}, {ID: 2, FirstName: "B"}}, nil
			}
			return nil, finalErr
		}}
	}
	invokeExport(t, oneThen(nil), &failAtWriter{failAt: 1}) // opening bracket
	invokeExport(t, oneThen(nil), &failAtWriter{failAt: 2}) // encoder
	invokeExport(t, oneThen(nil), &failAtWriter{failAt: 3}) // comma

	empty := &finalActressRepo{list: func(int, int) ([]models.Actress, error) { return nil, nil }}
	invokeExport(t, empty, &failAtWriter{failAt: 2}) // closing bracket

	firstErr := &finalActressRepo{list: func(int, int) ([]models.Actress, error) { return nil, errors.New("list failed") }}
	invokeExport(t, firstErr, httptest.NewRecorder())
	invokeExport(t, oneThen(errors.New("later list failed")), httptest.NewRecorder())
}

func TestImportActressesCoversAllItemOutcomes(t *testing.T) {
	repo := &finalActressRepo{
		findByJapaneseNameAndDMMID: func(_ string, id int) (*models.Actress, error) {
			switch id {
			case 3:
				return nil, errors.New("lookup failed")
			case 5:
				return &models.Actress{ID: 5, DMMID: 5, FirstName: "Old"}, nil
			case 6:
				return &models.Actress{ID: 6, DMMID: 6, FirstName: "Same"}, nil
			case 7:
				return &models.Actress{ID: 7, DMMID: 7, FirstName: "Old"}, nil
			default:
				return nil, database.ErrNotFound
			}
		},
		create: func(a *models.Actress) error {
			if a.DMMID == 4 {
				return errors.New("create failed")
			}
			return nil
		},
		update: func(a *models.Actress) error {
			if a.DMMID == 5 {
				return errors.New("update failed")
			}
			return nil
		},
	}
	body := `{"actresses":[
		{"dmm_id":1},
		{"dmm_id":-1,"first_name":"Bad"},
		{"dmm_id":3,"first_name":"Lookup"},
		{"dmm_id":4,"first_name":"CreateFail"},
		{"dmm_id":5,"first_name":"Changed"},
		{"dmm_id":6,"first_name":"Same"},
		{"dmm_id":7,"first_name":"Changed"},
		{"dmm_id":8,"first_name":"Created"}
	]}`
	router := gin.New()
	router.POST("/import", importActresses(depsWithFinalRepo(repo)))
	w := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/import", bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	router.ServeHTTP(w, req)
	require.Equal(t, http.StatusOK, w.Code, w.Body.String())
	assert.JSONEq(t, `{"imported":2,"skipped":1,"errors":5}`, w.Body.String())
}

func TestCancelActressSyncJobGetFailureAfterCancel(t *testing.T) {
	oldEnsure, oldCancel, oldGet := ensureSyncManager, cancelSyncJob, getSyncJob
	t.Cleanup(func() { ensureSyncManager, cancelSyncJob, getSyncJob = oldEnsure, oldCancel, oldGet })
	ensureSyncManager = func(*core.APIRuntime) *worker.ActressSyncManager { return &worker.ActressSyncManager{} }
	cancelSyncJob = func(*worker.ActressSyncManager, string) error { return nil }
	getSyncJob = func(*worker.ActressSyncManager, string) (*models.ActressSyncJob, error) {
		return nil, errors.New("reload failed")
	}

	rt := core.NewAPIRuntime(&core.APIDeps{})
	router := gin.New()
	router.POST("/:jobID", cancelActressSyncJob(rt))
	w := httptest.NewRecorder()
	router.ServeHTTP(w, httptest.NewRequest(http.MethodPost, "/job", nil))
	assert.Equal(t, http.StatusInternalServerError, w.Code)
}
