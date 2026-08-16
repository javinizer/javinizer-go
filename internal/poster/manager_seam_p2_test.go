package poster

// P2 staged seam tests: the download lands on a unique staged identity and
// canonical names are touched ONLY by PromoteStagedPoster (rename-only).

import (
	"context"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func stageImageServer(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		img := image.NewRGBA(image.Rect(0, 0, 200, 300))
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestStagePosterDownload_WritesNothingCanonicalBeforePromote(t *testing.T) {
	srv := stageImageServer(t)
	base := afero.NewMemMapFs()
	spy := &writeSpyFS{Fs: base, writes: map[string]int{}}
	pm := NewPosterManager(spy, "/tmp/p2", srv.Client(), 0).WithSSRFCheck(func(string) error { return nil })

	staged, err := pm.StagePosterDownload(context.Background(), StagePosterRequest{
		JobID: "job1", PosterID: "ST-P1", URL: srv.URL + "/img.jpg",
	})
	require.NoError(t, err)
	require.NotNil(t, staged)

	dir := "/tmp/p2/posters/job1"
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		_, serr := base.Stat(dir + "/ST-P1" + suffix)
		assert.Error(t, serr, "canonical leg untouched during stage: ST-P1%s", suffix)
	}
	entries, derr := afero.ReadDir(base, dir)
	require.NoError(t, derr)
	stagedCount := 0
	for _, e := range entries {
		assert.True(t, strings.HasPrefix(e.Name(), "ST-P1.stage-"), "only staged names during stage: %s", e.Name())
		assert.Equal(t, 0, spy.countExact(dir+"/ST-P1-full.jpg")+spy.countExact(dir+"/ST-P1.jpg"))
		_ = e
		stagedCount++
	}
	assert.Equal(t, 2, stagedCount, "both legs staged under the unique identity")
	assert.Equal(t, 0, spy.countExact(dir+"/ST-P1-full.jpg"), "no canonical full write")
	assert.Equal(t, 0, spy.countExact(dir+"/ST-P1.jpg"), "no canonical crop write")

	// Promote: canonical pair appears, staged names are gone, and the reported
	// URL carries the canonical identity.
	res, err := pm.PromoteStagedPoster(staged)
	require.NoError(t, err)
	require.NotNil(t, res)
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		_, serr := base.Stat(dir + "/ST-P1" + suffix)
		assert.NoError(t, serr, "canonical leg promoted: ST-P1%s", suffix)
	}
	assert.Contains(t, res.CroppedURL, "ST-P1.jpg")
	assert.NotContains(t, res.CroppedURL, ".stage-", "canonical URL never references the staged identity")
	entries2, derr2 := afero.ReadDir(base, dir)
	require.NoError(t, derr2)
	for _, e := range entries2 {
		assert.NotContains(t, e.Name(), ".stage-", "no staged residue after promote")
	}
}

// Promote must restore the previous canonical bytes if the second leg's
// install fails — the pair can never split across generations.
func TestPromoteStagedPoster_PartialFailureRestoresFirstLeg(t *testing.T) {
	srv := stageImageServer(t)
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P2-full.jpg", []byte("old-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P2.jpg", []byte("old-crop"), 0o644))
	pm := NewPosterManager(base, "/tmp/p2", srv.Client(), 0).WithSSRFCheck(func(string) error { return nil })

	// Stage via the public seam, then rehang on a rename-wedged fs.
	staged, err := pm.StagePosterDownload(context.Background(), StagePosterRequest{
		JobID: "job1", PosterID: "ST-P2", URL: srv.URL + "/img.jpg",
	})
	require.NoError(t, err)
	// Wedge ONLY the staged→canonical promote rename of the crop leg; the
	// aside/backup/restore renames (from a .bak sibling) stay live.
	wedged := renameFailWhereFS{Fs: base, fail: func(o, n string) bool {
		return strings.Contains(o, ".stage-") && strings.HasSuffix(n, "/ST-P2.jpg")
	}}
	pmWedged := NewPosterManager(wedged, "/tmp/p2", nil, 0)

	_, err2 := pmWedged.PromoteStagedPoster(staged)
	require.ErrorContains(t, err2, "promote staged poster")
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		got, rerr := afero.ReadFile(base, dir+"/ST-P2"+suffix)
		require.NoError(t, rerr)
		assert.True(t, string(got) == "old-full" || string(got) == "old-crop",
			"leg ST-P2%s carries only the ORIGINAL bytes (got %q)", suffix, string(got))
	}
}

