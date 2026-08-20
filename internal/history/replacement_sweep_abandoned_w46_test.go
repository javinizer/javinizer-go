package history

// POSTER-WRITE-HARDENING codex PR#215 wave-46 (P2) — stop abandoned sweeps
// before they claim anything. The wave-8 bounded discipline runs the
// pre-revert sweep in a goroutine behind a derived deadline; the caller
// stops waiting at the budget and the revert CONTINUES while the abandoned
// goroutine stays parked inside a stalled afero.ReadDir. When the
// filesystem finally answers, the abandoned sweep kept arbitrating: it
// entered sweepOne and created the destination's .dlbusy marker BEFORE any
// ctx check — and that ownerless claim collided with the continued revert
// (ErrReplacementBusy). The scan loops (Sweep / SweepDirs /
// SweepDestinations) now recheck ctx.Err() the moment a ReadDir returns,
// and sweepOne re-gates at entry, ahead of EVERY busy claim and journal
// op: an abandoned post-deadline sweep is a strict no-op.

import (
	"context"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// w46SeedCrashWindow journals one armed (install never confirmed)
// replacement whose destination is missing; unlike seedCrashWindow the file
// NAME is caller-chosen so several crash windows can share one directory
// (the wave-46 same-dir entry-loop coverage needs deterministic sibling
// ordering inside a single scan).
func w46SeedCrashWindow(t *testing.T, fs afero.Fs, repo *p3OpRepo, jobID, movieID, dir, fileName, hexTag string) (*models.BatchFileOperation, string, string) {
	t.Helper()
	dest := filepath.ToSlash(filepath.Join(dir, fileName))
	backup := dest + ".dlbak." + hexTag
	require.NoError(t, fs.MkdirAll(filepath.FromSlash(dir), 0o755))
	writeSweepFile(t, fs, backup, "original-"+movieID, time.Hour)
	op := journalRow(t, repo, jobID, movieID, dest, backup, 1, models.RevertStatusApplied)
	return op, filepath.FromSlash(dest), filepath.FromSlash(backup)
}

// w46WedgedReadDirFs parks the scan-dir Open (afero.ReadDir's entry point)
// like a stalled network filesystem that never observes the context: the
// caller releases it — after the deadline fires — to model "the wedged
// ReadDir finally returns". Every later Open of the same dir flows through
// immediately (the release channel stays closed).
type w46WedgedReadDirFs struct {
	afero.Fs
	dir     string // slash-form scan dir whose Open wedges
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (f *w46WedgedReadDirFs) Open(name string) (afero.File, error) {
	if filepath.ToSlash(name) == f.dir {
		f.once.Do(func() { close(f.entered) })
		<-f.release
	}
	return f.Fs.Open(name)
}

// w46CancelOnFirstTouchFs cancels the bound context the first time any fs
// traffic touches one of the trigger names (slash-form). Wave-46 restaging
// of the mid-sweep deadline tests: cancellation lands DURING a directory's
// heal (its busy-marker claim OpenFile), because a cancellation landing
// inside the ReadDir itself is now honored immediately after the scan —
// the in-flight dir no longer processes past-deadline entries.
type w46CancelOnFirstTouchFs struct {
	afero.Fs
	trigger map[string]bool
	cancel  context.CancelFunc
	mu      sync.Mutex
	fired   bool
}

func (f *w46CancelOnFirstTouchFs) fireOn(name string) {
	slash := filepath.ToSlash(name)
	f.mu.Lock()
	defer f.mu.Unlock()
	if !f.fired && f.trigger[slash] {
		f.fired = true
		f.cancel()
	}
}

func (f *w46CancelOnFirstTouchFs) Open(name string) (afero.File, error) {
	f.fireOn(name)
	return f.Fs.Open(name)
}

func (f *w46CancelOnFirstTouchFs) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	f.fireOn(name)
	return f.Fs.OpenFile(name, flag, perm)
}

// w46RequireAbandonedNoOp pins the strict no-op contract of an abandoned
// sweep: no busy marker was claimed, no bytes were restored onto the
// destination, the backup stays byte-intact, and the journal entry remains
// armed for the NEXT (live) sweep.
func w46RequireAbandonedNoOp(t *testing.T, fs afero.Fs, repo *p3OpRepo, op *models.BatchFileOperation, dest, backup, backupContent string) {
	t.Helper()
	busyExists, err := afero.Exists(fs, filepath.ToSlash(fsutil.ReplacementBusyPath(dest)))
	require.NoError(t, err)
	require.False(t, busyExists, "an abandoned sweep never claims the destination's busy marker")
	destExists, err := afero.Exists(fs, dest)
	require.NoError(t, err)
	require.False(t, destExists, "an abandoned sweep never publishes a restore")
	backupExists, err := afero.Exists(fs, backup)
	require.NoError(t, err)
	require.True(t, backupExists, "an abandoned sweep never consumes the backup")
	require.Equal(t, backupContent, string(mustRead2(t, fs, backup)), "the backup bytes survive untouched")
	require.Len(t, requireLedgerReplacements(t, repo, op.ID), 1, "the journal entry stays armed — an abandoned sweep runs no journal op")
}

// The wave-46 contract at the FULL sweep's scan loop: a ReadDir that stalls
// past the deadline surfaces entries nobody may arbitrate anymore — the
// sweep records the canceled error and processes NOTHING.
func TestSweepW46_StalledReadDirPastDeadlineProcessesNothing(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w46-stall", "STL-001", "/w46-stall", p3HexA)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	entered, release := make(chan struct{}), make(chan struct{})
	fs := &w46WedgedReadDirFs{Fs: base, dir: "/w46-stall", entered: entered, release: release}
	go func() {
		<-entered // the sweep is parked inside the wedged ReadDir
		cancel()
		close(release) // the filesystem answers only PAST the deadline
	}()

	healed, err := NewReplacementSweeper(fs, repo).Sweep(ctx)
	require.ErrorIs(t, err, context.Canceled, "the post-ReadDir recheck records the deadline")
	require.Equal(t, 0, healed, "entries a stalled ReadDir surfaces past the deadline arbitrate nothing")

	w46RequireAbandonedNoOp(t, base, repo, op, dest, backup, "original-STL-001")
}

// Same contract through the SCOPED sweep (the reverter's roots leg).
func TestSweepDirsW46_StalledReadDirPastDeadlineProcessesNothing(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w46-sdir", "SDR-001", "/w46-stalld", p3HexA)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	entered, release := make(chan struct{}), make(chan struct{})
	fs := &w46WedgedReadDirFs{Fs: base, dir: "/w46-stalld", entered: entered, release: release}
	go func() {
		<-entered
		cancel()
		close(release)
	}()

	healed, err := NewReplacementSweeper(fs, repo).SweepDirs(ctx, []string{"/w46-stalld"})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, healed)

	w46RequireAbandonedNoOp(t, base, repo, op, dest, backup, "original-SDR-001")
}

