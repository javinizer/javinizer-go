//go:build desktop

package desktop

import (
	"context"
	"errors"
	"fmt"
	"sync"

	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// wailsSaveFileDialog implements ChooseSavePathFunc via
// wailsruntime.SaveFileDialog. Like wailsRelauncher it must use the OnStartup
// context — any other ctx makes wails' runtime lookups fatal — so the ctx is
// captured in OnStartup (app.go) and stored behind a lock because
// ChooseSavePath runs on HTTP handler goroutines. Writing the file is the
// handler's job (proxy.go streams the request body to the chosen path).
type wailsSaveFileDialog struct {
	mu  sync.RWMutex
	ctx context.Context
}

func (s *wailsSaveFileDialog) setContext(ctx context.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ctx = ctx
}

// ChooseSavePath shows the native save dialog and returns the chosen
// destination path; ("", nil) means the user cancelled. The request ctx is
// intentionally ignored — only the OnStartup ctx carries the frontend handle.
func (s *wailsSaveFileDialog) ChooseSavePath(_ context.Context, filename string) (string, error) {
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
	return path, nil
}