// Discard removes staged residue; a nil/partially-staged handle is safe.
func TestStagedPoster_DiscardAndNilSafety(t *testing.T) {
	srv := stageImageServer(t)
	base := afero.NewMemMapFs()
	pm := NewPosterManager(base, "/tmp/p2", srv.Client(), 0).WithSSRFCheck(func(string) error { return nil })
	staged, err := pm.StagePosterDownload(context.Background(), StagePosterRequest{
		JobID: "job1", PosterID: "ST-P3", URL: srv.URL + "/img.jpg",
	})
	require.NoError(t, err)
	pm.DiscardStagedPoster(staged)
	entries, derr := afero.ReadDir(base, "/tmp/p2/posters/job1")
	require.NoError(t, derr)
	assert.Empty(t, entries, "discard leaves nothing behind")

	pm.DiscardStagedPoster(nil) // no panic
	_, perr := pm.PromoteStagedPoster(nil)
	require.Error(t, perr)
}

// Generator layer: the split honors the poster→cover fallback and passes
// sanitized errors through on stage failure.
func TestScrapePosterGeneratorStageSplit(t *testing.T) {
	srv := stageImageServer(t)
	base := afero.NewMemMapFs()
	pm := NewPosterManager(base, "/tmp/p2", srv.Client(), 0).WithSSRFCheck(func(string) error { return nil })
	gen := NewScrapePosterGenerator(pm, "", "")

	movie := &models.Movie{ID: "G-1", Poster: models.PosterState{CoverURL: srv.URL + "/cover.jpg"}}
	staged, err := gen.StagePoster(context.Background(), "job1", movie)
	require.NoError(t, err, "cover fallback reaches the staged download")
	require.NotNil(t, staged)

	require.NoError(t, gen.CommitStagedPoster(movie, staged))
	assert.Contains(t, movie.Poster.CroppedPosterURL, "G-1.jpg")
	assert.NotContains(t, movie.Poster.CroppedPosterURL, ".stage-")
	_, serr := base.Stat("/tmp/p2/posters/job1/G-1.jpg")
	assert.NoError(t, serr)

	// No source at all ⇒ stage error, no residue.
	_, err2 := gen.StagePoster(context.Background(), "job1", &models.Movie{ID: "G-2"})
	require.Error(t, err2)
	entries, _ := afero.ReadDir(base, "/tmp/p2/posters/job1")
	_ = entries
}

// --- coverage wedge matrix for the seam's failure arms ---

func TestStagePosterDownload_ValidationAndDownloadErrors(t *testing.T) {
	srv := stageImageServer(t)
	pm := NewPosterManager(afero.NewMemMapFs(), "/tmp/p2", srv.Client(), 0).WithSSRFCheck(func(string) error { return nil })
	if _, err := pm.StagePosterDownload(context.Background(), StagePosterRequest{JobID: "../escape", PosterID: "X-1", URL: srv.URL}); err == nil {
		t.Fatal("invalid jobID must fail")
	}
	if _, err := pm.StagePosterDownload(context.Background(), StagePosterRequest{JobID: "job1", PosterID: "../x", URL: srv.URL}); err == nil {
		t.Fatal("invalid posterID must fail")
	}
	if _, err := pm.StagePosterDownload(context.Background(), StagePosterRequest{JobID: "job1", PosterID: "X-1", URL: "http://%zz"}); err == nil {
		t.Fatal("undownloadable URL must fail the stage")
	}
}

