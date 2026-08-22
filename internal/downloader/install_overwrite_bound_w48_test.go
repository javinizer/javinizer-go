package downloader

// POSTER-WRITE-HARDENING wave-48 (codex P2, PR#215 finding 6) — the staged
// install input is bound to the VALIDATED OBJECT end to end, not merely
// re-derived by name: validateDownloadedMedia hands its open read handle down
// with the wave-45 identity record, downloadPoster binds the cropped
// candidate's post-write no-follow fd the same way, and installOverwriting
// publishes every leg through fsutil.PublishStagedBoundInfo with Handle=that
// validated handle (re-opened no-follow + fstat-bound when an earlier attempt
// consumed the original). A substitute planted at tempPath after validation
// now provably reaches the typed errStagedInputSubstituted refusal — or a
// reclassification whose compensation preserves it — BEFORE any bytes-at-dest
// mutation, and the targeted mid-publish windows self-heal (POSIX restage
// from the open handle) with foreign occupants preserved byte-intact.

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

// w48Stage writes the staged install input on OsFs and returns its
// wave-48 provenance bundle: the record frozen from the open descriptor's
// fstat and the descriptor itself, exactly the pair http.download hands
// down. Callers that want the recorded-only posture take .identity.
func w48Stage(t *testing.T, fs afero.Fs, dir, stagedBody string) (staged, dest string, prov stagedInstallProvenance) {
	t.Helper()
	require.NoError(t, os.MkdirAll(dir, 0o755))
	staged = filepath.Join(dir, ".staged-download")
	dest = filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(staged, []byte(stagedBody), 0o644))
	fh, err := fs.Open(staged)
	require.NoError(t, err)
	info, err := fh.Stat()
	require.NoError(t, err)
	return staged, dest, stagedInstallProvenance{identity: installedIdentityFromFileInfo(info), handle: fh}
}

// w48Closed reports whether fh is closed (post-consumption assertion: the
// retained validated handle must never leak out of an install).
func w48Closed(fh afero.File) bool {
	_, err := fh.Stat()
	return err != nil
}

// Finding 6, create and replace happy legs: the retained validated handle
// rides into the wave-30 bound publish (POSIX: name==fd proof + post-publish
// reverify), installs exactly the validated bytes, and is consumed (closed)
// by the publish — never leaked into the caller's tempdir cleanup.
func TestInstallOverwritingW48_BoundHandleInstallsEndToEnd(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()

	t.Run("create leg", func(t *testing.T) {
		staged, dest, prov := w48Stage(t, fs, filepath.Join(dir, "create"), "w48-genuine-create")

		d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
		skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{}, prov)
		require.NoError(t, err)
		require.False(t, skipped)
		require.False(t, replaced)
		require.True(t, w48Closed(prov.handle), "the bound publish consumed the validated handle — no descriptor leaks into tempdir cleanup")
		got, rerr := os.ReadFile(dest)
		require.NoError(t, rerr)
		require.Equal(t, "w48-genuine-create", string(got))
	})

	t.Run("replace leg", func(t *testing.T) {
		staged, dest, prov := w48Stage(t, fs, filepath.Join(dir, "replace"), "w48-genuine-replace")
		require.NoError(t, os.WriteFile(dest, []byte("old poster bytes"), 0o644))

		ledger := &w45Ledger{}
		d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
		skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
			downloadLedger{opID: "w48-replace", recorder: ledger}, prov)
		require.NoError(t, err)
		require.False(t, skipped)
		require.True(t, replaced)
		require.True(t, w48Closed(prov.handle))
		got, rerr := os.ReadFile(dest)
		require.NoError(t, rerr)
		require.Equal(t, "w48-genuine-replace", string(got))
		require.Equal(t, 1, ledger.records)
		require.Equal(t, 1, ledger.confirmed)
		require.Empty(t, ledger.released)
		// The journaled backup of the pre-existing bytes survives the
		// confirmed install (revert/sweep arbitration), byte-intact.
		entries, derr := os.ReadDir(filepath.Dir(dest))
		require.NoError(t, derr)
		backups := 0
		for _, e := range entries {
			if strings.Contains(e.Name(), ".dlbak.") {
				backups++
				old, berr := os.ReadFile(filepath.Join(filepath.Dir(dest), e.Name()))
				require.NoError(t, berr)
				require.Equal(t, "old poster bytes", string(old))
			}
		}
		require.Equal(t, 1, backups)
	})

	t.Run("replace leg with a consumed handle re-binds no-follow", func(t *testing.T) {
		// The recorded-only posture on the REAL OsFs: bindStagedProvenanceHandle
		// re-opens the staged name no-follow and binds the fresh fd to the
		// validation record (dev/inode + size + mtime) before the publish.
		staged, dest, prov := w48Stage(t, fs, filepath.Join(dir, "rebind"), "w48-genuine-rebind")
		require.NoError(t, prov.handle.Close())
		prov.handle = nil
		require.NoError(t, os.WriteFile(dest, []byte("old"), 0o644))

		ledger := &w45Ledger{}
		d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
		skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
			downloadLedger{opID: "w48-rebind", recorder: ledger}, prov)
		require.NoError(t, err)
		require.False(t, skipped)
		require.True(t, replaced)
		got, rerr := os.ReadFile(dest)
		require.NoError(t, rerr)
		require.Equal(t, "w48-genuine-rebind", string(got))
	})
}

