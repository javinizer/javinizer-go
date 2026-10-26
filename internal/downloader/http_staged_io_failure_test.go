package downloader

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

type stagingIOHTTPClient struct {
	body  io.ReadCloser
	calls int
}

func (c *stagingIOHTTPClient) Do(*http.Request) (*http.Response, error) {
	c.calls++
	return &http.Response{StatusCode: http.StatusOK, Body: c.body, Header: make(http.Header)}, nil
}

type stagingIOBody struct {
	io.Reader
	closes int
}

func (b *stagingIOBody) Close() error {
	b.closes++
	return nil
}

type stagingIOErrorReader struct{ err error }

func (r stagingIOErrorReader) Read([]byte) (int, error) { return 0, r.err }

type stagingIOFile struct {
	afero.File
	writeErr, closeErr error
	writes, closes     int
}

func (f *stagingIOFile) Write(p []byte) (int, error) {
	f.writes++
	if f.writeErr != nil {
		n, err := f.File.Write(p[:1])
		if err != nil {
			return n, err
		}
		return n, f.writeErr
	}
	return f.File.Write(p)
}

func (f *stagingIOFile) Close() error {
	f.closes++
	err := f.File.Close()
	if err != nil {
		return err
	}
	return f.closeErr
}

type stagingIOFS struct {
	afero.Fs
	createErr, writeErr, closeErr error
	claims                        int
	temp                          *stagingIOFile
	removed                       []string
}

func (f *stagingIOFS) OpenFile(name string, flags int, perm os.FileMode) (afero.File, error) {
	if flags&os.O_EXCL == 0 || !strings.HasSuffix(name, ".tmp") {
		return f.Fs.OpenFile(name, flags, perm)
	}
	f.claims++
	if f.createErr != nil {
		return nil, f.createErr
	}
	file, err := f.Fs.OpenFile(name, flags, perm)
	if err != nil {
		return nil, err
	}
	f.temp = &stagingIOFile{File: file, writeErr: f.writeErr, closeErr: f.closeErr}
	return f.temp, nil
}

func (f *stagingIOFS) Remove(name string) error {
	f.removed = append(f.removed, name)
	return f.Fs.Remove(name)
}

func TestDownloadStagedIOFailuresPreserveExistingMedia(t *testing.T) {
	for _, tc := range []struct {
		name                         string
		createErr, readErr, writeErr error
		closeErr, wantErr            error
		wantPrefix                   string
		retryable                    bool
	}{
		{name: "create", createErr: os.ErrPermission, wantErr: os.ErrPermission, wantPrefix: "failed to create file"},
		{name: "body read", readErr: io.ErrUnexpectedEOF, wantErr: io.ErrUnexpectedEOF, wantPrefix: "failed to write file", retryable: true},
		{name: "staging write", writeErr: io.ErrShortWrite, wantErr: io.ErrShortWrite, wantPrefix: "failed to write file"},
		{name: "staging close", closeErr: os.ErrPermission, wantErr: os.ErrPermission, wantPrefix: "failed to write file"},
		{name: "read error takes precedence over close", readErr: io.ErrUnexpectedEOF, closeErr: os.ErrPermission, wantErr: io.ErrUnexpectedEOF, wantPrefix: "failed to write file", retryable: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			base := afero.NewMemMapFs()
			dir := filepath.Join(t.TempDir(), "output")
			dest := filepath.Join(dir, "cover.jpg")
			foreign := dest + ".foreign.tmp"
			require.NoError(t, base.MkdirAll(dir, 0o755))
			require.NoError(t, afero.WriteFile(base, dest, []byte("existing canonical bytes"), 0o600))
			require.NoError(t, afero.WriteFile(base, foreign, []byte("unowned staging bytes"), 0o600))
			fs := &stagingIOFS{Fs: base, createErr: tc.createErr, writeErr: tc.writeErr, closeErr: tc.closeErr}
			var reader io.Reader = strings.NewReader("synthetic download bytes")
			if tc.readErr != nil {
				reader = io.MultiReader(reader, stagingIOErrorReader{err: tc.readErr})
			}
			body := &stagingIOBody{Reader: reader}
			client := &stagingIOHTTPClient{body: body}
			d := NewDownloader(client, fs, &Config{}, nil)
			result, err := d.download(t.Context(), "https://media.invalid/cover.jpg", dest, MediaTypeCover, true)
			require.EqualError(t, err, tc.wantPrefix+": "+tc.wantErr.Error())
			require.ErrorIs(t, err, tc.wantErr)
			require.Equal(t, tc.retryable, isRetryableError(err))
			require.NotNil(t, result)
			require.Same(t, err, result.Error)
			require.False(t, result.Downloaded)
			require.False(t, result.Replaced)
			require.False(t, result.Skipped)
			require.Equal(t, dest, result.LocalPath)
			require.Equal(t, 1, client.calls)
			require.Equal(t, 1, body.closes)
			require.Equal(t, 1, fs.claims)
			if tc.createErr != nil {
				require.Nil(t, fs.temp)
				require.Empty(t, fs.removed, "a failed claim owns no file to remove")
			} else {
				require.NotNil(t, fs.temp)
				require.Positive(t, fs.temp.writes, "the copy must actually write before the injected failure")
				require.Equal(t, 1, fs.temp.closes)
				require.Equal(t, []string{fs.temp.Name()}, fs.removed, "only the owned staging file may be removed")
				_, statErr := base.Stat(fs.temp.Name())
				require.ErrorIs(t, statErr, os.ErrNotExist)
			}
			for path, want := range map[string]string{dest: "existing canonical bytes", foreign: "unowned staging bytes"} {
				got, readErr := afero.ReadFile(base, path)
				require.NoError(t, readErr)
				require.Equal(t, want, string(got))
			}
			entries, readErr := afero.ReadDir(base, dir)
			require.NoError(t, readErr)
			require.Len(t, entries, 2, "no owned staging, backup, or busy marker should survive")
		})
	}
}

func TestDownloadRetryClassificationForWrappedNetworkErrors(t *testing.T) {
	// DNS errors already satisfy net.Error. Exercise their real wrapping
	// behavior rather than inventing an As hook to bypass that earlier check.
	dnsErr := &net.DNSError{Err: "synthetic resolver failure", Name: "media.invalid", IsTemporary: true}
	opErr := &net.OpError{Op: "read", Net: "tcp", Err: errors.New("synthetic connection reset")}
	for _, err := range []error{dnsErr, opErr, io.ErrUnexpectedEOF} {
		require.True(t, isRetryableError(fmt.Errorf("failed to download: %w", err)))
	}
	for _, err := range []error{nil, os.ErrPermission, io.ErrShortWrite, context.Canceled} {
		require.False(t, isRetryableError(err))
	}
}
