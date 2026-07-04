//go:build !desktop

package desktop

import "testing"

// TestRun_StubErrorWhenNotDesktopTag verifies that in a normal (non-desktop)
// build, Run returns an error because app_stub.go provides a no-op stub.
// This test is excluded from -tags desktop builds where app.go provides the
// real Wails implementation (which would attempt to start a server).
func TestRun_StubErrorWhenNotDesktopTag(t *testing.T) {
	err := Run(Options{ConfigFile: "configs/config.yaml"})
	if err == nil {
		t.Fatal("Run() in a non-desktop build should return an error, got nil")
	}
}
