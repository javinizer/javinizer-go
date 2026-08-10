package worker

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/mocks"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/scrape"
	"github.com/javinizer/javinizer-go/internal/worker/resultstore"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

// JobEditor-seam coverage: the WithEcho passthrough delegates to the editor
// inside the section and surfaces the echo.
func TestJobEditorImplWithEchoDelegates(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "JEDO-1", "")
	ed := &jobEditorImpl{store: store, lifecycle: &JobLifecycle{Status: models.JobStatusCompleted, done: make(chan struct{})}}
	rev, fam, err := ed.UpdateMovieFamilyWithEcho(context.Background(), "JEDO-1", "res-1", &models.Movie{ID: "JEDO-1", Title: "echo"}, FamilySaveOptions{})
	require.NoError(t, err)
	require.NotNil(t, rev)
	require.Len(t, fam, 1)
	live, lerr := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, lerr)
	assert.Equal(t, live.Revision, *rev, "echo = committed revision")
}

// audit F-R12-1: patch key selection must include the STORED canonical
// Movie.ID — otherwise the key-held rescrape generation window (named by the
// canonical byte marker) and the rekey relocation collide.
func TestLockKeysForIncludesStoredCanonicalMovieID(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	// Alias≠canonical family: matcher alias is the family key, canonical Movie.ID
	// names the bytes — resolvable both ways at insert time.
	store.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID:      "res-1",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "CANON-1", ContentID: "cid-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "ALIAS-1"},
	})
	pe := newEditorForStore(store)
	keys := pe.lockKeysFor("ALIAS-1", &models.Movie{ID: "NEW-1"})
	lower := []string{}
	for _, k := range keys {
		lower = append(lower, strings.ToLower(strings.TrimSpace(k)))
	}
	assert.Contains(t, lower, "alias-1", "matcher alias surfaces")
	assert.Contains(t, lower, "canon-1", "stored canonical surfaces (F-R12-1)")
	assert.Contains(t, lower, "new-1", "incoming ID surfaces")
	assert.Contains(t, lower, "cid:cid-1", "content-id surfaces")
	_ = keys
	// Same-case: incoming == stored folds to one logical key.
	sameKeys := pe.lockKeysFor("ALIAS-1", &models.Movie{ID: "CANON-1"})
	folded := map[string]struct{}{}
	for _, k := range sameKeys {
		folded[strings.ToLower(strings.TrimSpace(k))] = struct{}{}
	}
	for _, want := range []string{"alias-1", "canon-1", "cid:cid-1"} {
		_, ok := folded[want]
		assert.True(t, ok, "%s present in saved keys", want)
	}
}

// The destination-scan wedge inside relocation hard-errors (fail closed).
func TestRekeyDestinationScanWedgeFailsClosed(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	mem := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-DW")
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, ".rekey-x.json"), []byte("{\"old_id\":\"UNREL\",\"new_id\":\"UNREL2\"}"), 0o644))
	// probe ledger (codex cloud P1 folded scans): promote ReadDir #1, rekey
	// ReadDir #2 + witness read #3, in-flight ReadDir #4, crop ReadDir #5 —
	// wedge #6 = the relocation's destination content scan.
	fs := &openFailAfterNFS{Fs: mem, suffix: "/tmp/posters/JOB-DW", allow: 5}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-DW"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "destination check", "destination scan wedge rejects the rekey")
}

