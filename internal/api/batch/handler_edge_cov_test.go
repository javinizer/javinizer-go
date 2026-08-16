package batch

import (
	"bytes"
	"encoding/json"
	"errors"
	"image"
	"image/jpeg"
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/api/contracts"
	"github.com/javinizer/javinizer-go/internal/api/core"
	"github.com/javinizer/javinizer-go/internal/api/testkit"
	"github.com/javinizer/javinizer-go/internal/config"
	workermocks "github.com/javinizer/javinizer-go/internal/mocks/worker"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/poster"
	"github.com/javinizer/javinizer-go/internal/worker"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// --- admission plumbing units ---

func newGinCtx(t *testing.T) (*gin.Context, *httptest.ResponseRecorder) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	return c, w
}

func TestMapBatchEditErrorTypedMatrix(t *testing.T) {
	c, _ := newGinCtx(t)
	assert.False(t, mapBatchEditError(c, nil))

	cases := []struct {
		err  error
		code int
	}{
		{worker.ErrJobGone, 410},
		{worker.ErrJobNotFound, 404},
		{worker.ErrMovieFamilyEmpty, 404},
		{worker.ErrEditNotAdmitted, 409},
		{worker.ErrFamilyRekeyed, 409},
		{&worker.EditAdmissionConflictError{Message: "identity"}, 409},
	}
	for _, tc := range cases {
		c2, w := newGinCtx(t)
		assert.True(t, mapBatchEditError(c2, tc.err), "%v", tc.err)
		assert.Equal(t, tc.code, w.Code, "%v", tc.err)
	}
	c3, _ := newGinCtx(t)
	assert.False(t, mapBatchEditError(c3, errors.New("plain untyped error")))
}

func TestAdmitOrWriteErrorUnknownIs500(t *testing.T) {
	c, w := newGinCtx(t)
	acquire := func(string) (worker.BatchJobInterface, func(), error) {
		return nil, nil, errors.New("store exploded")
	}
	_, _, ok := admitOrWriteError(c, acquire)
	assert.False(t, ok)
	assert.Equal(t, 500, w.Code)
}

func TestRevisionEchoHelpersNotFoundArms(t *testing.T) {
	job := workermocks.NewMockBatchJobInterface(t)
	job.EXPECT().GetFileResultByResultID("nope").Return(nil, "", false)
	assert.Nil(t, currentResultRevision(job, "nope"))
	assert.Nil(t, familyRevisions(job, "nope"))

	job2 := workermocks.NewMockBatchJobInterface(t)
	job2.EXPECT().GetFileResultByResultID("res-x").Return(&resultstore.MovieResult{}, "/f/a.mp4", true)
	job2.EXPECT().FindFilePathsForMovieID(mock.Anything).Return([]string{"/f/a.mp4"})
	job2.EXPECT().GetMovieResult("/f/a.mp4").Return(nil, errors.New("read wedged"))
	assert.Empty(t, familyRevisions(job2, "res-x"))
}

// --- crop handler branches ---

func TestPosterCrop_FamilyMovedInsideKeyIsConflict(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPE-9")
	mockJob := workermocks.NewMockBatchJobInterface(t)
	movieID := "CROPE-9"
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(&resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/CROPE-9.mp4", MovieID: movieID},
		Movie:         &models.Movie{ID: movieID},
	}, "/path/to/CROPE-9.mp4", true).Once()
	mockJob.EXPECT().GetFileResultByResultID(movieID).Return(&resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/CROPE-9.mp4", MovieID: "OTHER-3"},
		Movie:         &models.Movie{ID: "OTHER-3"},
	}, "/path/to/CROPE-9.mp4", true)
	mockJob.EXPECT().FindMovieResultForMovieID(movieID).Return(nil, nil).Maybe()
	mockJob.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{"/path/to/CROPE-9.mp4"}).Maybe()
	mockJob.EXPECT().WithMovieEditLock(movieID, mock.Anything).
		RunAndReturn(func(_ string, fn func(*worker.LockedMovieOps) error) error {
			return fn(nil)
		})
	deps.JobStore = &cropErrorJobStore{job: mockJob}
	w := postCrop(t, router, job, "CROPE-9", contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	assert.Equal(t, 409, w.Code)
	assert.Contains(t, w.Body.String(), "moved to family")
}