// Finding 6: every non-publish exit keeps the retained-handle lifecycle —
// the unarmed-ledger skip closes it so the caller's staged unlink can never
// wedge on a held descriptor (the Windows "file in use" discipline).
func TestInstallOverwritingW48_SkipLegClosesRetainedHandle(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, dest, prov := w48Stage(t, fs, dir, "w48-staged")
	require.NoError(t, os.WriteFile(dest, []byte("existing artwork"), 0o644))

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{}, prov)
	require.NoError(t, err)
	require.True(t, skipped)
	require.True(t, w48Closed(prov.handle), "the skip exit closed the retained handle")
	require.Equal(t, "existing artwork", string(mustReadW48(t, dest)))
	// The staged name is exactly the caller's to reap — the close above makes
	// the unlink succeed on every platform.
	require.NoError(t, os.Remove(staged))
}

func mustReadW48(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	require.NoError(t, err)
	return data
}

// Finding 6, the pre-publish gap: the substitute rotated onto the staged name
// before install starts trips the wave-45 classify gate BEFORE any byte flow
// — and the retained handle (never consumed by a publish) is closed by the
// install's own defer while the substitute is preserved byte-intact.
func TestInstallOverwritingW48_PrePublishSubstitutionRefusedClosesHandle(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, dest, prov := w48Stage(t, fs, dir, "VALIDATED")
	// Platform-limited substitution shape: the validated handle is OPEN, and
	// on Windows Go opens files without FILE_SHARE_DELETE, so renaming the
	// staged name away from under it fails ("being used by another
	// process"). Windows' expressible substitution is the IN-PLACE rewrite
	// under the write share; POSIX keeps the rename-away shape. Both land a
	// name↔record mismatch (different size) at the wave-45 classify gate.
	if runtime.GOOS == "windows" {
		require.NoError(t, os.WriteFile(staged, []byte("VALIDATED-plant"), 0o600))
	} else {
		require.NoError(t, os.Rename(staged, staged+".hidden"))
		require.NoError(t, os.WriteFile(staged, []byte("VALIDATED-plant"), 0o600))
	}

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{}, prov)
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.False(t, skipped)
	require.False(t, replaced)
	require.True(t, w48Closed(prov.handle))
	require.Equal(t, "VALIDATED-plant", string(mustReadW48(t, staged)), "the substitute is preserved, never unlinked")
	if runtime.GOOS != "windows" {
		require.Equal(t, "VALIDATED", string(mustReadW48(t, staged+".hidden")),
			"POSIX rename-away: the validated object survived under the attacker's name")
	}
	_, derr := os.Lstat(dest)
	require.True(t, os.IsNotExist(derr), "no byte ever flowed into the destination")
}

// Finding 6, the verify→publish window itself (create intent, POSIX restage
// visible): the wedge replays the directory writer INSIDE the publish call —
// the bound publish's own verify already passed. The planted name is moved
// onto the destination by the kernel, the post-publish reverify catches the
// shaping, the no-replace republish of the restaged genuine bytes collision-
// refuses (destination occupied by the preserved plant), and the downloader's
// wave-15 reclassification + rollback compensation route the window plant
// through backup+restore — never treating foreign bytes as ours, never
// reporting success, and retracting the armed journal entry.
func TestInstallOverwritingW48_MidPublishSubstitutionOnCreateWindow(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, dest, prov := w48Stage(t, fs, dir, "w48 validation-genuine")

	attacked := false
	prev := publishStagedBoundFn
	publishStagedBoundFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		wedged := p.Publish
		p.Publish = func(fsys afero.Fs, src, dst string) error {
			if !attacked {
				attacked = true
				require.NoError(t, os.Rename(src, src+".w48-away"))
				require.NoError(t, os.WriteFile(src, []byte("w48 window plant"), 0o600))
			}
			return wedged(fsys, src, dst)
		}
		return prev(p)
	}
	t.Cleanup(func() { publishStagedBoundFn = prev })

	ledger := &w45Ledger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, _, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w48-window", recorder: ledger}, prov)
	require.Error(t, err, "a window substitution never reports an installed success")
	require.True(t, attacked, "the verify→publish window wedge fired")

	if runtime.GOOS == "windows" {
		// Platform-limited posture: MoveFileEx cannot act on an open handle,
		// so the Windows bound publish closes the validated handle BEFORE the
		// path publish (identity captured pre-close, re-verified post-publish)
		// — the verify→publish window wedge is fully expressible, but the
		// POSIX self-heal (restage from the still-open handle) is not: without
		// a live fd the genuine inode cannot be restaged, so the post-publish
		// identity break is a typed refusal-only posture on Windows.
		require.ErrorIs(t, err, errStagedInputSubstituted,
			"the closed-handle window substitution is the typed substitution refusal")
		require.Equal(t, "w48 window plant", string(mustReadW48(t, dest)),
			"the kernel-moved window plant is preserved at the destination name, byte-intact and never claimed as ours")
		require.Equal(t, "w48 validation-genuine", string(mustReadW48(t, staged+".w48-away")),
			"the validated object rode out the attack under the attacker's chosen name")
		require.Empty(t, ledger.released, "the create leg journalized nothing — no entry to retract")
		require.True(t, w48Closed(prov.handle))
		return
	}

	require.NotErrorIs(t, err, errStagedInputSubstituted,
		"the reclassified plant left the staged name VANISHED when the replace leg re-bound — the documented indeterminate error, not a proven-substitution reading of the plant")
	require.Contains(t, err.Error(), "vanished before the publish bind")

	require.Equal(t, "w48 window plant", string(mustReadW48(t, dest)),
		"the kernel-moved window plant is preserved at the destination name, byte-intact and never claimed as ours")
	require.Equal(t, "w48 validation-genuine", string(mustReadW48(t, staged+".w48-away")),
		"the validated object rode out the attack under the attacker's chosen name")
	require.Len(t, ledger.released, 1, "the armed journal entry was retracted with the rollback restore")
	_, berr := os.Lstat(ledger.released[0])
	require.True(t, os.IsNotExist(berr), "the set-aside was consumed by the restore-aside")
	require.True(t, w48Closed(prov.handle))
}