// audit F-R8-1: the witness pin, canonical ID and revision all derive from
// ONE result (the first MOVIE-BEARING family part) — a movie-less failed
// sibling part must never supply a divergent pin.
func TestRekeyWitnessPinSkipsMovielessPart(t *testing.T) {
	store3 := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	store3.UpdateFileResult("/f/a.mp4", &resultstore.MovieResult{
		ResultID: "res-bad", Status: models.JobStatusFailed, Revision: 1,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "MIX-9"}, // Movie nil (failed scrape)
	})
	seedFamilyResult(store3, "/f/b.mp4", "res-ok", "MIX-9", "")
	cur, curErr := store3.GetMovieResult("/f/b.mp4")
	require.NoError(t, curErr)
	okRevision := cur.Revision

	base3 := afero.NewMemMapFs()
	dir3 := filepath.Join("/tmp", "posters", "JOB-PN")
	require.NoError(t, base3.MkdirAll(dir3, 0o755))
	require.NoError(t, afero.WriteFile(base3, filepath.Join(dir3, "MIX-9.jpg"), []byte("x"), 0o644))
	committer3 := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-PN", newKeyedMutexRegistry())
	pe3 := newEditorForStore(store3)
	fsWedge := &seqRenameFailFS{Fs: base3, failOn: map[int]bool{3: true}}
	pe3.attachEnv(&posterEditEnv{fs: fsWedge, tempDir: "/tmp", jobID: "JOB-PN", committer: committer3, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m3 := &LockedMovieOps{pe: pe3, movieID: "MIX-9"}
	require.Error(t, m3.UpdateMovieFamily(context.Background(), &models.Movie{ID: "MIX-N9"}))
	data, rerr := afero.ReadFile(base3, filepath.Join(dir3, ".rekey-MIX-9.json"))
	require.NoError(t, rerr, "witness retained")
	var w rekeyWitness
	require.NoError(t, json.Unmarshal(data, &w))
	assert.Equal(t, "res-ok", w.ResultID, "pin follows the MOVIE-bearing part, not the failed sibling")
	assert.Equal(t, okRevision, w.PrevRevision, "revision matches the pinned part")
}

// audit F-R8-2: relocation refuses while a rescrape's parked backups for the
// canonical ID exist (in-flight generation owns those bytes).
func TestRekeyRefusedWhileParkedBackupsPending(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "SSNI-R1.jpg.rsbak.a1.b2"), []byte("parked"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	assert.Contains(t, err.Error(), "in-flight rescrape")

	// full-leg shape fences too
	require.NoError(t, fs.Remove(filepath.Join(dir, "SSNI-R1.jpg.rsbak.a1.b2")))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "SSNI-R1-full.jpg.rsbak.c3.d4"), []byte("parked"), 0o644))
	err = m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.ErrorAs(t, err, &cfe)

	// Clean the in-flight marker first, then wedge ONLY the FIFTH dir scan:
	// fence rekey #1, fence parked #2, fence crop #3, migration destination #4,
	// relocation backup #5 (wedge here).
	require.NoError(t, fs.Remove(filepath.Join(dir, "SSNI-R1-full.jpg.rsbak.c3.d4")))
	fs2 := &openFailAfterNFS{Fs: fs, suffix: dir, allow: 6}
	pe.attachEnv(&posterEditEnv{fs: fs2, tempDir: "/tmp", jobID: "JOB-9"})
	err = m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup-scan", "the in-relocation scan's error arm ran (not a fence scan's)")

	// Check the fence-stage fallback: wedges on the FIRST scan surface as a
	// witness-scan error, never admission.
	fs3 := openFailSuffixFS{Fs: fs, suffix: dir}
	pe.attachEnv(&posterEditEnv{fs: fs3, tempDir: "/tmp", jobID: "JOB-9"})
	err = m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)

	// The true F-R8-2 marker verification done — parks cleared above; the same
	// rekey proceeds (witness written, committed, swept).
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	require.NoError(t, m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"}), "parks cleared ⇒ relocation proceeds")
}

// The destination backup-scan wedge (sixth dir scan) hard-errors the
// relocation (fail closed). Probe order: fence rekey#1, fence parked#2,
// fence crop#3, migration destination#4, source backup#5, destination#6.
func TestRekeyDestinationBackupScanWedgeFailsClosed(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	mem := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-DX")
	require.NoError(t, mem.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(mem, filepath.Join(dir, "SSNI-R1-full.jpg"), []byte("x"), 0o644))
	fs := &openFailAfterNFS{Fs: mem, suffix: dir, allow: 6}
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-DX"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup-scan")
}

// F-R8-2 helper edges: dir read wedge fails closed; absence is not in-flight.
func TestRescrapeInFlightBackupPresentEdges(t *testing.T) {
	fs := afero.NewMemMapFs()
	hit, err := rescrapeInFlightBackupPresent(fs, "/no/such/dir", "PI-1")
	assert.False(t, hit)
	assert.NoError(t, err)

	dir := "/tmp/posters/JPI"
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	fsWedge := openFailSuffixFS{Fs: fs, suffix: dir}
	_, err = rescrapeInFlightBackupPresent(fsWedge, dir, "PI-1")
	assert.Error(t, err, "read wedge fails closed")
}

