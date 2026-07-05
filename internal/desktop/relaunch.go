//go:build desktop

package desktop

import (
	"context"
	"errors"
	"sync"

	"github.com/javinizer/javinizer-go/internal/logging"
	"github.com/javinizer/javinizer-go/internal/updater"
	wailsruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// wailsRelauncher shuts the running desktop app down so the detached bundle
// swap helper (spawned by the updater Engine) can complete the file swap and
// relaunch the new bundle. It wraps wails runtime.Quit, which is thread-safe
// (sync.Once in the wails application) but requires the OnStartup context —
// any other ctx makes wails' getFrontend log.Fatalf (pkg/runtime/runtime.go).
//
// The ctx is captured in OnStartup (app.go) after the Engine is constructed,
// so it is stored behind a lock; Relaunch reads it under the read lock.
type wailsRelauncher struct {
	mu  sync.RWMutex
	ctx context.Context
}

func (r *wailsRelauncher) setContext(ctx context.Context) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.ctx = ctx
}

// Relaunch quits the wails app using the captured OnStartup context. The
// passed ctx (the API request context) is intentionally ignored: only the
// OnStartup ctx carries the frontend handle runtime.Quit needs.
func (r *wailsRelauncher) Relaunch(_ context.Context) error {
	r.mu.RLock()
	wctx := r.ctx
	r.mu.RUnlock()
	if wctx == nil {
		logging.Errorf("desktop: relaunch requested before wails startup context was captured")
		return errors.New("desktop: wails startup context not captured")
	}
	wailsruntime.Quit(wctx)
	return nil
}

var _ updater.Relauncher = (*wailsRelauncher)(nil)
