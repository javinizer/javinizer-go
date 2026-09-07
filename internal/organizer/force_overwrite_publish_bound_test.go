package organizer

// Publish-bound crumb evidence (PR #249 codex P2): the force-overwrite audit
// crumb must key on PHYSICAL occupancy at the PUBLISH instant, never on a
// classification the publish outdates. These wedges act as the foreign
// process the (process-local) destination locks cannot exclude:
//
//   - copy lane: a wrapped fs hands back a source stream whose first Read
//     parks INSIDE the staging stream — after the execute-time classification,
//     before the publish — and mutates the destination there. The measured
//     window (staging start → publish rename) is asserted to contain the
//     wedge with real elapsed time, so the test genuinely demonstrates the
//     long classify → publish window the finding describes;
//   - move lane (organize + in-place-norenamefolder shape): the lane's
//     post-classify MkdirAll is the deterministic gap between the authorized
//     classification and the inline rename publish;
//   - in-place inner rename: the classification's own src probe (dst was
//     probed immediately before it, inside classifyExistingDestination) is
//     the deterministic mark between classification and the inline publish.
//
// Each lane is wedged in BOTH directions: an occupant PLANTED in the window
// must crumb (its bytes are really displaced at publish), an occupant
// VACATED in the window must stay silent (nothing is physically replaced).

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/operationmode"
)

// copyWindowWedgeFs is the foreign-process armature for the copy lane: the
// wrapped Open of the source hands back a file whose first Read
//  1. records that staging has begun (the execute-time classification is
//     already complete — it precedes the copy by construction), and
//  2. blocks until the test mutates the destination and releases the stream,
//
// holding the classify → publish window open measurably long. Rename records
// the publish instant so the test can assert the wedge really landed INSIDE
// the window instead of racing around it.
type copyWindowWedgeFs struct {
	afero.Fs
	slowSrc string

	stageStart   chan struct{}
	stageStartAt time.Time
	release      chan struct{}
	stageOnce    sync.Once
	publishAt    time.Time
}

func (w *copyWindowWedgeFs) Open(name string) (afero.File, error) {
	f, err := w.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(name) != filepath.Clean(w.slowSrc) {
		return f, nil
	}
	return &wedgeSourceFile{File: f, w: w}, nil
}

func (w *copyWindowWedgeFs) Rename(oldname, newname string) error {
	// The staged publish's replace-rename is the terminal rename of this
	// flow; stamping it gives the window's closing edge.
	w.publishAt = time.Now()
	return w.Fs.Rename(oldname, newname)
}

type wedgeSourceFile struct {
	afero.File
	w       *copyWindowWedgeFs
	stalled bool
}

func (s *wedgeSourceFile) Read(p []byte) (int, error) {
	if !s.stalled {
		s.stalled = true
		s.w.stageOnce.Do(func() {
			s.w.stageStartAt = time.Now()
			close(s.w.stageStart)
		})
		// Park mid-stream: the staged copy cannot reach the publish until the
		// foreign mutation below lands and the test releases the window.
		<-s.w.release
	}
	return s.File.Read(p)
}

