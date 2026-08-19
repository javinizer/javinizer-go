package history

// POSTER-WRITE-HARDENING codex PR#215 wave-21 (P1) — "apply re-arm metadata
// before publishing the backup": rearmReplacementBackup USED to publish the
// re-staged backup via copyRearmSourceBytes and apply
// RestoreStagingOwnership + Chmod + Chtimes to the PUBLISHED path
// afterwards. In a directory writable by another user the backup name could
// be swapped for a SYMLINK inside that publish→metadata window, and the
// path-based chown/chmod/chtimes would follow the link to an arbitrary
// target. The metadata application is now threaded INSIDE
// copyRearmSourceBytes onto the EXCLUSIVELY OWNED staged inode
// (fsutil.CreateExclusiveStagingFile — O_EXCL, the mode asserted at create,
// wave-7) strictly BEFORE the no-replace publish, so the published backup
// lands with mode+times+ownership complete and NO post-publish metadata
// calls remain in the flow. wave-29 (codex P1) hardened the pre-publish
// operations further: every staged-inode metadata leg runs THROUGH THE OPEN
// HANDLE (fchmod at create, handle-scoped times, fchown ownership) plus a
// publish-time identity proof (fsutil.VerifyStagedIdentity), so a staged
// NAME planted mid-flow can never redirect the metadata or the publish. On
// the virtual filesystems below, the fsutil helpers fall back to name-based
// legs against the stored (filepath.Clean'd) spelling, recorded through the
// same event log. These tests pin the ordering through recording seams, the
// untouched refusal path, and the wave-20 clean-kind publish class's
// surviving trigger. The wave-20 trichotomy pins live in
// reverter_rearm_pending_w20_test.go (which reuses the
// w21RearmPublishCompletedSeam helper below).

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

// w21RearmPublishCompletedSeam swaps the re-arm publish seam so the FIRST
// re-arm publish SUCCEEDS for real (the staged copy lands at the backup
// name) but reports fsutil.ErrPublishCompleted — the POSIX hard-link
// fallback's cleanup-with-failed-rollback leg, the only post-publish failure
// class left after wave-21. Later publishes delegate undisturbed.
func w21RearmPublishCompletedSeam(t *testing.T, fired *bool) {
	t.Helper()
	prev := rearmPublishFn
	rearmPublishFn = func(fsys afero.Fs, src, dst string) error {
		if *fired {
			return prev(fsys, src, dst)
		}
		*fired = true
		if err := prev(fsys, src, dst); err != nil {
			return err
		}
		return fmt.Errorf("no-replace publish %s -> %s: staged cleanup failed AND publish rollback failed: %w", src, dst, fsutil.ErrPublishCompleted)
	}
	t.Cleanup(func() { rearmPublishFn = prev })
}

// w21MetaEvent records one metadata/publish operation in call order.
type w21MetaEvent struct {
	op   string // "chmod" | "chtimes" | "ownership" | "publish"
	path string
	mode os.FileMode
}

// w21RecordMetaFs records Chmod/Chtimes operations in call order. The
// fallthrough keeps the wave-17a MemMapFs Windows-Chmod normalization so the
// test is host-portable.
type w21RecordMetaFs struct {
	afero.Fs
	events *[]w21MetaEvent
}

func (f *w21RecordMetaFs) Chmod(name string, mode os.FileMode) error {
	*f.events = append(*f.events, w21MetaEvent{op: "chmod", path: name, mode: mode})
	return f.Fs.Chmod(filepath.FromSlash(name), mode)
}

func (f *w21RecordMetaFs) Chtimes(name string, atime, mtime time.Time) error {
	*f.events = append(*f.events, w21MetaEvent{op: "chtimes", path: name})
	return f.Fs.Chtimes(name, atime, mtime)
}

