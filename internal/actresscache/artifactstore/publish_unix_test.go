//go:build darwin || linux

package artifactstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func publish(t *testing.T, s *Store, name string) Result {
	t.Helper()
	data, r := evidence(t, name)
	result, err := s.Publish(t.Context(), bytes.NewReader(data), r)
	if err != nil {
		t.Fatal(err)
	}
	if !result.Published || !result.FilesSynced || !result.DirectorySynced {
		t.Fatal(result)
	}
	return result
}
func TestPublishReloadAndLastGood(t *testing.T) {
	s, path := openStore(t)
	publish(t, s, "First")
	first, err := s.Load(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	publish(t, s, "Second")
	second, err := s.Load(t.Context())
	if err != nil || second.Manifest.ArtifactSHA256 == first.Manifest.ArtifactSHA256 {
		t.Fatalf("replacement %v", err)
	}
	publish(t, s, "Second") // Reuses immutable object, with a real file Sync.
	reopened, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	defer reopened.Close()
	if _, err := reopened.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(path, "current.json"), []byte("interrupted/corrupt"), 0600); err != nil {
		t.Fatal(err)
	}
	recovered, err := s.Load(t.Context())
	if err != nil || !recovered.Recovered {
		t.Fatalf("recovery %v", err)
	}
	publish(t, s, "Third")
	data, r := evidence(t, "Third")
	if err := os.WriteFile(filepath.Join(path, recovered.Manifest.ArtifactSHA256+".ndjson"), []byte("bad"), 0600); err != nil {
		t.Fatal(err)
	}
	if result, err := s.Publish(t.Context(), bytes.NewReader(data), r); err != nil || !result.Published {
		t.Fatalf("valid current despite damaged old %v", err)
	}
}
func TestFailureStagesPreserveLastGood(t *testing.T) {
	for _, phase := range []string{"write", "short-write", "file-sync-1", "file-sync-2", "file-sync-3", "close", "link", "replace-1", "replace-2", "dir-sync-1", "dir-sync-2", "dir-sync-3"} {
		t.Run(phase, func(t *testing.T) {
			s, _ := openStore(t)
			publish(t, s, "First")
			before, _ := s.Load(t.Context())
			data, r := evidence(t, "Second")
			sentinel := errors.New("injected " + phase)
			syncCount, replaceCount, dirCount := 0, 0, 0
			original := s.ops
			s.ops.write = func(f *os.File, data []byte) (int, error) {
				if phase == "write" {
					return 0, sentinel
				}
				if phase == "short-write" {
					return 0, nil
				}
				return original.write(f, data)
			}
			s.ops.syncFile = func(f *os.File) error {
				syncCount++
				if phase == fmt.Sprintf("file-sync-%d", syncCount) {
					return sentinel
				}
				return original.syncFile(f)
			}
			s.ops.closeFile = func(f *os.File) error {
				if phase == "close" {
					return sentinel
				}
				return original.closeFile(f)
			}
			s.ops.link = func(root *os.Root, a, b string) error {
				if phase == "link" {
					return sentinel
				}
				return original.link(root, a, b)
			}
			s.ops.replace = func(root *os.Root, a, b string) error {
				replaceCount++
				if phase == fmt.Sprintf("replace-%d", replaceCount) {
					return sentinel
				}
				return original.replace(root, a, b)
			}
			s.ops.syncDir = func(root *os.Root) error {
				dirCount++
				if phase == fmt.Sprintf("dir-sync-%d", dirCount) {
					return sentinel
				}
				return original.syncDir(root)
			}
			result, err := s.Publish(t.Context(), bytes.NewReader(data), r)
			if phase == "short-write" {
				sentinel = io.ErrShortWrite
			}
			if !errors.Is(err, sentinel) {
				t.Fatalf("wrong failure %v", err)
			}
			after, err := s.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if phase == "dir-sync-3" {
				if !result.Published || result.DirectorySynced || after.Manifest.ArtifactSHA256 == before.Manifest.ArtifactSHA256 {
					t.Fatalf("postvisibility %+v", result)
				}
			} else if result.Published || after.Manifest != before.Manifest {
				t.Fatalf("last good lost: %+v %v", result, err)
			}
		})
	}
}
func TestSingleWriterAndCancellation(t *testing.T) {
	s, path := openStore(t)
	publish(t, s, "First")
	other, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	defer other.Close()
	entered, release := make(chan struct{}), make(chan struct{})
	original := s.ops.syncFile
	var once sync.Once
	s.ops.syncFile = func(f *os.File) error { once.Do(func() { close(entered); <-release }); return original(f) }
	data, r := evidence(t, "Second")
	done := make(chan error, 1)
	go func() { _, err := s.Publish(context.Background(), bytes.NewReader(data), r); done <- err }()
	select {
	case <-entered:
	case <-time.After(5 * time.Second):
		t.Fatal("writer did not enter")
	}
	for _, target := range []*Store{s, other} {
		if _, err := target.Publish(t.Context(), nil, r); !errors.Is(err, ErrBusy) {
			t.Fatalf("writer admission %v", err)
		}
	}
	if _, err := s.Load(t.Context()); err != nil {
		t.Fatal(err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Publish(ctx, nil, r); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := s.Publish(t.Context(), nil, r); err == nil {
		t.Fatal("nil input")
	}
	// Cancel while the final current stage is synced, before its rename.
	s.ops = defaultOperations()
	ctx, cancel = context.WithCancel(t.Context())
	count := 0
	s.ops.syncFile = func(f *os.File) error {
		count++
		err := f.Sync()
		if count == 3 {
			cancel()
		}
		return err
	}
	data, r = evidence(t, "Third")
	before, _ := s.Load(t.Context())
	result, err := s.Publish(ctx, bytes.NewReader(data), r)
	if !errors.Is(err, context.Canceled) || result.Published {
		t.Fatalf("cancel before visibility %+v %v", result, err)
	}
	after, err := s.Load(t.Context())
	if err != nil || after.Manifest != before.Manifest {
		t.Fatal("cancel replaced last good")
	}
}
func TestUnsafeEntriesAndCapacity(t *testing.T) {
	for _, target := range []string{"current.json", ".writer.lock", "artifact"} {
		t.Run(target, func(t *testing.T) {
			s, path := openStore(t)
			data, r := evidence(t, "Name")
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
				t.Fatal(err)
			}
			name := target
			if target == "artifact" {
				snapshot, err := s.validate(t.Context(), data, r)
				if err != nil {
					t.Fatal(err)
				}
				name = snapshot.Digest() + ".ndjson"
			}
			if err := os.Symlink(outside, filepath.Join(path, name)); err != nil {
				t.Fatal(err)
			}
			if _, err := s.Publish(t.Context(), bytes.NewReader(data), r); err == nil {
				t.Fatal("unsafe entry accepted")
			}
			got, _ := os.ReadFile(outside)
			if string(got) != "untouched" {
				t.Fatal("outside modified")
			}
		})
	}
	for _, kind := range []string{"entries", "stored", "peak", "foreign", "directory", "manifest"} {
		t.Run(kind, func(t *testing.T) {
			s, path := openStore(t)
			data, r := evidence(t, "Name")
			switch kind {
			case "entries":
				s.limits.MaxDirectoryEntries = 4
			case "stored":
				s.limits.MaxStoredBytes = 1
			case "peak":
				s.limits.MaxStoredBytes = int64(len(data))
			case "foreign":
				if err := os.WriteFile(filepath.Join(path, "foreign"), nil, 0600); err != nil {
					t.Fatal(err)
				}
			case "directory":
				if err := os.Mkdir(filepath.Join(path, ".stage-dir"), 0700); err != nil {
					t.Fatal(err)
				}
			case "manifest":
				s.limits.MaxManifestBytes = 1
			}
			if _, err := s.Publish(t.Context(), bytes.NewReader(data), r); err == nil {
				t.Fatal("capacity/entry accepted")
			}
		})
	}
	root := t.TempDir()
	if err := os.Chmod(root, 0777); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(root, limits()); err == nil {
		t.Fatal("shared root")
	}
	symlink := filepath.Join(t.TempDir(), "link")
	if err := os.Symlink(t.TempDir(), symlink); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(symlink, limits()); err == nil {
		t.Fatal("symlink root")
	}
}

func TestClosedRootAndDirectGuards(t *testing.T) {
	s, path := openStore(t)
	data, r := evidence(t, "Whole")
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := syncDirectory(s.root); err == nil {
		t.Fatal("closed dir sync")
	}
	if _, err := s.lock(); err == nil {
		t.Fatal("closed lock")
	}
	if err := s.capacity(t.Context(), 1, 1); err == nil {
		t.Fatal("closed capacity")
	}
	if _, err := s.stage(t.Context(), data); err == nil {
		t.Fatal("closed stage")
	}
	if _, err := s.Publish(t.Context(), bytes.NewReader(data), r); err == nil {
		t.Fatal("closed publisher")
	}
	live, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	defer live.Close()
	if err := os.Mkdir(filepath.Join(path, "current.json"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := live.replaceSlot(t.Context(), "current.json", nil); err == nil {
		t.Fatal("special manifest")
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := live.stage(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := live.capacity(ctx, 1, 1); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := live.Publish(t.Context(), bytes.NewReader([]byte("bad")), r); err == nil {
		t.Fatal("invalid artifact")
	}
	if _, err := live.Publish(t.Context(), readerFunc(func([]byte) (int, error) { return 0, errors.New("input failure") }), r); err == nil {
		t.Fatal("input error")
	}
}
func TestImmutableCollisionAndCancellationBeforeLink(t *testing.T) {
	s, path := openStore(t)
	data, r := evidence(t, "Whole")
	snapshot, err := s.validate(t.Context(), data, r)
	if err != nil {
		t.Fatal(err)
	}
	name := snapshot.Digest() + ".ndjson"
	if err := os.WriteFile(filepath.Join(path, name), []byte("wrong"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := s.installObject(t.Context(), name, data); err == nil {
		t.Fatal("immutable collision")
	}
	got, _ := os.ReadFile(filepath.Join(path, name))
	if string(got) != "wrong" {
		t.Fatal("collision overwritten")
	}
	ctx, cancel := context.WithCancel(t.Context())
	s.ops.syncFile = func(f *os.File) error { err := f.Sync(); cancel(); return err }
	if err := s.installObject(ctx, "new.ndjson", data); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := os.Lstat(filepath.Join(path, "new.ndjson")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("canceled object visible")
	}
}

func TestDanglingManifestIsNotEmpty(t *testing.T) {
	for _, both := range []bool{false, true} {
		t.Run(fmt.Sprint(both), func(t *testing.T) {
			s, path := openStore(t)
			data, r := evidence(t, "Whole")
			m := writeSlot(t, path, "current.json", data, r)
			if both {
				writeSlot(t, path, "previous.json", data, r)
			}
			if err := os.Remove(filepath.Join(path, m.ArtifactSHA256+".ndjson")); err != nil {
				t.Fatal(err)
			}
			before, err := os.ReadFile(filepath.Join(path, "current.json"))
			if err != nil {
				t.Fatal(err)
			}
			if _, err := s.Load(t.Context()); err == nil || errors.Is(err, ErrEmpty) {
				t.Fatalf("dangling is empty: %v", err)
			}
			next, receipt := evidence(t, "Next")
			if result, err := s.Publish(t.Context(), bytes.NewReader(next), receipt); err == nil || result.Published {
				t.Fatalf("corrupt store overwritten: %+v %v", result, err)
			}
			after, err := os.ReadFile(filepath.Join(path, "current.json"))
			if err != nil || !bytes.Equal(before, after) {
				t.Fatal("dangling manifest mutated")
			}
		})
	}
}

func TestRenameErrorDoesNotProveNonexecution(t *testing.T) {
	s, _ := openStore(t)
	publish(t, s, "First")
	before, _ := s.Load(t.Context())
	data, r := evidence(t, "Second")
	sentinel := errors.New("reply lost after rename")
	original := s.ops.replace
	calls := 0
	s.ops.replace = func(root *os.Root, a, b string) error {
		calls++
		err := original(root, a, b)
		if err != nil {
			return err
		}
		if calls == 2 {
			return sentinel
		}
		return nil
	}
	result, err := s.Publish(t.Context(), bytes.NewReader(data), r)
	if !errors.Is(err, sentinel) || !errors.Is(err, ErrCommitUncertain) || !result.CommitAttempted || result.Published || result.DirectorySynced {
		t.Fatalf("uncertain outcome %+v %v", result, err)
	}
	after, err := s.Load(t.Context())
	if err != nil || after.Manifest.ArtifactSHA256 == before.Manifest.ArtifactSHA256 {
		t.Fatalf("real rename not observed: %v", err)
	}
	previous, err := s.loadSlot(t.Context(), "previous.json")
	if err != nil || previous.Manifest != before.Manifest {
		t.Fatal("last good recovery lost")
	}
}

func TestCapacityReservesLargerRecoveryManifest(t *testing.T) {
	s, path := openStore(t)
	oldData, oldReceipt := evidence(t, "Old")
	oldReceipt.Bytes = 10000
	oldReceipt.Rows = 100
	if _, err := s.Publish(t.Context(), bytes.NewReader(oldData), oldReceipt); err != nil {
		t.Fatal(err)
	}
	oldManifest, err := os.ReadFile(filepath.Join(path, "current.json"))
	if err != nil {
		t.Fatal(err)
	}
	data, r := evidence(t, "New")
	snapshot, err := s.validate(t.Context(), data, r)
	if err != nil {
		t.Fatal(err)
	}
	newManifest := mustJSON(t, Manifest{1, "supplied-file-only", snapshot.Digest(), int64(len(data)), r})
	if len(oldManifest) <= len(newManifest) {
		t.Fatal("fixture did not make old manifest larger")
	}
	s.limits.MaxStoredBytes = int64(len(oldManifest) + len(oldData) + 2*len(data) + 4*len(oldManifest) - 1)
	if result, err := s.Publish(t.Context(), bytes.NewReader(data), r); err == nil || result.CommitAttempted {
		t.Fatalf("recovery peak under-reserved: %+v %v", result, err)
	}
	after, err := os.ReadFile(filepath.Join(path, "current.json"))
	if err != nil || !bytes.Equal(after, oldManifest) {
		t.Fatal("capacity refusal changed current")
	}
}

func TestLockSyscallFailures(t *testing.T) {
	for _, kind := range []string{"open", "flock"} {
		t.Run(kind, func(t *testing.T) {
			s, _ := openStore(t)
			sentinel := errors.New("lock syscall failed")
			var handle *os.File
			if kind == "open" {
				s.ops.openFile = func(*os.Root, string, int, os.FileMode) (*os.File, error) { return nil, sentinel }
			} else {
				s.ops.lockHandle = func(file *os.File) error { handle = file; return sentinel }
			}
			if file, err := s.lock(); file != nil || !errors.Is(err, sentinel) {
				t.Fatalf("lock refusal %v", err)
			}
			if handle != nil {
				if _, err := handle.Stat(); !errors.Is(err, os.ErrClosed) {
					t.Fatalf("failed-lock handle leaked: %v", err)
				}
			}
		})
	}
}

func TestObjectReuseReopenFailureAndUnsafeDestination(t *testing.T) {
	s, path := openStore(t)
	data, r := evidence(t, "Whole")
	snapshot, err := s.validate(t.Context(), data, r)
	if err != nil {
		t.Fatal(err)
	}
	name := snapshot.Digest() + ".ndjson"
	if err := os.WriteFile(filepath.Join(path, name), data, 0600); err != nil {
		t.Fatal(err)
	}
	native := s.ops.openRead
	calls := 0
	sentinel := errors.New("reopen syscall failed")
	s.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
		calls++
		if calls == 2 {
			return nil, sentinel
		}
		return native(root, name)
	}
	if err := s.installObject(t.Context(), name, data); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join(path, name))
	if err != nil || !bytes.Equal(got, data) {
		t.Fatal("reuse failure changed immutable object")
	}
	s.ops = defaultOperations()
	if err := os.Mkdir(filepath.Join(path, "directory.ndjson"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := s.installObject(t.Context(), "directory.ndjson", data); err == nil {
		t.Fatal("nonregular object accepted")
	}
	if err := s.Close(); err != nil {
		t.Fatal(err)
	}
	if err := s.replaceSlot(t.Context(), "current.json", nil); err == nil {
		t.Fatal("closed replacement capability")
	}
}

func TestPublishOversizedReceiptDigestKeepsStoreEmpty(t *testing.T) {
	s, path := openStore(t)
	data, r := evidence(t, "Whole")
	r.SHA256 = strings.Repeat("ab", 1<<20)
	result, err := s.Publish(t.Context(), bytes.NewReader(data), r)
	if err == nil || result.CommitAttempted || result.Published {
		t.Fatalf("oversized receipt accepted: %+v %v", result, err)
	}
	if _, err := s.Load(t.Context()); !errors.Is(err, ErrEmpty) {
		t.Fatalf("receipt failure changed recovery: %v", err)
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if entry.Name() != ".writer.lock" {
			t.Fatalf("rejected receipt published %s", entry.Name())
		}
	}
}
