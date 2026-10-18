//go:build linux

package fuseintegration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"testing"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/spf13/afero"
	"golang.org/x/sys/unix"
)

var payload = bytes.Repeat([]byte("synthetic local fixture\x00\xff\n"), 4096)
var foreignBytes = []byte("foreign writer's existing bytes\x00\xff")

type result struct {
	digest   string
	identity *fsutil.BoundInstallIdentity
}
type operation struct {
	name                   string
	move, digest, identity bool
	run                    func(afero.Fs, string, string, fsutil.VerifiedSourceProof) (result, error)
}

func operations() []operation {
	return []operation{
		{name: "copy", run: func(fs afero.Fs, src, dst string, p fsutil.VerifiedSourceProof) (result, error) {
			return result{}, fsutil.CopyFileNoReplaceVerified(fs, src, dst, p)
		}},
		{name: "copy-mode", identity: true, run: func(fs afero.Fs, src, dst string, p fsutil.VerifiedSourceProof) (result, error) {
			id, err := fsutil.CopyFileNoReplaceVerifiedMode(fs, src, dst, p, 0o640)
			return result{identity: id}, err
		}},
		{name: "copy-install", identity: true, run: func(fs afero.Fs, src, dst string, p fsutil.VerifiedSourceProof) (result, error) {
			id, err := fsutil.CopyFileNoReplaceVerifiedInstall(fs, src, dst, p)
			return result{identity: id}, err
		}},
		{name: "copy-digest", digest: true, run: func(fs afero.Fs, src, dst string, p fsutil.VerifiedSourceProof) (result, error) {
			digest, err := fsutil.CopyFileNoReplaceVerifiedDigest(fs, src, dst, p)
			return result{digest: digest}, err
		}},
		{name: "copy-digest-mode", digest: true, identity: true, run: func(fs afero.Fs, src, dst string, p fsutil.VerifiedSourceProof) (result, error) {
			digest, id, err := fsutil.CopyFileNoReplaceVerifiedDigestMode(fs, src, dst, p, 0o640)
			return result{digest: digest, identity: id}, err
		}},
		{name: "cross-device-move", move: true, run: func(fs afero.Fs, src, dst string, p fsutil.VerifiedSourceProof) (result, error) {
			return result{}, fsutil.MoveFileNoReplaceVerified(fs, src, dst, p)
		}},
		{name: "cross-device-move-mode", move: true, identity: true, run: func(fs afero.Fs, src, dst string, p fsutil.VerifiedSourceProof) (result, error) {
			id, err := fsutil.MoveFileNoReplaceVerifiedMode(fs, src, dst, p, 0o640)
			return result{identity: id}, err
		}},
	}
}

func source(t *testing.T, f *fixture) (string, string, fsutil.VerifiedSourceProof) {
	t.Helper()
	src, dst := filepath.Join(f.source, "source.bin"), filepath.Join(f.mount, "movie.bin")
	must(t, os.WriteFile(src, payload, 0o640))
	admitted, err := os.Stat(src)
	must(t, err)
	proof := func(_ string, current os.FileInfo) error {
		if !os.SameFile(admitted, current) {
			return fmt.Errorf("source admission identity changed")
		}
		return nil
	}
	// No synthetic EXDEV seam: the real native-to-FUSE rename must fail in VFS.
	err = unix.Renameat2(unix.AT_FDCWD, src, unix.AT_FDCWD, dst, unix.RENAME_NOREPLACE)
	if !errors.Is(err, syscall.EXDEV) {
		t.Fatalf("expected natural cross-mount EXDEV, got %v", err)
	}
	assertBytes(t, src, payload)
	assertEntries(t, f.backing)
	return src, dst, proof
}