// w21SeamedRearmFixture builds dest (+ pinned mode/mtime) for a direct
// re-arm and installs the recording seams; it returns the reasoned backup
// path, the destination info, and the event log.
func w21SeamedRearmFixture(t *testing.T, dir string) (fs *w21RecordMetaFs, dest, backup string, info os.FileInfo, events *[]w21MetaEvent) {
	t.Helper()
	base := afero.NewMemMapFs()
	require.NoError(t, base.MkdirAll(dir, 0o755))
	dest = dir + "/poster.jpg"
	backup = dest + ".dlbak." + p3HexA
	require.NoError(t, afero.WriteFile(base, dest, []byte("current-bytes"), 0o640))
	// afero MemMapFs stores entries under filepath.Clean'd keys while its
	// Chmod does a RAW name lookup — normalize exactly like the wave-17a
	// harness so the slash-spelled fixture name hits on the Windows runner.
	require.NoError(t, base.Chmod(filepath.FromSlash(dest), 0o640))
	mtime := time.Unix(1690000000, 0)
	require.NoError(t, base.Chtimes(dest, mtime, mtime))
	var statErr error
	info, statErr = base.Stat(dest)
	require.NoError(t, statErr)

	events = &[]w21MetaEvent{}
	fs = &w21RecordMetaFs{Fs: base, events: events}

	prevOwn := restoreStagingOwnershipFn
	restoreStagingOwnershipFn = func(_ afero.Fs, staged afero.File, _ os.FileInfo) {
		stagedName := ""
		if staged != nil {
			stagedName = staged.Name()
		}
		*events = append(*events, w21MetaEvent{op: "ownership", path: stagedName})
	}
	prevPub := rearmPublishFn
	rearmPublishFn = func(fsys afero.Fs, src, dst string) error {
		*events = append(*events, w21MetaEvent{op: "publish", path: dst})
		return prevPub(fsys, src, dst)
	}
	t.Cleanup(func() {
		restoreStagingOwnershipFn = prevOwn
		rearmPublishFn = prevPub
	})
	return fs, dest, backup, info, events
}

// w21Slash normalizes separator spellings for assertions. Journal spellings
// stay slash-formed while the wave-29 handle-metadata fallbacks record the
// stored (filepath.Clean'd — backslash on the Windows runner) spelling; both
// forms name the same staged inode.
func w21Slash(p string) string { return strings.ReplaceAll(p, "\\", "/") }

// w21StagedName checks that path names the wave-21 exclusive re-arm staging
// inode for backup. The comparison is slash-neutral: the MemMapFs keying and
// the journal spelling legitimately diverge on the Windows runner.
func w21StagedName(t *testing.T, path, backup string) {
	t.Helper()
	normalizedPath, normalizedBackup := w21Slash(path), w21Slash(backup)
	require.True(t, strings.HasPrefix(normalizedPath, normalizedBackup) && strings.Contains(filepath.Base(path), rearmStagingSuffix),
		"%q must be the exclusively staged inode (<%s>%s.<hex>)", path, backup, rearmStagingSuffix)
}

// The core ordering pin: EVERY re-arm metadata operation — the create-time
// mode Chmod, the Chtimes fix-up, and the ownership hand-off — targets the
// exclusively staged inode and fires BEFORE the publish; the published
// backup path receives NO metadata call at any point, before or after.
func TestRearmW21_MetadataAppliedToStagedInodeBeforePublish(t *testing.T) {
	fs, dest, backup, info, events := w21SeamedRearmFixture(t, "/out/W21-ORDER")

	require.NoError(t, rearmReplacementBackup(fs, dest, backup, info))

	var publishIdx = -1
	for i, e := range *events {
		switch e.op {
		case "chmod", "chtimes", "ownership":
			require.NotEqual(t, w21Slash(backup), w21Slash(e.path),
				"post-publish metadata on the PUBLISHED name (%s %s) — the wave-21 hole", e.op, e.path)
			w21StagedName(t, e.path, backup)
			require.Equal(t, -1, publishIdx, "%s must run BEFORE the publish", e.op)
		case "publish":
			require.Equal(t, w21Slash(backup), w21Slash(e.path), "the publish targets the backup name")
			require.Equal(t, -1, publishIdx, "exactly one publish")
			publishIdx = i
		}
	}
	require.NotEqual(t, -1, publishIdx, "the publish seam fired")

	var chmodSeen, chtimesSeen, ownershipSeen bool
	for _, e := range *events {
		switch e.op {
		case "chmod":
			chmodSeen = true
			require.Equal(t, os.FileMode(0o640), e.mode, "the requested mode lands at the staging create")
		case "chtimes":
			chtimesSeen = true
		case "ownership":
			ownershipSeen = true
		}
	}
	require.True(t, chmodSeen, "the create-time mode assert ran")
	require.True(t, chtimesSeen, "the pre-publish Chtimes ran")
	require.True(t, ownershipSeen, "the pre-publish ownership hand-off ran")

	// The final backup ends up with the requested mode+times+ownership — the
	// wave-7 staging helper's perms audit — with the destination untouched.
	require.Equal(t, "current-bytes", string(mustRead2(t, fs, backup)))
	gotInfo, err := fs.Stat(backup)
	require.NoError(t, err)
	require.Equal(t, os.FileMode(0o640), gotInfo.Mode().Perm(), "requested mode on the published backup")
	require.True(t, gotInfo.ModTime().Equal(time.Unix(1690000000, 0)), "requested mtime on the published backup")
	require.Equal(t, "current-bytes", string(mustRead2(t, fs, dest)), "the re-arm is a copy, never a move")
	for _, name := range w15DirListing(t, fs, filepath.Dir(dest)) {
		require.NotContains(t, name, rearmStagingSuffix+".", "no staged residue after the publish: %q", name)
	}
}