// Same contract through the TARGETED sweep (the reverter's destinations
// leg — the exact seam the wave-8 goroutine drives first).
func TestSweepDestinationsW46_StalledReadDirPastDeadlineProcessesNothing(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, base, repo, "job-w46-sdest", "SDT-001", "/w46-stallg", p3HexA)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	entered, release := make(chan struct{}), make(chan struct{})
	fs := &w46WedgedReadDirFs{Fs: base, dir: "/w46-stallg", entered: entered, release: release}
	go func() {
		<-entered
		cancel()
		close(release)
	}()

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{dest})
	require.ErrorIs(t, err, context.Canceled)
	require.Equal(t, 0, healed)

	w46RequireAbandonedNoOp(t, base, repo, op, dest, backup, "original-SDT-001")
}

// The deadline can also land BETWEEN entries of ONE scan (the ReadDir came
// back inside the budget and the ctx died during the first entry's heal):
// sweepOne's entry gate makes every LATER sibling a strict no-op while the
// in-flight entry completes.
func TestSweepDestinationsW46_DeadlineMidDirSkipsRemainingEntries(t *testing.T) {
	base := afero.NewMemMapFs()
	repo := newP3OpRepo()
	opA, destA, backupA := w46SeedCrashWindow(t, base, repo, "job-w46-entry", "ENT-A", "/w46-entry", "poster-a.jpg", p3HexA)
	opB, destB, backupB := w46SeedCrashWindow(t, base, repo, "job-w46-entry", "ENT-B", "/w46-entry", "poster-b.jpg", p3HexB)

	ctx, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	// ReadDir sorts "poster-a" before "poster-b": cancellation lands at A's
	// busy claim, INSIDE A's heal — after A's entry gate already passed.
	fs := &w46CancelOnFirstTouchFs{Fs: base, cancel: cancel,
		trigger: map[string]bool{filepath.ToSlash(fsutil.ReplacementBusyPath(destA)): true}}

	healed, err := NewReplacementSweeper(fs, repo).SweepDestinations(ctx, []string{destA, destB})
	require.NoError(t, err, "the in-flight heal completes; the sweep result stays usable")
	require.Equal(t, 1, healed, "only the entry in flight when the deadline landed is healed")

	require.Equal(t, "original-ENT-A", string(mustRead2(t, base, destA)), "A restored fully inside the deadline")
	backupAExists, err := afero.Exists(base, backupA)
	require.NoError(t, err)
	require.False(t, backupAExists, "A's backup consumed by its completed heal")
	require.Empty(t, requireLedgerReplacements(t, repo, opA.ID), "A's journal entry consumed")

	w46RequireAbandonedNoOp(t, base, repo, opB, destB, backupB, "original-ENT-B")
}

// Unit gate for sweepOne itself: entered with an already-dead context, it
// returns 0 before ANY busy claim or journal op — the exact sequence the
// abandoned wave-8 goroutine would otherwise run after its stalled ReadDir.
func TestSweepOneW46_CanceledAtEntryClaimsNothing(t *testing.T) {
	fs := afero.NewMemMapFs()
	repo := newP3OpRepo()
	op, dest, backup := seedCrashWindow(t, fs, repo, "job-w46-direct", "DIR-001", "/w46-direct", p3HexA)

	sweeper := NewReplacementSweeper(fs, repo)
	idx, err := sweeper.index(context.Background())
	require.NoError(t, err)
	info, err := fs.Stat(backup)
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	got := sweeper.sweepOne(ctx, idx, "/w46-direct", info)
	require.Equal(t, 0, got, "a post-deadline entry gate refuses all arbitration")

	w46RequireAbandonedNoOp(t, fs, repo, op, dest, backup, "original-DIR-001")
}
