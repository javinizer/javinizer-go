package downloader

// POSTER-WRITE-HARDENING wave-45 (codex P2, PR#215 finding F1) — the
// validated download is bound through the publish: validateDownloadedMedia
// hands its open-handle identity record down as the install provenance, and
// installOverwriting re-proves the staged name against it before every byte
// flow into the destination (create path no-replace publishes AND the replace
// path's wave-26 baseline capture). A substitute rotated onto the staged name
// between validation and install is refused with errStagedInputSubstituted,
// preserved byte-intact, and the destination is never touched.

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
)

var errW45Stat = errors.New("w45 staged handle stat failure")

// w45StatFailFile fails Stat (fault injection for validateDownloadedMedia's
// identity-capture leg).
type w45StatFailFile struct{ afero.File }

func (w45StatFailFile) Stat() (os.FileInfo, error) { return nil, errW45Stat }

type w45StatFailFS struct{ afero.Fs }

func (f w45StatFailFS) Open(name string) (afero.File, error) {
	base, err := f.Fs.Open(name)
	if err != nil {
		return nil, err
	}
	return w45StatFailFile{File: base}, nil
}

// w45Ledger is the minimal armed-ledger recorder the provenance replace tests
// drive (same shape as the wave-25 recording ledger).
type w45Ledger struct {
	mu        sync.Mutex
	records   int
	released  []string
	confirmed int
}

func (l *w45Ledger) RecordReplacement(context.Context, string, string, string, ...models.ReplacementBackupFacts) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.records++
	return nil
}

func (l *w45Ledger) ConfirmReplacement(context.Context, string, string, string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.confirmed++
	return nil
}

func (l *w45Ledger) ReleaseReplacement(_ context.Context, _, _, backup string) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.released = append(l.released, backup)
	return nil
}

func (l *w45Ledger) MarkReplacementRestorePendingKind(context.Context, string, string, string, string) error {
	return nil
}

// w45Fixture stages one download input and returns its validation-time
// provenance bundle exactly the way http.download captures it (through the
// open handle's Stat → installedIdentityFromFileInfo). The wave-45 recorded-
// only posture is kept here — no retained handle — so these tests continue to
// pin the snapshot-gate behavior; wave-48's handle-bound legs live in
// install_overwrite_bound_w48_test.go.
func w45Fixture(t *testing.T, fs afero.Fs, dir, stagedBody string) (staged, dest string, provenance stagedInstallProvenance) {
	t.Helper()
	require.NoError(t, fs.MkdirAll(dir, 0o755))
	dest = dir + "/poster.jpg"
	staged = dir + "/.staged-download"
	require.NoError(t, afero.WriteFile(fs, staged, []byte(stagedBody), 0o644))
	fh, err := fs.Open(staged)
	require.NoError(t, err)
	info, err := fh.Stat()
	require.NoError(t, err)
	require.NoError(t, fh.Close())
	return staged, dest, stagedInstallProvenance{identity: installedIdentityFromFileInfo(info)}
}

// Finding F1, create path: a substitute rotated onto the staged name between
// validation and install is refused; the substitute is preserved byte-intact
// and the destination is never stored.
func TestInstallOverwritingW45_CreatePathRefusesSubstitutedStagedInput(t *testing.T) {
	fs := afero.NewMemMapFs()
	staged, dest, provenance := w45Fixture(t, fs, "/w45/create", "AAA")

	// The validation→install window swap: the validated object goes away and a
	// foreign substitute takes the staged name.
	require.NoError(t, fs.Remove(staged))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("BBBB-foreign-substitute"), 0o644))

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{}, provenance)
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.False(t, skipped)
	require.False(t, replaced)

	kept, rerr := afero.ReadFile(fs, staged)
	require.NoError(t, rerr)
	require.Equal(t, "BBBB-foreign-substitute", string(kept), "the foreign substitute is foreign bytes — never unlinked")
	exists, derr := afero.Exists(fs, dest)
	require.NoError(t, derr)
	require.False(t, exists, "the destination must not store the substitute")
}

// Finding F1, create path: an unproven (vanished) staged lookup stays the
// legacy indeterminate posture — the publish's own not-exist error surfaces,
// never the substitution sentinel.
func TestInstallOverwritingW45_VanishedStagedInputIsNotASubstitution(t *testing.T) {
	fs := afero.NewMemMapFs()
	staged, dest, provenance := w45Fixture(t, fs, "/w45/vanish", "AAA")
	require.NoError(t, fs.Remove(staged))

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, _, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{}, provenance)
	require.Error(t, err)
	require.NotErrorIs(t, err, errStagedInputSubstituted, "a lookup failure is indeterminate, not a proven substitution")
}

