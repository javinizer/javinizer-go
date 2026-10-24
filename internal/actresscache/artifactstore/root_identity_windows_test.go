//go:build windows

package artifactstore

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestHandleRootPathNamespaces(t *testing.T) {
	long := strings.Repeat("component-", 30)
	for _, test := range []struct{ path, want string }{
		{`C:\short`, `C:\short`},
		{`C:\` + long, `\\?\C:\` + long},
		{`\\server\share\` + long, `\\?\UNC\server\share\` + long},
		{`\\?\C:\` + long, `\\?\C:\` + long},
		{`\\?\UNC\server\share\` + long, `\\?\UNC\server\share\` + long},
		{`\??\C:\` + long, `\??\C:\` + long},
		{`\\.\C:\` + long, `\\.\C:\` + long},
	} {
		if got := handleRootPath(test.path); got != test.want {
			t.Fatalf("path namespace changed: got%q want%q", got, test.want)
		}
	}
}

func TestWindowsLongRootIdentity(t *testing.T) {
	path := filepath.Join(t.TempDir(), strings.Repeat("long-root-", 12), strings.Repeat("segment-", 15), "root")
	if len(path) <= 260 {
		t.Fatal("fixture did not exceed MAX_PATH")
	}
	if err := os.MkdirAll(path, 0700); err != nil {
		t.Fatalf("create long-root fixture: %v", err)
	}
	before, err := snapshotRoot(path)
	if err != nil {
		t.Fatalf("snapshot long root: %v", err)
	}
	store, err := Open(path, limits())
	if store != nil {
		t.Cleanup(func() { _ = store.Close() })
	}
	if err != nil {
		t.Fatalf("open long root: %v", err)
	}
	after, err := store.root.Stat(".")
	if err != nil {
		t.Fatal(err)
	}
	if !os.SameFile(before, after) {
		t.Fatal("long-root handle identity mismatch")
	}
}
