//go:build !darwin && !linux

package artifactstore

import (
	"os"
	"testing"
)

func TestWriterUnsupported(t *testing.T) {
	s, _ := openStore(t)
	_, r := evidence(t, "Name")
	if result, err := s.Publish(t.Context(), nil, r); err == nil || result.Published {
		t.Fatalf("unsupported writer %+v %v", result, err)
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