// Finding 6, the verify→publish window on the replace path: the kernel moves
// the plant onto the destination (the pre-existing bytes were already set
// aside and journaled), the post-publish reverify catches the substitution,
// and the POSIX restage-from-handle republishes the GENUINE bytes over the
// plant inside the bounded budget — the destination provably ends holding the
// validated object, self-healed, with the armed journal never disturbed.
func TestInstallOverwritingW48_MidPublishSubstitutionOnReplaceRepublishesGenuine(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, dest, prov := w48Stage(t, fs, dir, "w48 replace genuine bytes")
	require.NoError(t, os.WriteFile(dest, []byte("old poster bytes"), 0o644))

	attacked := false
	prev := publishStagedBoundFn
	publishStagedBoundFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		wedged := p.Publish
		p.Publish = func(fsys afero.Fs, src, dst string) error {
			if !attacked {
				attacked = true
				require.NoError(t, os.Rename(src, src+".w48-away"))
				require.NoError(t, os.WriteFile(src, []byte("w48 replace window plant"), 0o600))
			}
			return wedged(fsys, src, dst)
		}
		return prev(p)
	}
	t.Cleanup(func() { publishStagedBoundFn = prev })

	ledger := &w45Ledger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w48-replace-window", recorder: ledger}, prov)
	require.True(t, attacked)
	require.False(t, skipped)

	if runtime.GOOS == "windows" {
		// Platform-limited posture: the validated handle is CLOSED before the
		// path publish (MoveFileEx cannot act on an open handle), so the
		// POSIX restage-from-handle self-heal is inexpressible — the
		// post-publish identity break is the typed substitution refusal, and
		// it rides the unchanged publish-failure compensation: the plant
		// occupies the destination when the no-replace restore runs, so the
		// rollback is REFUSED (foreign bytes never clobbered), the journal
		// entry stays armed against the retained backup, and a later
		// sweep/revert recovers the original bytes. What matters — never
		// reported as success, attacker bytes preserved but never claimed,
		// pre-existing bytes recoverable — holds identically.
		require.Error(t, err)
		require.ErrorIs(t, err, errStagedInputSubstituted)
		require.True(t, replaced)
		require.Equal(t, "w48 replace window plant", string(mustReadW48(t, dest)),
			"the window plant is preserved at the destination byte-intact — the refused rollback never clobbers it")
		require.Equal(t, 1, ledger.records)
		require.Equal(t, 0, ledger.confirmed, "no success was ever confirmed")
		require.Empty(t, ledger.released, "the armed entry stays armed against the retained backup")
		require.True(t, w48Closed(prov.handle))
		var backupBodies []string
		entries, derr := os.ReadDir(dir)
		require.NoError(t, derr)
		awayFound := false
		for _, e := range entries {
			body, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if strings.Contains(e.Name(), ".dlbak.") {
				backupBodies = append(backupBodies, string(body))
			}
			if e.Name() == filepath.Base(staged)+".w48-away" {
				awayFound = true
				require.Equal(t, "w48 replace genuine bytes", string(body),
					"the validated object survived under the attacker's chosen name")
			}
		}
		require.Equal(t, []string{"old poster bytes"}, backupBodies,
			"the retained backup keeps the pre-existing bytes recoverable")
		require.True(t, awayFound)
		return
	}

	require.NoError(t, err, "the restage loop republished the validated object through the window attack")
	require.True(t, replaced)
	require.Equal(t, "w48 replace genuine bytes", string(mustReadW48(t, dest)),
		"the destination provably holds the validated object — the reverify caught the plant and the restage republished ours")
	require.Equal(t, 1, ledger.confirmed)
	require.True(t, w48Closed(prov.handle))
	entries, derr := os.ReadDir(dir)
	require.NoError(t, derr)
	for _, e := range entries {
		body, _ := os.ReadFile(filepath.Join(dir, e.Name()))
		require.NotContains(t, string(body), "w48 replace window plant", "no surviving directory entry carries attacker bytes")
	}
}

// w48BoundClassFs is no filesystem at all — the publishStagedBoundFn seam is
// replaced wholesale to hand back each fsutil bound-publish refusal class
// with the handle consumed exactly like production fsutil (it always closes).
func wedgeBoundPublishClassW48(t *testing.T, class error) {
	t.Helper()
	prev := publishStagedBoundFn
	publishStagedBoundFn = func(p fsutil.StagedPublish) (os.FileInfo, error) {
		if p.Handle != nil {
			_ = p.Handle.Close()
		}
		return nil, class
	}
	t.Cleanup(func() { publishStagedBoundFn = prev })
}