func TestFUSEKernelProtocol(t *testing.T) {
	t.Run("capable-rename-and-link", func(t *testing.T) {
		f := newFixture(t, profile{})
		a, b, c := filepath.Join(f.mount, "a"), filepath.Join(f.mount, "b"), filepath.Join(f.mount, "c")
		must(t, os.WriteFile(a, payload, 0o600))
		must(t, unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_NOREPLACE))
		assertBytes(t, b, payload)
		beforeCollision := f.control.snapshot()
		must(t, os.WriteFile(a, foreignBytes, 0o600))
		if err := unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_NOREPLACE); !errors.Is(err, syscall.EEXIST) {
			t.Fatalf("no-replace collision: %v", err)
		}
		if after := f.control.snapshot(); after.Rename2 != beforeCollision.Rename2 {
			t.Fatalf("VFS existing-target collision unexpectedly reached daemon: before=%+v after=%+v", beforeCollision, after)
		}
		assertBytes(t, a, foreignBytes)
		assertBytes(t, b, payload)
		must(t, os.Link(b, c))
		bi, err := os.Stat(b)
		must(t, err)
		ci, err := os.Stat(c)
		must(t, err)
		if !os.SameFile(bi, ci) {
			t.Fatal("capable LINK did not create a real hard link")
		}
		counts := f.control.snapshot()
		if counts.Rename2 != 1 || counts.RenameSuccess != 1 || counts.Links != 1 || counts.LinkSuccess != 1 {
			t.Fatalf("protocol not exercised: %+v", counts)
		}
		t.Logf("FUSE_PROTOCOL_CAPABLE: %+v", counts)
	})
	for _, tc := range []struct {
		name           string
		daemon, kernel syscall.Errno
	}{
		{"rename-enosys-maps-einval", syscall.ENOSYS, syscall.EINVAL},
		{"rename-eopnotsupp", syscall.EOPNOTSUPP, syscall.EOPNOTSUPP},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := newFixture(t, profile{renameErr: tc.daemon, linkErr: syscall.EPERM})
			a, b := filepath.Join(f.mount, "a"), filepath.Join(f.mount, "b")
			must(t, os.WriteFile(a, payload, 0o600))
			err := unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_NOREPLACE)
			if !errors.Is(err, tc.kernel) {
				t.Fatalf("daemon errno %v, kernel errno %v; want %v", tc.daemon, err, tc.kernel)
			}
			if err := os.Link(a, b); !errors.Is(err, syscall.EPERM) {
				t.Fatalf("daemon LINK EPERM did not reach kernel caller: %v", err)
			}
			if tc.daemon == syscall.ENOSYS {
				if err := unix.Renameat2(unix.AT_FDCWD, a, unix.AT_FDCWD, b, unix.RENAME_NOREPLACE); !errors.Is(err, syscall.EINVAL) {
					t.Fatalf("cached RENAME2 ENOSYS must remain EINVAL: %v", err)
				}
			}
			counts := f.control.snapshot()
			if counts.RenameUnsupported != 1 || counts.LinkDenied != 1 {
				t.Fatalf("missing protocol failures: %+v", counts)
			}
			assertBytes(t, a, payload)
			assertEntries(t, f.backing, "a")
			t.Logf("FUSE_PROTOCOL_UNSUPPORTED daemon_rename=%d kernel_rename=%d daemon_link=%d kernel_link=%d counters=%+v", tc.daemon, tc.kernel, syscall.EPERM, syscall.EPERM, counts)
		})
	}
	t.Run("link-enosys-kernel-policy", func(t *testing.T) {
		f := newFixture(t, profile{linkErr: syscall.ENOSYS})
		a, b := filepath.Join(f.mount, "a"), filepath.Join(f.mount, "b")
		must(t, os.WriteFile(a, payload, 0o600))
		first, second := os.Link(a, b), os.Link(a, b)
		counts := f.control.snapshot()
		// Older kernels return ENOSYS without caching; newer kernels map it
		// to EPERM and cache no_link. Both paths must be explicitly observed.
		kernelErr := syscall.Errno(0)
		switch {
		case errors.Is(first, syscall.EPERM) && errors.Is(second, syscall.EPERM) && counts.Links == 1 && counts.LinkDenied == 1:
			kernelErr = syscall.EPERM
		case errors.Is(first, syscall.ENOSYS) && errors.Is(second, syscall.ENOSYS) && counts.Links == 2 && counts.LinkDenied == 2:
			kernelErr = syscall.ENOSYS
		default:
			t.Fatalf("unexpected LINK ENOSYS kernel policy: first=%v second=%v counts=%+v", first, second, counts)
		}
		assertBytes(t, a, payload)
		assertEntries(t, f.backing, "a")
		t.Logf("FUSE_LINK_ENOSYS_POLICY_VERIFIED daemon_link=%d kernel_link=%d counters=%+v", syscall.ENOSYS, kernelErr, counts)
	})

}

