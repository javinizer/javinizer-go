//go:build linux

package fuseintegration

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/hanwen/go-fuse/v2/fs"
	"github.com/hanwen/go-fuse/v2/fuse"
	"golang.org/x/sys/unix"
)

// This is a real userspace FUSE server: application syscalls cross the Linux
// VFS and /dev/fuse before these handlers run. No fsutil syscall seam is used.
// Only fresh, process-owned, synthetic temporary files are exposed.
type profile struct {
	renameErr     syscall.Errno
	linkErr       syscall.Errno
	streamEIO     bool
	publishEIO    bool
	raceCollision bool
}

type counters struct {
	Creates, ProbeCreates, StageCreates, CleanupCreates uint64
	Rename2, RenameUnsupported, RenameSuccess           uint64
	Links, LinkDenied, LinkSuccess                      uint64
	WriteCalls, WrittenBytes, WriteErrors               uint64
	FinalPublishes, FinalErrors, CollisionPlants        uint64
}

type control struct {
	mu      sync.Mutex
	profile profile
	counts  counters
	backing string
}

func (c *control) snapshot() counters {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.counts
}

type faultNode struct {
	*fs.LoopbackNode
	control *control
}

// Disable the kernel passthrough optimization: every write must reach the
// observable daemon callback, even on a kernel that supports passthrough.
type trackedFile struct{ *fs.LoopbackFile }

func (*trackedFile) PassthroughFd() (int, bool) { return -1, false }

func (n *faultNode) Create(ctx context.Context, name string, flags, mode uint32, out *fuse.EntryOut) (*fs.Inode, fs.FileHandle, uint32, syscall.Errno) {
	n.control.mu.Lock()
	n.control.counts.Creates++
	if strings.HasPrefix(name, ".nrprobe.") {
		n.control.counts.ProbeCreates++
	}
	if payloadStageName(name) {
		n.control.counts.StageCreates++
	} else if strings.Contains(name, ".nrstg.") {
		n.control.counts.CleanupCreates++
	}
	n.control.mu.Unlock()
	inode, handle, fuseFlags, errno := n.LoopbackNode.Create(ctx, name, flags, mode, out)
	if errno != 0 {
		return inode, handle, fuseFlags, errno
	}
	return inode, &trackedFile{handle.(*fs.LoopbackFile)}, fuseFlags | fuse.FOPEN_DIRECT_IO, errno
}

func (n *faultNode) Open(ctx context.Context, flags uint32) (fs.FileHandle, uint32, syscall.Errno) {
	handle, fuseFlags, errno := n.LoopbackNode.Open(ctx, flags)
	if errno != 0 {
		return handle, fuseFlags, errno
	}
	return &trackedFile{handle.(*fs.LoopbackFile)}, fuseFlags | fuse.FOPEN_DIRECT_IO, errno
}

func (n *faultNode) Write(ctx context.Context, handle fs.FileHandle, data []byte, off int64) (uint32, syscall.Errno) {
	n.control.mu.Lock()
	n.control.counts.WriteCalls++
	fail := n.control.profile.streamEIO && payloadStageName(filepath.Base(n.Path(n.Root())))
	if fail {
		n.control.counts.WriteErrors++
	}
	n.control.mu.Unlock()
	if fail {
		return 0, syscall.EIO
	}
	written, errno := handle.(fs.FileWriter).Write(ctx, data, off)
	n.control.mu.Lock()
	n.control.counts.WrittenBytes += uint64(written)
	n.control.mu.Unlock()
	return written, errno
}

// Force ordinary READ/WRITE even if Go or the kernel attempts copy_file_range.
func (*faultNode) CopyFileRange(context.Context, fs.FileHandle, uint64, *fs.Inode, fs.FileHandle, uint64, uint64, uint64) (uint32, syscall.Errno) {
	return 0, syscall.EOPNOTSUPP
}

