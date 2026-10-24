// Package artifactstore publishes validated offline evidence in a private local
// directory. It has no application runtime, network or identity mutation wiring.
package artifactstore

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/javinizer/javinizer-go/internal/actresscache/sources/r18dump"
)

// ErrBusy means another writer owns this instance or the directory's OS lock.
var ErrBusy = errors.New("artifact writer busy")

// ErrEmpty means neither recovery slot contains a committed manifest.
var ErrEmpty = errors.New("no committed artifact")

// ErrReadContention means a manifest kept changing through the finite read
// budget. It does not classify the committed generations as corrupt or empty.
var ErrReadContention = errors.New("manifest read retry budget exhausted")

const (
	manifestReadAttempts = 3
	currentManifest      = "current.json"
	previousManifest     = "previous.json"
)

var errManifestChanged = errors.New("regular manifest replaced during open")

var errMissingManifest = errors.New("manifest leaf absent")
var errReplaceAttempted = errors.New("replacement syscall attempted")

// ErrCommitUncertain means current replacement returned an error after being
// attempted. An error is not proof of nonexecution; inspect validated recovery
// slots before choosing a subsequent action. No automatic rollback occurs.
var ErrCommitUncertain = errors.New("current manifest visibility uncertain")

// Limits bounds canonical manifest bytes, upstream receipt claims and the
// artifact decoder. Every budget must be positive. Directory accounting is
// bounded by MaxDirectoryEntries and conservatively counts hard links twice.
type Limits struct {
	MaxDirectoryEntries int
	MaxStoredBytes      int64
	Artifact            actresscache.Limits
	MaxManifestBytes    int64
	MaxSourceBytes      int64
	MaxSourceRows       int
}

// Manifest binds exact canonical artifact bytes to an asserted supplied-file
// receipt. Scope is always supplied-file-only: no pruning authority is expressed.
type Manifest struct {
	Version        int             `json:"version"`
	Scope          string          `json:"scope"`
	ArtifactSHA256 string          `json:"artifact_sha256"`
	ArtifactBytes  int64           `json:"artifact_bytes"`
	Source         r18dump.Receipt `json:"source"`
}

// View owns a validated snapshot and its manifest. Recovered identifies fallback
// to previous.json; callers must not silently interpret recovery as a new refresh.
type View struct {
	Snapshot  *actresscache.Snapshot
	Manifest  Manifest
	Recovered bool
}

// Result distinguishes visibility from durability. Published means the current
// manifest replacement succeeded, even if a subsequent directory sync failed.
// CommitAttempted also identifies failed replacement attempts of uncertain
// visibility. FilesSynced records successful file Sync calls; DirectorySynced records the
// final directory Sync, not a universal power-loss guarantee.
type Result struct {
	CommitAttempted bool
	Published       bool
	FilesSynced     bool
	DirectorySynced bool
}

// Store pins an existing private directory with os.Root. Cooperating instances
// use a retained lock-file inode; callers must exclude untrusted directory writers.
// Close must not race operations; a Store must not be copied.
type Store struct {
	root   *os.Root
	limits Limits
	writer sync.Mutex
	ops    operations
}

type operations struct {
	openRead   func(*os.Root, string) (*os.File, error)
	openFile   func(*os.Root, string, int, os.FileMode) (*os.File, error)
	readDir    func(*os.File, int) ([]os.DirEntry, error)
	entryInfo  func(os.DirEntry) (os.FileInfo, error)
	lockHandle func(*os.File) error
	write      func(*os.File, []byte) (int, error)
	syncFile   func(*os.File) error
	closeFile  func(*os.File) error
	syncDir    func(*os.Root) error
	link       func(*os.Root, string, string) error
	replace    func(*os.Root, string, string) error
}

// Open opens an existing absolute clean non-symlink directory. It does not create
// directories or edit permissions. Root-relative access cannot escape that root.
func Open(path string, limits Limits) (*Store, error) {
	return openWithRoot(path, limits, os.OpenRoot)
}

func openWithRoot(path string, limits Limits, openRoot func(string) (*os.Root, error)) (*Store, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || limits.MaxManifestBytes <= 0 || limits.MaxManifestBytes > 1<<20 || limits.MaxSourceBytes <= 0 || limits.MaxSourceRows <= 0 || limits.MaxDirectoryEntries <= 0 || limits.MaxDirectoryEntries > 1_000_000 || limits.MaxStoredBytes <= 0 {
		return nil, fmt.Errorf("invalid root or limits")
	}
	if _, err := actresscache.Build(context.Background(), nil, limits.Artifact); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
		return nil, fmt.Errorf("root must be a real directory")
	}
	if err := privateDirectory(before); err != nil {
		return nil, err
	}
	root, err := openRoot(path)
	if err != nil {
		return nil, err
	}
	after, err := root.Stat(".")
	if err != nil || !os.SameFile(before, after) {
		_ = root.Close()
		return nil, errors.Join(errors.New("root identity changed"), err)
	}
	return &Store{root: root, limits: limits, ops: defaultOperations()}, nil
}

