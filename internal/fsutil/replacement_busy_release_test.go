package fsutil

import (
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

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

// releaseRefusalFile reports ordinary afero.File operation failures while
// retaining the real descriptor and its creation-bound kernel identity.
// afterTokenRead changes only the fixture's metadata after exact bytes are read.
type releaseRefusalFile struct {
	*releaseStatusFile
	statCalls      int
	failStat       int
	fault          error
	readFailure    bool
	closeResult    error
	readBytes      int
	tokenBytes     int
	afterTokenRead func() error
}

func (f *releaseRefusalFile) Stat() (os.FileInfo, error) {
	f.statCalls++
	if f.statCalls == f.failStat {
		return nil, &os.PathError{Op: "stat", Path: f.Name(), Err: f.fault}
	}
	return f.File.Stat()
}

func (f *releaseRefusalFile) Close() error {
	f.closeResult = f.releaseStatusFile.Close()
	return f.closeResult
}

func (f *releaseRefusalFile) Read(p []byte) (int, error) {
	n, err := f.File.Read(p)
	f.readBytes += n
	if f.readFailure {
		// Reader permits data and an error together. Exact bytes alone must
		// not authorize release when the read also reports an I/O failure.
		return n, f.fault
	}
	if f.afterTokenRead != nil && f.readBytes == f.tokenBytes {
		change := f.afterTokenRead
		f.afterTokenRead = nil
		if changeErr := change(); changeErr != nil {
			return n, changeErr
		}
	}
	return n, err
}

type releaseRefusalFS struct {
	afero.Fs
	openSeekOffset int64
	openSeekErr    error
	file           *releaseRefusalFile
	openErr        error
	opens          int
	flags          int
}

func (f *releaseRefusalFS) OpenFile(path string, flags int, mode os.FileMode) (afero.File, error) {
	f.opens++
	f.flags = flags
	if f.openErr != nil {
		return nil, &os.PathError{Op: "open", Path: path, Err: f.openErr}
	}
	file, err := f.Fs.OpenFile(path, flags, mode)
	if err != nil {
		return nil, err
	}
	f.file.File = file
	// Negative control on this exact real descriptor; querying its current
	// offset neither consumes token bytes nor changes the file position.
	f.openSeekOffset, f.openSeekErr = file.Seek(0, io.SeekCurrent)
	return f.file, nil
}

// This matrix tests the existing adapter/helper contract, not manufactured
// native kernel faults. Nil identity is specifically a defensive helper input;
// it does not assert a production claimant ever discards its creation proof.
func TestReplacementBusyFixedReleaseRefusalPreservesOwnership(t *testing.T) {
	for _, kind := range []string{"nil_identity", "open_failure", "initial_stat_failure", "read_failure", "recheck_stat_failure", "metadata_changed_after_read"} {
		t.Run(kind, func(t *testing.T) {
			base := afero.NewOsFs()
			dir := t.TempDir()
			dest := filepath.Join(dir, "poster.jpg")
			foreign := filepath.Join(dir, "foreign.marker")
			require.NoError(t, os.WriteFile(foreign, []byte("foreign sentinel"), 0600))
			foreignPin, err := os.Open(foreign)
			require.NoError(t, err)
			foreignInfo, foreignStatErr := foreignPin.Stat()
			foreignCloseErr := foreignPin.Close()
			require.NoError(t, foreignStatErr)
			require.NoError(t, foreignCloseErr)
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
			argument := claimed
			fault := errors.New("fixture release I/O failure")
			file := &releaseRefusalFile{
				releaseStatusFile: &releaseStatusFile{writeN: 1},
				fault:             fault, tokenBytes: len(token),
			}
			fs := &releaseRefusalFS{Fs: base, file: file}
			want := ""
			switch kind {
			case "nil_identity":
				argument = nil
				want = "requires creation identity"
			case "open_failure":
				fs.openErr = fault
				want = "open owned busy marker for release"
			case "initial_stat_failure":
				file.failStat = 1
				want = "stat owned busy marker for release"
			case "read_failure":
				file.readFailure = true
				want = "read owned busy marker for release"
			case "recheck_stat_failure":
				file.failStat = 2
				want = "recheck owned busy marker for release"
			case "metadata_changed_after_read":
				file.afterTokenRead = func() error {
					return base.Chtimes(marker, info.ModTime(), info.ModTime().Add(2*time.Second))
				}
				want = "recheck owned busy marker identity for release"
			}
			visible, err := markReplacementBusyReleased(fs, marker, token, argument)
			require.False(t, visible)
			require.ErrorContains(t, err, want)
			if kind != "nil_identity" && kind != "metadata_changed_after_read" {
				require.ErrorIs(t, err, fault)
			}
			require.Zero(t, file.writes)
			require.Zero(t, file.syncs)
			if kind == "nil_identity" {
				require.Zero(t, fs.opens)
			} else {
				require.Equal(t, 1, fs.opens)
				require.Equal(t, os.O_RDWR, fs.flags, "no create/truncate/append")
			}
			if kind == "nil_identity" || kind == "open_failure" {
				require.Zero(t, file.closes)
				require.Nil(t, file.File)
			} else {
				require.Equal(t, 1, file.closes)
				require.NoError(t, file.closeResult)
				require.NoError(t, fs.openSeekErr, "open descriptor must pass the same probe")
				require.Zero(t, fs.openSeekOffset)
				// Unlike Windows Stat, Seek checks the real file's closed state
				// portably; no adapter method or injected failure is involved.
				_, closedErr := file.File.Seek(0, io.SeekCurrent)
				require.ErrorIs(t, closedErr, os.ErrClosed, "actual descriptor closed")
			}
			if kind == "read_failure" {
				require.Positive(t, file.readBytes, "data returned with an error must not authorize release")
				require.LessOrEqual(t, file.readBytes, len(token))
			}
			wantStats := map[string]int{
				"nil_identity": 0, "open_failure": 0,
				"initial_stat_failure": 1, "read_failure": 1,
				"recheck_stat_failure": 2, "metadata_changed_after_read": 2,
			}
			require.Equal(t, wantStats[kind], file.statCalls)
			if kind == "recheck_stat_failure" || kind == "metadata_changed_after_read" {
				require.Equal(t, len(token), file.readBytes)
				require.Equal(t, 2, file.statCalls)
			}
			data, err := os.ReadFile(marker)
			require.NoError(t, err)
			require.Equal(t, token, string(data), "release bit remains zero")
			post, err := base.Open(marker)
			require.NoError(t, err)
			postInfo, postStatErr := post.Stat()
			postCloseErr := post.Close()
			require.NoError(t, postStatErr)
			require.NoError(t, postCloseErr)
			require.True(t, os.SameFile(info, postInfo))
			require.Equal(t, info.Size(), postInfo.Size())
			require.Equal(t, info.Mode(), postInfo.Mode())
			if kind == "metadata_changed_after_read" {
				require.True(t, postInfo.ModTime().Equal(info.ModTime().Add(2*time.Second)))
			} else {
				require.True(t, postInfo.ModTime().Equal(info.ModTime()))
			}
			data, err = os.ReadFile(foreign)
			require.NoError(t, err)
			require.Equal(t, "foreign sentinel", string(data))
			foreignAfter, err := os.Stat(foreign)
			require.NoError(t, err)
			require.True(t, os.SameFile(foreignInfo, foreignAfter))
			require.True(t, foreignInfo.ModTime().Equal(foreignAfter.ModTime()))
			blockedRelease, _, err := AcquireReplacementBusyEx(base, dest)
			if blockedRelease != nil {
				t.Cleanup(blockedRelease)
			}
			require.ErrorIs(t, err, ErrReplacementBusy, "refusal never releases the live owner")

			// Remove only the adapter fault (or supply the retained valid claim).
			// Metadata is rebound from the same owned inode on the next attempt.
			retryFile := &releaseStatusFile{writeN: 1}
			retryFS := &releaseStatusFS{Fs: base, file: retryFile}
			visible, err = markReplacementBusyReleased(retryFS, marker, token, claimed)
			require.NoError(t, err)
			require.True(t, visible)
			require.Equal(t, 1, retryFile.writes)
			require.Equal(t, 1, retryFile.syncs)
			require.Equal(t, 1, retryFile.closes)
			data, err = os.ReadFile(marker)
			require.NoError(t, err)
			require.Equal(t, token[:len(token)-1]+"1", string(data))
			nextRelease, nextToken, err := AcquireReplacementBusyEx(base, dest)
			require.NoError(t, err)
			t.Cleanup(nextRelease)
			release() // previous capability must not delete a new claimant
			data, err = os.ReadFile(marker)
			require.NoError(t, err)
			require.Equal(t, nextToken, string(data))
			data, err = os.ReadFile(foreign)
			require.NoError(t, err)
			require.Equal(t, "foreign sentinel", string(data))
			nextRelease()
			require.NoFileExists(t, marker)
		})
	}
}