func TestPosterCrop_ManagerErrorRestoresBackupAnd400(t *testing.T) {
	deps, job, router := cropJobFixture(t, "CROPE-2")
	// Wedge the STAGED full-size read (post-codex-P2 the canonical read is
	// fail-closed at staging — see TestPosterCrop_StagingSourceReadError);
	// CropWithBounds then errors → cropErr → 400, canonical untouched.
	deps.Fs = &brokenFS{Fs: deps.GetFs(), failOpen: func(n string) bool {
		return strings.Contains(filepath.Base(n), ".crop-") && strings.HasSuffix(filepath.Base(n), "-full.jpg")
	}}
	w := postCrop(t, router, job, "CROPE-2", contracts.PosterCropRequest{X: 0, Y: 0, Width: 100, Height: 100})
	assert.Equal(t, 400, w.Code)
	_, err := os.Stat(filepath.Join("data/temp/posters", job.GetID(), "CROPE-2-full.jpg"))
	require.NoError(t, err, "the pre-op bytes must remain: backup restore never deleted them")
}

// --- from-URL handler branches (mock job + real downloader on test server) ---

func fromURLFixture(t *testing.T, movieID string) (*coreDepsOut, *worker.BatchJob, *workermocks.MockBatchJobInterface, *gin.Engine, *httptest.Server) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	deps := createTestDeps(t, &config.Config{System: config.SystemConfig{TempDir: "data/temp"}}, "")
	job := deps.JobStore.CreateJobBatch([]string{"/path/to/" + movieID + ".mp4"})
	job.Controller().SetJobStatus(models.JobStatusCompleted)

	imgSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		img := image.NewRGBA(image.Rect(0, 0, 64, 96))
		w.Header().Set("Content-Type", "image/jpeg")
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	t.Cleanup(imgSrv.Close)

	mockJ := workermocks.NewMockBatchJobInterface(t)
	mockJ.EXPECT().GetFileResultByResultID(movieID).Return(&resultstore.MovieResult{
		ResultID:      movieID,
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/" + movieID + ".mp4", MovieID: movieID},
		Movie:         &models.Movie{ID: movieID},
	}, "/path/to/"+movieID+".mp4", true).Once()
	mockJ.EXPECT().FindMovieResultForMovieID(movieID).Return(&resultstore.MovieResult{Movie: &models.Movie{ID: movieID}}, nil).Maybe()
	mockJ.EXPECT().FindFilePathsForMovieID(movieID).Return([]string{"/path/to/" + movieID + ".mp4"}).Maybe()
	deps.JobStore = &cropErrorJobStore{job: mockJ}

	rt := testkit.GetTestRuntime(deps)
	rt.GetRuntime().GetPosterManager(func() poster.PosterManagerInterface {
		return poster.NewPosterManager(deps.GetFs(), "data/temp", &http.Client{}, 0).
			WithSSRFCheck(func(string) error { return nil })
	})

	router := gin.New()
	router.POST("/batch/:id/results/:resultId/poster-from-url", updateBatchMoviePosterFromURL(rt))
	return deps, job, mockJ, router, imgSrv
}

type coreDepsOut = core.APIDeps

func postFromURLRequest(t *testing.T, router *gin.Engine, job *worker.BatchJob, resultID, url string) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(contracts.PosterFromURLRequest{URL: url})
	require.NoError(t, err)
	req := httptest.NewRequest("POST", "/batch/"+job.GetID()+"/results/"+resultID+"/poster-from-url", bytes.NewBuffer(body))
	req.Header.Set("Content-Type", "application/json")
	w := httptest.NewRecorder()
	router.ServeHTTP(w, req)
	return w
}

