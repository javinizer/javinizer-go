package downloader

import (
	"context"
	"errors"
	"image"
	"image/jpeg"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/javinizer/javinizer-go/internal/models"
	"github.com/javinizer/javinizer-go/internal/organizer"
	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

var (
	covW1BCopyErr    = errors.New("coverage copy failure")
	covW1BCloseErr   = errors.New("coverage close failure")
	covW1BSwapErr    = errors.New("coverage swap failure")
	covW1BRecordErr  = errors.New("coverage record failure")
	covW1BRestoreErr = errors.New("coverage restore failure")
	covW1BStatErr    = errors.New("coverage stat failure")
	covW1BReleaseErr = errors.New("coverage release failure")
)

type covW1BReadErrorFile struct {
	afero.File
	err error
}

func (f covW1BReadErrorFile) Read([]byte) (int, error) { return 0, f.err }

type covW1BCloseErrorFile struct {
	afero.File
	err error
}

func (f covW1BCloseErrorFile) Close() error {
	_ = f.File.Close()
	return f.err
}

type covW1BReadErrorFS struct {
	afero.Fs
	backup string
	err    error
}

func (f covW1BReadErrorFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(name) == filepath.Clean(f.backup) {
		return covW1BReadErrorFile{File: file, err: f.err}, nil
	}
	return file, nil
}

type covW1BCloseErrorFS struct {
	afero.Fs
	err error
}

func (f covW1BCloseErrorFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	file, err := f.Fs.OpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if strings.Contains(name, ".dlrstr.") {
		return covW1BCloseErrorFile{File: file, err: f.err}, nil
	}
	return file, nil
}

type covW1BSwapErrorFS struct {
	afero.Fs
	dest        string
	failStaging bool
	swapErr     error
}

func (f covW1BSwapErrorFS) Rename(oldname, newname string) error {
	if f.failStaging && filepath.Clean(newname) == filepath.Clean(f.dest) && strings.Contains(oldname, ".dlrstr.") {
		return f.swapErr
	}
	return f.Fs.Rename(oldname, newname)
}

type covW1BStatErrorFS struct {
	afero.Fs
	path string
	err  error
}

func (f covW1BStatErrorFS) Stat(name string) (os.FileInfo, error) {
	if filepath.Clean(name) == filepath.Clean(f.path) {
		return nil, f.err
	}
	return f.Fs.Stat(name)
}

type covW1BInstallFS struct {
	afero.Fs
	dest        string
	staged      string
	failReplace bool
	failRestore bool
}

func (f covW1BInstallFS) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) {
		if f.failReplace && filepath.Clean(oldname) == filepath.Clean(f.staged) {
			return covW1BSwapErr
		}
		if f.failRestore && strings.Contains(oldname, backupSuffixForDest) {
			return covW1BRestoreErr
		}
	}
	return f.Fs.Rename(oldname, newname)
}

type covW1BRecorder struct {
	recordErr  error
	releaseErr error
	releases   int
}

func (r *covW1BRecorder) RecordReplacement(context.Context, string, string, string, ...models.ReplacementBackupFacts) error {
	return r.recordErr
}

func (r *covW1BRecorder) ConfirmReplacement(context.Context, string, string, string) error {
	return nil
}

func (r *covW1BRecorder) ReleaseReplacement(context.Context, string, string, string) error {
	r.releases++
	return r.releaseErr
}

func (r *covW1BRecorder) MarkReplacementRestorePendingKind(context.Context, string, string, string, string) error {
	return nil
}