// Finding F1: the unmodified validated object installs normally with the
// provenance armed (match leg on the create path).
func TestInstallOverwritingW45_ValidatedStagedInputInstalls(t *testing.T) {
	fs := afero.NewMemMapFs()
	staged, dest, provenance := w45Fixture(t, fs, "/w45/match", "CCC")

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest, downloadLedger{}, provenance)
	require.NoError(t, err)
	require.False(t, skipped)
	require.False(t, replaced)
	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	require.Equal(t, "CCC", string(got))
}

// Finding F1, replace path: the wave-26 baseline must capture THE VALIDATED
// object — a substitute is refused BEFORE the publish and rides the
// publish-failure compensation (set-aside restore + journal retract), leaving
// the destination's pre-existing bytes in place and the substitute intact.
func TestInstallOverwritingW45_ReplacePathRefusesSubstitutedStagedInput(t *testing.T) {
	fs := afero.NewMemMapFs()
	staged, dest, provenance := w45Fixture(t, fs, "/w45/replace", "NEW-VALID")
	require.NoError(t, afero.WriteFile(fs, dest, []byte("old poster bytes"), 0o644))

	require.NoError(t, fs.Remove(staged))
	require.NoError(t, afero.WriteFile(fs, staged, []byte("SUBSTITUTE!!!bigger"), 0o644))

	ledger := &w45Ledger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w45-swap", recorder: ledger}, provenance)
	require.Error(t, err)
	require.ErrorIs(t, err, errStagedInputSubstituted)
	require.False(t, skipped)
	require.True(t, replaced)

	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	require.Equal(t, "old poster bytes", string(got), "the rollback restore returned the pre-existing bytes")
	kept, serr := afero.ReadFile(fs, staged)
	require.NoError(t, serr)
	require.Equal(t, "SUBSTITUTE!!!bigger", string(kept), "the substitute is preserved")
	require.Len(t, ledger.released, 1, "the armed journal entry was retracted with the rollback")
	backupExists, berr := afero.Exists(fs, ledger.released[0])
	require.NoError(t, berr)
	require.False(t, backupExists, "the set-aside was consumed by the restore-aside")
}

// Finding F1, replace path with provenance: the unmodified validated object
// replaces normally.
func TestInstallOverwritingW45_ReplacePathValidatedInputInstalls(t *testing.T) {
	fs := afero.NewMemMapFs()
	staged, dest, provenance := w45Fixture(t, fs, "/w45/replacematch", "NEW-BYTES")
	require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), 0o644))

	ledger := &w45Ledger{}
	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{opID: "w45-ok", recorder: ledger}, provenance)
	require.NoError(t, err)
	require.False(t, skipped)
	require.True(t, replaced)
	got, rerr := afero.ReadFile(fs, dest)
	require.NoError(t, rerr)
	require.Equal(t, "NEW-BYTES", string(got))
	require.Equal(t, 1, ledger.confirmed)
}

