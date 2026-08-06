package worker

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
)

// fileFirstMovieResult skips files with no recorded result.
func TestFileFirstMovieResultSkipsVoidResults(t *testing.T) {
	store := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "FF-1", "")
	store.UpdateFileResult("/f/b.mp4", &resultstore.MovieResult{
		Status:        models.JobStatusCompleted,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "FF-1"},
	})
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "FF-1"}
	// Whole-family save walks both files; the void second one must be skipped
	// throughout (fileFirstMovieResult + identity walk), not fail the save.
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "FF-1", Title: "saved"}))
}

// rejectIdentityChangeLocked tolerates a family member whose result row is
// missing entirely (index lists the path, but no result row yet).
func TestForeignFamilyCheckSkipsMissingResultRows(t *testing.T) {
	store := resultstore.New(2, []string{"/f/a.mp4", "/f/x.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "FW-1", "")
	store.SetFileMatchInfo("/f/x.mp4", models.FileMatchInfo{Path: "/f/x.mp4", MovieID: "FW-1"})
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "FW-1"}
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "FW-1"}))
}

// EditAdmissionConflictError's zero-value message default.
func TestEditAdmissionConflictErrorDefaultMessage(t *testing.T) {
	var e EditAdmissionConflictError
	assert.Equal(t, "edit conflicts with job state", e.Error())
}

// backupCoverOriginal's nil guard.
func TestBackupCoverOriginalNilGuards(t *testing.T) {
	backupCoverOriginal(nil, &models.Movie{})
	backupCoverOriginal(&models.Movie{}, nil)
}

// ApplyFieldOverride wrapper: empty movieID falls back to the resultID as the
// initial key, then converges after one rekey-driven retry.
func TestApplyFieldOverrideWrapperKeyFallbackAndRetry(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	movie, prov := overrideFixture()
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-flip", Status: models.JobStatusCompleted, Movie: movie,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: movie.ID},
	})
	store.SetProvenance("/f/a.mp4", prov)
	pe := NewPosterEditor(store, store, nil)
	_, _, err := pe.ApplyFieldOverride(context.Background(), "res-flip", "", "maker", "dmm")
	require.NoError(t, err)
}

func TestFileFirstMovieResultNilWhenNoMovieRows(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	pe := newEditorForStore(store)
	m := &LockedMovieOps{pe: pe, movieID: "NOPE-1"}
	assert.Nil(t, fileFirstMovieResult(m, []string{"/f/a.mp4"}))
}

type errGetMovieLookup struct {
	resultstore.ResultReadFacade
	errPaths map[string]bool
}

func (l *errGetMovieLookup) GetMovieResult(fp string) (*resultstore.MovieResult, error) {
	if l.errPaths[fp] {
		return nil, assert.AnError
	}
	return l.ResultReadFacade.GetMovieResult(fp)
}

// The identity walk cannot treat an unreadable row as foreign; it is skipped.
func TestForeignFamilyCheckToleratesUnreadableRows(t *testing.T) {
	inner := resultstore.New(2, []string{"/f/a.mp4", "/f/x.mp4"})
	seedFamilyResult(inner, "/f/a.mp4", "res-1", "FW-2", "")
	seedFamilyResult(inner, "/f/x.mp4", "res-2", "TGT-2", "")
	l := &errGetMovieLookup{ResultReadFacade: inner, errPaths: map[string]bool{"/f/x.mp4": true}}
	pe := NewPosterEditor(l, inner, nil)
	m := &LockedMovieOps{pe: pe, movieID: "FW-2"}
	// Payload rekeys to an existing ID whose owning row is unreadable
	// mid-check: the walk skips it (fail-open for the self-check).
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "TGT-2"}))
}

type flipFMLookup struct {
	resultstore.ResultReadFacade
	calls  int
	byCall map[int]string
	vanish map[int]bool
}

func (f *flipFMLookup) GetFileResultByResultID(id string) (*resultstore.MovieResult, string, bool) {
	f.calls++
	if f.vanish[f.calls] {
		return nil, "", false
	}
	r, fp, ok := f.ResultReadFacade.GetFileResultByResultID(id)
	if !ok || r == nil {
		return r, fp, ok
	}
	c := r.Clone()
	if v := f.byCall[f.calls]; v != "" {
		c.FileMatchInfo.MovieID = v
	}
	return c, fp, true
}

func flipStore(t *testing.T) resultstore.Store {
	t.Helper()
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-flip", "PING-1", "")
	return store
}

// A family whose apparent identity changes between every resolution exhausts
// the 3-attempt retry budget before any write happens.
func TestApplyFieldOverrideWrapperExhaustsRetryBudget(t *testing.T) {
	fl := &flipFMLookup{ResultReadFacade: flipStore(t), byCall: map[int]string{2: "A", 3: "A", 5: "B", 6: "B", 8: "C"}}
	pe := NewPosterEditor(fl, nil, nil)
	_, _, err := pe.ApplyFieldOverride(context.Background(), "res-flip", "PING-1", "maker", "dmm")
	assert.ErrorContains(t, err, "retry budget exhausted")
	assert.ErrorIs(t, err, ErrFamilyRekeyed)
}

// A rekey whose re-resolution finds the result gone converts to the typed
// empty-family error.
func TestApplyFieldOverrideWrapperReresolutionVanished(t *testing.T) {
	fl := &flipFMLookup{ResultReadFacade: flipStore(t), byCall: map[int]string{2: "A"}, vanish: map[int]bool{3: true}}
	pe := NewPosterEditor(fl, nil, nil)
	_, _, err := pe.ApplyFieldOverride(context.Background(), "res-flip", "PING-1", "maker", "dmm")
	require.ErrorIs(t, err, ErrMovieFamilyEmpty)
}

// Unknown resultID passes the empty-family error straight through.
func TestApplyFieldOverrideWrapperUnknownResult(t *testing.T) {
	pe := NewPosterEditor(flipStore(t), nil, nil)
	_, _, err := pe.ApplyFieldOverride(context.Background(), "res-ghost", "PING-1", "maker", "dmm")
	require.ErrorIs(t, err, ErrMovieFamilyEmpty)
}

// All-excluded must NOT cancel the job while unrelated files remain (final
// guard arm of cancelIfAll).
func TestExcludeFamilyDoesNotCancelWithRemainingWork(t *testing.T) {
	store := resultstore.New(2, []string{"/f/a.mp4", "/f/z.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "EXC-2", "")
	seedFamilyResult(store, "/f/z.mp4", "res-z", "EXC-OTHER", "")
	lc := &JobLifecycle{Status: models.JobStatusRunning, done: make(chan struct{})}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{lifecycle: lc})
	m := &LockedMovieOps{pe: pe, movieID: "EXC-2"}
	require.NoError(t, m.ExcludeFamily(context.Background()))
	assert.Equal(t, models.JobStatusRunning, lc.GetJobStatus(), "unrelated pending work keeps the job alive")
}