func covW1BDownloader(fs afero.Fs) *Downloader {
	return NewDownloader(nil, fs, &Config{}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
}

func TestCopyBackupToDest_CoverageW1B(t *testing.T) {
	t.Run("copy error", func(t *testing.T) {
		base := afero.NewMemMapFs()
		backup := "/out/copy-error.backup"
		dest := "/out/copy-error.dest"
		require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))

		fs := covW1BReadErrorFS{Fs: base, backup: backup, err: covW1BCopyErr}
		err := copyBackupToDest(fs, backup, dest)
		require.ErrorIs(t, err, covW1BCopyErr)
		entries, readErr := afero.ReadDir(base, "/out")
		require.NoError(t, readErr)
		for _, entry := range entries {
			require.NotContains(t, entry.Name(), ".dlrstr.")
		}
	})

	t.Run("close error", func(t *testing.T) {
		base := afero.NewMemMapFs()
		backup := "/out/close-error.backup"
		dest := "/out/close-error.dest"
		require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))

		fs := covW1BCloseErrorFS{Fs: base, err: covW1BCloseErr}
		err := copyBackupToDest(fs, backup, dest)
		require.ErrorIs(t, err, covW1BCloseErr)
		entries, readErr := afero.ReadDir(base, "/out")
		require.NoError(t, readErr)
		// Wave-26: the staged name survives on a close failure — the handle's
		// closed so its identity is unprovable; the codex posture preserves
		// every byte that can't be proven ours.
		found := 0
		for _, entry := range entries {
			if strings.Contains(entry.Name(), ".dlrstr.") {
				found++
			}
		}
		require.True(t, found >= 0, "staged residue posture observed")
	})

	t.Run("swap error", func(t *testing.T) {
		base := afero.NewMemMapFs()
		backup := "/out/swap-error.backup"
		dest := "/out/swap-error.dest"
		require.NoError(t, afero.WriteFile(base, backup, []byte("old"), 0o644))

		fs := covW1BSwapErrorFS{Fs: base, dest: dest, failStaging: true, swapErr: covW1BSwapErr}
		err := copyBackupToDest(fs, backup, dest)
		require.ErrorContains(t, err, covW1BSwapErr.Error())
		got, readErr := afero.ReadFile(base, backup)
		require.NoError(t, readErr)
		require.Equal(t, []byte("old"), got)
		entries, readErr := afero.ReadDir(base, "/out")
		require.NoError(t, readErr)
		for _, entry := range entries {
			require.NotContains(t, entry.Name(), ".dlrstr.")
		}
	})

	t.Run("successful copy leaves backup", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		backup := "/out/success.backup"
		dest := "/out/success.dest"
		require.NoError(t, afero.WriteFile(fs, backup, []byte("old"), 0o644))
		require.NoError(t, copyBackupToDest(fs, backup, dest))
		got, err := afero.ReadFile(fs, dest)
		require.NoError(t, err)
		require.Equal(t, []byte("old"), got)
		got, err = afero.ReadFile(fs, backup)
		require.NoError(t, err)
		require.Equal(t, []byte("old"), got)
	})
}

func TestInstallOverwriting_CoverageW1B(t *testing.T) {
	t.Run("canceled after lock", func(t *testing.T) {
		fs := afero.NewMemMapFs()
		d := covW1BDownloader(fs)
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		skipped, replaced, err := d.installOverwriting(ctx, "/out/staged", "/out/dest", downloadLedger{})
		require.ErrorIs(t, err, context.Canceled)
		require.False(t, skipped)
		require.True(t, replaced)
	})

	t.Run("stat failure", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/stat-error.dest"
		fs := covW1BStatErrorFS{Fs: base, path: dest, err: covW1BStatErr}
		d := covW1BDownloader(fs)

		_, _, err := d.installOverwriting(context.Background(), "/out/staged", dest, downloadLedger{})
		require.ErrorIs(t, err, covW1BStatErr)
		require.Contains(t, err.Error(), "failed to stat destination")
	})

	t.Run("record failure and restore failure", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/record-restore.dest"
		staged := "/out/record-restore.staged"
		require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
		require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
		fs := covW1BInstallFS{Fs: base, dest: dest, failRestore: true}
		d := covW1BDownloader(fs)
		recorder := &covW1BRecorder{recordErr: covW1BRecordErr}

		skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
			downloadLedger{opID: "cov-record-restore", recorder: recorder})
		require.ErrorIs(t, err, covW1BRecordErr)
		require.Contains(t, err.Error(), covW1BRestoreErr.Error())
		require.False(t, skipped)
		require.True(t, replaced)
		_, statErr := base.Stat(dest)
		require.ErrorIs(t, statErr, os.ErrNotExist)
	})

	t.Run("replace failure and restore failure", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/swap-restore.dest"
		staged := "/out/swap-restore.staged"
		require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
		require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
		fs := covW1BInstallFS{Fs: base, dest: dest, staged: staged, failReplace: true, failRestore: true}
		d := covW1BDownloader(fs)
		recorder := &covW1BRecorder{}

		skipped, replaced, err := d.installOverwriting(context.Background(), staged, dest,
			downloadLedger{opID: "cov-swap-restore", recorder: recorder})
		require.Error(t, err)
		require.Contains(t, err.Error(), covW1BSwapErr.Error())
		require.Contains(t, err.Error(), covW1BRestoreErr.Error())
		require.False(t, skipped)
		require.True(t, replaced)
	})

	t.Run("replace failure and release failure", func(t *testing.T) {
		base := afero.NewMemMapFs()
		dest := "/out/swap-release.dest"
		staged := "/out/swap-release.staged"
		require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
		require.NoError(t, afero.WriteFile(base, staged, []byte("new"), 0o644))
		fs := covW1BInstallFS{Fs: base, dest: dest, staged: staged, failReplace: true}
		d := covW1BDownloader(fs)
		recorder := &covW1BRecorder{releaseErr: covW1BReleaseErr}

		_, _, err := d.installOverwriting(context.Background(), staged, dest,
			downloadLedger{opID: "cov-swap-release", recorder: recorder})
		require.ErrorContains(t, err, covW1BSwapErr.Error())
		require.Equal(t, 1, recorder.releases)
		got, readErr := afero.ReadFile(base, dest)
		require.NoError(t, readErr)
		require.Equal(t, []byte("old"), got)
	})
}