func defaultOperations() operations {
	return operations{
		openRead: (*os.Root).Open, openFile: (*os.Root).OpenFile, readDir: (*os.File).ReadDir,
		entryInfo: os.DirEntry.Info, lockHandle: lockHandle,
		write: (*os.File).Write, syncFile: (*os.File).Sync, closeFile: (*os.File).Close, syncDir: syncDirectory,
		link: (*os.Root).Link, replace: replaceManifest,
	}
}

// Close closes the directory capability. Callers must finish operations first.
func (s *Store) Close() error { return s.root.Close() }

// Load validates current.json and its exact artifact; on any invalid current
// slot, it tries previous.json. It never selects orphan artifacts or staging files.
func (s *Store) Load(ctx context.Context) (View, error) {
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	current, err := s.loadSlot(ctx, currentManifest)
	if err == nil {
		return current, nil
	}
	if ctx.Err() != nil {
		return View{}, ctx.Err()
	}
	previous, previousErr := s.loadSlot(ctx, previousManifest)
	if previousErr == nil {
		previous.Recovered = true
		return previous, nil
	}
	if err := ctx.Err(); err != nil {
		return View{}, err
	}
	if errors.Is(err, errMissingManifest) && errors.Is(previousErr, errMissingManifest) {
		return View{}, ErrEmpty
	}
	return View{}, fmt.Errorf("recovery read failed: %w", errors.Join(err, previousErr))
}

func (s *Store) loadSlot(ctx context.Context, name string) (View, error) {
	data, err := s.readManifest(ctx, name)
	if err != nil {
		return View{}, err
	}
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return View{}, err
	}
	canonical, _ := json.Marshal(m)
	canonical = append(canonical, '\n')
	if !bytes.Equal(data, canonical) || m.Version != 1 || m.Scope != "supplied-file-only" || !validDigest(m.ArtifactSHA256) || m.ArtifactBytes <= 0 || m.ArtifactBytes > s.limits.Artifact.MaxBytes {
		return View{}, fmt.Errorf("invalid canonical manifest")
	}
	artifact, err := s.readRegular(ctx, m.ArtifactSHA256+".ndjson", m.ArtifactBytes)
	if err != nil {
		return View{}, err
	}
	if int64(len(artifact)) != m.ArtifactBytes || fmt.Sprintf("%x", sha256.Sum256(artifact)) != m.ArtifactSHA256 {
		return View{}, fmt.Errorf("artifact digest or size mismatch")
	}
	snapshot, err := s.validate(ctx, artifact, m.Source)
	if err != nil {
		return View{}, err
	}
	return View{Snapshot: snapshot, Manifest: m}, nil
}

// Only a verified regular-manifest inode replacement is retryable. Parsing,
// digest/object errors, unsafe entries and arbitrary I/O failures are terminal.
func (s *Store) readManifest(ctx context.Context, name string) ([]byte, error) {
	for attempt := 0; attempt < manifestReadAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		data, err := s.readRegular(ctx, name, s.limits.MaxManifestBytes)
		if !errors.Is(err, errManifestChanged) {
			return data, err
		}
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return nil, ErrReadContention
}

func (s *Store) validate(ctx context.Context, data []byte, receipt r18dump.Receipt) (*actresscache.Snapshot, error) {
	if !validDigest(receipt.SHA256) || receipt.Bytes <= 0 || receipt.Bytes > s.limits.MaxSourceBytes || receipt.Rows < 0 || receipt.Rows > s.limits.MaxSourceRows || receipt.ActressRows < 0 || receipt.ActressRows > receipt.Rows || receipt.EmptyActressBlock != (receipt.ActressRows == 0) {
		return nil, fmt.Errorf("invalid supplied-file receipt")
	}
	snapshot, err := actresscache.Decode(ctx, bytes.NewReader(data), s.limits.Artifact)
	if err != nil {
		return nil, err
	}
	if !bytes.Equal(data, snapshot.Bytes()) {
		return nil, fmt.Errorf("artifact must already be canonical")
	}
	records := snapshot.Records()
	if len(records) != receipt.ActressRows {
		return nil, fmt.Errorf("receipt record count mismatch")
	}
	for _, record := range records {
		if record.Source != "r18dev" || record.Provenance.DatasetSHA256 != receipt.SHA256 {
			return nil, fmt.Errorf("receipt provenance mismatch")
		}
	}
	return snapshot, nil
}

