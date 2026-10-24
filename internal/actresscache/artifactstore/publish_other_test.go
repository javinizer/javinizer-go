//go:build !darwin && !linux

package artifactstore

import (
	"errors"
	"os"
	"testing"
)

func TestWriterUnsupported(t *testing.T) {
	s, path := openStore(t)
	_, r := evidence(t, "Name")
	reads := 0
	input := readerFunc(func([]byte) (int, error) {
		reads++
		return 0, errors.New("unsupported writer must not read input")
	})
	if result, err := s.Publish(t.Context(), input, r); err == nil || result.CommitAttempted || result.Published {
		t.Fatalf("unsupported writer %+v %v", result, err)
	}
	if reads != 0 {
		t.Fatalf("unsupported writer read input %d times", reads)
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatalf("unsupported writer created directory entries: %v %v", entries, err)
	}
}

func TestUnsupportedPrimitivesFailClosed(t *testing.T) {
	s, path := openStore(t)
	if err := syncDirectory(s.root); err == nil {
		t.Fatal("unsupported directory sync")
	}
	if err := replaceManifest(s.root, "absent", "current.json"); err == nil {
		t.Fatal("unsupported replacement")
	}
	if err := lockHandle(nil); err == nil {
		t.Fatal("unsupported native lock")
	}
	entries, err := os.ReadDir(path)
	if err != nil || len(entries) != 0 {
		t.Fatal("unsupported primitives mutated directory")
	}
}
