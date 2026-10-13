package workflow

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

func TestReplacementBusyControlNames(t *testing.T) {
	token := "0123456789abcdef0123456789abcdef"
	cases := []struct {
		name    string
		control bool
	}{
		{"fanart.jpg.dlbusy", true}, {"fanart.jpg.dlbusy.takeover-123-abcd", true},
		{"fanart.jpg.dlbusy.quarantine-123-0", true}, {"fanart.jpg.dlbusy.takeover-123-ffffffffffffffff", true},
		{"fanart.jpg.dlbusy.vac." + token, true}, {"fanart.jpg.dlbusy.takeover-123-abcd.vac." + token, true},
		{"fanart.jpg.dlbusy.quarantine-123-abcd.vac." + token + ".vac." + token, true},
		{"FANART.JPG.DLBUSY.TAKEOVER-123-ABCD.VAC." + strings.ToUpper(token), true},
		{"movie.dlbusy.nfo", false}, {"movie.dlbusy.mp4", false}, {"movie.dlbusy.fanart.jpg", false},
		{"movie.dlbusy.takeover-123-abcd.nfo", false}, {"movie.dlbusy.quarantine-123-abcd.jpg", false},
		{"movie.dlbusy.takeover-pid-abcd", false}, {"movie.dlbusy.takeover-0-abcd", false},
		{"movie.dlbusy.takeover-001-abcd", false}, {"movie.dlbusy.takeover-123-abcdef12345678901", false},
		{"movie.dlbusy.quarantine-123-xyz", false}, {"movie.dlbusy.takeover-123-abcd.trailing", false},
		{"movie.dlbusy.vac.001", false}, {"movie.dlbusy.vac." + token + ".nfo", false},
		{"movie.dlbusy-reference.txt", false}, {"日本語.dlbusy.metadata.nfo", false}, {"", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.control, stagedBusyReservation(tc.name)) })
	}
}

type reservationNameCaptureFS struct {
	afero.Fs
	names        []string
	marker       string
	foreignToken string
}

func (f *reservationNameCaptureFS) OpenFile(path string, flags int, mode os.FileMode) (afero.File, error) {
	if flags&os.O_EXCL != 0 {
		f.names = append(f.names, path)
	}
	return f.Fs.OpenFile(path, flags, mode)
}
func (f *reservationNameCaptureFS) LstatIfPossible(path string) (os.FileInfo, bool, error) {
	return f.Fs.(afero.Lstater).LstatIfPossible(path)
}
func (f *reservationNameCaptureFS) Rename(oldPath, newPath string) error {
	if err := f.Fs.Rename(oldPath, newPath); err != nil {
		return err
	}
	if oldPath == f.marker && f.foreignToken != "" {
		token := f.foreignToken
		f.foreignToken = ""
		if err := afero.WriteFile(f.Fs, newPath, []byte(token), 0o600); err != nil {
			return err
		}
		return afero.WriteFile(f.Fs, oldPath, []byte(token), 0o600)
	}
	return nil
}

func TestReplacementBusyControlNamesMatchProducers(t *testing.T) {
	base := afero.NewOsFs()
	root := t.TempDir()
	foreignRelease, foreignToken, err := fsutil.AcquireReplacementBusyEx(base, filepath.Join(root, "foreign"))
	require.NoError(t, err)
	defer foreignRelease()
	fs := &reservationNameCaptureFS{Fs: base}
	for _, artifact := range []string{"movie.dlbusy.nfo", "日本語.dlbusy.fanart.jpg", "MOVIE.MP4"} {
		for i := 0; i < 16; i++ {
			destination := filepath.Join(root, artifact)
			release, err := fsutil.AcquireReplacementBusy(fs, destination)
			require.NoError(t, err)
			release()
			fs.marker = fsutil.ReplacementBusyPath(destination)
			fs.foreignToken = foreignToken
			require.NoError(t, afero.WriteFile(base, fs.marker, []byte("pid=999999999,time=1"), 0o600))
			_, err = fsutil.AcquireReplacementBusy(fs, destination)
			require.ErrorIs(t, err, fsutil.ErrReplacementBusy)
			require.NoError(t, base.Remove(fs.marker))
			fs.marker = ""
		}
	}
	require.NotEmpty(t, fs.names)
	takeoverSeen, quarantineSeen, vacuumSeen := false, false, false
	for _, generated := range fs.names {
		name := filepath.Base(generated)
		require.True(t, stagedBusyReservation(name), name)
		require.True(t, stagedBusyReservation(strings.ToUpper(name)), name)
		for _, extension := range []string{".nfo", ".jpg", ".mp4", ".srt"} {
			require.False(t, stagedBusyReservation(name+extension), name+extension)
		}
		takeoverSeen = takeoverSeen || strings.Contains(name, ".takeover-")
		quarantineSeen = quarantineSeen || strings.Contains(name, ".quarantine-")
		vacuumSeen = vacuumSeen || strings.Contains(name, ".vac.")
	}
	require.True(t, takeoverSeen)
	require.True(t, quarantineSeen)
	require.True(t, vacuumSeen)
}

func FuzzReplacementBusyControlNamesPreserveArtifactExtensions(f *testing.F) {
	for _, seed := range []string{"movie", "movie.dlbusy", "movie.dlbusy.takeover-123-abcd", "movie.dlbusy.quarantine-123-abcd", "MOVIE.DLBUSY", "日本語.dlbusy", "movie.dlbusy.vac.0123456789abcdef0123456789abcdef"} {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, name string) {
		for _, extension := range []string{".nfo", ".jpg", ".mp4", ".srt"} {
			require.False(t, stagedBusyReservation(name+extension))
		}
	})
}