// Finding 6, refusal-class routing: the fsutil bound-publish classes whose
// staged name is UNPROVEN (verify) or whose destination could not be proven
// to name the validated object (identity-break family) surface through the
// typed substitution refusal on BOTH publish legs — the substitute/name
// occupant is preserved byte-intact, the destination is published-then-
// restored exactly like a publish failure, and the journal entry is
// retracted. Ordinary/complete publish failures ride through verbatim.
func TestInstallOverwritingW48_BoundPublishRefusalClassRouting(t *testing.T) {
	verify := fmt.Errorf("w48: %w: planted", fsutil.ErrPublishStagedVerify)
	identityBreak := fmt.Errorf("w48: %w", fsutil.ErrPublishStagedIdentityBreak)
	exhausted := fmt.Errorf("w48: %w: %w", fsutil.ErrPublishStagedExhausted, fsutil.ErrPublishStagedIdentityBreak)
	completed := fmt.Errorf("w48: %w: linked but unproven", fsutil.ErrPublishCompleted)

	t.Run("create leg classes", func(t *testing.T) {
		for _, tc := range []struct {
			name        string
			class       error
			substituted bool
		}{
			{"staged verify failure", verify, true},
			{"post-publish identity break", identityBreak, true},
			{"republish budget exhausted", exhausted, true},
			{"completed-carrying publish error", completed, false},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fs := afero.NewMemMapFs()
				staged, dest, prov := w48StageMem(t, fs, "/w48c/"+tc.name, "AAA")
				wedgeBoundPublishClassW48(t, tc.class)

				d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
				skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{}, prov)
				require.Error(t, err)
				require.False(t, skipped)
				require.False(t, replaced)
				if tc.substituted {
					require.ErrorIs(t, err, errStagedInputSubstituted)
					require.Equal(t, "AAA", string(mustReadDownloaderW7(t, fs, staged)),
						"the unproven staged name is preserved byte-intact, never unlinked")
				} else {
					require.NotErrorIs(t, err, errStagedInputSubstituted, "non-refusal classes ride through verbatim")
					require.ErrorIs(t, err, fsutil.ErrPublishCompleted)
				}
				exists, derr := afero.Exists(fs, dest)
				require.NoError(t, derr)
				require.False(t, exists)
			})
		}
	})

	t.Run("replace leg classes ride the publish-failure compensation", func(t *testing.T) {
		for _, tc := range []struct {
			name  string
			class error
		}{
			{"staged verify failure", verify},
			{"post-publish identity break", identityBreak},
		} {
			t.Run(tc.name, func(t *testing.T) {
				fs := afero.NewMemMapFs()
				staged, dest, prov := w48StageMem(t, fs, "/w48r/"+tc.name, "NEW")
				require.NoError(t, afero.WriteFile(fs, dest, []byte("old poster bytes"), 0o644))
				wedgeBoundPublishClassW48(t, tc.class)

				ledger := &w45Ledger{}
				d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
				skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
					downloadLedger{opID: "w48-" + tc.name, recorder: ledger}, prov)
				require.Error(t, err)
				require.ErrorIs(t, err, errStagedInputSubstituted)
				require.False(t, skipped)
				require.True(t, replaced)
				require.Equal(t, "old poster bytes", string(mustReadDownloaderW7(t, fs, dest)),
					"the set-aside restore returned the pre-existing destination bytes")
				require.Len(t, ledger.released, 1, "the armed journal entry was retracted with the rollback")
				require.Equal(t, "NEW", string(mustReadDownloaderW7(t, fs, staged)),
					"the unproven staged name is preserved byte-intact")
			})
		}
	})
}

// w48StageMem mirrors w48Stage onto MemMapFs (virtual leg: no dev/inode, the
// identity record carries size+mtime; the bound publish closes the mem handle
// before the path publish).
func w48StageMem(t *testing.T, fs afero.Fs, dir, stagedBody string) (staged, dest string, prov stagedInstallProvenance) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	dest = dir + "/poster.jpg"
	staged = dir + "/.staged-download"
	require.NoError(t, afero.WriteFile(fs, staged, []byte(stagedBody), 0o644))
	fh, err := fs.Open(staged)
	require.NoError(t, err)
	info, err := fh.Stat()
	require.NoError(t, err)
	return staged, dest, stagedInstallProvenance{identity: installedIdentityFromFileInfo(info), handle: fh}
}