// audit F-R9-1: relocation refuses when a park backup exists under the NEW
// identity too — a foreign family's rescrape litter its losing closeout
// would otherwise restore over our committed bytes.
func TestRekeyDestinationFencedByParkedBackup(t *testing.T) {
	store, fs, dir := familyRelocationSetup(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "SSNI-N9.jpg.rsbak.a1.b2"), []byte("litter"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	assert.Contains(t, err.Error(), "in-flight rescrape")
	require.NoError(t, fs.Remove(filepath.Join(dir, "SSNI-N9.jpg.rsbak.a1.b2")))

	// destination wedge fails CLOSED: second relocation scan (dest backup) —
	// 5th dir Open: fence rekey(1), fence crop(2), dest witness content(3),
	// source backup(4), dest backup(5 wedged)
	fs2 := &openFailAfterNFS{Fs: fs, suffix: dir, allow: 6}
	pe.attachEnv(&posterEditEnv{fs: fs2, tempDir: "/tmp", jobID: "JOB-9"})
	err = m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N9"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup-scan", "destination scan wedge fails the relocation")
}

// codex P1: unsafe scraper IDs must never move bytes outside the job dir.
func TestParkCanonicalPosterPairRejectsTraversalID(t *testing.T) {
	fs := afero.NewMemMapFs()
	bad := filepath.Join(t.TempDir(), "..", "escape-full.jpg") // resolution guard test
	_ = bad
	b := parkCanonicalPosterPair(fs, "/tmp/posters/JOB-X", "../escape", 0)
	assert.False(t, b.hadFull, "traversal ID: no parking of outside paths")
	assert.False(t, b.hadCrop)
	assert.Nil(t, b.parkErr) // leg-mark semantics unarmed; generation proceeds only via manager's own safety gates
}

// codex P2: a parked leg that refuses to move aborts BEFORE any generation —
// no recoverable copy, no committed-state loss.
func TestRescrapeParkFailureAbortsGeneration(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "PF-9", "")
	fs := afero.NewMemMapFs()
	jobID := models.NewJobID()
	dir := filepath.Join("/tmp", "posters", jobID.String())
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "PF-9.jpg"), []byte("committed"), 0o644))
	wedgedFS := &seqRenameFailFS{Fs: fs, failOn: map[int]bool{1: true}}
	wf := &stubRescrapeWorkflow{scrapeResult: &scrape.ScrapeResult{Movie: &models.Movie{ID: "PF-9"}, Status: scrape.StatusCompleted}}
	inputs := rescrapePhaseInputs{
		WF: wf, ResultMap: store, Finder: store, JobID: jobID,
		PosterGen: &spyPosterGen{},
		Fs:        wedgedFS, TempDir: "/tmp",
		EditLockFn: func(ids ...string) func() { return func() {} },
	}
	phase := NewRescrapePhase()
	_, err := phase.Rescrape(context.Background(), inputs, RescrapeCmd{MovieID: "PF-9", FilePath: "/f/a.mp4"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "backup park")
	got, rerr := afero.ReadFile(fs, filepath.Join(dir, "PF-9.jpg"))
	require.NoError(t, rerr)
	assert.Equal(t, "committed", string(got), "pre-existing bytes never moved nor overwritten")
}

// codex cloud P1: a target rekeyed to another family between handler
// resolution and key acquisition must 409 — never commit the payload to the
// old family's siblings while the target lives elsewhere.
func TestUpdateMovieFamilyRejectsMovedFamilyInKey(t *testing.T) {
	store := flipStore(t)
	lk := &flipFMLookup{ResultReadFacade: store, byCall: map[int]string{1: "NEWB-1"}, vanish: map[int]bool{}}
	pe := NewPosterEditor(lk, store, nil)
	pe.attachEnv(&posterEditEnv{fs: afero.NewMemMapFs(), tempDir: "/tmp", jobID: "JOB-MV"})
	err := pe.UpdateMovieFamily(context.Background(), "PING-1", "res-flip", &models.Movie{ID: "PING-1", Title: "moved"}, FamilySaveOptions{})
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe, "moved family surfaces as admission conflict (409 upstream)")
	assert.Contains(t, err.Error(), "moved to family")
	cur, gerr := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, gerr)
	assert.NotEqual(t, "moved", cur.Movie.Title, "payload never committed to the old family's row")
}