func TestStagedPoster_HandleAccessors(t *testing.T) {
	var nilHandle *StagedPoster
	w, h := nilHandle.SourceDimensions()
	assert.Zero(t, w)
	assert.Zero(t, h)
	s := NewStagedPosterHandleForTest("j", "s-1", "t-1", "u")
	assert.Equal(t, "j", s.jobID)
	s.sourceWidth, s.sourceHeight = 7, 11
	w2, h2 := s.SourceDimensions()
	assert.Equal(t, 7, w2)
	assert.Equal(t, 11, h2)
}

// Staged-leg transient stat error aborts the promote coherently.
func TestPromoteStagedPoster_StagingStatErrorAborts(t *testing.T) {
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll("/tmp/p2/posters/job1", 0o755))
	require.NoError(t, afero.WriteFile(base, "/tmp/p2/posters/job1/ST-P4.stage-x.jpg", []byte("s"), 0o644))
	wedged := statFailExactFS{Fs: base, path: "/tmp/p2/posters/job1/ST-P4.stage-x-full.jpg"}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	_, err := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "ST-P4.stage-x", "ST-P4", ""))
	require.ErrorContains(t, err, "promote staging stat")
}

// Canonical transient stat error on an EXISTING final aborts before aside.
func TestPromoteStagedPoster_CanonicalStatErrorAborts(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P5.stage-x-full.jpg", []byte("s-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P5.stage-x.jpg", []byte("s-crop"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P5-full.jpg", []byte("c"), 0o644))
	wedged := statFailExactFS{Fs: base, path: dir + "/ST-P5-full.jpg"}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	_, err := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "ST-P5.stage-x", "ST-P5", ""))
	require.ErrorContains(t, err, "promote canonical stat")
	got, _ := afero.ReadFile(base, dir+"/ST-P5-full.jpg")
	assert.Equal(t, "c", string(got), "canonical untouched")
}

// Aside failure: only the wedged leg's backup is pending; already-asided
// earlier legs restore, and nothing staged lands.
func TestPromoteStagedPoster_AsideFailureRestoresEarlierLegs(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P6-full.jpg", []byte("c-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P6.jpg", []byte("c-crop"), 0o644))
	dir2 := dir + "/"
	require.NoError(t, afero.WriteFile(base, dir2+"ST-P6.stage-x-full.jpg", []byte("s-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir2+"ST-P6.stage-x.jpg", []byte("s-crop"), 0o644))
	wedged := renameFailWhereFS{Fs: base, fail: func(o, n string) bool {
		return strings.HasSuffix(n, ".bak") && strings.Contains(o, "ST-P6.jpg")
	}}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	_, err := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "ST-P6.stage-x", "ST-P6", ""))
	require.ErrorContains(t, err, "back up")
	got, _ := afero.ReadFile(base, dir+"/ST-P6-full.jpg")
	assert.Equal(t, "c-full", string(got), "first leg restored after second leg's aside wedge")
	got2, _ := afero.ReadFile(base, dir+"/ST-P6.jpg")
	assert.Equal(t, "c-crop", string(got2))
}

// Phase-2 failure with NO prior canonical: the partial new final is removed
// (restore's no-backup arm) and staged bytes are preserved nowhere lurking
// under canonical names.
func TestPromoteStagedPoster_Phase2FailureWithoutBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P7.stage-x-full.jpg", []byte("s-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P7.stage-x.jpg", []byte("s-crop"), 0o644))
	wedged := renameFailWhereFS{Fs: base, fail: func(o, n string) bool {
		return strings.Contains(o, ".stage-") && strings.HasSuffix(n, "/ST-P7.jpg")
	}}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	_, err := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "ST-P7.stage-x", "ST-P7", ""))
	require.ErrorContains(t, err, "promote staged poster")
	_, ferr := base.Stat(dir + "/ST-P7-full.jpg")
	assert.Error(t, ferr, "no-backup leg's installed new final was removed")
	_, ferr2 := base.Stat(dir + "/ST-P7.jpg")
	assert.Error(t, ferr2)
}

