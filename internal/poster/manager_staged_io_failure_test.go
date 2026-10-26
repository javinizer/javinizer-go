package poster

import (
	"context"
	"errors"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// These fixtures fail the actual file/body I/O used by the public download
// method, before the canonical poster pair is eligible for replacement.
func TestPosterDownloadStagedIOFailure(t *testing.T) {
	for _, tc := range []struct {
		name   string
		create bool
		read   bool
		write  bool
		close  bool
		prefix string
	}{
		{name: "create", create: true, prefix: "failed to create temp file: "},
		{name: "body_read", read: true, prefix: "failed to write image: "},
		{name: "file_write", write: true, prefix: "failed to write image: "},
		{name: "file_close", close: true, prefix: "failed to close temp file: "},
		{name: "write_error_precedes_close_error", write: true, close: true, prefix: "failed to write image: "},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := afero.NewMemMapFs()
			root := filepath.Join(string(filepath.Separator), "poster-io")
			dir := filepath.Join(root, "posters", "job-io")
			require.NoError(t, base.MkdirAll(dir, 0755))
			originals := map[string]string{
				"TEST-001-full.jpg":         "previous full poster",
				"TEST-001.jpg":              "previous cropped poster",
				"TEST-001-full-foreign.tmp": "another operation's staging bytes",
			}
			for name, value := range originals {
				require.NoError(t, afero.WriteFile(base, filepath.Join(dir, name), []byte(value), 0644))
			}

			fault := errors.New("injected poster I/O failure")
			closeFault := errors.New("injected poster close failure")
			fs := &posterStagedIOFS{Fs: base, directory: dir}
			if tc.create {
				fs.createErr = fault
			}
			if tc.write {
				fs.writeErr = fault
			}
			if tc.close {
				fs.closeErr = closeFault
			}
			body := &posterStagedIOBody{reader: strings.NewReader("synthetic download bytes")}
			if tc.read {
				body.readErr = fault
			}
			requestCount, ssrfChecks := 0, 0
			const rawURL = "https://poster-io.invalid/image.jpg"
			client := posterStagedIOClient(func(req *http.Request) (*http.Response, error) {
				requestCount++
				require.Equal(t, rawURL, req.URL.String())
				require.Equal(t, http.MethodGet, req.Method)
				return &http.Response{StatusCode: http.StatusOK, Header: make(http.Header), Body: body}, nil
			})
			pm := NewPosterManager(fs, root, client, 0).WithSSRFCheck(func(value string) error {
				ssrfChecks++
				require.Equal(t, rawURL, value)
				return nil
			})

			result, err := pm.DownloadFromURL(context.Background(), "job-io", "TEST-001", rawURL, "", "")
			require.Nil(t, result)
			wantErr := fault
			if tc.close && !tc.write {
				wantErr = closeFault
			}
			require.ErrorIs(t, err, wantErr)
			require.EqualError(t, err, tc.prefix+wantErr.Error())
			if tc.write && tc.close {
				require.NotErrorIs(t, err, closeFault)
			}
			require.Equal(t, 1, requestCount)
			require.Equal(t, 1, ssrfChecks)
			require.Equal(t, 1, body.closes, "the response body must close on every staging failure")
			require.Equal(t, 1, fs.createAttempts)
			require.Empty(t, fs.renamed, "canonical files must not be parked or replaced")
			if tc.create {
				require.Nil(t, fs.staging)
				require.Empty(t, fs.removed, "a failed create owns no file to remove")
			} else {
				require.NotNil(t, fs.staging)
				require.Equal(t, 1, fs.staging.closes)
				require.NoError(t, fs.staging.underlyingCloseErr)
				require.Positive(t, fs.staging.writes, "exercise actual io.Copy writes")
				require.Positive(t, fs.staging.bytesWritten, "exercise cleanup of partially populated staging")
				require.Equal(t, []string{fs.staging.Name()}, fs.removed, "only this operation's staging may be removed")
				_, statErr := base.Stat(fs.staging.Name())
				require.ErrorIs(t, statErr, os.ErrNotExist)
			}
			entries, err := afero.ReadDir(base, dir)
			require.NoError(t, err)
			require.Len(t, entries, len(originals), "no staging or backup residue")
			for name, value := range originals {
				got, err := afero.ReadFile(base, filepath.Join(dir, name))
				require.NoError(t, err)
				require.Equal(t, value, string(got))
			}
		})
	}
}