// driveCopyWindowWedge runs the authorized copy lane with the destination
// mutated mid-staging by the wrapped-fs armature and returns the result plus
// the measured window. mutate runs while the staging stream is provably
// parked between classification and publish.
func driveCopyWindowWedge(t *testing.T, base afero.Fs, src, dst string, window time.Duration, mutate func(fs afero.Fs)) (*OrganizeResult, time.Duration) {
	t.Helper()
	require.NoError(t, base.MkdirAll(filepath.Dir(src), 0o755))
	require.NoError(t, afero.WriteFile(base, src, []byte("winner-bytes"), 0o644))

	wedge := &copyWindowWedgeFs{
		Fs:         base,
		slowSrc:    src,
		stageStart: make(chan struct{}),
		release:    make(chan struct{}),
	}
	org := NewOrganizer(wedge, &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
	plan, err := org.PlanOrganize(context.Background(), OrganizeCmd{
		Match:       models.FileMatchInfo{MovieID: "ABC-123", Path: src, Name: filepath.Base(src), Extension: filepath.Ext(src)},
		Movie:       &models.Movie{ID: "ABC-123"},
		DestDir:     mustDestinationRoot(t, dst),
		MoveFiles:   false,
		LinkMode:    LinkModeNone,
		ForceUpdate: true,
	})
	require.NoError(t, err)
	require.Empty(t, plan.Conflicts, "force suppresses the file-occupation conflict at plan")
	require.Equal(t, filepath.ToSlash(dst), filepath.ToSlash(plan.TargetPath))
	plan.moveFiles = false
	plan.LinkMode = LinkModeNone

	type outcome struct {
		res *OrganizeResult
		err error
	}
	done := make(chan outcome, 1)
	go func() {
		res, execErr := plan.executeStrategy.Execute(plan)
		done <- outcome{res, execErr}
	}()

	<-wedge.stageStart // staging is mid-stream: classification is behind us, publish ahead
	wedgeAt := time.Now()
	mutate(base) // the foreign plant/vacate the finding describes
	time.Sleep(window)
	close(wedge.release)

	out := <-done
	require.NoError(t, out.err)

	require.False(t, wedge.publishAt.IsZero(), "the staged publish must have run")
	assert.True(t, wedge.stageStartAt.Before(wedgeAt) || wedge.stageStartAt.Equal(wedgeAt))
	assert.True(t, wedge.publishAt.After(wedgeAt),
		"the mutation provably landed INSIDE the classify → publish window")
	measured := wedge.publishAt.Sub(wedge.stageStartAt)
	assert.GreaterOrEqual(t, measured, window,
		"the staging window was held open for real elapsed time — no microsecond-only window")
	return out.res, measured
}

// mustDestinationRoot extracts the destination library root from the computed
// destination path (destDir/<ID>/<ID>.mkv).
func mustDestinationRoot(t *testing.T, dst string) string {
	t.Helper()
	require.Equal(t, "ABC-123", filepath.Base(filepath.Dir(dst)),
		"fixture layout <root>/ABC-123/ABC-123.mkv")
	return filepath.Dir(filepath.Dir(dst))
}

func TestForceOverwriteAudit_PublishBound_CopyLaneWindow(t *testing.T) {
	run := func(t *testing.T, mkFs func(t *testing.T) (afero.Fs, string, string)) {
		fs, src, dst := mkFs(t)

		t.Run("occupant planted mid-staging crumbs at publish", func(t *testing.T) {
			res, window := driveCopyWindowWedge(t, fs, src, dst, 60*time.Millisecond,
				func(base afero.Fs) {
					require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o755))
					require.NoError(t, afero.WriteFile(base, dst, []byte("planted-foreign-bytes"), 0o644))
				})
			t.Logf("measured classify→publish staging window: %s", window)
			require.True(t, res.Moved)
			require.Len(t, res.Warnings, 1,
				"the publish displaced bytes a mid-staging plant landed — classify-time silence was the finding's false negative")
			assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(res.Warnings[0]))
			content, err := afero.ReadFile(fs, dst)
			require.NoError(t, err)
			assert.Equal(t, []byte("winner-bytes"), content,
				"the plant's foreign bytes were really displaced by the publish")
		})

		t.Run("occupant vacated mid-staging stays silent", func(t *testing.T) {
			require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
			require.NoError(t, afero.WriteFile(fs, dst, []byte("resident-bytes"), 0o644))

			res, window := driveCopyWindowWedge(t, fs, src, dst, 60*time.Millisecond,
				func(base afero.Fs) {
					require.NoError(t, base.Remove(dst))
				})
			t.Logf("measured classify→publish staging window: %s", window)
			require.True(t, res.Moved)
			assert.Empty(t, res.Warnings,
				"the publish landed on a name vacated mid-staging — nothing was displaced, so no crumb may fire")
			content, err := afero.ReadFile(fs, dst)
			require.NoError(t, err)
			assert.Equal(t, []byte("winner-bytes"), content)
		})
	}

	t.Run("MemMapFs", func(t *testing.T) {
		run(t, func(t *testing.T) (afero.Fs, string, string) {
			fs := afero.NewMemMapFs()
			return fs, "/in/A.mkv", "/dest/ABC-123/ABC-123.mkv"
		})
	})

	t.Run("OsFs (bound publish POSIX leg)", func(t *testing.T) {
		if testing.Short() {
			t.Skip("os-level staging fixture")
		}
		run(t, func(t *testing.T) (afero.Fs, string, string) {
			dir := t.TempDir()
			fs := afero.NewOsFs()
			return fs, filepath.Join(dir, "in", "A.mkv"), filepath.Join(dir, "dest", "ABC-123", "ABC-123.mkv")
		})
	})
}