// familyGrowLookup simulates a concurrent rescrape joining a new result into
// the family between the client's snapshot and the keyed save.
type familyGrowLookup struct {
	resultstore.ResultReadFacade
	extraPath   string
	extraResult *resultstore.MovieResult
}

func (l *familyGrowLookup) FindFilePathsForMovieID(id string) []string {
	return append(l.ResultReadFacade.FindFilePathsForMovieID(id), l.extraPath)
}

func (l *familyGrowLookup) GetMovieResult(fp string) (*resultstore.MovieResult, error) {
	if fp == l.extraPath {
		return l.extraResult, nil
	}
	return l.ResultReadFacade.GetMovieResult(fp)
}

// codex cloud P1: with revision context supplied, a member that joined the
// family after the snapshot must conflict — the commit would otherwise
// overwrite a movie the client never saw.
func TestUpdateMovieFamilyConflictsWhenFamilyGrows(t *testing.T) {
	base := flipStore(t)
	cur, _, ok := base.GetFileResultByResultID("res-flip")
	require.True(t, ok)
	rev := cur.Revision
	lk := &familyGrowLookup{
		ResultReadFacade: base,
		extraPath:        "/f/ghost.mp4",
		extraResult:      &resultstore.MovieResult{ResultID: "res-ghost", Revision: 9, Movie: &models.Movie{ID: "PING-1"}},
	}
	pe := NewPosterEditor(lk, base, nil)
	pe.attachEnv(&posterEditEnv{fs: afero.NewMemMapFs(), tempDir: "/tmp", jobID: "JOB-GW"})
	err := pe.UpdateMovieFamily(context.Background(), "PING-1", "res-flip", &models.Movie{ID: "PING-1", Title: "save"}, FamilySaveOptions{ExpectedResultRevision: &rev})
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	assert.Contains(t, err.Error(), "result set of family")
	got, gerr := base.GetMovieResult("/f/a.mp4")
	require.NoError(t, gerr)
	assert.NotEqual(t, "save", got.Movie.Title, "old family's row untouched")
}

// codex cloud P1 multipart: the map path ingests every guarded ID and joins
// the target — a grown member conflicts identically to the scalar form.
func TestUpdateMovieFamilyMultipartMapConflictsOnGrownFamily(t *testing.T) {
	base := flipStore(t)
	cur, _, ok := base.GetFileResultByResultID("res-flip")
	require.True(t, ok)
	lk := &familyGrowLookup{
		ResultReadFacade: base,
		extraPath:        "/f/ghost.mp4",
		extraResult:      &resultstore.MovieResult{ResultID: "res-ghost", Revision: 9, Movie: &models.Movie{ID: "PING-1"}},
	}
	pe := NewPosterEditor(lk, base, nil)
	pe.attachEnv(&posterEditEnv{fs: afero.NewMemMapFs(), tempDir: "/tmp", jobID: "JOB-GM"})
	err := pe.UpdateMovieFamily(context.Background(), "PING-1", "res-flip", &models.Movie{ID: "PING-1", Title: "save"}, FamilySaveOptions{ExpectedResultRevisions: map[string]uint64{"res-flip": cur.Revision}})
	require.Error(t, err)
	var cfe *EditAdmissionConflictError
	require.ErrorAs(t, err, &cfe)
	assert.Contains(t, err.Error(), "result set of family")
}

