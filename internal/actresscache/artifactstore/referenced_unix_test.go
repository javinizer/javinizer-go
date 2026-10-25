//go:build darwin || linux

package artifactstore

import (
	"os"
	"strings"
	"testing"
)

func TestLoadReferencedSnapshotRejectsReplacementWithoutRetry(t *testing.T) {
	for _, afterOpen := range []bool{false, true} {
		t.Run(map[bool]string{false: "before-open", true: "after-open"}[afterOpen], func(t *testing.T) {
			store, path := openStore(t)
			data, _ := evidence(t, "Pinned")
			digest := writeReferencedObject(t, path, data)
			native := store.ops.openRead
			calls := 0
			store.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
				calls++
				var pinned *os.File
				var err error
				if afterOpen {
					pinned, err = native(root, name)
					if err != nil {
						return nil, err
					}
				}
				if err := root.Rename(name, ".stage-saved"); err != nil {
					t.Fatal(err)
				}
				if err := root.WriteFile(name, data, 0600); err != nil {
					t.Fatal(err)
				}
				if pinned != nil {
					return pinned, nil
				}
				return native(root, name)
			}
			_, err := store.LoadReferencedSnapshot(t.Context(), digest)
			if err == nil || calls != 1 || (!strings.Contains(err.Error(), "identity") && !strings.Contains(err.Error(), "replaced")) {
				t.Fatalf("replaced immutable object was accepted/retried: calls=%d err=%v", calls, err)
			}
		})
	}
}
