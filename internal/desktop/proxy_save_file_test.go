package desktop

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

type saveFileCall struct {
	filename string
	content  []byte
}

func doSaveFileRequest(t *testing.T, h http.Handler, body string) (*httptest.ResponseRecorder, saveFileResponse) {
	t.Helper()
	req := httptest.NewRequest(http.MethodPost, "/desktop/save-file", strings.NewReader(body))
	req.Host = "wails.localhost"
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)
	var resp saveFileResponse
	if err := json.NewDecoder(w.Result().Body).Decode(&resp); err != nil {
		t.Fatalf("response body is not JSON: %v", err)
	}
	return w, resp
}

func TestSaveFile_Success(t *testing.T) {
	var calls []saveFileCall
	saveFile := func(_ context.Context, filename string, content []byte) (string, error) {
		calls = append(calls, saveFileCall{filename: filename, content: content})
		return "/Users/test/Downloads/word-replacements.json", nil
	}
	h := newReverseProxyHandler("http://127.0.0.1:1", saveFile)

	w, resp := doSaveFileRequest(t, h, `{"filename":"word-replacements.json","content":"[{\"original\":\"foo\"}]"}`)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if !resp.Saved {
		t.Errorf("saved = false, want true")
	}
	if resp.Path != "/Users/test/Downloads/word-replacements.json" {
		t.Errorf("path = %q, want %q", resp.Path, "/Users/test/Downloads/word-replacements.json")
	}
	if len(calls) != 1 {
		t.Fatalf("saveFile called %d times, want 1", len(calls))
	}
	if calls[0].filename != "word-replacements.json" {
		t.Errorf("filename = %q, want %q", calls[0].filename, "word-replacements.json")
	}
	if string(calls[0].content) != `[{"original":"foo"}]` {
		t.Errorf("content = %q, want %q", calls[0].content, `[{"original":"foo"}]`)
	}
}

func TestSaveFile_Cancelled(t *testing.T) {
	saveFile := func(_ context.Context, _ string, _ []byte) (string, error) {
		return "", nil
	}
	h := newReverseProxyHandler("http://127.0.0.1:1", saveFile)

	w, resp := doSaveFileRequest(t, h, `{"filename":"a.json","content":"[]"}`)

	if w.Code != http.StatusOK {
		t.Errorf("status = %d, want %d", w.Code, http.StatusOK)
	}
	if resp.Saved {
		t.Errorf("saved = true, want false")
	}
	if resp.Path != "" {
		t.Errorf("path = %q, want empty", resp.Path)
	}
}

func TestSaveFile_DialogError(t *testing.T) {
	saveFile := func(_ context.Context, _ string, _ []byte) (string, error) {
		return "", errors.New("disk full")
	}
	h := newReverseProxyHandler("http://127.0.0.1:1", saveFile)

	w, resp := doSaveFileRequest(t, h, `{"filename":"a.json","content":"[]"}`)

	if w.Code != http.StatusInternalServerError {
		t.Errorf("status = %d, want %d", w.Code, http.StatusInternalServerError)
	}
	if !strings.Contains(resp.Error, "disk full") {
		t.Errorf("error = %q, want it to contain %q", resp.Error, "disk full")
	}
}

func TestSaveFile_NilSaveFunc(t *testing.T) {
	h := newReverseProxyHandler("http://127.0.0.1:1", nil)

	w, resp := doSaveFileRequest(t, h, `{"filename":"a.json","content":"[]"}`)

	if w.Code != http.StatusNotFound {
		t.Errorf("status = %d, want %d", w.Code, http.StatusNotFound)
	}
	if resp.Error == "" {
		t.Errorf("error = %q, want non-empty", resp.Error)
	}
}

func TestSaveFile_InvalidJSON(t *testing.T) {
	h := newReverseProxyHandler("http://127.0.0.1:1", func(context.Context, string, []byte) (string, error) {
		return "", nil
	})

	w, _ := doSaveFileRequest(t, h, `{"filename":`)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
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
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			called := false
			saveFile := func(context.Context, string, []byte) (string, error) {
				called = true
				return "", nil
			}
			h := newReverseProxyHandler("http://127.0.0.1:1", saveFile)
			body := fmt.Sprintf(`{"filename":%q,"content":"[]"}`, tc.file)

			w, resp := doSaveFileRequest(t, h, body)

			if w.Code != http.StatusBadRequest {
				t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
			}
			if resp.Error == "" {
				t.Errorf("error = %q, want non-empty", resp.Error)
			}
			if called {
				t.Errorf("saveFile must not be called for filename %q", tc.file)
			}
		})
	}
}

func TestSaveFile_OversizedBody(t *testing.T) {
	saveFile := func(context.Context, string, []byte) (string, error) {
		return "", nil
	}
	h := newReverseProxyHandler("http://127.0.0.1:1", saveFile)
	body := `{"filename":"a.json","content":"` + strings.Repeat(" ", maxSaveFileBytes) + `"}`

	w, _ := doSaveFileRequest(t, h, body)

	if w.Code != http.StatusBadRequest {
		t.Errorf("status = %d, want %d", w.Code, http.StatusBadRequest)
	}
}

func TestSaveFile_GetIsProxiedNotHandled(t *testing.T) {
	backendHit := false
	backend := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		backendHit = true
		_, _ = w.Write([]byte("ok"))
	}))
	defer backend.Close()

	saveFile := func(context.Context, string, []byte) (string, error) {
		return "x", nil
	}
	h := newReverseProxyHandler(backend.URL, saveFile)

	req := httptest.NewRequest(http.MethodGet, "/desktop/save-file", nil)
	w := httptest.NewRecorder()
	h.ServeHTTP(w, req)

	if !backendHit {
		t.Errorf("GET /desktop/save-file must be proxied to the backend, not intercepted")
	}
}
