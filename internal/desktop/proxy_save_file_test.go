package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func doSaveFileRequest(t *testing.T, h http.Handler, filename, body string) (*httptest.ResponseRecorder, saveFileResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/desktop/save-file?filename="+url.QueryEscape(filename), strings.NewReader(body))
	req.Host = "wails.localhost"
	req.Header.Set("Content-Type", "application/octet-stream")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var resp saveFileResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("response body is not JSON: %v", err)
	}
	return w, resp
}

func leftoverTemps(t *testing.T, dir string) []string {
	t.Helper()
	matches, err := filepath.Glob(filepath.Join(dir, ".*.tmp-*"))
	if err != nil {
		t.Fatalf("glob temp leftovers: %v", err)
	}
	return matches
}

func TestSaveFile_Success(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "word-replacements.json")
	// A previous export sits at the destination; a successful write replaces it.
	if err := os.WriteFile(dst, []byte("ORIGINAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	var gotFilename string
	choosePath := func(_ context.Context, filename string) (string, error) {
		gotFilename = filename
		return dst, nil
	}
	h := newReverseProxyHandler("http://127.0.0.1:1", choosePath)

	payload := `[{"original":"foo"}]`
	w, resp := doSaveFileRequest(t, h, "word-replacements.json", payload)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !resp.Saved {
		t.Errorf("saved = false, want true")
	}
	if resp.Path != dst {
		t.Errorf("path = %q, want %q", resp.Path, dst)
	}
	if gotFilename != "word-replacements.json" {
		t.Errorf("choosePath filename = %q, want %q", gotFilename, "word-replacements.json")
	}
	written, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("read written file: %v", err)
	}
	if string(written) != payload {
		t.Errorf("written content = %q, want %q", written, payload)
	}
	if temps := leftoverTemps(t, filepath.Dir(dst)); len(temps) != 0 {
		t.Errorf("temp leftovers after success: %v", temps)
	}
}

// TestSaveFile_ExistingFileSurvivesFailedExport pins the overwrite-atomicity
// contract end to end against the real filesystem: a failed stream must not
// truncate or delete the file the user chose to replace.
func TestSaveFile_ExistingFileSurvivesFailedExport(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "actresses.json")
	if err := os.WriteFile(dst, []byte("ORIGINAL"), 0o600); err != nil {
		t.Fatal(err)
	}
	choosePath := func(context.Context, string) (string, error) { return dst, nil }
	h := newReverseProxyHandler("http://127.0.0.1:1", choosePath)

	req := httptest.NewRequest(http.MethodPost, "/desktop/save-file?filename=actresses.json", errReader{})
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	got, err := os.ReadFile(dst)
	if err != nil {
		t.Fatalf("destination must still exist: %v", err)
	}
	if string(got) != "ORIGINAL" {
		t.Errorf("destination = %q, want the untouched ORIGINAL content", got)
	}
	if temps := leftoverTemps(t, dir); len(temps) != 0 {
		t.Errorf("temp leftovers after failure: %v", temps)
	}
}

func TestSaveFile_Cancelled(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "must-not-exist.json")
	choosePath := func(context.Context, string) (string, error) {
		return "", nil
	}
	h := newReverseProxyHandler("http://127.0.0.1:1", choosePath)

	w, resp := doSaveFileRequest(t, h, "a.json", "[]")

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if resp.Saved {
		t.Errorf("saved = true, want false")
	}
	if resp.Path != "" {
		t.Errorf("path = %q, want empty", resp.Path)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("cancelled dialog must not write any file, stat err = %v", err)
	}
}

func TestSaveFile_DialogError(t *testing.T) {
	choosePath := func(context.Context, string) (string, error) {
		return "", errors.New("disk full")
	}
	h := newReverseProxyHandler("http://127.0.0.1:1", choosePath)

	w, resp := doSaveFileRequest(t, h, "a.json", "[]")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(resp.Error, "disk full") {
		t.Errorf("error = %q, want it to contain %q", resp.Error, "disk full")
	}
}

func TestSaveFile_NilChooseFunc(t *testing.T) {
	h := newReverseProxyHandler("http://127.0.0.1:1", nil)

	w, resp := doSaveFileRequest(t, h, "a.json", "[]")

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if resp.Error == "" {
		t.Errorf("error = %q, want non-empty", resp.Error)
	}
}

func TestSaveFile_TempCreateFailure(t *testing.T) {
	dst := filepath.Join(t.TempDir(), "no-such-dir", "a.json")
	choosePath := func(context.Context, string) (string, error) {
		return dst, nil
	}
	h := newReverseProxyHandler("http://127.0.0.1:1", choosePath)

	w, resp := doSaveFileRequest(t, h, "a.json", "[]")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(resp.Error, "failed to create temp file") {
		t.Errorf("error = %q, want it to contain %q", resp.Error, "failed to create temp file")
	}
}