func (n *faultNode) Rename(ctx context.Context, name string, newParent fs.InodeEmbedder, newName string, flags uint32) syscall.Errno {
	n.control.mu.Lock()
	p := n.control.profile
	if flags&unix.RENAME_NOREPLACE != 0 {
		n.control.counts.Rename2++
		if p.renameErr != 0 {
			n.control.counts.RenameUnsupported++
			n.control.mu.Unlock()
			return p.renameErr
		}
	}
	final := newName == "movie.bin" && flags&unix.RENAME_NOREPLACE != 0
	if final {
		n.control.counts.FinalPublishes++
	}
	if final && p.publishEIO {
		n.control.counts.FinalErrors++
		n.control.mu.Unlock()
		return syscall.EIO
	}
	plant := final && p.raceCollision
	n.control.mu.Unlock()
	if plant {
		if errno := n.plantForeign(newName); errno != 0 {
			return errno
		}
	}
	errno := n.LoopbackNode.Rename(ctx, name, newParent, newName, flags)
	if errno == 0 && flags&unix.RENAME_NOREPLACE != 0 {
		n.control.mu.Lock()
		n.control.counts.RenameSuccess++
		n.control.mu.Unlock()
	}
	return errno
}

// payloadStageName excludes the empty .vac.<token> reservations made by
// identity-bound cleanup. They are counted separately, never as payload files.
func payloadStageName(name string) bool {
	const prefix = "movie.bin.nrstg."
	if !strings.HasPrefix(name, prefix) || len(name) == len(prefix) {
		return false
	}
	for _, r := range strings.TrimPrefix(name, prefix) {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

func (n *faultNode) plantForeign(name string) syscall.Errno {
	// A second writer wins after the application's absence check, before
	// the real backing renameat2/link primitive decides publication.
	f, err := os.OpenFile(filepath.Join(n.control.backing, name), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return fs.ToErrno(err)
	}
	_, err = f.Write(foreignBytes)
	closeErr := f.Close()
	if err != nil {
		return fs.ToErrno(err)
	}
	if closeErr != nil {
		return fs.ToErrno(closeErr)
	}
	n.control.mu.Lock()
	n.control.counts.CollisionPlants++
	n.control.mu.Unlock()
	return 0
}

func (n *faultNode) Link(ctx context.Context, target fs.InodeEmbedder, name string, out *fuse.EntryOut) (*fs.Inode, syscall.Errno) {
	n.control.mu.Lock()
	n.control.counts.Links++
	p := n.control.profile
	if p.linkErr != 0 {
		n.control.counts.LinkDenied++
		n.control.mu.Unlock()
		return nil, p.linkErr
	}
	final := name == "movie.bin"
	if final {
		n.control.counts.FinalPublishes++
	}
	if final && p.publishEIO {
		n.control.counts.FinalErrors++
		n.control.mu.Unlock()
		return nil, syscall.EIO
	}
	n.control.mu.Unlock()
	if final && p.raceCollision {
		if errno := n.plantForeign(name); errno != 0 {
			return nil, errno
		}
	}
	inode, errno := n.LoopbackNode.Link(ctx, target, name, out)
	if errno == 0 {
		n.control.mu.Lock()
		n.control.counts.LinkSuccess++
		n.control.mu.Unlock()
	}
	return inode, errno
}

// Prerequisites are mandatory. A green job may never mean "FUSE skipped".
func TestMain(m *testing.M) {
	if err := prerequisites(); err != nil {
		fmt.Fprintln(os.Stderr, "FUSE_PREREQUISITE_FAILED:", err)
		os.Exit(1)
	}
	os.Exit(m.Run())
}

func prerequisites() error {
	if os.Getenv("JAVINIZER_FUSE_INTEGRATION") != "1" {
		return errors.New("set JAVINIZER_FUSE_INTEGRATION=1 to opt in to owned temporary mounts")
	}
	if os.Geteuid() == 0 {
		return errors.New("run as the ordinary runner user, never root")
	}
	var tempFS unix.Statfs_t
	if err := unix.Statfs(os.TempDir(), &tempFS); err != nil {
		return fmt.Errorf("inspect local temporary filesystem: %w", err)
	}
	switch tempFS.Type {
	case unix.EXT4_SUPER_MAGIC, unix.TMPFS_MAGIC, unix.OVERLAYFS_SUPER_MAGIC, unix.XFS_SUPER_MAGIC, unix.BTRFS_SUPER_MAGIC:
	default:
		return fmt.Errorf("temporary filesystem type 0x%x is not an approved local fixture filesystem", tempFS.Type)
	}
	dev, err := os.Stat("/dev/fuse")
	if err != nil {
		return fmt.Errorf("existing /dev/fuse required: %w; no device or security changes are attempted", err)
	}
	if dev.Mode()&os.ModeCharDevice == 0 {
		return errors.New("/dev/fuse is not a character device")
	}
	if err := unix.Access("/dev/fuse", unix.R_OK|unix.W_OK); err != nil {
		return fmt.Errorf("/dev/fuse is not already accessible: %w", err)
	}
	helper, err := exec.LookPath("fusermount3")
	if err != nil {
		return err
	}
	actual, err := filepath.EvalSymlinks(helper)
	if err != nil {
		return err
	}
	expected, err := filepath.EvalSymlinks("/usr/bin/fusermount3")
	if err != nil {
		return fmt.Errorf("packaged /usr/bin/fusermount3 required: %w", err)
	}
	if actual != expected {
		return fmt.Errorf("unexpected helper %q; place /usr/bin first in PATH", helper)
	}
	info, err := os.Stat(expected)
	if err != nil {
		return err
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok || st.Uid != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o022 != 0 {
		return errors.New("fusermount3 must be the existing root-owned, non-writable packaged helper")
	}
	return nil
}

type fixture struct {
	root, backing, mount, source string
	rootInfo                     os.FileInfo
	dirInfo                      map[string]os.FileInfo
	mountInfo                    os.FileInfo
	mountedID                    string
	server                       *fuse.Server
	control                      *control
	closed                       bool
}

func newFixture(t *testing.T, p profile) *fixture {
	t.Helper()
	root, err := os.MkdirTemp("", "javinizer-fuse-")
	must(t, err)
	f := &fixture{dirInfo: make(map[string]os.FileInfo), root: root, backing: filepath.Join(root, "backing"), mount: filepath.Join(root, "mount"), source: filepath.Join(root, "source")}
	f.rootInfo, err = os.Lstat(root)
	must(t, err)
	t.Cleanup(func() { f.close(t) })
	for _, path := range []string{f.backing, f.mount, f.source} {
		must(t, os.Mkdir(path, 0o700))
		f.dirInfo[path], err = os.Lstat(path)
		must(t, err)
	}
	f.mountInfo, err = os.Lstat(f.mount)
	must(t, err)
	base, err := fs.NewLoopbackRoot(f.backing)
	must(t, err)
	f.control = &control{profile: p, backing: f.backing}
	node := &faultNode{LoopbackNode: base.(*fs.LoopbackNode), control: f.control}
	node.RootData.RootNode = node
	node.RootData.NewNode = func(rd *fs.LoopbackRoot, _ *fs.Inode, _ string, _ *syscall.Stat_t) fs.InodeEmbedder {
		return &faultNode{LoopbackNode: &fs.LoopbackNode{RootData: rd}, control: f.control}
	}
	zero := time.Duration(0)
	f.server, err = fs.Mount(f.mount, node, &fs.Options{
		MountOptions: fuse.MountOptions{Name: "javinizer-test", FsName: "javinizer-synthetic", Options: []string{"nodev", "nosuid", "noexec"}, DisableSplice: true},
		AttrTimeout:  &zero, EntryTimeout: &zero, NegativeTimeout: &zero,
	})
	must(t, err)
	f.mountedID = exactMountID(t, f.mount)
	if f.mountedID == "" {
		t.Fatal("new FUSE mount absent from mountinfo")
	}
	var st unix.Statfs_t
	must(t, unix.Statfs(f.mount, &st))
	if st.Type != unix.FUSE_SUPER_MAGIC {
		t.Fatalf("not a kernel FUSE mount: type=%x", st.Type)
	}
	mounted, err := os.Stat(f.mount)
	must(t, err)
	if mounted.Sys().(*syscall.Stat_t).Dev == f.rootInfo.Sys().(*syscall.Stat_t).Dev {
		t.Fatal("FUSE mount did not obtain a distinct device")
	}
	if !hasMount(t, f.mount) {
		t.Fatal("FUSE mount absent from /proc/self/mountinfo")
	}

	// Successful round-trip through the mounted namespace AND counted daemon
	// WRITE guards against testing the ordinary empty mountpoint by accident.
	canary := []byte("javinizer kernel FUSE canary\x00\xff")
	must(t, os.WriteFile(filepath.Join(f.mount, "canary"), canary, 0o600))
	assertBytes(t, filepath.Join(f.backing, "canary"), canary)
	counts := f.control.snapshot()
	if counts.Creates != 1 || counts.WriteCalls == 0 || counts.WrittenBytes != uint64(len(canary)) {
		t.Fatalf("daemon canary not exercised: %+v", counts)
	}
	must(t, os.Remove(filepath.Join(f.mount, "canary")))
	f.control.mu.Lock()
	f.control.counts = counters{}
	f.control.mu.Unlock()
	t.Logf("FUSE_MOUNT_VERIFIED type=%x device=%d", st.Type, mounted.Sys().(*syscall.Stat_t).Dev)
	return f
}

func hasMount(t *testing.T, path string) bool {
	t.Helper()
	data, err := os.ReadFile("/proc/self/mountinfo")
	must(t, err)
	// mkdtemp paths may inherit spaces through TMPDIR. mountinfo escapes them.
	escaped := mountInfoPath(path)
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && (fields[4] == escaped || strings.HasPrefix(fields[4], escaped+"/")) {
			return true
		}
	}
	return false
}

func exactMountID(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile("/proc/self/mountinfo")
	must(t, err)
	return mountIDFrom(data, path)
}

func (f *fixture) close(t *testing.T) {
	t.Helper()
	if f.closed {
		return
	}
	current, err := os.Lstat(f.root)
	if err != nil || !os.SameFile(current, f.rootInfo) {
		t.Errorf("FUSE_CLEANUP_FAILED: root identity changed; refusing unmount and deletion: %v", err)
		return
	}
	if f.server != nil {
		if actualID := exactMountID(t, f.mount); f.mountedID == "" || actualID != f.mountedID {
			t.Errorf("FUSE_CLEANUP_FAILED: mount identity changed (expected %s, got %s); refusing unmount and deletion", f.mountedID, actualID)
			return
		}
		if err := f.server.Unmount(); err != nil {
			t.Errorf("FUSE_CLEANUP_FAILED: unmount %s: %v; fixture retained untouched", f.mount, err)
			return
		}
		f.server = nil
	}
	if hasMount(t, f.root) {
		t.Errorf("FUSE_CLEANUP_FAILED: remaining mount under %s; fixture retained", f.root)
		return
	}
	current, err = os.Lstat(f.root)
	if err != nil || !os.SameFile(current, f.rootInfo) {
		t.Errorf("FUSE_CLEANUP_FAILED: root identity changed; fixture retained: %v", err)
		return
	}
	if f.mountInfo != nil {
		current, err = os.Lstat(f.mount)
		if err != nil || !os.SameFile(current, f.mountInfo) {
			t.Errorf("FUSE_CLEANUP_FAILED: mountpoint identity changed; fixture retained: %v", err)
			return
		}
	}
	// Do not RemoveAll, lazy-unmount, recurse into unknown directories or erase
	// an active mount on any failure. This fixture only creates flat files.
	for _, dir := range []string{f.source, f.backing, f.mount} {
		info, err := os.Lstat(dir)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil || !info.IsDir() || !os.SameFile(info, f.dirInfo[dir]) {
			t.Errorf("FUSE_CLEANUP_FAILED: directory identity changed at %s; retained: %v", dir, err)
			return
		}
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Errorf("FUSE_CLEANUP_FAILED: %v", err)
			return
		}
		for _, entry := range entries {
			if entry.IsDir() {
				t.Errorf("FUSE_CLEANUP_FAILED: unexpected directory %s; retained", filepath.Join(dir, entry.Name()))
				return
			}
			if err := os.Remove(filepath.Join(dir, entry.Name())); err != nil {
				t.Errorf("FUSE_CLEANUP_FAILED: %v", err)
				return
			}
		}
		if err := os.Remove(dir); err != nil {
			t.Errorf("FUSE_CLEANUP_FAILED: %v", err)
			return
		}
	}
	if err := os.Remove(f.root); err != nil {
		t.Errorf("FUSE_CLEANUP_FAILED: %v", err)
		return
	}
	f.closed = true
	t.Log("FUSE_CLEANUP_VERIFIED: unmounted and removed owned fixture")
}

func must(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
func assertBytes(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	must(t, err)
	if !bytes.Equal(got, want) {
		t.Fatalf("bytes changed at %s: got %d bytes, want %d", path, len(got), len(want))
	}
}
func assertEntries(t *testing.T, path string, want ...string) {
	t.Helper()
	entries, err := os.ReadDir(path)
	must(t, err)
	got := make([]string, len(entries))
	for i, entry := range entries {
		got[i] = entry.Name()
	}
	if strings.Join(got, "\x00") != strings.Join(want, "\x00") {
		t.Fatalf("unexpected residue in %s: got %q, want %q", path, got, want)
	}
}