// info == nil keeps the copy-only posture: no metadata operation at all
// (mode defaults land at create, no Chtimes/ownership), publish still last.
func TestRearmW21_NilInfoRunsNoMetadataLegs(t *testing.T) {
	fs, dest, backup, _, events := w21SeamedRearmFixture(t, "/out/W21-NILINFO")

	require.NoError(t, rearmReplacementBackup(fs, dest, backup, nil))

	for _, e := range *events {
		require.NotContains(t, []string{"chtimes", "ownership"}, e.op,
			"nil info: no %s leg at all", e.op)
	}
	require.NotEmpty(t, *events)
	require.Equal(t, "publish", (*events)[len(*events)-1].op, "the publish stays the final operation")
	require.Equal(t, w21Slash(backup), w21Slash((*events)[len(*events)-1].path))
	require.Equal(t, "current-bytes", string(mustRead2(t, fs, backup)))
}

// The REFUSAL leg is unchanged by the pre-publish reordering: a foreign
// claim inside the copy→publish window still surfaces the typed
// fsutil.ErrPublishCollision, the occupant keeps its bytes, the publish is
// the ONLY operation targeting the backup name, and staging residue is
// cleaned up.
func TestRearmW21_RefusalPathUnchanged(t *testing.T) {
	fs, dest, backup, info, events := w21SeamedRearmFixture(t, "/out/W21-REFUSAL")
	race := &w15BackupRaceFs{Fs: fs, target: backup, foreign: []byte("foreign-bytes")}

	err := rearmReplacementBackup(race, dest, backup, info)
	require.ErrorIs(t, err, fsutil.ErrPublishCollision, "the refusal still carries the typed collision class")
	require.True(t, race.fired, "the injected foreign claim raced the publish")

	for _, e := range *events {
		if e.op == "publish" {
			continue
		}
		require.NotEqual(t, w21Slash(backup), w21Slash(e.path), "%s never targets the published backup name", e.op)
		w21StagedName(t, e.path, backup)
	}
	require.Equal(t, "foreign-bytes", string(mustRead2(t, fs, backup)), "the foreign occupant is untouched")
	require.Equal(t, "current-bytes", string(mustRead2(t, fs, dest)), "the re-arm source is untouched")
	for _, name := range w15DirListing(t, fs, filepath.Dir(dest)) {
		require.NotContains(t, name, rearmStagingSuffix+".", "the staged re-arm copy is cleaned up on refusal: %q", name)
	}
}

