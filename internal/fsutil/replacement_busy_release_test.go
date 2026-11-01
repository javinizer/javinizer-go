package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type releaseStatusFile struct {
	afero.File
	writeN                      int
	writeErr, syncErr, closeErr error
	writes, syncs, closes       int
	offset                      int64
}

func (f *releaseStatusFile) Fd() uintptr { return f.File.(interface{ Fd() uintptr }).Fd() }
func (f *releaseStatusFile) WriteAt(p []byte, offset int64) (int, error) {
	f.writes++
	f.offset = offset
	if len(p) != 1 || p[0] != '1' {
		return 0, fmt.Errorf("unexpected release write %q", p)
	}
	if f.writeN == 0 {
		return 0, f.writeErr
	}
	n, err := f.File.WriteAt(p, offset)
	return n, errors.Join(err, f.writeErr)
}
func (f *releaseStatusFile) Sync() error  { f.syncs++; return errors.Join(f.File.Sync(), f.syncErr) }
func (f *releaseStatusFile) Close() error { f.closes++; return errors.Join(f.File.Close(), f.closeErr) }

type releaseStatusFS struct {
	afero.Fs
	file  *releaseStatusFile
	flags int
}

func (f *releaseStatusFS) OpenFile(path string, flag int, mode os.FileMode) (afero.File, error) {
	f.flags = flag
	handle, err := f.Fs.OpenFile(path, flag, mode)
	if err != nil {
		return nil, err
	}
	f.file.File = handle
	return f.file, nil
}

// The protocol unit matrix uses real owned files and ordinary afero failures.
// It does not claim to manufacture native kernel write/sync/close faults.
func TestReplacementBusyFixedReleaseStatusVisibility(t *testing.T) {
	fault := errors.New("owned release handle failure")
	cases := []struct {
		name               string
		n                  int
		write, sync, close error
		visible            bool
	}{
		{"success", 1, nil, nil, nil, true},
		{"zero_write", 0, nil, nil, nil, false},
		{"zero_write_error", 0, fault, nil, nil, false},
		{"visible_write_error", 1, fault, nil, nil, true},
		{"visible_sync_error", 1, nil, fault, nil, true},
		{"visible_close_error", 1, nil, nil, fault, true},
		{"visible_all_errors", 1, fault, fault, fault, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			base := afero.NewOsFs()
			dest := filepath.Join(t.TempDir(), "poster.jpg")
			release, token, err := AcquireReplacementBusyEx(base, dest)
			require.NoError(t, err)
			t.Cleanup(release)
			require.True(t, strings.HasSuffix(token, replacementBusyLiveSuffix))
			marker := ReplacementBusyPath(dest)
			pin, err := base.Open(marker)
			require.NoError(t, err)
			info, statErr := pin.Stat()
			var device, inode uint64
			var strong bool
			if statErr == nil {
				device, inode, strong = streamedHandleIdentity(pin, info)
			}
			closeErr := pin.Close()
			require.NoError(t, statErr)
			require.NoError(t, closeErr)
			require.True(t, strong)
			claimed := newBoundInstallIdentity(info, device, inode, strong)
			file := &releaseStatusFile{writeN: tc.n, writeErr: tc.write, syncErr: tc.sync, closeErr: tc.close}
			fs := &releaseStatusFS{Fs: base, file: file}
			visible, err := markReplacementBusyReleased(fs, marker, token, claimed)
			require.Equal(t, tc.visible, visible)
			if tc.name == "success" {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}
			if tc.write != nil || tc.sync != nil || tc.close != nil {
				require.ErrorIs(t, err, fault)
			}
			require.Equal(t, os.O_RDWR, fs.flags, "never create/truncate/append or bypass adapter")
			require.Equal(t, 1, file.writes)
			require.Equal(t, 1, file.closes)
			require.Equal(t, tc.n, file.syncs, "sync even when a complete byte write also reports an error")
			require.Equal(t, int64(len(token)-1), file.offset)
			data, err := os.ReadFile(marker)
			require.NoError(t, err)
			expected := token
			if tc.visible {
				expected = token[:len(token)-1] + "1"
			}
			require.Equal(t, expected, string(data), "fixed status has no partial appended field")
			post, err := base.Open(marker)
			require.NoError(t, err)
			postInfo, postStatErr := post.Stat()
			postCloseErr := post.Close()
			require.NoError(t, postStatErr)
			require.NoError(t, postCloseErr)
			require.True(t, os.SameFile(info, postInfo), "status transition retains the owned object")
			if !tc.visible {
				_, _, err = AcquireReplacementBusyEx(base, dest)
				require.ErrorIs(t, err, ErrReplacementBusy, "unwritten status keeps owner live")
			} else {
				nextRelease, nextToken, err := AcquireReplacementBusyEx(base, dest)
				require.NoError(t, err, "visible release is reclaimable even after durability/close errors")
				t.Cleanup(nextRelease)
				release()
				data, err = os.ReadFile(marker)
				require.NoError(t, err)
				require.Equal(t, nextToken, string(data))
				nextRelease()
				require.NoFileExists(t, marker)
			}
		})
	}
}

func TestReplacementBusyFixedReleaseRefusesUnownedOrLegacyStatus(t *testing.T) {
	for _, kind := range []string{"copied_token_new_inode", "changed_token", "weak_identity", "legacy_token"} {
		t.Run(kind, func(t *testing.T) {
			base := afero.NewOsFs()
			dest := filepath.Join(t.TempDir(), "poster.jpg")
			release, token, err := AcquireReplacementBusyEx(base, dest)
			require.NoError(t, err)
			t.Cleanup(release)
			marker := ReplacementBusyPath(dest)
			pin, err := base.Open(marker)
			require.NoError(t, err)
			info, statErr := pin.Stat()
			var device, inode uint64
			var strong bool
			if statErr == nil {
				device, inode, strong = streamedHandleIdentity(pin, info)
			}
			closeErr := pin.Close()
			require.NoError(t, statErr)
			require.NoError(t, closeErr)
			require.True(t, strong)
			claimed := newBoundInstallIdentity(info, device, inode, strong)
			expected := token
			switch kind {
			case "copied_token_new_inode":
				require.NoError(t, os.Rename(marker, marker+".original"))
				require.NoError(t, os.WriteFile(marker, []byte(token), 0o600))
				require.NoError(t, os.Chtimes(marker, info.ModTime(), info.ModTime()))
			case "changed_token":
				expected = strings.Replace(token, "pid=", "pid=9", 1)
				require.NoError(t, os.WriteFile(marker, []byte(expected), 0o600))
			case "weak_identity":
				claimed = NewWeakBoundInstallIdentity(info)
			case "legacy_token":
				token = strings.TrimSuffix(token, replacementBusyLiveSuffix)
				expected = token
				require.NoError(t, os.WriteFile(marker, []byte(token), 0o600))
			}
			file := &releaseStatusFile{writeN: 1}
			fs := &releaseStatusFS{Fs: base, file: file}
			visible, err := markReplacementBusyReleased(fs, marker, token, claimed)
			require.Error(t, err)
			require.False(t, visible)
			require.Zero(t, file.writes)
			require.Zero(t, file.syncs)
			if file.File != nil {
				require.Equal(t, 1, file.closes)
			}
			data, err := os.ReadFile(marker)
			require.NoError(t, err)
			require.Equal(t, expected, string(data))
			if kind == "copied_token_new_inode" {
				original, err := os.ReadFile(marker + ".original")
				require.NoError(t, err)
				require.Equal(t, token, string(original))
			}
		})
	}
}
