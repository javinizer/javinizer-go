package downloader

import (
	"errors"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

// errOpenFS fails Open on the given path prefix (fault injection for the
// validateDownloadedMedia read path).
type errOpenFS struct {
	afero.Fs
	err error
}

func (f errOpenFS) Open(name string) (afero.File, error) { return nil, f.err }

// errReadFile fails Read (fault injection for the short-read path).
type errReadFile struct {
	afero.File
	err error
}

func (f errReadFile) Read([]byte) (int, error) { return 0, f.err }

type openErrReadFS struct{ afero.Fs }

func (fsx openErrReadFS) Open(name string) (afero.File, error) {
	return errReadFile{File: mustBaseFile(fsx.Fs, name), err: errors.New("simulated read failure")}, nil
}

func mustBaseFile(fs afero.Fs, name string) afero.File {
	f, err := fs.Open(name)
	if err != nil {
		panic(err)
	}
	return f
}

func TestValidateDownloadedMedia(t *testing.T) {
	t.Parallel()

	writeTmp := func(fs afero.Fs, body string) string {
		t.Helper()
		require.NoError(t, afero.WriteFile(fs, "/tmp-dl", []byte(body), 0o644))
		return "/tmp-dl"
	}

	cases := []struct {
		name        string
		contentType string
		body        string
		dest        string
		wantErr     bool
	}{
		{"declared text/html", "text/html; charset=utf-8", "image-ish", "/out/cover.jpg", true},
		{"declared text/plain prose", "text/plain", "rate limit exceeded", "/out/cover.jpg", true},
		{"declared application/json", "application/json", "image-ish", "/out/cover.jpg", true},
		{"declared application/xml", "application/xml", "image-ish", "/out/cover.jpg", true},
		{"declared text/xml", "text/xml", "image-ish", "/out/cover.jpg", true},
		{"declared +xml suffix", "application/atom+xml", "image-ish", "/out/cover.jpg", true},
		{"html doctype body", "application/octet-stream", "<!DOCTYPE html><html></html>", "/out/cover.jpg", true},
		{"bare html tag body", "application/octet-stream", "<html><body>blocked</body></html>", "/out/cover.jpg", true},
		{"head tag body", "application/octet-stream", "<head><title>err</title></head>", "/out/cover.jpg", true},
		{"xml declaration body", "application/octet-stream", "<?xml version='1.0'?><Error/>", "/out/cover.jpg", true},
		{"s3-style error body", "application/octet-stream", "<Error><Code>Denied</Code></Error>", "/out/cover.jpg", true},
		{"response wrapper body", "application/octet-stream", "<Response><Status>403</Status></Response>", "/out/cover.jpg", true},
		{"json object body", "", `{"error":"nope"}`, "/out/cover.jpg", true},
		{"opaque binary passes", "", "\xff\xd8\xff\xe0jpeg-bytes", "/out/cover.jpg", false},
		{"unknown extension passes", "", "rate limit exceeded", "/out/payload.dat", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			fs := afero.NewMemMapFs()
			tmp := writeTmp(fs, tc.body)
			info, handle, err := validateDownloadedMedia(fs, tmp, tc.contentType, tc.dest)
			if handle != nil {
				defer func() { _ = handle.Close() }()
			}
			if tc.wantErr {
				require.Error(t, err)
				require.Nil(t, handle, "every refusal closes and returns no handle (wave-48)")
				require.Nil(t, info)
			} else {
				require.NoError(t, err)
				require.NotNil(t, handle, "acceptance hands the validated object's handle back OPEN (wave-48)")
				require.NotNil(t, info)
			}
		})
	}

	t.Run("open failure surfaces as error", func(t *testing.T) {
		t.Parallel()
		_, _, err := validateDownloadedMedia(errOpenFS{Fs: afero.NewMemMapFs(), err: errors.New("boom")}, "/gone", "", "/out/cover.jpg")
		require.Error(t, err)
	})

	t.Run("read failure surfaces as error", func(t *testing.T) {
		t.Parallel()
		fs := afero.NewMemMapFs()
		tmp := writeTmp(fs, "opaque")
		_, _, err := validateDownloadedMedia(openErrReadFS{Fs: fs}, tmp, "", "/out/cover.jpg")
		require.Error(t, err)
	})
}
