//go:build darwin || linux

package artifactstore

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLoadDuringSuccessivePublications(t *testing.T) {
	reader, path := openStore(t)
	writer, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	publish(t, writer, "A")
	dataB, receiptB := evidence(t, "B")
	dataC, receiptC := evidence(t, "C")
	native := reader.ops.openRead
	changedCurrent, changedPrevious := false, false
	reader.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
		if name == "current.json" && !changedCurrent {
			changedCurrent = true
			if _, err := writer.Publish(t.Context(), bytes.NewReader(dataB), receiptB); err != nil {
				return nil, err
			}
		}
		if name == "previous.json" && !changedPrevious {
			changedPrevious = true
			if _, err := writer.Publish(t.Context(), bytes.NewReader(dataC), receiptC); err != nil {
				return nil, err
			}
		}
		return native(root, name)
	}
	view, err := reader.Load(t.Context())
	if err != nil {
		t.Fatalf("valid committed generation lost during replacements: %v", err)
	}
	if !changedCurrent || view.Snapshot == nil {
		t.Fatal("fixture did not overlap publication")
	}
}

func TestLoadRetriesBothSlotsAndExhaustsFiniteBudget(t *testing.T) {
	for _, exhaust := range []bool{false, true} {
		t.Run(fmt.Sprint(exhaust), func(t *testing.T) {
			reader, path := openStore(t)
			writer, err := Open(path, limits())
			if err != nil {
				t.Fatal(err)
			}
			defer writer.Close()
			publish(t, writer, "A")
			native := reader.ops.openRead
			current, previous, generation := 0, 0, 0
			reader.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
				replace := false
				if name == "current.json" {
					current++
					replace = true
				}
				if name == "previous.json" {
					previous++
					replace = exhaust || previous == 1
				}
				if replace {
					generation++
					data, r := evidence(t, fmt.Sprintf("generation-%d", generation))
					if _, err := writer.Publish(t.Context(), bytes.NewReader(data), r); err != nil {
						return nil, err
					}
				}
				return native(root, name)
			}
			view, err := reader.Load(t.Context())
			if exhaust {
				if !errors.Is(err, ErrReadContention) || errors.Is(err, ErrEmpty) || current != manifestReadAttempts || previous != manifestReadAttempts {
					t.Fatalf("unbounded/wrong exhaustion: current%d previous%d err%v", current, previous, err)
				}
			} else {
				if err != nil || !view.Recovered || current != manifestReadAttempts || previous != 2 {
					t.Fatalf("stable fallback: current%d previous%d err%v", current, previous, err)
				}
				if got := view.Snapshot.Records()[0].Metadata.Aliases[0]; got != "generation-3" {
					t.Fatalf("not a committed fallback: %s", got)
				}
			}
		})
	}
}

func TestPinnedManifestSurvivesLaterPublication(t *testing.T) {
	reader, path := openStore(t)
	writer, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	publish(t, writer, "A")
	native := reader.ops.openRead
	changed := false
	reader.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
		file, err := native(root, name)
		if err == nil && name == "current.json" && !changed {
			changed = true
			data, r := evidence(t, "B")
			if _, publishErr := writer.Publish(t.Context(), bytes.NewReader(data), r); publishErr != nil {
				_ = file.Close()
				return nil, publishErr
			}
		}
		return file, err
	}
	view, err := reader.Load(t.Context())
	if err != nil || view.Recovered || !changed || view.Snapshot.Records()[0].Metadata.Aliases[0] != "A" {
		t.Fatalf("pinned generation reclassified: %+v %v", view, err)
	}
}

func TestManifestRetryRejectsUnsafeAndCorruptChanges(t *testing.T) {
	for _, kind := range []string{"symlink", "directory", "oversized", "corrupt", "io-error", "vanished"} {
		t.Run(kind, func(t *testing.T) {
			reader, _ := openStore(t)
			publish(t, reader, "A")
			native := reader.ops.openRead
			calls := 0
			sentinel := errors.New("unrelated open failure")
			reader.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
				if name == "current.json" {
					calls++
					if calls == 1 {
						if kind == "io-error" {
							return nil, sentinel
						}
						if err := root.Rename(name, ".stage-saved"); err != nil {
							return nil, err
						}
						switch kind {
						case "symlink":
							if err := root.Symlink(".stage-saved", name); err != nil {
								return nil, err
							}
						case "directory":
							if err := root.Mkdir(name, 0700); err != nil {
								return nil, err
							}
						case "oversized":
							if err := root.WriteFile(name, bytes.Repeat([]byte("x"), int(reader.limits.MaxManifestBytes)+1), 0600); err != nil {
								return nil, err
							}
						case "corrupt":
							if err := root.WriteFile(name, []byte("malformed JSON\n"), 0600); err != nil {
								return nil, err
							}
						case "vanished":
							file, err := native(root, ".stage-saved")
							if err != nil {
								return nil, err
							}
							if err := root.WriteFile(name, []byte("other"), 0600); err != nil {
								_ = file.Close()
								return nil, err
							}
							_ = file.Close()
							file, err = native(root, name)
							if err != nil {
								return nil, err
							}
							if err := root.Remove(name); err != nil {
								_ = file.Close()
								return nil, err
							}
							return file, nil
						}
					}
				}
				return native(root, name)
			}
			_, err := reader.Load(t.Context())
			if err == nil || errors.Is(err, ErrReadContention) || errors.Is(err, ErrEmpty) {
				t.Fatalf("unsafe/corrupt classified as retry exhaustion: %v", err)
			}
			want := 1
			if kind == "corrupt" {
				want = 2
			} // One observed regular replacement, then terminal validation.
			if calls != want {
				t.Fatalf("%s retried %d times want%d", kind, calls, want)
			}
			if kind == "io-error" && !errors.Is(err, sentinel) {
				t.Fatal(err)
			}
		})
	}
}

