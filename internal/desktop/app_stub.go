//go:build !desktop

package desktop

import "errors"

// Run is the no-op stub used when the binary is built without the `desktop`
// tag (the normal CLI/test build). The real implementation lives in app.go
// (//go:build desktop) and pulls in the wails dependency, which requires
// per-platform webview headers and must not enter the default build/test path.
func Run(opts Options) error {
	return errors.New("desktop mode is not built into this binary; rebuild with -tags desktop")
}