// Finding 6, the re-bind discipline (codex: "re-open no-follow +
// fstat==validated record + only that handle is used").
func TestBindStagedProvenanceHandleW48(t *testing.T) {
	t.Run("fresh fd equals the validation record (OsFs dev/ino included)", func(t *testing.T) {
		fs := afero.NewOsFs()
		staged, _, prov := w48Stage(t, fs, t.TempDir(), "w48-bind-me")
		require.NoError(t, prov.handle.Close())

		bound, err := bindStagedProvenanceHandle(fs, staged, prov.identity)
		require.NoError(t, err)
		defer func() { _ = bound.Close() }()
		info, serr := bound.Stat()
		require.NoError(t, serr)
		require.Equal(t, prov.identity.size, info.Size())
		if prov.identity.hasDevIno {
			dev, ino, ok := restoreSourceIdentity(info)
			require.True(t, ok)
			require.Equal(t, prov.identity.dev, dev)
			require.Equal(t, prov.identity.ino, ino)
		}
	})

	t.Run("vanished name is indeterminate, never the substitution sentinel", func(t *testing.T) {
		fs := afero.NewOsFs()
		staged, _, prov := w48Stage(t, fs, t.TempDir(), "w48-bind")
		require.NoError(t, prov.handle.Close())
		require.NoError(t, os.Remove(staged))

		bound, err := bindStagedProvenanceHandle(fs, staged, prov.identity)
		require.Nil(t, bound)
		require.Error(t, err)
		require.Contains(t, err.Error(), "vanished before the publish bind")
		require.ErrorIs(t, err, os.ErrNotExist)
		require.NotErrorIs(t, err, errStagedInputSubstituted)
	})

	t.Run("no-follow open refusal is the proven-substitution posture", func(t *testing.T) {
		fs := afero.NewOsFs()
		staged, _, prov := w48Stage(t, fs, t.TempDir(), "w48-bind")
		require.NoError(t, prov.handle.Close())
		require.NoError(t, os.Remove(staged))
		require.NoError(t, os.Mkdir(staged, 0o755))
		defer func() { _ = os.Remove(staged) }()

		bound, err := bindStagedProvenanceHandle(fs, staged, prov.identity)
		require.Nil(t, bound)
		require.ErrorIs(t, err, errStagedInputSubstituted, "an openable-but-wrong-shaped occupant can never serve as the byte source")
	})

	t.Run("fd fstat≠record refuses and preserves the substitute", func(t *testing.T) {
		fs := afero.NewOsFs()
		dir := t.TempDir()
		staged, _, prov := w48Stage(t, fs, dir, "VALID")
		require.NoError(t, prov.handle.Close())
		require.NoError(t, os.Rename(staged, staged+".hidden"))
		require.NoError(t, os.WriteFile(staged, []byte("VALID-but-longer-substitute"), 0o600))

		bound, err := bindStagedProvenanceHandle(fs, staged, prov.identity)
		require.Nil(t, bound)
		require.ErrorIs(t, err, errStagedInputSubstituted)
		require.Equal(t, "VALID-but-longer-substitute", string(mustReadW48(t, staged)))
	})

	t.Run("open failure is refuse-closed, never a bare fallback", func(t *testing.T) {
		fs := w48OpenFileFailFs{Fs: afero.NewMemMapFs(), err: errors.New("w48 open wedge")}
		require.NoError(t, afero.WriteFile(fs, "/staged", []byte("AAA"), 0o644))
		rec := stagedInstallProvenance{identity: captureInstalledDestIdentity(fs, "/staged")}

		bound, err := bindStagedProvenanceHandle(fs, "/staged", rec.identity)
		require.Nil(t, bound)
		require.ErrorIs(t, err, errStagedInputSubstituted)
		require.Contains(t, err.Error(), "refused the no-follow publish bind open")
	})

	t.Run("fd fstat failure is refuse-closed", func(t *testing.T) {
		fs := w48StatFailOpenFileFs{Fs: afero.NewMemMapFs()}
		require.NoError(t, afero.WriteFile(fs, "/staged", []byte("AAA"), 0o644))
		rec := stagedInstallProvenance{identity: captureInstalledDestIdentity(fs, "/staged")}

		bound, err := bindStagedProvenanceHandle(fs, "/staged", rec.identity)
		require.Nil(t, bound)
		require.ErrorIs(t, err, errStagedInputSubstituted)
		require.Contains(t, err.Error(), "failed the publish bind fstat")
	})
}

// w48OpenFileFailFs fails OpenFile (the no-follow re-open fault).
type w48OpenFileFailFs struct {
	afero.Fs
	err error
}

func (f w48OpenFileFailFs) OpenFile(name string, flags int, perm os.FileMode) (afero.File, error) {
	if flags&os.O_WRONLY == 0 && flags&os.O_RDWR == 0 && flags&os.O_CREATE == 0 {
		return nil, f.err
	}
	return f.Fs.OpenFile(name, flags, perm)
}

// w48StatFailOpenFileFs answers OpenFile with a descriptor whose Stat fails
// (the re-opened fd cannot even prove its own identity).
type w48StatFailOpenFileFs struct{ afero.Fs }

func (f w48StatFailOpenFileFs) OpenFile(name string, flags int, perm os.FileMode) (afero.File, error) {
	base, err := f.Fs.OpenFile(name, flags, perm)
	if err != nil {
		return nil, err
	}
	return w48StatFailFile{File: base}, nil
}

type w48StatFailFile struct{ afero.File }

func (w48StatFailFile) Stat() (os.FileInfo, error) { return nil, errors.New("w48 fd stat wedge") }

// w57LstatFailFs wedges the path-identity capture (LstatIfPossible) while
// leaving OpenFile intact, so the no-follow re-open still succeeds against an
// existing candidate. It isolates bindCandidateProvenance's SECOND both-fail
// leg — Lstat fails AND the re-opened handle's fstat fails (oerr == nil,
// serr != nil, identity unknown) — from the first leg, where an absent name
// fails BOTH Lstat and the open. Composed over w48StatFailOpenFileFs the
// opened fd's Stat wedges, exercising the wave-57 serr != nil refusal.
type w57LstatFailFs struct{ afero.Fs }