func TestManifestRetryCancellationAndStrictObjects(t *testing.T) {
	t.Run("cancellation", func(t *testing.T) {
		reader, path := openStore(t)
		writer, err := Open(path, limits())
		if err != nil {
			t.Fatal(err)
		}
		defer writer.Close()
		publish(t, writer, "A")
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()
		native := reader.ops.openRead
		calls := 0
		reader.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
			if name == "current.json" {
				calls++
				data, r := evidence(t, "B")
				if _, err := writer.Publish(t.Context(), bytes.NewReader(data), r); err != nil {
					return nil, err
				}
				cancel()
			}
			return native(root, name)
		}
		if _, err := reader.Load(ctx); !errors.Is(err, context.Canceled) || calls != 1 {
			t.Fatalf("canceled retry: calls%d err%v", calls, err)
		}
	})
	t.Run("immutable-object", func(t *testing.T) {
		reader, _ := openStore(t)
		publish(t, reader, "A")
		native := reader.ops.openRead
		objects := 0
		reader.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
			if strings.HasSuffix(name, ".ndjson") {
				objects++
				data, err := root.ReadFile(name)
				if err != nil {
					return nil, err
				}
				if err := root.Rename(name, ".stage-object-original"); err != nil {
					return nil, err
				}
				if err := root.WriteFile(name, data, 0600); err != nil {
					return nil, err
				}
			}
			return native(root, name)
		}
		if _, err := reader.Load(t.Context()); err == nil || errors.Is(err, ErrReadContention) || objects != 1 {
			t.Fatalf("immutable object retried: count%d err%v", objects, err)
		}
	})
}

func TestImmutableObjectChangedAfterOpenDoesNotRetry(t *testing.T) {
	reader, _ := openStore(t)
	publish(t, reader, "A")
	native := reader.ops.openRead
	objects := 0
	reader.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
		file, err := native(root, name)
		if err != nil {
			return nil, err
		}
		if strings.HasSuffix(name, ".ndjson") {
			objects++
			data, err := root.ReadFile(name)
			if err != nil {
				_ = file.Close()
				return nil, err
			}
			if err := root.Rename(name, ".stage-old-object"); err != nil {
				_ = file.Close()
				return nil, err
			}
			if err := root.WriteFile(name, data, 0600); err != nil {
				_ = file.Close()
				return nil, err
			}
		}
		return file, nil
	}
	if _, err := reader.Load(t.Context()); err == nil || errors.Is(err, ErrReadContention) || errors.Is(err, ErrEmpty) || objects != 1 {
		t.Fatalf("changed immutable object accepted/retried: objects%d err%v", objects, err)
	}
}

func TestCorruptionIsNotRetried(t *testing.T) {
	for _, kind := range []string{"manifest", "digest"} {
		t.Run(kind, func(t *testing.T) {
			reader, path := openStore(t)
			publish(t, reader, "A")
			view, err := reader.Load(t.Context())
			if err != nil {
				t.Fatal(err)
			}
			if kind == "manifest" {
				if err := os.WriteFile(filepath.Join(path, "current.json"), []byte("bad JSON\n"), 0600); err != nil {
					t.Fatal(err)
				}
			} else {
				data := view.Snapshot.Bytes()
				data[len(data)-2] ^= 1
				if err := os.WriteFile(filepath.Join(path, view.Manifest.ArtifactSHA256+".ndjson"), data, 0600); err != nil {
					t.Fatal(err)
				}
			}
			native := reader.ops.openRead
			current, objects := 0, 0
			reader.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
				if name == "current.json" {
					current++
				}
				if strings.HasSuffix(name, ".ndjson") {
					objects++
				}
				return native(root, name)
			}
			if _, err := reader.Load(t.Context()); err == nil || errors.Is(err, ErrReadContention) || errors.Is(err, ErrEmpty) || current != 1 {
				t.Fatalf("corruption retried: current%d objects%d err%v", current, objects, err)
			}
			if (kind == "digest" && objects != 1) || (kind == "manifest" && objects != 0) {
				t.Fatalf("unexpected object reads %d", objects)
			}
		})
	}
}

func TestCancellationOnFinalFallbackAttemptDominatesContention(t *testing.T) {
	reader, path := openStore(t)
	writer, err := Open(path, limits())
	if err != nil {
		t.Fatal(err)
	}
	defer writer.Close()
	publish(t, writer, "A")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	native := reader.ops.openRead
	current, previous := 0, 0
	reader.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
		if name == "current.json" || name == "previous.json" {
			if name == "current.json" {
				current++
			} else {
				previous++
			}
			data, r := evidence(t, fmt.Sprintf("C%d-P%d", current, previous))
			if _, err := writer.Publish(t.Context(), bytes.NewReader(data), r); err != nil {
				return nil, err
			}
			if name == "previous.json" && previous == manifestReadAttempts {
				cancel()
			}
		}
		return native(root, name)
	}
	if _, err := reader.Load(ctx); err != context.Canceled || errors.Is(err, ErrReadContention) || current != manifestReadAttempts || previous != manifestReadAttempts {
		t.Fatalf("final cancellation current%d previous%d err%v", current, previous, err)
	}
}