// Finding F1, kernel-identity binding (OsFs): dev/inode provenance catches a
// rename-away substitution even before size/mtime are consulted; an
// untouched staged input still matches.
func TestInstallOverwritingW45_OsFsKernelIdentityBinding(t *testing.T) {
	fs := afero.NewOsFs()
	dir := t.TempDir()

	t.Run("substitute refused", func(t *testing.T) {
		sub := filepath.Join(dir, "refuse")
		staged := filepath.Join(sub, ".staged-download")
		dest := filepath.Join(sub, "poster.jpg")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		require.NoError(t, os.WriteFile(staged, []byte("VALID-DATA"), 0o644))
		fh, err := os.Open(staged)
		require.NoError(t, err)
		info, err := fh.Stat()
		require.NoError(t, err)
		require.NoError(t, fh.Close())
		identity := installedIdentityFromFileInfo(info)
		// restoreSourceIdentity exposes dev/inode only on the POSIX Stat_t
		// targets (restore_source_identity_other.go answers not-OK elsewhere):
		// on windows Sys() carries no unix dev/ino, so the kernel-identity
		// precondition keys on ACTUAL identity availability (the w25/w37x
		// platform-keyed posture). The substitute below diverges in SIZE too
		// ("VALID-DATA!" vs "VALID-DATA"), so the refusal further down is
		// proven on every platform — by the dev/ino binding where exposed,
		// and by the every-platform size+mtime comparator (pointed back at
		// the staged input) where it is not.
		if _, _, ok := restoreSourceIdentity(info); ok {
			require.True(t, identity.hasDevIno, "OsFs stat carries kernel identity")
		} else {
			require.False(t, identity.hasDevIno, "this platform's OsFs stat exposes no dev/ino — size+mtime carries the refusal")
		}

		require.NoError(t, os.Rename(staged, staged+".hidden"))
		require.NoError(t, os.WriteFile(staged, []byte("VALID-DATA!"), 0o600))

		d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
		_, _, ierr := d.installOverwriting(context.Background(), staged, dest, downloadLedger{}, stagedInstallProvenance{identity: identity})
		require.ErrorIs(t, ierr, errStagedInputSubstituted)
		kept, rerr := os.ReadFile(staged)
		require.NoError(t, rerr)
		require.Equal(t, "VALID-DATA!", string(kept))
		orig, herr := os.ReadFile(staged + ".hidden")
		require.NoError(t, herr)
		require.Equal(t, "VALID-DATA", string(orig))
		_, derr := os.Lstat(dest)
		require.True(t, os.IsNotExist(derr), "destination untouched")
	})

	t.Run("unmodified staged input matches", func(t *testing.T) {
		sub := filepath.Join(dir, "match")
		staged := filepath.Join(sub, ".staged-download")
		dest := filepath.Join(sub, "poster.jpg")
		require.NoError(t, os.MkdirAll(sub, 0o755))
		require.NoError(t, os.WriteFile(staged, []byte("VALID-DATA"), 0o644))
		fh, err := os.Open(staged)
		require.NoError(t, err)
		info, err := fh.Stat()
		require.NoError(t, err)
		require.NoError(t, fh.Close())

		d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
		skipped, replaced, ierr := d.installOverwriting(context.Background(), staged, dest,
			downloadLedger{}, stagedInstallProvenance{identity: installedIdentityFromFileInfo(info)})
		require.NoError(t, ierr)
		require.False(t, skipped)
		require.False(t, replaced)
		got, rerr := os.ReadFile(dest)
		require.NoError(t, rerr)
		require.Equal(t, "VALID-DATA", string(got))
	})
}

// Finding F1: a symlink planted at the staged name is a proven substitution
// (the no-follow classifier sees the link object itself), and the link is
// never followed or unlinked.
func TestInstallOverwritingW45_SymlinkOccupantRefused(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlink creation needs privileges on windows")
	}
	fs := afero.NewOsFs()
	dir := t.TempDir()
	staged := filepath.Join(dir, ".staged-download")
	dest := filepath.Join(dir, "poster.jpg")
	require.NoError(t, os.WriteFile(staged, []byte("VALID"), 0o644))
	fh, err := os.Open(staged)
	require.NoError(t, err)
	info, err := fh.Stat()
	require.NoError(t, err)
	require.NoError(t, fh.Close())
	require.NoError(t, os.Remove(staged))
	target := filepath.Join(dir, "foreign-target")
	require.NoError(t, os.WriteFile(target, []byte("foreign bytes"), 0o644))
	if serr := os.Symlink(target, staged); serr != nil {
		t.Skipf("symlink unavailable: %v", serr)
	}

	d := NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
	_, _, ierr := d.installOverwriting(context.Background(), staged, dest,
		downloadLedger{}, stagedInstallProvenance{identity: installedIdentityFromFileInfo(info)})
	require.ErrorIs(t, ierr, errStagedInputSubstituted)
	linkInfo, lerr := os.Lstat(staged)
	require.NoError(t, lerr)
	require.NotZero(t, linkInfo.Mode()&os.ModeSymlink, "the planted link object stays byte-intact")
	_, derr := os.Lstat(dest)
	require.True(t, os.IsNotExist(derr))
}

// Finding F1: the classifier's live legs.
func TestClassifyStagedInputW45(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.Equal(t, stagedInputUnrecorded, classifyStagedInput(fs, "/gone", installedDestIdentity{}),
		"no provenance record → legacy posture")

	_, _, provenance := w45Fixture(t, fs, "/w45/cls", "AAA")
	require.Equal(t, stagedInputIndeterminate, classifyStagedInput(fs, "/w45/cls/absent", provenance.identity),
		"lookup failure is indeterminate, never a proven substitution")
	require.Equal(t, stagedInputMatch, classifyStagedInput(fs, "/w45/cls/.staged-download", provenance.identity))
}

// Finding F1: validateDownloadedMedia hands back the handle-derived identity;
// a failing identity capture fails closed.
func TestValidateDownloadedMediaW45_ReturnsHandleIdentity(t *testing.T) {
	fs := afero.NewMemMapFs()
	require.NoError(t, afero.WriteFile(fs, "/tmp-dl", []byte("\xff\xd8\xff\xe0jpeg-bytes"), 0o644))

	info, handle, err := validateDownloadedMedia(fs, "/tmp-dl", "", "/out/cover.jpg")
	require.NoError(t, err)
	require.NotNil(t, info)
	require.NotNil(t, handle, "wave-48: acceptance hands the validated object's handle back OPEN")
	defer func() { _ = handle.Close() }()
	require.EqualValues(t, len("\xff\xd8\xff\xe0jpeg-bytes"), info.Size())
	provenance := installedIdentityFromFileInfo(info)
	require.True(t, provenance.known, "the validated record freezes into a usable provenance snapshot")

	_, _, err = validateDownloadedMedia(w45StatFailFS{Fs: fs}, "/tmp-dl", "", "/out/cover.jpg")
	require.ErrorContains(t, err, "failed to stat downloaded file")
	require.ErrorIs(t, err, errW45Stat)
}