// codex cloud P1 control: ID-less family rows are not membership evidence —
// the drift check skips them and the guarded save proceeds.
func TestUpdateMovieFamilyMembershipSkipsIDlessRows(t *testing.T) {
	base := flipStore(t)
	cur, _, ok := base.GetFileResultByResultID("res-flip")
	require.True(t, ok)
	rev := cur.Revision
	lk := &familyGrowLookup{
		ResultReadFacade: base,
		extraPath:        "/f/idlest.mp4",
		extraResult:      &resultstore.MovieResult{},
	}
	pe := NewPosterEditor(lk, base, nil)
	pe.attachEnv(&posterEditEnv{fs: afero.NewMemMapFs(), tempDir: "/tmp", jobID: "JOB-IDL"})
	err := pe.UpdateMovieFamily(context.Background(), "PING-1", "res-flip", &models.Movie{ID: "PING-1", Title: "save"}, FamilySaveOptions{ExpectedResultRevision: &rev})
	require.NoError(t, err)
	got, gerr := base.GetMovieResult("/f/a.mp4")
	require.NoError(t, gerr)
	assert.Equal(t, "save", got.Movie.Title, "guarded save committed")
}

// codex cloud P1 contract: without revision context the membership guard is
// off — legacy callers keep documented LWW even when the family drifted.
func TestUpdateMovieFamilyNoCASAllowsMembershipDrift(t *testing.T) {
	base := resultstore.New(2, []string{"/f/a.mp4", "/f/b.mp4"})
	seedFamilyResult(base, "/f/a.mp4", "res-flip", "PING-1", "")
	seedFamilyResult(base, "/f/b.mp4", "res-ghost", "PING-1", "") // a second member — drift the snapshot would not know
	pe := newEditorForStore(base)
	pe.attachEnv(&posterEditEnv{fs: afero.NewMemMapFs(), tempDir: "/tmp", jobID: "JOB-LWW"})
	err := pe.UpdateMovieFamily(context.Background(), "PING-1", "res-flip", &models.Movie{ID: "PING-1", Title: "save"}, FamilySaveOptions{})
	require.NoError(t, err, "no revision context → documented LWW, no drift conflict")
	got, gerr := base.GetMovieResult("/f/b.mp4")
	require.NoError(t, gerr)
	assert.Equal(t, "save", got.Movie.Title, "LWW applies family-wide without CAS")
}

// famIDClearedLookup strips FileMatchInfo.MovieID on a single result ID so
// the echo-capture fallback arm is observable — post-commit results normally
// carry a normalized MovieID, making the fallback defensive.
type famIDClearedLookup struct {
	resultstore.ResultReadFacade
	clearID string
}

func (l famIDClearedLookup) GetFileResultByResultID(id string) (*resultstore.MovieResult, string, bool) {
	r, fp, ok := l.ResultReadFacade.GetFileResultByResultID(id)
	if !ok || r == nil || id != l.clearID {
		return r, fp, ok
	}
	c := r.Clone()
	c.FileMatchInfo.MovieID = ""
	return c, fp, ok
}

// F-R15-1 fallback: an empty FileMatchInfo.MovieID falls back to the stored
// Movie.ID for the family key before revision mapping.
func TestUpdateMovieFamilyWithEchoFamIDFallsBackToMovieID(t *testing.T) {
	store := resultstore.New(1, []string{"/f/e.mp4"})
	seedFamilyResult(store, "/f/e.mp4", "res-e2", "FALL-2", "")
	pe := NewPosterEditor(famIDClearedLookup{ResultReadFacade: store, clearID: "res-e2"}, store, nil)
	pe.attachEnv(&posterEditEnv{fs: afero.NewMemMapFs(), tempDir: "/tmp", jobID: "JOB-F2"})
	rev, fam, err := pe.UpdateMovieFamilyWithEcho(context.Background(), "FALL-2", "res-e2", &models.Movie{ID: "FALL-2", Title: "fallback"}, FamilySaveOptions{})
	require.NoError(t, err)
	require.NotNil(t, rev)
	require.NotEmpty(t, fam)
	require.Contains(t, fam, "res-e2")
}