func TestPosterFromURL_FamilyMovedDuringDownload(t *testing.T) {
	_, job, mockJ, router, ts := fromURLFixture(t, "URLC-1")
	mockJ.EXPECT().GetFileResultByResultID("URLC-1").Return(&resultstore.MovieResult{
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/URLC-1.mp4", MovieID: "OTHER-9"},
		Movie:         &models.Movie{ID: "OTHER-9"},
	}, "/path/to/URLC-1.mp4", true)
	mockJ.EXPECT().WithMovieEditLock("URLC-1", mock.Anything).
		RunAndReturn(func(_ string, fn func(*worker.LockedMovieOps) error) error { return fn(nil) })
	w := postFromURLRequest(t, router, job, "URLC-1", ts.URL+"/pic.jpg")
	assert.Equal(t, 409, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "moved to family")
}

func TestPosterFromURL_RevisionChangedDuringDownload(t *testing.T) {
	_, job, mockJ, router, ts := fromURLFixture(t, "URLC-2")
	mockJ.EXPECT().GetFileResultByResultID("URLC-2").Return(&resultstore.MovieResult{
		ResultID:      "URLC-2",
		Revision:      5,
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/URLC-2.mp4", MovieID: "URLC-2"},
		Movie:         &models.Movie{ID: "URLC-2"},
	}, "/path/to/URLC-2.mp4", true)
	mockJ.EXPECT().WithMovieEditLock("URLC-2", mock.Anything).
		RunAndReturn(func(_ string, fn func(*worker.LockedMovieOps) error) error { return fn(nil) })
	w := postFromURLRequest(t, router, job, "URLC-2", ts.URL+"/pic.jpg")
	assert.Equal(t, 409, w.Code, "body: %s", w.Body.String())
	assert.Contains(t, w.Body.String(), "retry")
}

func TestPosterFromURL_CommitFailureFinalizesAndReturnsTyped404(t *testing.T) {
	_, job, mockJ, router, ts := fromURLFixture(t, "URLC-3")
	// Capture a LockedMovieOps wired to an EMPTY family store so the commit
	// inside the keyed section fails with the typed empty-family error.
	emptyStore := resultstore.New(0, nil)
	pe := worker.NewPosterEditor(emptyStore, emptyStore, nil)
	var captured *worker.LockedMovieOps
	_ = pe.WithMovieEditLock("URLC-3", func(m *worker.LockedMovieOps) error { captured = m; return nil })
	mockJ.EXPECT().GetFileResultByResultID("URLC-3").Return(&resultstore.MovieResult{
		ResultID:      "URLC-3",
		FileMatchInfo: models.FileMatchInfo{Path: "/path/to/URLC-3.mp4", MovieID: "URLC-3"},
		Movie:         &models.Movie{ID: "URLC-3"},
	}, "/path/to/URLC-3.mp4", true)
	mockJ.EXPECT().WithMovieEditLock("URLC-3", mock.Anything).
		RunAndReturn(func(_ string, fn func(*worker.LockedMovieOps) error) error { return fn(captured) })
	w := postFromURLRequest(t, router, job, "URLC-3", ts.URL+"/pic.jpg")
	assert.Equal(t, 404, w.Code, "body: %s", w.Body.String())
}

func TestPosterFromURL_DownloadRefusedClassified500(t *testing.T) {
	_, job, mockJ, router, _ := fromURLFixture(t, "URLC-6")
	_ = mockJ
	deferredPort := freePort(t)
	w := postFromURLRequest(t, router, job, "URLC-6", "http://127.0.0.1:"+strconv.Itoa(deferredPort)+"/pic.jpg")
	t.Logf("status=%d body=%s", w.Code, w.Body.String())
	assert.True(t, w.Code == 500 || w.Code == 502, "connection-refused is neither SSRF nor status-coded: %d %s", w.Code, w.Body.String())
}

func freePort(t *testing.T) int {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer func() { _ = ln.Close() }()
	return ln.Addr().(*net.TCPAddr).Port
}

func TestPosterFromURL_DownloadStatusClassifiedBadGateway(t *testing.T) {
	gin.SetMode(gin.TestMode)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusServiceUnavailable)
	}))
	t.Cleanup(ts.Close)
	_, job, mockJ, router, _ := fromURLFixture(t, "URLC-4")
	_ = mockJ
	w := postFromURLRequest(t, router, job, "URLC-4", ts.URL+"/down")
	assert.True(t, w.Code == 502 || w.Code == 500, "download failure classifies as 5xx, got %d: %s", w.Code, w.Body.String())
}