// moveClassifyGapWedgeFs wedges a destination mutation between the authorized
// move lane's execute-time classification and its publish: the lane takes a
// post-classify MkdirAll(TargetDir), so that call gates the wedge (exactly
// once — the fsutil composite's own MkdirAll is the second call and sees the
// mutation's result). Deterministic, goroutine-free.
type moveClassifyGapWedgeFs struct {
	afero.Fs
	dir   string
	fired bool
	once  sync.Once
	wedge func(fs afero.Fs)
}

func (w *moveClassifyGapWedgeFs) MkdirAll(path string, perm os.FileMode) error {
	err := w.Fs.MkdirAll(path, perm)
	if filepath.Clean(path) == filepath.Clean(w.dir) {
		w.once.Do(func() {
			w.fired = true
			// The classification is complete; the rename publish has not yet
			// probed — a cross-process plant/vacate lands exactly here.
			w.wedge(w.Fs)
		})
	}
	return err
}

func moveWedgeOrganizer(base afero.Fs, targetDir string, wedge func(fs afero.Fs)) *Organizer {
	wrapped := &moveClassifyGapWedgeFs{Fs: base, dir: targetDir, wedge: wedge}
	return NewOrganizer(wrapped, &Config{
		FolderFormat:  "<ID>",
		FileFormat:    "<ID>",
		RenameFile:    true,
		OperationMode: operationmode.OperationModeOrganize,
	}, nil, nil)
}

func TestForceOverwriteAudit_PublishBound_MoveLaneWindow(t *testing.T) {
	const dst = "/dest/ABC-123/ABC-123.mkv"

	t.Run("occupant planted post-classification crumbs at publish", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		org := moveWedgeOrganizer(base, "/dest/ABC-123", func(fs afero.Fs) {
			require.NoError(t, afero.WriteFile(fs, dst, []byte("planted-foreign-bytes"), 0o644))
		})

		plan := forceAuditPlan(t, org, forceAuditCmd("/in/A.mkv", true, true))
		result, err := plan.executeStrategy.Execute(plan)
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1,
			"the plant landed between classification and publish — the rename really displaced it")
		assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))
		content, readErr := afero.ReadFile(base, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content)
		srcGone, statErr := afero.Exists(base, "/in/A.mkv")
		require.NoError(t, statErr)
		assert.False(t, srcGone, "the move consumed the source")
	})

	t.Run("occupant vacated post-classification stays silent", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, afero.WriteFile(base, dst, []byte("resident-bytes"), 0o644))
		org := moveWedgeOrganizer(base, filepath.Dir(dst), func(fs afero.Fs) {
			require.NoError(t, fs.Remove(dst))
		})

		plan := forceAuditPlan(t, org, forceAuditCmd("/in/A.mkv", true, true))
		result, err := plan.executeStrategy.Execute(plan)
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings,
			"the occupant vanished between classification and publish — nothing was displaced")
		content, readErr := afero.ReadFile(base, dst)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("winner-bytes"), content)
	})
}

// innerRenameWedgeFs wedges a destination mutation between the in-place inner
// rename lane's classification and its inline rename publish: the lane's
// classifyAuthorizedDestination probes dst then src (one
// classifyExistingDestination call — see its implementation), so the FIRST
// probe of the renamed source path is the deterministic mark after the
// classification's own dst evidence went stale.
type innerRenameWedgeFs struct {
	afero.Fs
	srcAfterDirRename string
	once              sync.Once
	wedge             func(fs afero.Fs)
}

func (w *innerRenameWedgeFs) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	if filepath.Clean(path) == filepath.Clean(w.srcAfterDirRename) {
		w.once.Do(func() {
			w.wedge(w.Fs) // dst already probed by the classification
		})
	}
	return w.Fs.(afero.Lstater).LstatIfPossible(path)
}

// failTargetRenameFs refuses the rename publish onto dst outright (a
// non-cross-device hard failure) so the authorized move lanes surface the
// publish failure: no crumb, no replacement, both objects preserved.
type failTargetRenameFs struct {
	afero.Fs
	dst string
}

func (f failTargetRenameFs) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dst) {
		return errors.New("simulated publish denial")
	}
	return f.Fs.Rename(oldname, newname)
}