// audit F-R15-1: the echo variant returns post-commit revisions captured
// within the keyed section — they match the COMMITTED state, provably.
func TestUpdateMovieFamilyWithEchoCapturesCommitRevision(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "MIX-7", "")
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: afero.NewMemMapFs(), tempDir: "/tmp", jobID: "JOB-E"})
	before, berr := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, berr)
	rev, fam, err := pe.UpdateMovieFamilyWithEcho(context.Background(), "MIX-7", "res-1", &models.Movie{ID: "MIX-7", Title: "echo title"}, FamilySaveOptions{})
	require.NoError(t, err)
	require.NotNil(t, rev)
	after, gerr := store.GetMovieResult("/f/a.mp4")
	require.NoError(t, gerr)
	assert.Equal(t, before.Revision+1, *rev, "echo = landed revision")
	assert.Equal(t, *rev, after.Revision, "echo matches committed state")
	require.Len(t, fam, 1)
	assert.Equal(t, after.Revision, fam["res-1"])
}

// nil-echo variant: updateMovieFamily delegates reject identity, echo nil.
func TestUpdateMovieFamilyWithEchoFailureReturnsNil(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	pe := newEditorForStore(store)
	rev, fam, err := pe.UpdateMovieFamilyWithEcho(context.Background(), "GONE-7", "res-x", &models.Movie{ID: "GONE-7"}, FamilySaveOptions{})
	require.Error(t, err)
	assert.Nil(t, rev)
	assert.Nil(t, fam)
}

// audit F-R7-1: relocation pins the initiating ResultID in the witness.
func TestRekeyWitnessPinnedResultID(t *testing.T) {
	store3 := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store3, "/f/a.mp4", "res-3", "SSNI-R7", "")
	base3 := afero.NewMemMapFs()
	dir3 := filepath.Join("/tmp", "posters", "JOB-7")
	require.NoError(t, base3.MkdirAll(dir3, 0o755))
	require.NoError(t, afero.WriteFile(base3, filepath.Join(dir3, "SSNI-R7.jpg"), []byte("x"), 0o644))
	committer3 := NewEditCommitter(failTransactor{err: errors.New("tx wedged")}, newKeyedMutexRegistry(), "JOB-7", newKeyedMutexRegistry())
	pe3 := newEditorForStore(store3)
	fsWedge := &seqRenameFailFS{Fs: base3, failOn: map[int]bool{3: true}} // witness(1), forward(2), reverse-fail(3)
	pe3.attachEnv(&posterEditEnv{fs: fsWedge, tempDir: "/tmp", jobID: "JOB-7", committer: committer3, envelope: func(map[string]*resultstore.MovieResult, map[string]*resultstore.ProvenanceData, map[string]bool) (*models.Job, error) {
		return &models.Job{}, nil
	}})
	m3 := &LockedMovieOps{pe: pe3, movieID: "SSNI-R7"}
	require.Error(t, m3.UpdateMovieFamily(context.Background(), &models.Movie{ID: "SSNI-N7"}))
	data, rerr := afero.ReadFile(base3, filepath.Join(dir3, ".rekey-SSNI-R7.json"))
	require.NoError(t, rerr, "witness retained on incomplete rollback")
	var w rekeyWitness
	require.NoError(t, json.Unmarshal(data, &w))
	assert.Equal(t, "res-3", w.ResultID, "initiating result pinned")
	assert.Equal(t, "SSNI-R7", w.OldID)
	assert.Equal(t, "SSNI-N7", w.NewID)
}

// audit F-R7-1: arbitration with a pinned ResultID ignores a sibling family
// still on the old spelling.
func TestReconcileRekeyScopedToPinnedResult(t *testing.T) {
	fs, dir := witnessFixture(t)
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9-full.jpg"), []byte("newfull"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "NEW-9.jpg"), []byte("newcrop"), 0o644))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, "OLD-9.jpg"), []byte("oldcrop"), 0o644))
	witness, _ := json.Marshal(rekeyWitness{OldID: "OLD-9", NewID: "NEW-9", PrevRevision: 0, ResultID: "res-a"})
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), witness, 0o644))
	repo := mocks.NewMockJobRepositoryInterface(t)
	repo.EXPECT().FindByID(mock.Anything, "JOB-W1").Return(envelopeRow(t, "JOB-W1", map[string]*resultstore.MovieResult{
		"/f/a.mp4": {ResultID: "res-a", Revision: 1, Movie: &models.Movie{ID: "NEW-9"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/a.mp4", MovieID: "NEW-9"}},
		"/f/b.mp4": {ResultID: "res-b", Revision: 0, Movie: &models.Movie{ID: "OLD-9"}, FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "OLD-9"}},
	}), nil)
	cl := &TempDirCleaner{fs: fs, tempDir: "/tmp", jobRepo: repo}
	n, err := cl.ReconcileRekeyWitnesses(context.Background())
	require.NoError(t, err)
	assert.Equal(t, 0, n, "sibling's OLD presence does not flip the pinned result's commit → no reversal")
	_, wErr := fs.Stat(filepath.Join(dir, ".rekey-OLD-9.json"))
	assert.Error(t, wErr, "witness swept after committed arbitration")
	_, newErr := fs.Stat(filepath.Join(dir, "NEW-9.jpg"))
	assert.NoError(t, newErr, "committed new bytes stay")
}

