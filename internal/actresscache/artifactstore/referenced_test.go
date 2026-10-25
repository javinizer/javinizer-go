package artifactstore

import (
	"bytes"
	"context"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/actresscache"
)

func writeReferencedObject(t *testing.T, path string, data []byte) string {
	t.Helper()
	digest := fmt.Sprintf("%x", sha256.Sum256(data))
	if err := os.WriteFile(filepath.Join(path, digest+".ndjson"), data, 0600); err != nil {
		t.Fatal(err)
	}
	return digest
}

func TestLoadReferencedSnapshotUsesOnlyAdmittedDigest(t *testing.T) {
	store, path := openStore(t)
	data, _ := evidence(t, "Pinned old generation")
	digest := writeReferencedObject(t, path, data)
	newData, receipt := evidence(t, "Current is not the requested generation")
	writeSlot(t, path, currentManifest, newData, receipt)
	if err := os.WriteFile(filepath.Join(path, previousManifest), []byte("corrupt manifest"), 0600); err != nil {
		t.Fatal(err)
	}
	// No scan, current/previous read or lock creation is needed. Even after a
	// pinned object rotates out of both manifest slots, its exact job reference
	// remains usable. The test stands in for that already admitted job reference.
	native := store.ops.openRead
	calls := 0
	store.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
		calls++
		if name != digest+".ndjson" {
			t.Fatalf("read outside exact durable-job reference: %s", name)
		}
		return native(root, name)
	}
	store.ops.readDir = func(*os.File, int) ([]os.DirEntry, error) {
		t.Fatal("referenced load scanned the directory")
		return nil, nil
	}
	store.ops.openFile = func(*os.Root, string, int, os.FileMode) (*os.File, error) {
		t.Fatal("referenced load attempted a write")
		return nil, nil
	}
	snapshot, err := store.LoadReferencedSnapshot(t.Context(), digest)
	if err != nil || snapshot == nil || snapshot.Digest() != digest || !bytes.Equal(snapshot.Bytes(), data) || calls != 1 {
		t.Fatalf("referenced load: snapshot=%v reads=%d err=%v", snapshot, calls, err)
	}
	if _, err := os.Stat(filepath.Join(path, ".writer.lock")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("reader created lock: %v", err)
	}
}

func TestLoadReferencedSnapshotRejectsInvalidDigestBeforeIO(t *testing.T) {
	store, _ := openStore(t)
	store.ops.openRead = func(*os.Root, string) (*os.File, error) {
		t.Fatal("invalid digest opened a file")
		return nil, nil
	}
	for _, digest := range []string{"", "a", strings.Repeat("0", 63), strings.Repeat("0", 65), strings.Repeat("A", 64), strings.Repeat("g", 64), "../" + strings.Repeat("0", 61), strings.Repeat("0", 64) + ".ndjson"} {
		if snapshot, err := store.LoadReferencedSnapshot(t.Context(), digest); err == nil || snapshot != nil {
			t.Fatalf("invalid digest accepted: %q", digest)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := store.LoadReferencedSnapshot(ctx, strings.Repeat("0", 64)); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestLoadReferencedSnapshotFailsClosed(t *testing.T) {
	for _, kind := range []string{"missing", "wrong-digest", "malformed", "noncanonical", "other-source", "directory", "symlink", "oversized", "empty"} {
		t.Run(kind, func(t *testing.T) {
			store, path := openStore(t)
			data, receipt := evidence(t, "Current")
			// A valid current generation must not rescue a bad pinned object.
			writeSlot(t, path, currentManifest, data, receipt)
			digest := strings.Repeat("0", 64)
			switch kind {
			case "missing":
			case "wrong-digest":
				if err := os.WriteFile(filepath.Join(path, digest+".ndjson"), data, 0600); err != nil {
					t.Fatal(err)
				}
			case "malformed":
				digest = writeReferencedObject(t, path, []byte("malformed\n"))
			case "noncanonical":
				digest = writeReferencedObject(t, path, append([]byte(" "), data...))
			case "other-source":
				snapshot, err := actresscache.Decode(t.Context(), bytes.NewReader(data), limits().Artifact)
				if err != nil {
					t.Fatal(err)
				}
				records := snapshot.Records()
				records[0].Source = "other"
				snapshot, err = actresscache.Build(t.Context(), records, limits().Artifact)
				if err != nil {
					t.Fatal(err)
				}
				digest = writeReferencedObject(t, path, snapshot.Bytes())
			case "directory":
				if err := os.Mkdir(filepath.Join(path, digest+".ndjson"), 0700); err != nil {
					t.Fatal(err)
				}
			case "symlink":
				if err := os.Symlink(filepath.Join(path, currentManifest), filepath.Join(path, digest+".ndjson")); err != nil {
					t.Skipf("symlinks unavailable: %v", err)
				}
			case "oversized":
				digest = writeReferencedObject(t, path, data)
				store.limits.Artifact.MaxBytes = int64(len(data) - 1)
			case "empty":
				snapshot, err := actresscache.Build(t.Context(), nil, limits().Artifact)
				if err != nil {
					t.Fatal(err)
				}
				digest = writeReferencedObject(t, path, snapshot.Bytes())
			}
			snapshot, err := store.LoadReferencedSnapshot(t.Context(), digest)
			if kind == "empty" {
				if err != nil || snapshot == nil || len(snapshot.Records()) != 0 {
					t.Fatalf("explicit empty object rejected: %v", err)
				}
			} else if err == nil || snapshot != nil {
				t.Fatalf("bad reference accepted: %v", snapshot)
			}
			if kind == "missing" && !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("missing reference did not fail missing: %v", err)
			}
		})
	}
}

func TestLoadReferencedSnapshotPreservesReadError(t *testing.T) {
	store, path := openStore(t)
	data, _ := evidence(t, "Old")
	digest := writeReferencedObject(t, path, data)
	sentinel := errors.New("object unavailable")
	calls := 0
	store.ops.openRead = func(*os.Root, string) (*os.File, error) {
		calls++
		return nil, sentinel
	}
	if _, err := store.LoadReferencedSnapshot(t.Context(), digest); !errors.Is(err, sentinel) || calls != 1 {
		t.Fatalf("read error changed or retried: calls=%d err=%v", calls, err)
	}
}