// Publish validates a bounded canonical artifact and asserted r18 dump receipt,
// installs immutable bytes, then replaces current.json with previous.json kept.
// Cancellation is checked immediately before every visibility-changing operation.
// After current replacement succeeds, cancellation cannot undo publication.
func (s *Store) Publish(ctx context.Context, input io.Reader, receipt r18dump.Receipt) (Result, error) {
	if !s.writer.TryLock() {
		return Result{}, ErrBusy
	}
	defer s.writer.Unlock()
	if err := ctx.Err(); err != nil {
		return Result{}, err
	}
	lock, err := s.lock()
	if err != nil {
		return Result{}, err
	}
	defer func() { _ = lock.Close() }()
	if input == nil {
		return Result{}, fmt.Errorf("nil artifact input")
	}
	data, err := readBounded(ctx, input, s.limits.Artifact.MaxBytes)
	if err != nil {
		return Result{}, err
	}
	snapshot, err := s.validate(ctx, data, receipt)
	if err != nil {
		return Result{}, err
	}
	m := Manifest{1, "supplied-file-only", snapshot.Digest(), int64(len(data)), receipt}
	manifest, _ := json.Marshal(m)
	manifest = append(manifest, '\n')
	if int64(len(manifest)) > s.limits.MaxManifestBytes {
		return Result{}, fmt.Errorf("manifest byte budget exceeded")
	}
	previous, err := s.Load(ctx)
	if err != nil && !errors.Is(err, ErrEmpty) {
		return Result{}, err
	}
	var old []byte
	if err == nil {
		old, _ = json.Marshal(previous.Manifest)
		old = append(old, '\n')
	}
	if err := s.capacity(ctx, int64(len(data)), int64(max(len(manifest), len(old)))); err != nil {
		return Result{}, err
	}
	if err := s.installObject(ctx, m.ArtifactSHA256+".ndjson", data); err != nil {
		return Result{}, err
	}
	if old != nil {
		if err := s.replaceSlot(ctx, previousManifest, old); err != nil {
			return Result{}, err
		}
		if err := s.ops.syncDir(s.root); err != nil {
			return Result{}, err
		}
	}
	if err := s.replaceSlot(ctx, currentManifest, manifest); err != nil {
		if errors.Is(err, errReplaceAttempted) {
			return Result{CommitAttempted: true, FilesSynced: true}, errors.Join(ErrCommitUncertain, err)
		}
		return Result{}, err
	}
	result := Result{CommitAttempted: true, Published: true, FilesSynced: true}
	if err := s.ops.syncDir(s.root); err != nil {
		return result, fmt.Errorf("published; final directory sync failed: %w", err)
	}
	result.DirectorySynced = true
	return result, nil
}

func (s *Store) capacity(ctx context.Context, artifactBytes, manifestBytes int64) error {
	dir, err := s.ops.openRead(s.root, ".")
	if err != nil {
		return err
	}
	defer func() { _ = dir.Close() }()
	entries, err := s.ops.readDir(dir, s.limits.MaxDirectoryEntries+1)
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if len(entries) > s.limits.MaxDirectoryEntries-4 {
		return fmt.Errorf("directory entry budget exceeded")
	}
	remaining := s.limits.MaxStoredBytes
	for _, entry := range entries {
		if err := ctx.Err(); err != nil {
			return err
		}
		name := entry.Name()
		allowed := name == currentManifest || name == previousManifest || name == ".writer.lock" || strings.HasPrefix(name, ".stage-") || (strings.HasSuffix(name, ".ndjson") && validDigest(strings.TrimSuffix(name, ".ndjson")))
		info, err := s.ops.entryInfo(entry)
		if err != nil {
			return err
		}
		if !allowed || !info.Mode().IsRegular() || info.Size() > remaining {
			return fmt.Errorf("unsafe entry or stored byte budget exceeded")
		}
		remaining -= info.Size()
	}
	// Reserve conservative peak occupancy, including stage+linked data and both
	// manifest stages. Never prune files to make room for a publication.
	for _, cost := range []int64{artifactBytes, artifactBytes, manifestBytes, manifestBytes, manifestBytes, manifestBytes} {
		if cost > remaining {
			return fmt.Errorf("publication peak byte budget exceeded")
		}
		remaining -= cost
	}
	return nil
}