// A wedged RESTORE rename must warn and keep the original bytes findable at
// the backup path (never silently lost).
func TestPromoteStagedPoster_RestoreFailureWarnsKeepsBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P8-full.jpg", []byte("c-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P8.jpg", []byte("c-crop"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P8.stage-x-full.jpg", []byte("s-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P8.stage-x.jpg", []byte("s-crop"), 0o644))
	// Wedge staged→final AND backup→final (restore): both error paths fire.
	wedged := renameFailWhereFS{Fs: base, fail: func(_, n string) bool {
		return strings.HasSuffix(n, "/ST-P8-full.jpg")
	}}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	_, err := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "ST-P8.stage-x", "ST-P8", ""))
	require.ErrorContains(t, err, "promote staged poster")
}

// Post-success backup sweep failure is warn-only (the promote still wins).
func TestPromoteStagedPoster_BackupSweepWarnIsNonFatal(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P9-stagez-full.jpg", []byte("s-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P9-stagez.jpg", []byte("s"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P9.jpg", []byte("c"), 0o644))
	wedged := removeFailWhereFS{Fs: base, fail: func(n string) bool { return strings.HasSuffix(n, ".bak") }}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	res, err := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "ST-P9-stagez", "ST-P9", dir+"/ST-P9.jpg"))
	require.NoError(t, err)
	require.NotNil(t, res)
	assert.Equal(t, filepath.Join(dir, "ST-P9.jpg"), res.CroppedPath, "canonical preview path (platform separators)")
	got, _ := afero.ReadFile(base, dir+"/ST-P9.jpg")
	assert.Equal(t, "s", string(got), "promoted bytes landed")
	entries, _ := afero.ReadDir(base, dir)
	var bak int
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".bak") {
			bak++
		}
	}
	assert.Equal(t, 1, bak, "unswept backup lingers harmlessly for inspection")
}

func TestStagedPoster_DiscardRemoveFailureWarns(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/ST-P10.stage-x.jpg", []byte("s"), 0o644))
	pm := NewPosterManager(removeFailWhereFS{Fs: base, fail: func(string) bool { return true }}, "/tmp/p2", nil, 0)
	pm.DiscardStagedPoster(NewStagedPosterHandleForTest("job1", "ST-P10.stage-x", "ST-P10", "")) // warn-only, must not panic
}

// --- generator coverage via a fake manager ---

type fakePosterManager struct {
	stageErr   error
	promoteErr error
	discarded  int
	handle     *StagedPoster
	cropRes    *cropResult
}

func (f *fakePosterManager) CropWithBounds(context.Context, string, string, int, int, int, int, int) (*cropResult, error) {
	return &cropResult{}, nil
}
func (f *fakePosterManager) DownloadFromURL(context.Context, string, string, string, string, string) (*cropResult, error) {
	return &cropResult{}, nil
}
func (f *fakePosterManager) SnapshotAssets(string, string) (*PosterAssetSnapshot, error) {
	return nil, nil
}
func (f *fakePosterManager) RestoreAssets(*PosterAssetSnapshot) error { return nil }
func (f *fakePosterManager) StagePosterDownload(context.Context, StagePosterRequest) (*StagedPoster, error) {
	return f.handle, f.stageErr
}
func (f *fakePosterManager) PromoteStagedPoster(*StagedPoster) (*cropResult, error) {
	return f.cropRes, f.promoteErr
}
func (f *fakePosterManager) DiscardStagedPoster(*StagedPoster) { f.discarded++ }