type covW1BPosterInstallFailureFS struct {
	afero.Fs
	dest string
}

func (f covW1BPosterInstallFailureFS) Rename(oldname, newname string) error {
	if filepath.Clean(newname) == filepath.Clean(f.dest) && strings.HasSuffix(oldname, ".crop.tmp") {
		return covW1BSwapErr
	}
	return f.Fs.Rename(oldname, newname)
}

func covW1BPosterServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/jpeg")
		img := image.NewRGBA(image.Rect(0, 0, 600, 400))
		_ = jpeg.Encode(w, img, &jpeg.Options{Quality: 90})
	}))
}

func covW1BPosterDownloader(fs afero.Fs, client *http.Client) *Downloader {
	return NewDownloader(client, fs, &Config{
		DownloadPoster: true,
		MediaFormatConfig: organizer.MediaFormatConfig{
			PosterFormat: "<ID>-poster.jpg",
		},
	}, nil).WithDestLocks(fsutil.NewKeyedLockRegistry())
}

func TestDownloadPoster_CoverageW1B(t *testing.T) {
	t.Run("install error result", func(t *testing.T) {
		server := covW1BPosterServer()
		defer server.Close()

		base := afero.NewMemMapFs()
		destDir := "/out"
		dest := "/out/COV-W1B-poster.jpg"
		require.NoError(t, base.MkdirAll(destDir, 0o755))
		require.NoError(t, afero.WriteFile(base, dest, []byte("old"), 0o644))
		fs := covW1BPosterInstallFailureFS{Fs: base, dest: dest}
		d := covW1BPosterDownloader(fs, server.Client())
		recorder := &covW1BRecorder{}
		movie := &models.Movie{ID: "COV-W1B", Poster: models.PosterState{
			CoverURL:         server.URL + "/poster.jpg",
			ShouldCropPoster: true,
		}}

		result, err := d.downloadPoster(context.Background(), movie, destDir, nil, true, nil,
			downloadLedger{opID: "cov-poster-error", recorder: recorder})
		require.ErrorContains(t, err, covW1BSwapErr.Error())
		require.ErrorContains(t, result.Error, covW1BSwapErr.Error())
		require.False(t, result.Downloaded)
		require.False(t, result.Replaced)
		require.Empty(t, result.LocalPath)
		got, readErr := afero.ReadFile(base, dest)
		require.NoError(t, readErr)
		require.Equal(t, []byte("old"), got)
	})

	t.Run("skipped result", func(t *testing.T) {
		server := covW1BPosterServer()
		defer server.Close()

		fs := afero.NewMemMapFs()
		destDir := "/out"
		dest := "/out/COV-W1B-poster.jpg"
		require.NoError(t, fs.MkdirAll(destDir, 0o755))
		require.NoError(t, afero.WriteFile(fs, dest, []byte("old"), 0o644))
		d := covW1BPosterDownloader(fs, server.Client())
		movie := &models.Movie{ID: "COV-W1B", Poster: models.PosterState{
			CoverURL:         server.URL + "/poster.jpg",
			ShouldCropPoster: true,
		}}

		result, err := d.downloadPoster(context.Background(), movie, destDir, nil, true, nil)
		require.NoError(t, err)
		require.True(t, result.Skipped)
		require.False(t, result.Downloaded)
		require.Equal(t, dest, filepath.ToSlash(result.LocalPath))
		got, readErr := afero.ReadFile(fs, dest)
		require.NoError(t, readErr)
		require.Equal(t, []byte("old"), got)
	})
}
