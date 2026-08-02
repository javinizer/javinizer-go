//go:build desktop

package desktop

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// wailsSaveFileDialog implements SaveFileFunc via wailsruntime.SaveFileDialog.
// Like wailsRelauncher it must use the OnStartup context — any other ctx makes
// wails' runtime lookups fatal — so the ctx is captured in OnStartup (app.go)
// and stored behind a lock because SaveFile runs on HTTP handler goroutines.
type wailsSaveFileDialog struct {
	mu  sync.RWMutex
	ctx context.Context
}

func (s *wailsSaveFileDialog) setContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
}

// SaveFile shows the native save dialog and writes content to the chosen
// path. An empty path with nil error means the user cancelled the dialog.
// The request ctx is intentionally ignored; only the OnStartup ctx works.
func (s *wailsSaveFileDialog) SaveFile(_ context.Context, filename string, content []byte) (string, error) {
	s.mu.RLock()
	wctx := s.ctx
	s.mu.RUnlock()
	if wctx == nil {
		return "", errors.New("desktop: wails startup context not captured")
	}
	path, err := wailsruntime.SaveFileDialog(wctx, wailsruntime.SaveDialogOptions{
		Title:           "Save export",
		DefaultFilename: filename,
		Filters: []wailsruntime.FileFilter{
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
		},
	})
	if err != nil {
		return "", fmt.Errorf("desktop: save dialog failed: %w", err)
	}
	if path == "" {
		return "", nil
	}
	if err := os.WriteFile(path, content, 0o644); err != nil {
		return "", fmt.Errorf("desktop: failed to write %s: %w", path, err)
	}
	return path, nil
}