func TestSaveFile_RejectsBadFilenames(t *testing.T) {
	cases := []struct {
		name string
		file string
	}{
		{"empty", ""},
		{"dot", "."},
		{"dotdot", ".."},
		{"parent traversal", "../evil.json"},
		{"subdir", "dir/file.json"},
		{"absolute posix", "/etc/passwd"},
		{"windows separator", `dir\file.json`},
		{"windows absolute", `C:\\evil\\file.json`},
		{"nul byte", "a\x00b.json"},
		{"newline", "a\nb.json"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			choosePath := func(context.Context, string) (string, error) {
				called = true
				return "", nil
			}
			h := newReverseProxyHandler("http://127.0.0.1:1", choosePath)

			w, resp := doSaveFileRequest(t, h, tc.file, "[]")

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
			if resp.Error == "" {
				t.Errorf("error = %q, want non-empty", resp.Error)
			}
			if called {
				t.Errorf("choosePath must not be called for filename %q", tc.file)
			}
		})
	}
}

func TestSaveFile_BodyAtLimitBoundary(t *testing.T) {
	choosePath := func(context.Context, string) (string, error) {
		return filepath.Join(t.TempDir(), "a.json"), nil
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSaveFileLimit(w, r, choosePath, 16)
	})

	// Exactly maxBytes must succeed; maxBytes+1 must 413.
	at := httptest.NewRecorder()
	h.ServeHTTP(at, httptest.NewRequest(http.MethodPost, "/desktop/save-file?filename=a.json", strings.NewReader(strings.Repeat("x", 16))))
	if at.Code != http.StatusOK {
		t.Errorf("at-limit: status = %d, want %d", at.Code, http.StatusOK)
	}

	over := httptest.NewRecorder()
	h.ServeHTTP(over, httptest.NewRequest(http.MethodPost, "/desktop/save-file?filename=a.json", strings.NewReader(strings.Repeat("x", 17))))
	if over.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("over-limit: status = %d, want %d", over.Code, http.StatusRequestEntityTooLarge)
	}
}

func TestSaveFile_OversizedBody(t *testing.T) {
	dir := t.TempDir()
	dst := filepath.Join(dir, "a.json")
	choosePath := func(context.Context, string) (string, error) {
		return dst, nil
	}
	// Exercise the injectable limit; routing through the full handler would
	// need a 1 GiB body.
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSaveFileLimit(w, r, choosePath, 16)
	})

	req := httptest.NewRequest(http.MethodPost, "/desktop/save-file?filename=a.json", strings.NewReader("this body is far longer than sixteen bytes"))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if w.Code != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want %d", w.Code, http.StatusRequestEntityTooLarge)
	}
	if _, err := os.Stat(dst); !os.IsNotExist(err) {
		t.Errorf("oversized export must never touch the destination, stat err = %v", err)
	}
	if temps := leftoverTemps(t, dir); len(temps) != 0 {
		t.Errorf("temp leftovers after overflow: %v", temps)
	}
}

type errReader struct{}

func (errReader) Read([]byte) (int, error) { return 0, errors.New("client went away") }

type failingWriteCloser struct {
	writeErr error
	syncErr  error
	closeErr error
}

func (f *failingWriteCloser) Write(p []byte) (int, error) {
	if f.writeErr != nil {
		return 0, f.writeErr
	}
	return len(p), nil
}

func (f *failingWriteCloser) Sync() error { return f.syncErr }

func (f *failingWriteCloser) Close() error { return f.closeErr }

type fsFailure struct {
	createTempErr error
	writeErr      error
	syncErr       error
	closeErr      error
	removeErr     error
	renameErr     error
}

func fsForFailure(ff fsFailure, removed *[]string) saveFileFS {
	return saveFileFS{
		createTemp: func(dir, pattern string) (io.WriteCloser, string, error) {
			if ff.createTempErr != nil {
				return nil, "", ff.createTempErr
			}
			return &failingWriteCloser{writeErr: ff.writeErr, syncErr: ff.syncErr, closeErr: ff.closeErr}, filepath.Join(dir, ".tmp-test"), nil
		},
		rename: func(string, string) error {
			return ff.renameErr
		},
		remove: func(name string) error {
			*removed = append(*removed, name)
			return ff.removeErr
		},
	}
}

func doSaveFileRequestFS(t *testing.T, fs saveFileFS, body string) (*httptest.ResponseRecorder, saveFileResponse) {
	t.Helper()
	choosePath := func(context.Context, string) (string, error) {
		return filepath.Join(t.TempDir(), "a.json"), nil
	}
	h := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handleSaveFileFS(w, r, choosePath, 64, fs)
	})
	req := httptest.NewRequest(http.MethodPost, "/desktop/save-file?filename=a.json", strings.NewReader(body))
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var resp saveFileResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("response body is not JSON: %v", err)
	}
	return w, resp
}