func (s *Store) installObject(ctx context.Context, name string, data []byte) error {
	existing, err := s.readRegular(ctx, name, int64(len(data)))
	if err == nil {
		if !bytes.Equal(existing, data) {
			return fmt.Errorf("immutable object collision")
		}
		file, err := s.ops.openRead(s.root, name)
		if err != nil {
			return err
		}
		defer func() { _ = file.Close() }()
		return s.ops.syncFile(file)
	}
	if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	staged, err := s.stage(ctx, data)
	if err != nil {
		return err
	}
	defer func() { _ = s.root.Remove(staged) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ops.link(s.root, staged, name); err != nil {
		return err
	}
	// The hard-link is no-replace; never remove or overwrite an occupied object.
	return s.ops.syncDir(s.root)
}

func (s *Store) replaceSlot(ctx context.Context, name string, data []byte) error {
	// Reject an existing symlink/special entry before replacement. Rename itself
	// replaces the leaf, never follows it. No remove-destination fallback exists.
	if info, err := s.root.Lstat(name); err == nil {
		if !info.Mode().IsRegular() {
			return fmt.Errorf("unsafe manifest entry")
		}
	} else if !errors.Is(err, os.ErrNotExist) {
		return err
	}
	staged, err := s.stage(ctx, data)
	if err != nil {
		return err
	}
	defer func() { _ = s.root.Remove(staged) }()
	if err := ctx.Err(); err != nil {
		return err
	}
	if err := s.ops.replace(s.root, staged, name); err != nil {
		return errors.Join(errReplaceAttempted, err)
	}
	return nil
}

func (s *Store) stage(ctx context.Context, data []byte) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	name := ".stage-" + rand.Text()
	file, err := s.ops.openFile(s.root, name, os.O_CREATE|os.O_EXCL|os.O_RDWR, 0600)
	if err != nil {
		return "", err
	}
	failed := true
	defer func() {
		if failed {
			_ = file.Close()
			_ = s.root.Remove(name)
		}
	}()
	if n, err := s.ops.write(file, data); err != nil {
		return "", err
	} else if n != len(data) {
		return "", io.ErrShortWrite
	}
	if err := s.ops.syncFile(file); err != nil {
		return "", err
	}
	if err := s.ops.closeFile(file); err != nil {
		return "", err
	}
	failed = false
	return name, nil
}

func (s *Store) readRegular(ctx context.Context, name string, limit int64) ([]byte, error) {
	before, err := s.root.Lstat(name)
	if err != nil {
		if (name == currentManifest || name == previousManifest) && errors.Is(err, os.ErrNotExist) {
			return nil, errors.Join(errMissingManifest, err)
		}
		return nil, err
	}
	if !before.Mode().IsRegular() || before.Size() > limit {
		return nil, fmt.Errorf("unsafe or oversized store entry")
	}
	file, err := s.ops.openRead(s.root, name)
	if err != nil {
		return nil, err
	}
	defer func() { _ = file.Close() }()
	after, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if !after.Mode().IsRegular() {
		return nil, fmt.Errorf("opened store entry is not regular")
	}
	// Check the leaf without following it even when the descriptor still pins
	// the original inode: a symlink back to that inode is not a safe store entry.
	observed, err := s.root.Lstat(name)
	if err != nil {
		return nil, err
	}
	if !observed.Mode().IsRegular() || observed.Size() > limit {
		return nil, fmt.Errorf("unsafe or oversized opened store leaf")
	}
	if !os.SameFile(before, after) {
		if name == currentManifest || name == previousManifest {
			return nil, errManifestChanged
		}
		return nil, fmt.Errorf("entry identity changed")
	}
	// Immutable objects cannot change identity even after their descriptor opens.
	// A pinned manifest may remain a valid older committed generation instead.
	if name != currentManifest && name != previousManifest && !os.SameFile(after, observed) {
		return nil, fmt.Errorf("immutable object replaced after open")
	}

	return readBounded(ctx, file, limit)
}

func readBounded(ctx context.Context, input io.Reader, limit int64) ([]byte, error) {
	reader := &contextReader{ctx, input}
	data, err := io.ReadAll(io.LimitReader(reader, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, fmt.Errorf("byte budget exceeded")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return data, nil
}

type contextReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r *contextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}
func validDigest(value string) bool {
	// Receipt strings are caller supplied: bound decoding before allocating.
	if len(value) != 64 {
		return false
	}
	decoded, err := hex.DecodeString(value)
	return err == nil && len(decoded) == 32 && hex.EncodeToString(decoded) == value
}