func (w57LstatFailFs) LstatIfPossible(string) (os.FileInfo, bool, error) {
	return nil, false, errors.New("w57 lstat wedge")
}

// w54SubstFs wraps an OpenFile result so the descriptor's Stat reports a
// FOREIGN size (wave-54, finding 2): Lstat captures the real object, the
// no-follow open + fstat diverges — a racer substituted the candidate inside
// the Lstat→open window. bindCandidateProvenance must refuse typed.
type w54SubstFs struct{ afero.Fs }

func (f w54SubstFs) OpenFile(name string, flags int, perm os.FileMode) (afero.File, error) {
	base, err := f.Fs.OpenFile(name, flags, perm)
	if err != nil {
		return nil, err
	}
	return w54SubstFile{File: base}, nil
}

type w54SubstFile struct{ afero.File }

type w54SubstInfo struct{ os.FileInfo }

func (w54SubstInfo) Size() int64 { return 9999 } // foreign size → fstat ≠ Lstat snapshot

func (f w54SubstFile) Stat() (os.FileInfo, error) {
	info, err := f.File.Stat()
	if err != nil {
		return nil, err
	}
	return w54SubstInfo{FileInfo: info}, nil
}

// Finding 6, media leg: downloadPoster's candidate binding hands an open fd
// when the crop/write output opens cleanly, and degrades to the recorded-only
// wave-47 posture (never a failure) when it does not.
func TestBindCandidateProvenanceW48(t *testing.T) {
	t.Run("candidate binds to its own open fd", func(t *testing.T) {
		fs := afero.NewOsFs()
		dir := t.TempDir()
		candidate := filepath.Join(dir, "poster.jpg.crop.tmp")
		require.NoError(t, os.WriteFile(candidate, []byte("cropped candidate"), 0o644))

		prov, err := bindCandidateProvenance(fs, candidate, installedDestIdentity{})
		require.NoError(t, err)
		require.True(t, prov.identity.known)
		require.NotNil(t, prov.handle, "the candidate's fd rides into the install bound end to end")
		defer func() { _ = prov.handle.Close() }()
		got, rerr := prov.handle.Stat()
		require.NoError(t, rerr)
		require.EqualValues(t, len("cropped candidate"), got.Size())
	})

	t.Run("re-open failure degrades to the recorded-only posture", func(t *testing.T) {
		fs := w48OpenFileFailFs{Fs: afero.NewMemMapFs(), err: errors.New("w48 candidate open wedge")}
		require.NoError(t, afero.WriteFile(fs, "/candidate", []byte("cropped"), 0o644))

		prov, err := bindCandidateProvenance(fs, "/candidate", installedDestIdentity{})
		require.NoError(t, err, "a Lstat-known candidate with a failed re-open degrades, never fails closed")
		require.Nil(t, prov.handle)
		require.True(t, prov.identity.known, "the wave-47 post-write capture is still handed down")
		require.EqualValues(t, len("cropped"), prov.identity.size)
	})

	t.Run("fd stat failure degrades to the recorded-only posture", func(t *testing.T) {
		fs := w48StatFailOpenFileFs{Fs: afero.NewMemMapFs()}
		require.NoError(t, afero.WriteFile(fs, "/candidate", []byte("cropped"), 0o644))

		prov, err := bindCandidateProvenance(fs, "/candidate", installedDestIdentity{})
		require.NoError(t, err, "a Lstat-known candidate with a failed fstat degrades, never fails closed")
		require.Nil(t, prov.handle)
		require.True(t, prov.identity.known)
	})

	// Wave-53 (codex P3, PR#215 finding 3): when BOTH the path identity capture
	// and the no-follow re-open fail, the candidate is completely unprobeable —
	// fail CLOSED (typed refusal, nothing recorded or touched) instead of
	// degrading to an unauthenticated path-only publish.
	t.Run("both capture and re-open fail refuses closed", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		// No file at /candidate: captureInstalledDestIdentity (Lstat) returns
		// not-exist (known=false) AND restoreOpenReplacementSource (the no-follow
		// re-open) returns not-exist — both fail, so the candidate is unprobeable.
		prov, err := bindCandidateProvenance(fs, "/candidate", installedDestIdentity{})
		require.Error(t, err)
		require.ErrorIs(t, err, errCandidateProvenanceUnprobeable)
		require.False(t, prov.identity.known, "nothing verifiable is handed down on the both-fail refusal")
		require.Nil(t, prov.handle, "no handle is opened on the both-fail refusal")
	})

	// Wave-57 (codex P2): Lstat fails AND the fd fstat fails — neither the
	// first snapshot nor the handle yields an identity, so a path-only publish
	// is refused closed (errCandidateProvenanceUnprobeable), with nothing
	// recorded or touched.
	t.Run("unprobeable fstat on a missing candidate refuses closed", func(t *testing.T) {
		fs := w48StatFailOpenFileFs{Fs: afero.NewMemMapFs()}
		// Absent on the underlying FS: capture cannot prove identity; the
		// open succeeds through the fake but its Stat wedged.
		prov, err := bindCandidateProvenance(fs, "/candidate-w57", installedDestIdentity{})
		require.ErrorIs(t, err, errCandidateProvenanceUnprobeable)
		require.False(t, prov.identity.known)
		require.Nil(t, prov.handle)
	})

	// Wave-57 (codex P2): the SECOND both-fail leg — Lstat fails AND the
	// no-follow re-open SUCCEEDS but its fstat wedges. The opened handle yields
	// no identity either (serr != nil, identity unknown), so a pathname-only
	// publish is refused closed (errCandidateProvenanceUnprobeable). Unlike the
	// "missing candidate" case above — where the absent name fails BOTH Lstat
	// and the open — here the candidate IS openable: only its path identity and
	// the fd's own fstat are unprobeable, so the leg guarded by
	// `if serr != nil { ... if !provenance.identity.known` is actually reached.
	t.Run("unprobeable fstat on an openable candidate refuses closed", func(t *testing.T) {
		fs := w57LstatFailFs{Fs: w48StatFailOpenFileFs{Fs: afero.NewMemMapFs()}}
		// The candidate EXISTS on the underlying FS so the no-follow re-open
		// succeeds; w57LstatFailFs wedges the Lstat gate (identity unknown),
		// and w48StatFailFile wedges the opened fd's Stat.
		require.NoError(t, afero.WriteFile(fs, "/candidate-w57", []byte("cropped"), 0o644))

		prov, err := bindCandidateProvenance(fs, "/candidate-w57", installedDestIdentity{})
		require.ErrorIs(t, err, errCandidateProvenanceUnprobeable)
		require.False(t, prov.identity.known, "Lstat wedged → no path identity handed down")
		require.Nil(t, prov.handle, "the wedged fd is closed on the refusal")
	})

	// Wave-54 (codex P2): the no-follow open + fstat MUST
	// equal the 1st Lstat snapshot — a racer substituting the candidate before
	// the open publishes the substitute. A fstat that diverges (foreign size)
	// is refused typed; the substitute is preserved, nothing installed.
	t.Run("fstat diverges from the Lstat snapshot refuses typed", func(t *testing.T) {
		fs := w54SubstFs{Fs: afero.NewMemMapFs()}
		require.NoError(t, afero.WriteFile(fs, "/candidate", []byte("cropped"), 0o644))
		prov, err := bindCandidateProvenance(fs, "/candidate", installedDestIdentity{})
		require.ErrorIs(t, err, errStagedInputSubstituted)
		require.False(t, prov.identity.known, "nothing verifiable is handed down on the substitution refusal")
		require.Nil(t, prov.handle)
	})
}