func TestFUSEUnsupportedPreflight(t *testing.T) {
	for _, renameErr := range []syscall.Errno{syscall.ENOSYS, syscall.EOPNOTSUPP} {
		for _, op := range operations() {
			t.Run(fmt.Sprintf("errno-%d/%s", renameErr, op.name), func(t *testing.T) {
				// Fresh mount per application operation: ENOSYS capability caching
				// cannot hide the real first RENAME2 failure behind another case.
				f := newFixture(t, profile{renameErr: renameErr, linkErr: syscall.EPERM})
				src, dst, proof := source(t, f)
				got, err := op.run(afero.NewOsFs(), src, dst, proof)
				if !errors.Is(err, fsutil.ErrPublishNoReplaceUnsupported) {
					t.Fatalf("expected typed unsupported refusal: %v", err)
				}
				if errors.Is(err, syscall.EINVAL) || errors.Is(err, syscall.EXDEV) || fsutil.PublishCompleted(err) {
					t.Fatalf("unsupported error was misclassified: %v", err)
				}
				if got.digest != "" || got.identity != nil {
					t.Fatalf("refusal asserted successful publication: %+v", got)
				}
				assertBytes(t, src, payload)
				assertEntries(t, f.source, "source.bin")
				assertEntries(t, f.backing)
				counts := f.control.snapshot()
				if counts.ProbeCreates != 1 || counts.RenameUnsupported != 1 || counts.LinkDenied == 0 {
					t.Fatalf("application did not exercise real unsupported preflight: %+v", counts)
				}
				if counts.Creates != 1 || counts.StageCreates != 0 || counts.CleanupCreates != 0 || counts.WrittenBytes != 0 || counts.FinalPublishes != 0 {
					t.Fatalf("incapable destination consumed payload before refusal: %+v", counts)
				}
				t.Logf("FUSE_PREFLIGHT_ZERO_PAYLOAD: %+v", counts)
			})
		}
	}
}

func TestFUSEPublicationSuccess(t *testing.T) {
	for _, mode := range []struct {
		name string
		p    profile
	}{
		{"rename-capable", profile{}},
		{"hardlink-fallback-capable", profile{renameErr: syscall.EOPNOTSUPP}},
	} {
		for _, op := range operations() {
			t.Run(mode.name+"/"+op.name, func(t *testing.T) {
				f := newFixture(t, mode.p)
				src, dst, proof := source(t, f)
				got, err := op.run(afero.NewOsFs(), src, dst, proof)
				must(t, err)
				assertBytes(t, dst, payload)
				assertBytes(t, filepath.Join(f.backing, "movie.bin"), payload)
				assertEntries(t, f.backing, "movie.bin")
				if op.move {
					assertEntries(t, f.source)
				} else {
					assertBytes(t, src, payload)
					assertEntries(t, f.source, "source.bin")
				}
				if op.digest {
					sum := sha256.Sum256(payload)
					if got.digest != hex.EncodeToString(sum[:]) {
						t.Fatalf("incorrect successful digest: %s", got.digest)
					}
				}
				if op.identity {
					if got.identity == nil {
						t.Fatal("successful API omitted installed identity")
					}
					info, err := os.Stat(dst)
					must(t, err)
					if !os.SameFile(got.identity.FileInfo(), info) {
						t.Fatal("returned identity does not name installed bytes")
					}
				}
				counts := f.control.snapshot()
				if counts.ProbeCreates != 1 || counts.StageCreates != 1 || counts.WrittenBytes != uint64(len(payload)) {
					t.Fatalf("positive path bypassed required preflight/stream: %+v", counts)
				}
				if mode.p.renameErr == 0 && (counts.RenameSuccess < 2 || counts.FinalPublishes != 1) {
					t.Fatalf("native no-replace publication not forwarded: %+v", counts)
				}
				if mode.p.renameErr != 0 && (counts.LinkSuccess < 2 || counts.RenameUnsupported < 2 || counts.FinalPublishes != 1) {
					t.Fatalf("native hard-link fallback not forwarded: %+v", counts)
				}
				t.Logf("FUSE_PUBLICATION_BYTES_VERIFIED: %+v", counts)
			})
		}
	}
}