// w45SwapFS interposes the validation→install window deterministically: the
// FIRST no-follow lookup of the download temp name substitutes a foreign
// payload at the name — exactly the directory-writer race finding F1 closes.
//
// Platform-limited swap shape: POSIX plays the race as rename-aside + plant
// (the validated object survives at name.hidden while a fresh inode takes
// the name). Windows can never express that shape here — the wave-48
// validated handle holds the temp name OPEN and Go opens files without
// FILE_SHARE_DELETE, so MoveFileW on it fails ("being used by another
// process"); the expressible Windows substitution is the IN-PLACE rewrite
// under the write share (same name, new bytes). Both land the identical
// refusal through the every-platform size+mtime comparator — the Windows
// identity binding is checked but the rename-away evidence shape simply
// does not exist there (the .hidden assertions below are POSIX-only).
type w45SwapFS struct {
	afero.Fs
	mu      sync.Mutex
	swapped bool
	swapErr error
}

func (f *w45SwapFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	f.mu.Lock()
	if !f.swapped && f.swapErr == nil && strings.HasSuffix(name, ".tmp") {
		if _, err := f.Fs.Stat(name); err == nil {
			if runtime.GOOS == "windows" {
				// In-place rewrite: the rename-away race is inexpressible while
				// the validated handle is open on Windows.
				if werr := afero.WriteFile(f.Fs, name, []byte("planted substitute payload"), 0o600); werr != nil {
					f.swapErr = werr
				} else {
					f.swapped = true
				}
			} else if rerr := f.Fs.Rename(name, name+".hidden"); rerr != nil {
				f.swapErr = rerr
			} else if werr := afero.WriteFile(f.Fs, name, []byte("planted substitute payload"), 0o600); werr != nil {
				f.swapErr = werr
			} else {
				f.swapped = true
			}
		}
	}
	swapErr := f.swapErr
	f.mu.Unlock()
	if ls, ok := f.Fs.(afero.Lstater); ok && swapErr == nil {
		return ls.LstatIfPossible(name)
	}
	return nil, false, errors.New("w45 swap double failed")
}

// Finding F1 end-to-end through http.download: a mid-window substitution is
// refused with the typed sentinel, the substitute is preserved byte-intact,
// and the destination is never stored.
func TestDownloadW45_SwapBetweenValidationAndInstallRefused(t *testing.T) {
	payload := []byte("\xff\xd8\xff\xe0 genuine downloaded jpeg bytes")
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/octet-stream")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(payload)
	}))
	defer server.Close()

	tmpDir := t.TempDir()
	dest := filepath.Join(tmpDir, "poster.jpg")
	fsys := &w45SwapFS{Fs: afero.NewOsFs()}
	d := NewDownloader(http.DefaultClient, fsys, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())

	result, err := d.download(context.Background(), server.URL+"/poster.jpg", dest, MediaTypePoster, true)
	require.Error(t, err)
	require.NotNil(t, result)
	require.ErrorIs(t, result.Error, errStagedInputSubstituted)
	require.False(t, result.Downloaded)

	_, derr := os.Lstat(dest)
	require.True(t, os.IsNotExist(derr), "the substitute must never land at the destination")

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
	require.Len(t, tempNames, 1, "the retained staged name is warn-left for manual cleanup")
	substitute, serr := os.ReadFile(filepath.Join(tmpDir, tempNames[0]))
	require.NoError(t, serr)
	require.Equal(t, "planted substitute payload", string(substitute),
		"http.download must NOT unlink the substitute on the substitution refusal")
	if runtime.GOOS == "windows" {
		// Windows' expressible substitution was the in-place rewrite (see
		// w45SwapFS): there is no rename-aside evidence file at all.
		require.Empty(t, hiddenNames, "the rename-away race is inexpressible under a held handle on Windows")
	} else {
		require.Len(t, hiddenNames, 1)
		validated, verr := os.ReadFile(filepath.Join(tmpDir, hiddenNames[0]))
		require.NoError(t, verr)
		require.Equal(t, payload, validated, "the validated object rode out the refusal untouched")
	}
}