// stagedPublishVerdict's direct arms — classification of every bound-publish
// refusal family onto the substitution sentinel, with completed-carrying and
// ordinary publish errors riding through verbatim.
func TestStagedPublishVerdictW48(t *testing.T) {
	require.NoError(t, stagedPublishVerdict(nil))

	for _, class := range []error{
		fsutil.ErrPublishStagedVerify,
		fsutil.ErrPublishStagedIdentityBreak,
		fsutil.ErrPublishStagedExhausted,
		fsutil.ErrPublishStagedForeignOccupant,
		fsutil.ErrPublishStagedIdentityIndeterminate,
	} {
		err := stagedPublishVerdict(fmt.Errorf("w48: %w", class))
		require.Error(t, err, "%v", class)
		require.ErrorIs(t, err, errStagedInputSubstituted, "%v must surface through the substitution refusal", class)
	}

	require.NoError(t, stagedPublishVerdict(fmt.Errorf("w48: %w", fsutil.ErrPublishCompleted)),
		"completed-carrying errors keep the wave-41 caller legs")
	require.NoError(t, stagedPublishVerdict(errors.New("w48 ordinary failure")),
		"ordinary publish failures ride to the caller's own classification")
	require.NoError(t, stagedPublishVerdict(fmt.Errorf("w48: %w", fsutil.ErrPublishCollision)),
		"collisions are the caller's reclassification loop, never a substitution refusal")
}

// Finding 6, replace-leg indeterminate binding: the staged name vanishing
// between validation and the replace publish is NOT a proven substitution —
// the bind's own not-exist error rides the publish-failure compensation (set-
// aside restore + journal retract), the destination's pre-existing bytes are
// restored, and nothing is published.
func TestInstallOverwritingW48_ReplaceBindVanishedRidesCompensation(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, dest, prov := w48Stage(t, fs, dir, "w48-vanish-me")
	require.NoError(t, prov.handle.Close())
	prov.handle = nil
	require.NoError(t, os.WriteFile(dest, []byte("old poster bytes"), 0o644))
	require.NoError(t, os.Remove(staged))

	ledger := &w45Ledger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w48-vanish", recorder: ledger}, prov)
	require.Error(t, err)
	require.NotErrorIs(t, err, errStagedInputSubstituted)
	require.Contains(t, err.Error(), "vanished before the publish bind")
	require.False(t, skipped)
	require.True(t, replaced)
	require.Equal(t, "old poster bytes", string(mustReadW48(t, dest)),
		"the compensation restored the pre-existing destination bytes")
	require.Len(t, ledger.released, 1, "the armed journal entry was retracted")
}