func TestFUSEPublicationFailuresPreserveBytes(t *testing.T) {
	for _, scenario := range []struct {
		name             string
		p                profile
		expected         error
		existing, cancel bool
	}{
		{name: "existing-collision", expected: fsutil.ErrPublishCollision, existing: true},
		{name: "collision-after-positive-probe", p: profile{raceCollision: true}, expected: fsutil.ErrPublishCollision},
		{name: "stream-eio", p: profile{streamEIO: true}, expected: syscall.EIO},
		{name: "final-publish-eio", p: profile{publishEIO: true}, expected: syscall.EIO},
		{name: "fallback-collision-after-positive-probe", p: profile{renameErr: syscall.EOPNOTSUPP, raceCollision: true}, expected: fsutil.ErrPublishCollision},
		{name: "fallback-final-publish-eio", p: profile{renameErr: syscall.EOPNOTSUPP, publishEIO: true}, expected: syscall.EIO},
		{name: "admission-canceled", expected: context.Canceled, cancel: true},
	} {
		for _, op := range operations() {
			t.Run(scenario.name+"/"+op.name, func(t *testing.T) {
				f := newFixture(t, scenario.p)
				src, dst, proof := source(t, f)
				if scenario.existing {
					must(t, os.WriteFile(dst, foreignBytes, 0o600))
					f.control.mu.Lock()
					f.control.counts = counters{}
					f.control.mu.Unlock()
				}
				if scenario.cancel {
					proof = func(string, os.FileInfo) error { return context.Canceled }
				}
				got, err := op.run(afero.NewOsFs(), src, dst, proof)
				if !errors.Is(err, scenario.expected) {
					t.Fatalf("expected %v, got %v", scenario.expected, err)
				}
				if fsutil.PublishCompleted(err) || got.digest != "" || got.identity != nil {
					t.Fatalf("failure asserted successful publication: %+v, %v", got, err)
				}
				assertBytes(t, src, payload)
				assertEntries(t, f.source, "source.bin")
				if scenario.existing || scenario.p.raceCollision {
					assertBytes(t, dst, foreignBytes)
					assertBytes(t, filepath.Join(f.backing, "movie.bin"), foreignBytes)
					assertEntries(t, f.backing, "movie.bin")
				} else {
					assertEntries(t, f.backing)
				}
				counts := f.control.snapshot()
				if scenario.existing || scenario.cancel {
					if counts.StageCreates != 0 || counts.WrittenBytes != 0 {
						t.Fatalf("early refusal streamed bytes: %+v", counts)
					}
				} else {
					if counts.ProbeCreates != 1 || counts.StageCreates != 1 {
						t.Fatalf("fault did not follow a positive real preflight: %+v", counts)
					}
					if scenario.p.streamEIO && counts.WriteErrors == 0 {
						t.Fatalf("daemon stream EIO was not exercised: %+v", counts)
					}
					if scenario.p.publishEIO && counts.FinalErrors != 1 {
						t.Fatalf("daemon final EIO was not exercised: %+v", counts)
					}
					if scenario.p.raceCollision && counts.CollisionPlants != 1 {
						t.Fatalf("foreign writer did not race final publish: %+v", counts)
					}
				}
				t.Logf("FUSE_FAILURE_PRESERVED source=%d foreign=%t counters=%+v", len(payload), scenario.existing || scenario.p.raceCollision, counts)
			})
		}
	}
}

func TestFUSECleanupUnmounts(t *testing.T) {
	f := newFixture(t, profile{})
	f.close(t)
	if !f.closed {
		t.Fatal("cleanup did not finish")
	}
	if _, err := os.Lstat(f.root); !os.IsNotExist(err) {
		t.Fatalf("fixture not removed: %v", err)
	}
	if hasMount(t, f.root) {
		t.Fatal("fixture mount survived cleanup")
	}
}