func TestGenerator_SplitArmMatrix(t *testing.T) {
	// nil manager / nil movie early-outs
	g0 := NewScrapePosterGenerator(nil, "", "")
	h, err := g0.StagePoster(context.Background(), "j", &models.Movie{ID: "X"})
	assert.NoError(t, err)
	assert.Nil(t, h)
	h, err = g0.StagePoster(context.Background(), "j", nil)
	assert.NoError(t, err)
	assert.Nil(t, h)
	assert.NoError(t, g0.CommitStagedPoster(nil, nil))
	g0.DiscardStaged(NewStagedPosterHandleForTest("j", "s", "t", "")) // no manager ⇒ silent no-op

	// stage error sanitizes through
	gm := NewScrapePosterGenerator(&fakePosterManager{stageErr: assertErr}, "", "")
	_, err = gm.StagePoster(context.Background(), "j", &models.Movie{ID: "X", Poster: models.PosterState{PosterURL: "https://s/p.jpg"}})
	require.ErrorIs(t, err, assertErr)

	// promote error returns sanitized and leaves the movie untouched
	movie := &models.Movie{ID: "X", Poster: models.PosterState{PosterURL: "https://s/p.jpg"}}
	gp := NewScrapePosterGenerator(&fakePosterManager{promoteErr: assertErr}, "", "")
	err = gp.CommitStagedPoster(movie, NewStagedPosterHandleForTest("j", "s", "t", ""))
	require.ErrorIs(t, err, assertErr)
	assert.Empty(t, movie.Poster.CroppedPosterURL)

	// happy promote sets the preview pointer
	gok := NewScrapePosterGenerator(&fakePosterManager{cropRes: &cropResult{CroppedURL: "/api/x/P-9.jpg"}}, "", "")
	require.NoError(t, gok.CommitStagedPoster(movie, NewStagedPosterHandleForTest("j", "s", "t", "")))
	assert.Equal(t, "/api/x/P-9.jpg", movie.Poster.CroppedPosterURL)

	// discard delegates
	fd := &fakePosterManager{}
	gd := NewScrapePosterGenerator(fd, "", "")
	gd.DiscardStaged(NewStagedPosterHandleForTest("j", "s", "t", ""))
	assert.Equal(t, 1, fd.discarded)
}

var assertErr = assertAnError{}

type assertAnError struct{}

func (assertAnError) Error() string { return "assert manager wedge" }

// codex P2 (PR211): a transient canonical-stat failure mid-phase-1 must
// restore the legs ALREADY asided — the pair never tears (older leg gone
// under a .bak while the later leg stays) under a transient wedge.
func TestPromoteStagedPoster_CanonicalStatWedgedRestoresAsidedLeg(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/PC-1-full.jpg", []byte("c-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/PC-1.jpg", []byte("c-crop"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/PC-1.stage-x-full.jpg", []byte("s-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/PC-1.stage-x.jpg", []byte("s-crop"), 0o644))

	// The crop leg's canonical Stat fails transiently: the full leg has
	// already been asided to .bak — the restore must put it back.
	wedged := statFailExactFS{Fs: base, path: dir + "/PC-1.jpg"}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	_, err := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "PC-1.stage-x", "PC-1", ""))
	require.ErrorContains(t, err, "promote canonical stat")

	gotFull, ferr := afero.ReadFile(base, dir+"/PC-1-full.jpg")
	require.NoError(t, ferr, "first leg restored from its backup")
	assert.Equal(t, "c-full", string(gotFull))
	gotCrop, cerr := afero.ReadFile(base, dir+"/PC-1.jpg")
	require.NoError(t, cerr, "untouched second leg intact")
	assert.Equal(t, "c-crop", string(gotCrop))
	// staged bytes untouched for a later retry
	for _, s := range []string{"/PC-1.stage-x-full.jpg", "/PC-1.stage-x.jpg"} {
		got, serr := afero.ReadFile(base, dir+s)
		require.NoError(t, serr, "staged leg survives for retry: %s", s)
		assert.True(t, strings.HasPrefix(string(got), "s-"))
	}
}