// Finding 6, end-to-end REPLACE leg through http.download: the wave-45
// end-to-end covered the create leg; here the destination pre-exists, the
// w45SwapFS window hook replays the validation→install rotation on the first
// provenance lookup, and the refusal must route through the replace
// compensation — pre-existing destination bytes restored, armed journal entry
// retracted with its backup consumed, foreign substitute preserved at the
// retained temp name, validated object recoverable at the attacker's name.
func TestDownloadW48_ReplaceWindowSubstitutionRidesCompensation(t *testing.T) {
	payload := []byte("\xff\xd8\xff\xe0 genuine downloaded jpeg bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "poster.jpg")
	require.NoError(t, os.WriteFile(dest, []byte("pre-existing poster bytes"), 0o644))
	fsys := &w45SwapFS{Fs: afero.NewOsFs()}
	ledger := &w45Ledger{}
	d := NewDownloader(http.DefaultClient, fsys, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	result, err := d.download(context.Background(), server.URL+"/poster.jpg", dest, MediaTypePoster, true, nil,
		downloadLedger{opID: "w48-http-replace", recorder: ledger})
	require.Error(t, err)
	require.NotNil(t, result)
	require.ErrorIs(t, result.Error, errStagedInputSubstituted)
	require.False(t, result.Downloaded)

	require.Equal(t, "pre-existing poster bytes", string(mustReadW48(t, dest)),
		"the set-aside restore returned the pre-existing destination bytes")
	require.Len(t, ledger.released, 1, "the armed journal entry was retracted with the rollback")
	_, berr := os.Lstat(ledger.released[0])
	require.True(t, os.IsNotExist(berr), "the set-aside was consumed by the restore-aside")

	entries, rerr := os.ReadDir(tmpDir)
	require.NoError(t, rerr)
	var tempNames, hiddenNames []string
	for _, e := range entries {
		switch {
		case strings.HasSuffix(e.Name(), ".tmp"):
			tempNames = append(tempNames, e.Name())
		case strings.HasSuffix(e.Name(), ".tmp.hidden"):
			hiddenNames = append(hiddenNames, e.Name())
		}
	}
	require.Len(t, tempNames, 1, "the retained staged name keeps the preserved substitute")
	substitute, serr := os.ReadFile(filepath.Join(tmpDir, tempNames[0]))
	require.NoError(t, serr)
	require.Equal(t, "planted substitute payload", string(substitute))
	if runtime.GOOS == "windows" {
		// Windows' expressible substitution was the in-place rewrite (see
		// w45SwapFS): the rename-aside evidence file never existed, and the
		// identity binding was still checked before any byte flow — the
		// platform-limited "handle cannot be held across mutations" posture.
		require.Empty(t, hiddenNames, "the rename-away race is inexpressible under a held handle on Windows")
	} else {
		require.Len(t, hiddenNames, 1)
		validated, verr := os.ReadFile(filepath.Join(tmpDir, hiddenNames[0]))
		require.NoError(t, verr)
		require.Equal(t, payload, validated, "the validated object rode out the refusal untouched")
	}
}

// Finding 6, media leg: the cropped candidate's no-follow fd rides the bound
// publish end to end on the REAL OsFs — w47's hook tests pin the recorded-
// only posture on MemMapFs; here the genuine OS descriptor proves the
// candidate at publish adjacency and is consumed by the publish.
func TestDownloadPosterW48_OsFsCroppedCandidateBoundInstall(t *testing.T) {
	server := serveTwoToneSource(t)

	fs := afero.NewOsFs()
	destDir := t.TempDir()
	movie := w42CropMovie("W48-OSFS", server.URL+"/cover.jpg")
	d := NewDownloader(server.Client(), fs, w42CropPosterConfig(), nil)
	dest := d.pathResolver.ResolvePosterPath(movie, nil, true, d.buildTemplateContext(movie, nil), destDir)

	result, err := d.downloadPoster(context.Background(), movie, destDir, nil, true, nil, downloadLedger{})
	require.NoError(t, err)
	require.True(t, result.Downloaded)
	require.Equal(t, dest, result.LocalPath)

	_, w, h := decodeResultPoster(t, fs, dest)
	require.InDelta(t, 472, w, 3, "the bound install published the cropped candidate")
	require.Equal(t, 600, h)

	entries, rerr := os.ReadDir(destDir)
	require.NoError(t, rerr)
	require.Len(t, entries, 1, "both scratch names reaped — no held handle wedged a cleanup")
	require.Equal(t, filepath.Base(dest), entries[0].Name())
}

// The symlink occupant at the staged name during the re-bind (OsFs): the
// no-follow open refuses the link object itself, which IS the proven
// substitution — routed to the typed sentinel with the link preserved.
func TestBindStagedProvenanceHandleW48_SymlinkOccupantRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged, _, prov := w48Stage(t, fs, dir, "VALID")
	require.NoError(t, prov.handle.Close())
	require.NoError(t, os.Remove(staged))
	target := filepath.Join(dir, "foreign-target")
	require.NoError(t, os.WriteFile(target, []byte("foreign bytes"), 0o644))
	if err := os.Symlink(target, staged); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}

	bound, err := bindStagedProvenanceHandle(fs, staged, prov.identity)
	require.Nil(t, bound)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	linkInfo, lerr := os.Lstat(staged)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "the planted link stays byte-intact — never followed, never unlinked")
}