func TestForceOverwriteAudit_PublishBound_FailedPublishNoCrumb(t *testing.T) {
	t.Run("in-place move lane: refused publish carries no crumb", func(t *testing.T) {
		base := afero.NewMemMapFs()
		cfg := &Config{
			FolderFormat: "<ID>", FileFormat: "<ID> vidfile",
			RenameFile: true, OperationMode: operationmode.OperationModeInPlace,
		}
		require.NoError(t, base.MkdirAll("/pool/mixed", 0o755))
		require.NoError(t, afero.WriteFile(base, "/pool/mixed/ABC-100 ownA.mkv", []byte("a-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(base, "/pool/mixed/DEF-999 other.mkv", []byte("other-tenant"), 0o644))
		org := NewOrganizer(failTargetRenameFs{Fs: base, dst: "/pool/mixed/ABC-100 vidfile.mkv"}, cfg, nil, ipfMatcher(t))

		_, err := org.Organize(context.Background(), OrganizeCmd{
			Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: "/pool/mixed/ABC-100 ownA.mkv", Name: "ABC-100 ownA.mkv", Extension: ".mkv"},
			Movie:       &models.Movie{ID: "ABC-100"},
			DestDir:     "/dest",
			MoveFiles:   true,
			ForceUpdate: true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to move file")
		content, readErr := afero.ReadFile(base, "/pool/mixed/ABC-100 ownA.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content, "a refused publish deleted nothing")
	})

	t.Run("in-place-norenamefolder lane: refused publish carries no crumb", func(t *testing.T) {
		base := afero.NewMemMapFs()
		cfg := &Config{
			FolderFormat: "<ID>", FileFormat: "<ID> vidfile",
			RenameFile: true, OperationMode: operationmode.OperationModeInPlaceNoRenameFolder,
		}
		require.NoError(t, base.MkdirAll("/pool", 0o755))
		require.NoError(t, afero.WriteFile(base, "/pool/ABC-100 ownA.mkv", []byte("a-bytes"), 0o644))
		org := NewOrganizer(failTargetRenameFs{Fs: base, dst: "/pool/ABC-100 vidfile.mkv"}, cfg, nil, nil)

		_, err := org.Organize(context.Background(), OrganizeCmd{
			Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: "/pool/ABC-100 ownA.mkv", Name: "ABC-100 ownA.mkv", Extension: ".mkv"},
			Movie:       &models.Movie{ID: "ABC-100"},
			DestDir:     "/dest",
			MoveFiles:   true,
			ForceUpdate: true,
		})
		require.Error(t, err)
		assert.Contains(t, err.Error(), "failed to rename file")
		content, readErr := afero.ReadFile(base, "/pool/ABC-100 ownA.mkv")
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content, "a refused publish deleted nothing")
	})
}

// linkInstallWedgeFs wedges a destination mutation between the authorized
// link lane's regular-file-gate occupancy probe (the SECOND LstatIfPossible
// of the destination — the first is the lane-head classification) and the
// Remove that publishes the install: the wrapped probe answers truthfully,
// THEN mutates, so the Remove runs on the mutated physical state.
type linkInstallWedgeFs struct {
	afero.Fs
	dst    string
	probes int
	wedge  func(fs afero.Fs)
}

func (w *linkInstallWedgeFs) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	info, did, err := w.Fs.(afero.Lstater).LstatIfPossible(path)
	if filepath.Clean(path) == filepath.Clean(w.dst) {
		w.probes++
		if w.probes == 2 {
			// The gate has its real answer; the foreign mutation lands before the
			// lane's Remove — exactly the probe → publish window.
			w.wedge(w.Fs)
		}
	}
	return info, did, err
}

func TestForceOverwriteAudit_PublishBound_LinkLaneWindow(t *testing.T) {
	const dst = "/dest/ABC-123/ABC-123.mkv"

	t.Run("occupant planted between the probe and the Remove crumbs", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		strategy := newOrganizeStrategy(&linkInstallWedgeFs{
			Fs:  base,
			dst: dst,
			wedge: func(fs afero.Fs) {
				require.NoError(t, fs.MkdirAll(filepath.Dir(dst), 0o755))
				require.NoError(t, afero.WriteFile(fs, dst, []byte("planted-foreign-bytes"), 0o644))
			},
		}, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
		ml := strategy.linker.(*MemLinker)

		result, err := strategy.Execute(forceAuditLinkPlan("/in/A.mkv", dst, LinkModeHard, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.Len(t, result.Warnings, 1,
			"the Remove physically unlinked the plant's entry — probe-time ('vacant') evidence was the finding's false negative")
		assert.Equal(t, authorizedOverwriteWarning(dst), filepath.ToSlash(result.Warnings[0]))
		require.Len(t, ml.Links, 1, "the install landed on the cleared name")
	})

	t.Run("occupant vacated between the probe and the Remove stays silent", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/in", 0o755))
		require.NoError(t, afero.WriteFile(base, "/in/A.mkv", []byte("winner-bytes"), 0o644))
		require.NoError(t, base.MkdirAll(filepath.Dir(dst), 0o755))
		require.NoError(t, afero.WriteFile(base, dst, []byte("resident-bytes"), 0o644))
		strategy := newOrganizeStrategy(&linkInstallWedgeFs{
			Fs:  base,
			dst: dst,
			wedge: func(fs afero.Fs) {
				require.NoError(t, fs.Remove(dst))
			},
		}, &Config{FolderFormat: "<ID>", FileFormat: "<ID>", RenameFile: true}, nil, &MemLinker{})
		ml := strategy.linker.(*MemLinker)

		result, err := strategy.Execute(forceAuditLinkPlan("/in/A.mkv", dst, LinkModeHard, true))
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings,
			"the Remove saw an already-vacant name — nothing was displaced, even though the probe saw an occupant")
		require.Len(t, ml.Links, 1)
	})
}

func TestForceOverwriteAudit_PublishBound_InnerRenameWindow(t *testing.T) {
	cfg := func() *Config {
		return &Config{
			FolderFormat: "shared", FileFormat: "<ID> vidfile",
			RenameFile: true, OperationMode: operationmode.OperationModeInPlace,
		}
	}
	cmd := OrganizeCmd{
		Match:       models.FileMatchInfo{MovieID: "ABC-100", Path: "/pool/oldA/ABC-100 ownA.mkv", Name: "ABC-100 ownA.mkv", Extension: ".mkv"},
		Movie:       &models.Movie{ID: "ABC-100"},
		DestDir:     "/dest",
		MoveFiles:   true,
		ForceUpdate: true,
	}
	const target = "/pool/shared/ABC-100 vidfile.mkv"

	t.Run("occupant planted post-classification crumbs at the inline publish", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/pool/oldA", 0o755))
		require.NoError(t, afero.WriteFile(base, "/pool/oldA/ABC-100 ownA.mkv", []byte("a-bytes"), 0o644))
		org := NewOrganizer(&innerRenameWedgeFs{
			Fs:                base,
			srcAfterDirRename: "/pool/shared/ABC-100 ownA.mkv",
			wedge: func(fs afero.Fs) {
				require.NoError(t, afero.WriteFile(fs, target, []byte("planted-foreign-bytes"), 0o644))
			},
		}, cfg(), nil, ipfMatcher(t))

		result, err := org.Organize(context.Background(), cmd)
		require.NoError(t, err)
		require.True(t, result.Moved)
		require.True(t, result.InPlaceRenamed)
		require.Len(t, result.Warnings, 1,
			"the classification saw the name VACANT, yet the publish displaced a plant — publish-bound evidence catches it")
		assert.Equal(t, authorizedOverwriteWarning(target), filepath.ToSlash(result.Warnings[0]))
		content, readErr := afero.ReadFile(base, target)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content, "the plant's bytes were displaced")
	})

	t.Run("occupant vacated post-classification stays silent", func(t *testing.T) {
		base := afero.NewMemMapFs()
		require.NoError(t, base.MkdirAll("/pool/oldA", 0o755))
		require.NoError(t, afero.WriteFile(base, "/pool/oldA/ABC-100 ownA.mkv", []byte("a-bytes"), 0o644))
		require.NoError(t, afero.WriteFile(base, "/pool/oldA/ABC-100 vidfile.mkv", []byte("resident-bytes"), 0o644))
		org := NewOrganizer(&innerRenameWedgeFs{
			Fs:                base,
			srcAfterDirRename: "/pool/shared/ABC-100 ownA.mkv",
			wedge: func(fs afero.Fs) {
				require.NoError(t, fs.Remove(target))
			},
		}, cfg(), nil, ipfMatcher(t))

		result, err := org.Organize(context.Background(), cmd)
		require.NoError(t, err)
		require.True(t, result.Moved)
		assert.Empty(t, result.Warnings,
			"the classification saw an occupant, but it vanished before the publish displaced nothing — no crumb")
		content, readErr := afero.ReadFile(base, target)
		require.NoError(t, readErr)
		assert.Equal(t, []byte("a-bytes"), content)
	})
}