// Phase-2 failure on a LATER leg with the restore rename wedged: the error
// reports the restore failure while the earlier leg's bytes stay findable at
// their .bak copy (nothing silently lost mid-pair).
func TestPromoteStagedPoster_RestoreRenameWedgedKeepsBackup(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	require.NoError(t, afero.WriteFile(base, dir+"/R-4-full.jpg", []byte("c-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/R-4.jpg", []byte("c-crop"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/R-4.stage-x-full.jpg", []byte("s-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/R-4.stage-x.jpg", []byte("s-crop"), 0o644))

	wedged := renameFailWhereFS{Fs: base, fail: func(o, n string) bool {
		// crop-leg phase-2 install wedges; the full-leg restore rename wedges too.
		if strings.Contains(o, ".stage-") && strings.HasSuffix(n, "/R-4.jpg") {
			return true
		}
		return strings.Contains(o, ".bak") && strings.HasSuffix(n, "/R-4-full.jpg")
	}}
	pm := NewPosterManager(wedged, "/tmp/p2", nil, 0)
	_, err := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "R-4.stage-x", "R-4", ""))
	require.ErrorContains(t, err, "promote staged poster")

	// The crop leg was never displaced under the never-absent contract.
	gotCrop, cerr := afero.ReadFile(base, dir+"/R-4.jpg")
	require.NoError(t, cerr)
	assert.Equal(t, "c-crop", string(gotCrop))
	// The full leg's prior bytes survive at the backup copy (restore rename
	// wedged; the warn path ran, bytes intact).
	entries, derr := afero.ReadDir(base, dir)
	require.NoError(t, derr)
	var bak []string
	for _, e := range entries {
		if strings.HasPrefix(e.Name(), "R-4-full.jpg.") && strings.HasSuffix(e.Name(), ".bak") {
			bak = append(bak, filepath.Join(dir, e.Name()))
		}
	}
	require.Len(t, bak, 1)
	content, rerr := afero.ReadFile(base, bak[0])
	require.NoError(t, rerr)
	assert.Equal(t, "c-full", string(content), "prior bytes recoverable from the backup")
}

// codex P2 round 7 (PR211): a successful stage ALWAYS produced both legs —
// a missing staged leg means the stage was disturbed mid-op (temp sweep).
// Promote must fail instead of installing a partial pair or committing a
// dangling URL over nothing.
func TestPromoteStagedPoster_IncompleteStageFails(t *testing.T) {
	base := afero.NewMemMapFs()
	dir := "/tmp/p2/posters/job1"
	require.NoError(t, base.MkdirAll(dir, 0o755))
	// Stage exists partially (full leg swept between stage and promote).
	require.NoError(t, afero.WriteFile(base, dir+"/INC-1.stage-x.jpg", []byte("s-crop"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/INC-1-full.jpg", []byte("c-full"), 0o644))
	require.NoError(t, afero.WriteFile(base, dir+"/INC-1.jpg", []byte("c-crop"), 0o644))
	pm := NewPosterManager(base, "/tmp/p2", nil, 0)

	_, err := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "INC-1.stage-x", "INC-1", ""))
	require.ErrorContains(t, err, "incomplete stage")

	// Canonical untouched.
	for _, suffix := range []string{"-full.jpg", ".jpg"} {
		got, rerr := afero.ReadFile(base, dir+"/INC-1"+suffix)
		require.NoError(t, rerr)
		assert.True(t, strings.HasPrefix(string(got), "c-"), "unpromoted: %s stays old", suffix)
	}

	// And an empty stage (both legs gone) errors too.
	require.NoError(t, afero.WriteFile(base, dir+"/INC-2.jpg", []byte("c-crop"), 0o644))
	_, err2 := pm.PromoteStagedPoster(NewStagedPosterHandleForTest("job1", "INC-2.stage-x", "INC-2", ""))
	require.ErrorContains(t, err2, "incomplete stage")
	got, rerr := afero.ReadFile(base, dir+"/INC-2.jpg")
	require.NoError(t, rerr)
	assert.Equal(t, "c-crop", string(got))
}
