package artifactstore

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestRootOpenAndIdentityRaces(t *testing.T) {
	sentinel := errors.New("open root syscall refused")
	if _, err := openWithRoot(t.TempDir(), limits(), func(string) (*os.Root, error) { return nil, sentinel }); !errors.Is(err, sentinel) {
		t.Fatal(err)
	}
	path := t.TempDir()
	var closed *os.Root
	if _, err := openWithRoot(path, limits(), func(path string) (*os.Root, error) {
		root, err := os.OpenRoot(path)
		if err != nil {
			return nil, err
		}
		closed = root
		if err := root.Close(); err != nil {
			return nil, err
		}
		return root, nil
	}); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("closed directory stat: %v", err)
	}
	if closed == nil {
		t.Fatal("root not opened")
	}
	path = filepath.Join(t.TempDir(), "root")
	if err := os.Mkdir(path, 0700); err != nil {
		t.Fatal(err)
	}
	var swapped *os.Root
	if _, err := openWithRoot(path, limits(), func(path string) (*os.Root, error) {
		if err := os.Rename(path, path+"-original"); err != nil {
			return nil, err
		}
		if err := os.Mkdir(path, 0700); err != nil {
			return nil, err
		}
		root, err := os.OpenRoot(path)
		swapped = root
		return root, err
	}); err == nil {
		t.Fatal("directory substitution accepted")
	}
	if swapped == nil {
		t.Fatal("substituted root missing")
	}
	if _, err := swapped.Stat("."); !errors.Is(err, os.ErrClosed) {
		t.Fatalf("rejected capability leaked: %v", err)
	}
}

func TestReadOpenStatAndIdentityFailures(t *testing.T) {
	for _, kind := range []string{"open", "closed-stat", "substitution"} {
		t.Run(kind, func(t *testing.T) {
			s, path := openStore(t)
			name := "object"
			original := []byte("original")
			if err := os.WriteFile(filepath.Join(path, name), original, 0600); err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("read open syscall refused")
			native := s.ops.openRead
			s.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
				switch kind {
				case "open":
					return nil, sentinel
				case "closed-stat":
					file, err := native(root, name)
					if err != nil {
						return nil, err
					}
					if err := file.Close(); err != nil {
						return nil, err
					}
					return file, nil
				case "substitution":
					if err := root.Rename(name, name+"-saved"); err != nil {
						return nil, err
					}
					if err := root.WriteFile(name, []byte("replacement"), 0600); err != nil {
						return nil, err
					}
				}
				return native(root, name)
			}
			if data, err := s.readRegular(t.Context(), name, 100); err == nil || data != nil {
				t.Fatalf("unsafe read succeeded: %q %v", data, err)
			}
			if kind == "substitution" {
				got, err := os.ReadFile(filepath.Join(path, name+"-saved"))
				if err != nil || !bytes.Equal(got, original) {
					t.Fatal("original changed")
				}
			}
		})
	}
}

func TestLoadCancellationDoesNotTryFallback(t *testing.T) {
	s, path := openStore(t)
	data, r := evidence(t, "Whole")
	writeSlot(t, path, "current.json", data, r)
	writeSlot(t, path, "previous.json", data, r)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	native := s.ops.openRead
	calls := 0
	s.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
		calls++
		file, err := native(root, name)
		cancel()
		return file, err
	}
	if _, err := s.Load(ctx); !errors.Is(err, context.Canceled) || calls != 1 {
		t.Fatalf("cancellation fallback: %d %v", calls, err)
	}
}

func TestCapacityEnumerationFailures(t *testing.T) {
	for _, kind := range []string{"read-dir", "entry-info"} {
		t.Run(kind, func(t *testing.T) {
			s, path := openStore(t)
			if err := os.WriteFile(filepath.Join(path, ".stage-existing"), []byte("retained"), 0600); err != nil {
				t.Fatal(err)
			}
			sentinel := errors.New("enumeration syscall failed")
			if kind == "read-dir" {
				s.ops.readDir = func(*os.File, int) ([]os.DirEntry, error) { return nil, sentinel }
			} else {
				s.ops.entryInfo = func(os.DirEntry) (os.FileInfo, error) { return nil, sentinel }
			}
			if err := s.capacity(t.Context(), 1, 1); !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
			got, err := os.ReadFile(filepath.Join(path, ".stage-existing"))
			if err != nil || string(got) != "retained" {
				t.Fatal("capacity failure modified entry")
			}
		})
	}
}