func TestSaveFileFS_CreateTempError(t *testing.T) {
	removed := []string{}
	fs := fsForFailure(fsFailure{createTempErr: errors.New("perm denied")}, &removed)

	w, resp := doSaveFileRequestFS(t, fs, "[]")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(resp.Error, "failed to create temp file") {
		t.Errorf("error = %q, want it to contain %q", resp.Error, "failed to create temp file")
	}
	if len(removed) != 0 {
		t.Errorf("nothing to clean up when temp creation fails, removed = %v", removed)
	}
}

func TestSaveFileFS_WriteFailure(t *testing.T) {
	removed := []string{}
	fs := fsForFailure(fsFailure{writeErr: errors.New("disk io")}, &removed)

	w, resp := doSaveFileRequestFS(t, fs, "[]")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(resp.Error, "failed to write") {
		t.Errorf("error = %q, want it to contain %q", resp.Error, "failed to write")
	}
	if len(removed) != 1 {
		t.Errorf("partial temp must be removed after a write failure, removed = %v", removed)
	}
}

func TestSaveFileFS_FinalizeFailureClose(t *testing.T) {
	removed := []string{}
	fs := fsForFailure(fsFailure{closeErr: errors.New("flush failed")}, &removed)

	w, resp := doSaveFileRequestFS(t, fs, "[]")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(resp.Error, "failed to finalize") {
		t.Errorf("error = %q, want it to contain %q", resp.Error, "failed to finalize")
	}
	if len(removed) != 1 {
		t.Errorf("partial temp must be removed after a finalize failure, removed = %v", removed)
	}
}

func TestSaveFileFS_FinalizeFailureSync(t *testing.T) {
	removed := []string{}
	fs := fsForFailure(fsFailure{syncErr: errors.New("fsync failed")}, &removed)

	_, resp := doSaveFileRequestFS(t, fs, "[]")

	if !strings.Contains(resp.Error, "failed to finalize") {
		t.Errorf("error = %q, want sync failure to surface as finalize failure", resp.Error)
	}
}

func TestSaveFileFS_WriteAndFinalizeFailure(t *testing.T) {
	removed := []string{}
	fs := fsForFailure(fsFailure{writeErr: errors.New("disk io"), closeErr: errors.New("flush failed")}, &removed)

	_, resp := doSaveFileRequestFS(t, fs, "[]")

	if !strings.Contains(resp.Error, "failed to write") {
		t.Errorf("error = %q, want it to contain %q", resp.Error, "failed to write")
	}
	if !strings.Contains(resp.Error, "; also failed to finalize") {
		t.Errorf("error = %q, want it to also report the close failure", resp.Error)
	}
}

func TestSaveFileFS_RemoveFailureSurfaced(t *testing.T) {
	removed := []string{}
	fs := fsForFailure(fsFailure{writeErr: errors.New("disk io"), removeErr: errors.New("file locked")}, &removed)

	_, resp := doSaveFileRequestFS(t, fs, "[]")

	if len(removed) != 1 {
		t.Errorf("cleanup must be attempted after a write failure, removed = %v", removed)
	}
	if !strings.Contains(resp.Error, "also failed to remove partial file") {
		t.Errorf("error = %q, want it to surface the cleanup failure", resp.Error)
	}
}

func TestSaveFileFS_RenameFailure(t *testing.T) {
	removed := []string{}
	fs := fsForFailure(fsFailure{renameErr: errors.New("target busy")}, &removed)

	w, resp := doSaveFileRequestFS(t, fs, "[]")

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(resp.Error, "failed to move export into place") {
		t.Errorf("error = %q, want it to contain %q", resp.Error, "failed to move export into place")
	}
	if len(removed) != 1 {
		t.Errorf("temp must be removed when rename fails, removed = %v", removed)
	}
}

func TestSaveFileFS_RenameAndRemoveFailure(t *testing.T) {
	removed := []string{}
	fs := fsForFailure(fsFailure{renameErr: errors.New("target busy"), removeErr: errors.New("locked")}, &removed)

	_, resp := doSaveFileRequestFS(t, fs, "[]")

	if !strings.Contains(resp.Error, "also failed to remove partial file") {
		t.Errorf("error = %q, want rename-failure removal problems surfaced too", resp.Error)
	}
}

func TestSaveFile_GetIsProxiedNotHandled(t *testing.T) {
	backendHit := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendHit = true
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	choosePath := func(context.Context, string) (string, error) {
		return "x", nil
	}
	h := newReverseProxyHandler(backend.URL, choosePath)

	req := httptest.NewRequest(http.MethodGet, "/desktop/save-file", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !backendHit {
		t.Errorf("GET /desktop/save-file must be proxied to the backend, not intercepted")
	}
}