// audit F-R7-1: relocation refuses when a SIBLING family shares the canonical
// poster ID.
func TestRekeyRefusedWhenSiblingSharesCanonicalID(t *testing.T) {
	store := resultstore.New(5, []string{"/f/a.mp4", "/f/b.mp4", "/f/c.mp4", "/f/d.mp4", "/f/e.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-a", "SHAR-1", "")
	// Sibling family: SAME canonical movie ID under a DIFFERENT matcher alias.
	store.UpdateFileResult("/f/b.mp4", &resultstore.MovieResult{
		ResultID:      "res-b",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SHAR-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "sibling-alias"},
	})
	store.SetFileMatchInfo("/f/b.mp4", models.FileMatchInfo{Path: "/f/b.mp4", MovieID: "sibling-alias"})
	// Rows the sibling-scan must SKIP: movieless + wrong canonical.
	store.UpdateFileResult("/f/c.mp4", &resultstore.MovieResult{
		ResultID: "res-c", Status: models.JobStatusRunning,
		FileMatchInfo: models.FileMatchInfo{Path: "/f/c.mp4", MovieID: "OTHER-9"},
	})
	store.UpdateFileResult("/f/d.mp4", &resultstore.MovieResult{
		ResultID:      "res-d",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "UNREL-5"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/d.mp4", MovieID: "UNREL-5"},
	})
	// Shares the canonical ID but has NO matcher alias → not a family; the
	// sibling scan skips it without fencing.
	store.UpdateFileResult("/f/e.mp4", &resultstore.MovieResult{
		ResultID:      "res-e",
		Status:        models.JobStatusCompleted,
		Movie:         &models.Movie{ID: "SHAR-1"},
		FileMatchInfo: models.FileMatchInfo{Path: "/f/e.mp4"},
	})
	fs := afero.NewMemMapFs()
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SHAR-1"}
	err := m.UpdateMovieFamily(context.Background(), &models.Movie{ID: "TGT-1"})
	require.Error(t, err)
	assert.Contains(t, err.Error(), "shared with family", "sibling co-ownership refuses the relocation")
}

// audit F-R7-3: single-save fences the STORED identity too (rekey via single
// save meets a pending witness at the old ID).
func TestUpdateMovieSingleFencedByStoredIDWitness(t *testing.T) {
	store := resultstore.New(1, []string{"/f/a.mp4"})
	seedFamilyResult(store, "/f/a.mp4", "res-1", "SSNI-R1", "")
	fs := afero.NewMemMapFs()
	dir := filepath.Join("/tmp", "posters", "JOB-9")
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(fs, filepath.Join(dir, ".rekey-OLD-9.json"), []byte("{\"old_id\":\"SSNI-R1\",\"new_id\":\"ELSE-9\"}"), 0o644))
	pe := newEditorForStore(store)
	pe.attachEnv(&posterEditEnv{fs: fs, tempDir: "/tmp", jobID: "JOB-9"})
	m := &LockedMovieOps{pe: pe, movieID: "SSNI-R1"}
	err := m.updateMovieSingleLocked(context.Background(), "/f/a.mp4", &models.Movie{ID: "SSNI-NEW-9", Title: "x"})
	require.Error(t, err, "witness at the STORED id fences the single-save rekey")
	assert.Contains(t, err.Error(), "rekey witness unresolved")
}