// The publish-completed class at unit level: the publish lands the staged
// copy at the backup name but reports fsutil.ErrPublishCompleted — the two
// classifiers read it as the OWNED name (clean pending kind), the metadata
// is already in place, and the publish stays the last mutation.
func TestRearmW21_PublishCompletedLeavesOwnedBackupWithFullMetadata(t *testing.T) {
	fs, dest, backup, info, events := w21SeamedRearmFixture(t, "/out/W21-PUBDONE")

	published := false
	w21RearmPublishCompletedSeam(t, &published)
	// Re-install the ordering capture on top of the publish-completed wrapper.
	publishCompletedWrapper := rearmPublishFn
	rearmPublishFn = func(fsys afero.Fs, src, dst string) error {
		*events = append(*events, w21MetaEvent{op: "publish", path: dst})
		return publishCompletedWrapper(fsys, src, dst)
	}
	t.Cleanup(func() { rearmPublishFn = publishCompletedWrapper })

	err := rearmReplacementBackup(fs, dest, backup, info)
	require.ErrorIs(t, err, fsutil.ErrPublishCompleted)
	require.True(t, fsutil.PublishCompleted(err), "the shared classifier reads the class")
	require.True(t, published, "the publish itself succeeded")
	require.Equal(t, models.RestorePendingKindClean, rearmPendingKind(err),
		"the unit-level error maps to the clean owned-name kind")

	require.Equal(t, "current-bytes", string(mustRead2(t, fs, backup)))
	gotInfo, serr := fs.Stat(backup)
	require.NoError(t, serr)
	require.Equal(t, os.FileMode(0o640), gotInfo.Mode().Perm(), "metadata landed pre-publish — the completed publish carries it")
	require.True(t, gotInfo.ModTime().Equal(time.Unix(1690000000, 0)))
	for _, e := range *events {
		if e.op == "publish" {
			continue
		}
		w21StagedName(t, e.path, backup)
	}
}

// The staged-name failures land BEFORE the publish: no publish seam call at
// all, nothing at the backup name, staged residue gone.
func TestRearmW21_PrePublishFailuresNeverReachThePublishSeam(t *testing.T) {
	t.Run("staging open wedged", func(t *testing.T) {
		fs, dest, backup, info, events := w21SeamedRearmFixture(t, "/out/W21-PP-OPEN")
		wedge := &w8RearmFailFs{Fs: fs}
		err := rearmReplacementBackup(wedge, dest, backup, info)
		require.Error(t, err)
		for _, e := range *events {
			require.NotEqual(t, "publish", e.op, "no publish after a staging-open failure")
		}
		_, serr := fs.Stat(backup)
		require.ErrorIs(t, serr, os.ErrNotExist)
	})

	t.Run("staged Chtimes wedged", func(t *testing.T) {
		fs, dest, backup, info, events := w21SeamedRearmFixture(t, "/out/W21-PP-CTIMES")
		wedge := &w17aChtimesFailFs{Fs: fs}
		err := rearmReplacementBackup(wedge, dest, backup, info)
		require.ErrorContains(t, err, "re-arm chtimes wedged")
		for _, e := range *events {
			require.NotEqual(t, "publish", e.op, "no publish after a pre-publish metadata failure")
		}
		_, serr := fs.Stat(backup)
		require.ErrorIs(t, serr, os.ErrNotExist)
		for _, name := range w15DirListing(t, fs, filepath.Dir(dest)) {
			require.NotContains(t, name, rearmStagingSuffix+".", "staged residue removed: %q", name)
		}
	})

	t.Run("publish seam refusal keeps name untouched", func(t *testing.T) {
		fs, dest, backup, info, events := w21SeamedRearmFixture(t, "/out/W21-PP-PUB")
		prev := rearmPublishFn
		rearmPublishFn = func(afero.Fs, string, string) error {
			*events = append(*events, w21MetaEvent{op: "publish", path: backup})
			return fmt.Errorf("re-arm install backup %s: %w", backup, fsutil.ErrPublishNoReplaceUnsupported)
		}
		t.Cleanup(func() { rearmPublishFn = prev })

		err := rearmReplacementBackup(fs, dest, backup, info)
		require.ErrorIs(t, err, fsutil.ErrPublishNoReplaceUnsupported)
		_, serr := fs.Stat(backup)
		require.ErrorIs(t, serr, os.ErrNotExist)
	})
}