func TestPosterCopyFileIOFailure(t *testing.T) {
	for _, failure := range []string{"reader", "writer"} {
		t.Run(failure, func(t *testing.T) {
			base := afero.NewMemMapFs()
			src := filepath.Join(string(filepath.Separator), "source.jpg")
			dst := filepath.Join(string(filepath.Separator), "copy.tmp")
			const source = "synthetic source content"
			require.NoError(t, afero.WriteFile(base, src, []byte(source), 0644))
			fault := errors.New("injected copy " + failure + " failure")
			closeFault := errors.New("injected copy close failure")
			fs := &posterCopyIOFS{Fs: base, source: src, destination: dst, closeErr: closeFault}
			if failure == "reader" {
				fs.readErr = fault
			} else {
				fs.writeErr = fault
			}

			err := copyFile(fs, src, dst)
			require.ErrorIs(t, err, fault)
			require.EqualError(t, err, "failed to copy file contents: "+fault.Error())
			require.NotErrorIs(t, err, closeFault, "a deferred close failure must not mask the copy failure")
			require.NotNil(t, fs.in)
			require.NotNil(t, fs.out)
			require.Positive(t, fs.in.reads)
			require.Positive(t, fs.out.writes)
			require.Equal(t, 1, fs.in.closes)
			require.Equal(t, 1, fs.out.closes)
			require.NoError(t, fs.in.underlyingCloseErr)
			require.NoError(t, fs.out.underlyingCloseErr)
			got, err := afero.ReadFile(base, src)
			require.NoError(t, err)
			require.Equal(t, source, string(got), "copy failure must not change the source")
			partial, err := afero.ReadFile(base, dst)
			require.NoError(t, err)
			require.NotEmpty(t, partial)
			require.Less(t, len(partial), len(source), "prove a real interrupted copy, not the successful path")
		})
	}
}

type posterStagedIOClient func(*http.Request) (*http.Response, error)

func (f posterStagedIOClient) Do(req *http.Request) (*http.Response, error) { return f(req) }

type posterStagedIOBody struct {
	reader  io.Reader
	readErr error
	closes  int
}

func (b *posterStagedIOBody) Read(p []byte) (int, error) {
	n, err := b.reader.Read(p)
	if n > 0 && b.readErr != nil {
		return n, b.readErr
	}
	return n, err
}

func (b *posterStagedIOBody) Close() error { b.closes++; return nil }

type posterIOFailureFile struct {
	afero.File
	readErr            error
	writeErr           error
	closeErr           error
	reads              int
	writes             int
	bytesWritten       int
	closes             int
	underlyingCloseErr error
}

func (f *posterIOFailureFile) Read(p []byte) (int, error) {
	f.reads++
	if f.readErr != nil && len(p) > 1 {
		n, err := f.File.Read(p[:1])
		if n > 0 {
			return n, f.readErr
		}
		return n, err
	}
	return f.File.Read(p)
}

func (f *posterIOFailureFile) Write(p []byte) (int, error) {
	f.writes++
	if f.writeErr != nil && len(p) > 1 {
		n, err := f.File.Write(p[:1])
		f.bytesWritten += n
		if err != nil {
			return n, err
		}
		return n, f.writeErr
	}
	n, err := f.File.Write(p)
	f.bytesWritten += n
	return n, err
}

func (f *posterIOFailureFile) Close() error {
	f.closes++
	f.underlyingCloseErr = f.File.Close()
	if f.closeErr != nil {
		return f.closeErr
	}
	return f.underlyingCloseErr
}

type posterStagedIOFS struct {
	afero.Fs
	directory      string
	createErr      error
	writeErr       error
	closeErr       error
	createAttempts int
	staging        *posterIOFailureFile
	removed        []string
	renamed        []string
}

func (f *posterStagedIOFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	if filepath.Dir(name) == f.directory && strings.HasPrefix(filepath.Base(name), "TEST-001-full-") && strings.HasSuffix(name, ".tmp") {
		f.createAttempts++
		if f.createErr != nil {
			return nil, f.createErr
		}
		file, err := f.Fs.OpenFile(name, flag, perm)
		if err != nil {
			return nil, err
		}
		f.staging = &posterIOFailureFile{File: file, writeErr: f.writeErr, closeErr: f.closeErr}
		return f.staging, nil
	}
	return f.Fs.OpenFile(name, flag, perm)
}

func (f *posterStagedIOFS) Remove(name string) error {
	f.removed = append(f.removed, name)
	return f.Fs.Remove(name)
}

func (f *posterStagedIOFS) Rename(oldname, newname string) error {
	f.renamed = append(f.renamed, oldname+" -> "+newname)
	return f.Fs.Rename(oldname, newname)
}

type posterCopyIOFS struct {
	afero.Fs
	source      string
	destination string
	readErr     error
	writeErr    error
	closeErr    error
	in          *posterIOFailureFile
	out         *posterIOFailureFile
}

func (f *posterCopyIOFS) Open(name string) (afero.File, error) {
	file, err := f.Fs.Open(name)
	if err != nil || name != f.source {
		return file, err
	}
	f.in = &posterIOFailureFile{File: file, readErr: f.readErr}
	return f.in, nil
}

func (f *posterCopyIOFS) Create(name string) (afero.File, error) {
	file, err := f.Fs.Create(name)
	if err != nil || name != f.destination {
		return file, err
	}
	f.out = &posterIOFailureFile{File: file, writeErr: f.writeErr, closeErr: f.closeErr}
	return f.out, nil
}
