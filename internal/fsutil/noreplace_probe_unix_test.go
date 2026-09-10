//go:build !windows

package fsutil

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"sync/atomic"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/require"
)

func stubNoClobberProbe(t *testing.T, fn func(afero.Fs, string, string) error) {
	t.Helper()
	prev := publishNoClobberProbe
	publishNoClobberProbe = fn
	t.Cleanup(func() { publishNoClobberProbe = prev })
}

func TestNoClobberProbeVerdicts(t *testing.T) {
	for _, tc := range []struct {
		name       string
		err        error
		conclusive bool
	}{
		{"capable", nil, true},
		{"eperm", fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM), true},
		{"enosys", fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.ENOSYS), true},
		{"eopnotsupp", fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EOPNOTSUPP), true},
		{"enotsup", fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.ENOTSUP), true},
		{"io", syscall.EIO, false},
		{"access", syscall.EACCES, false},
		{"untyped capability", syscall.EPERM, false},
		{"unsupported without errno", ErrPublishNoReplaceUnsupported, false},
		{"unsupported transient", fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EACCES), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			calls := 0
			stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
				calls++
				require.Equal(t, dir, filepath.Dir(src))
				require.Equal(t, dir, filepath.Dir(dst))
				if calls > 1 || tc.err == nil {
					return PublishNoReplace(fs, src, dst)
				}
				return tc.err
			})
			err := ProbeNoClobberPublish(afero.NewOsFs(), dir)
			if tc.err == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.err)
			}
			err = ProbeNoClobberPublish(afero.NewOsFs(), filepath.Join(dir, "unused", ".."))
			if tc.conclusive && tc.err != nil {
				require.ErrorIs(t, err, tc.err)
			} else {
				require.NoError(t, err)
			}
			want := 1
			if !tc.conclusive {
				// Indeterminate verdicts are never cached, so the re-probe
				// statement above paid for another full attempt.
				want = 2
			}
			require.Equal(t, want, calls)
			entries, err := os.ReadDir(dir)
			require.NoError(t, err)
			require.Empty(t, entries)
		})
	}
}

func TestNoClobberProbeBypassAndPathFailure(t *testing.T) {
	stubNoClobberProbe(t, func(afero.Fs, string, string) error { t.Fatal("unexpected publish"); return nil })
	require.NoError(t, ProbeNoClobberPublish(afero.NewMemMapFs(), "/missing"))
	platform := noClobberProbePlatform
	noClobberProbePlatform = "windows"
	t.Cleanup(func() { noClobberProbePlatform = platform })
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), "/missing"))
	noClobberProbePlatform = platform
	prev := noClobberProbeAbs
	noClobberProbeAbs = func(string) (string, error) { return "", syscall.ENOENT }
	t.Cleanup(func() { noClobberProbeAbs = prev })
	require.ErrorContains(t, ProbeNoClobberPublish(afero.NewOsFs(), "."), "resolve no-clobber probe directory")
}

func TestNoClobberProbeCreationFailures(t *testing.T) {
	require.ErrorContains(t, ProbeNoClobberPublish(afero.NewOsFs(), filepath.Join(t.TempDir(), "missing")), "create no-clobber probe")
	prev := createNoClobberProbe
	t.Cleanup(func() { createNoClobberProbe = prev })
	var file afero.File
	createNoClobberProbe = func(fs afero.Fs, dest, suffix string, start uint64, mode os.FileMode) (string, afero.File, error) {
		path, fh, err := prev(fs, dest, suffix, start, mode)
		require.NoError(t, err)
		file = fh
		return path, &probeStatFailureFile{File: fh}, nil
	}
	dir := t.TempDir()
	require.ErrorContains(t, ProbeNoClobberPublish(afero.NewOsFs(), dir), "capture no-clobber probe identity")
	_, err := file.Stat()
	require.Error(t, err, "retained handle must still close")
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1, "unidentified probe retained")
}

type probeStatFailureFile struct{ afero.File }

func (f *probeStatFailureFile) Stat() (os.FileInfo, error) { return nil, syscall.EIO }

func TestNoClobberProbeExclusiveCollision(t *testing.T) {
	dir := t.TempDir()
	planted := filepath.Join(dir, ".nrprobe."+strconv.FormatUint(noreplaceOrdinal.Load()+1, 16))
	require.NoError(t, os.WriteFile(planted, []byte("foreign"), 0o600))
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	got, err := os.ReadFile(planted)
	require.NoError(t, err)
	require.Equal(t, "foreign", string(got))
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Len(t, entries, 1)
}

func TestNoClobberProbeBoundUnlink(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "probe")
	fh, err := os.Create(path)
	require.NoError(t, err)
	defer fh.Close()
	created, err := fh.Stat()
	require.NoError(t, err)
	foreign := filepath.Join(dir, "foreign")
	require.NoError(t, os.WriteFile(foreign, []byte("foreign"), 0o600))
	other, err := os.Stat(foreign)
	require.NoError(t, err)
	for _, tc := range []struct {
		name                       string
		fd                         caseProbeFile
		identity, lookup           os.FileInfo
		lookupErr, removeErr, want error
		removes                    int
	}{
		{"fstat error", w40FailStatFile{syscall.EIO}, created, created, nil, nil, syscall.EIO, 0},
		{"no identity", fh, nil, created, nil, nil, ErrTakeAsideForeign, 0},
		{"descriptor swap", w40ScratchFile{other}, created, created, nil, nil, ErrTakeAsideForeign, 0},
		{"lookup error", fh, created, nil, syscall.EACCES, nil, syscall.EACCES, 0},
		{"vanished", fh, created, nil, os.ErrNotExist, nil, os.ErrNotExist, 0},
		{"pathname swap", fh, created, other, nil, nil, ErrTakeAsideForeign, 0},
		{"unlink error", fh, created, created, nil, syscall.EPERM, syscall.EPERM, 1},
		{"unlink vanished", fh, created, created, nil, os.ErrNotExist, os.ErrNotExist, 1},
		{"success", fh, created, created, nil, nil, nil, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			removed := 0
			ops := caseProbeOps{
				stat:   func(p string) (os.FileInfo, error) { require.Equal(t, path, p); return tc.lookup, tc.lookupErr },
				remove: func(p string) error { require.Equal(t, path, p); removed++; return tc.removeErr },
				rename: func(string, string) error { t.Fatal("must never vacate"); return nil },
			}
			err := unlinkNoClobberProbe(ops, path, tc.fd, tc.identity)
			if tc.want == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tc.want)
			}
			require.Equal(t, tc.removes, removed)
		})
	}
}

func TestNoClobberProbeCleanupNeverMasksVerdict(t *testing.T) {
	for _, capable := range []bool{false, true} {
		for _, substitution := range []bool{false, true} {
			t.Run(fmt.Sprintf("capable=%v/substitution=%v", capable, substitution), func(t *testing.T) {
				dir := t.TempDir()
				prev := noClobberProbeOps
				t.Cleanup(func() { noClobberProbeOps = prev })
				if !substitution {
					noClobberProbeOps.remove = func(string) error { return syscall.EINVAL }
				}
				calls := 0
				var retained string
				refusal := fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM)
				stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
					calls++
					retained = src
					if capable {
						require.NoError(t, PublishNoReplace(fs, src, dst))
						retained = dst
					}
					if substitution {
						require.NoError(t, os.Rename(retained, retained+".saved"))
						require.NoError(t, os.WriteFile(retained, []byte("foreign"), 0o600))
					}
					if capable {
						return nil
					}
					return refusal
				})
				for i := 0; i < 2; i++ {
					err := ProbeNoClobberPublish(afero.NewOsFs(), dir)
					if capable {
						require.NoError(t, err)
					} else {
						require.ErrorIs(t, err, refusal)
						require.NotErrorIs(t, err, syscall.EINVAL)
					}
				}
				require.Equal(t, 1, calls)
				if substitution {
					got, err := os.ReadFile(retained)
					require.NoError(t, err)
					require.Equal(t, "foreign", string(got))
				}
			})
		}
	}
}

func TestNoClobberProbeSymlinkSubstitution(t *testing.T) {
	dir := t.TempDir()
	var source string
	stubNoClobberProbe(t, func(_ afero.Fs, src, _ string) error {
		source = src
		require.NoError(t, os.Rename(src, src+".saved"))
		require.NoError(t, os.Symlink(src+".saved", src))
		return fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM)
	})
	require.ErrorIs(t, ProbeNoClobberPublish(afero.NewOsFs(), dir), ErrPublishNoReplaceUnsupported)
	info, err := os.Lstat(source)
	require.NoError(t, err)
	require.NotZero(t, info.Mode()&os.ModeSymlink)
}

// TestNoClobberProbeCollisionRetry: a collision on the synthetic sibling is
// probe-name noise, not a verdict — the loop swaps in a fresh ordinal pair
// (codex P2, PR #255).
func TestNoClobberProbeCollisionRetry(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
		calls++
		if calls == 1 {
			return fmt.Errorf("planted sibling: %w", ErrPublishCollision)
		}
		return PublishNoReplace(fs, src, dst)
	})
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 2, calls)
	// verdict is conclusive and cached: a second probe pays nothing
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 2, calls)
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	require.Empty(t, entries)
}

// TestNoClobberProbeCollisionExhausted: persistent pair collisions fail as
// indeterminate (never cached) after noClobberProbeMaxAttempts.
func TestNoClobberProbeCollisionExhausted(t *testing.T) {
	dir := t.TempDir()
	calls := 0
	stubNoClobberProbe(t, func(afero.Fs, string, string) error {
		calls++
		return fmt.Errorf("planted sibling: %w", ErrPublishCollision)
	})
	err := ProbeNoClobberPublish(afero.NewOsFs(), dir)
	require.ErrorIs(t, err, ErrPublishCollision)
	require.Contains(t, err.Error(), "still occupied")
	require.Equal(t, noClobberProbeMaxAttempts, calls)
	// uncached: re-probing pays full attempts again
	_ = ProbeNoClobberPublish(afero.NewOsFs(), dir)
	require.Equal(t, 2*noClobberProbeMaxAttempts, calls)
}

func TestNoClobberProbeSingleFlightAndDirectoryCount(t *testing.T) {
	var calls atomic.Int32
	entered, release := make(chan struct{}), make(chan struct{})
	slow, other := t.TempDir(), t.TempDir()
	stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
		calls.Add(1)
		if filepath.Dir(src) == slow {
			close(entered)
			<-release
		}
		return PublishNoReplace(fs, src, dst)
	})
	var wg sync.WaitGroup
	errs := make(chan error, 32)
	for i := 0; i < 32; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); errs <- ProbeNoClobberPublish(afero.NewOsFs(), slow) }()
	}
	<-entered
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), other), "another key progresses during IO")
	close(release)
	wg.Wait()
	close(errs)
	for err := range errs {
		require.NoError(t, err)
	}
	require.EqualValues(t, 2, calls.Load())
	for i := 0; i < 20; i++ {
		dir := t.TempDir()
		for j := 0; j < 3; j++ {
			require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
		}
	}
	require.EqualValues(t, 22, calls.Load())
}

func TestNoClobberProbeWaiterSharesIndeterminate(t *testing.T) {
	dir := t.TempDir()
	stubNoClobberDirID(t, func(string) string { return "w" })
	key := dir + "|w"
	flight := &noClobberFlight{done: make(chan struct{}), err: syscall.EIO, id: "w"}
	noClobberCacheMu.Lock()
	noClobberInflight[key] = flight
	noClobberCacheMu.Unlock()
	close(flight.done)
	require.ErrorIs(t, ProbeNoClobberPublish(afero.NewOsFs(), dir), syscall.EIO)
	noClobberCacheMu.Lock()
	delete(noClobberInflight, key)
	_, cached := noClobberCache[key]
	noClobberCacheMu.Unlock()
	require.False(t, cached)
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
}

func stubNoClobberDirID(t *testing.T, fn func(string) string) {
	t.Helper()
	prev := noClobberProbeDirID
	noClobberProbeDirID = fn
	t.Cleanup(func() { noClobberProbeDirID = prev })
}

// TestNoClobberProbeCacheInvalidatesOnDirectoryIdentityChange: a
// remount/retarget under the same absolute path changes the identity in the
// cache key, so a conclusive verdict from the old filesystem is re-probed
// against the new one instead of being served forever (codex P2, PR #255).
func TestNoClobberProbeCacheInvalidatesOnDirectoryIdentityChange(t *testing.T) {
	dir := t.TempDir()
	mount := "mountA"
	stubNoClobberDirID(t, func(string) string { return mount })
	refusal := fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM)
	calls := 0
	stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
		calls++
		if mount == "mountA" {
			return refusal
		}
		return PublishNoReplace(fs, src, dst)
	})
	require.ErrorIs(t, ProbeNoClobberPublish(afero.NewOsFs(), dir), refusal)
	require.ErrorIs(t, ProbeNoClobberPublish(afero.NewOsFs(), dir), refusal)
	require.Equal(t, 1, calls, "conclusive verdict caches within one filesystem identity")

	mount = "mountB"
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir), "identity change invalidates the cached refusal")
	require.Equal(t, 2, calls, "new identity pays for a fresh probe")
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 2, calls)
}

// TestNoClobberProbeDirectoryIdentityDegradation: identity lookup failures
// degrade to path-only caching instead of blocking, and the real identity
// helper handles both missing paths and live directories.
func TestNoClobberProbeDirectoryIdentityDegradation(t *testing.T) {
	stubNoClobberDirID(t, func(string) string { return "" })
	dir := t.TempDir()
	calls := 0
	stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
		calls++
		return PublishNoReplace(fs, src, dst)
	})
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 1, calls)

	require.Empty(t, noClobberDirIdentity(filepath.Join(t.TempDir(), "missing")))
	require.NotEmpty(t, noClobberDirIdentity(t.TempDir()))
}

type probeNonStatTInfo struct{}

func (probeNonStatTInfo) Name() string       { return "fake" }
func (probeNonStatTInfo) Size() int64        { return 0 }
func (probeNonStatTInfo) Mode() os.FileMode  { return os.ModeDir }
func (probeNonStatTInfo) ModTime() time.Time { return time.Now() }
func (probeNonStatTInfo) IsDir() bool        { return true }
func (probeNonStatTInfo) Sys() any           { return struct{}{} }

// TestNoClobberDirIdentityDegradedInputs: identity lookup fails closed when
// the stat errors or the platform reports a non-Stat_t identity source,
// degrading callers to path-only cache keys instead of blocking probes.
func TestNoClobberDirIdentityDegradedInputs(t *testing.T) {
	prev := noClobberProbeStat
	t.Cleanup(func() { noClobberProbeStat = prev })

	noClobberProbeStat = func(string) (os.FileInfo, error) { return nil, syscall.EIO }
	require.Empty(t, noClobberDirIdentity(t.TempDir()))

	noClobberProbeStat = func(string) (os.FileInfo, error) { return probeNonStatTInfo{}, nil }
	require.Empty(t, noClobberDirIdentity(t.TempDir()))
}

// TestNoClobberProbeIdentityDriftForbidsCaching: an identity sample taken
// before the probe that differs after proves the verdict belongs to another
// filesystem — it must never be cached under the sampled key, and the next
// call re-probes (codex P2, PR #255).
func TestNoClobberProbeIdentityDriftForbidsCaching(t *testing.T) {
	dir := t.TempDir()
	ids := []string{"A", "B", "B", "B"}
	idx := 0
	stubNoClobberDirID(t, func(string) string {
		v := ids[idx]
		if idx < len(ids)-1 {
			idx++
		}
		return v
	})
	calls := 0
	stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
		calls++
		return PublishNoReplace(fs, src, dst)
	})
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 2, calls, "drifted verdict skipped the cache; steady identity re-probed and cached")
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 2, calls)
}

// TestNoClobberProbePerpetualIdentityFlapReturnsIndeterminate: identities
// that disagree on every sampling window must not surface any physical
// probe verdict to CopyFileNoReplace — each drifted result describes the
// other filesystem, so after the retry budget the caller gets an
// indeterminate unstable-identity error and nothing is cached
// (codex P2, PR #255). The flap alternates between a real identity and the
// empty degradation marker, covering both cache-key arms mid-round.
func TestNoClobberProbePerpetualIdentityFlapReturnsIndeterminate(t *testing.T) {
	dir := t.TempDir()
	idx := 0
	stubNoClobberDirID(t, func(string) string {
		idx++
		if idx%2 == 0 {
			return "A"
		}
		return ""
	})
	calls := 0
	stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
		calls++
		return PublishNoReplace(fs, src, dst)
	})
	require.ErrorIs(t, ProbeNoClobberPublish(afero.NewOsFs(), dir), ErrNoClobberProbeUnstable)
	require.Equal(t, noClobberProbeMaxAttempts, calls, "every round is fully re-probed while identities flap")
	require.ErrorIs(t, ProbeNoClobberPublish(afero.NewOsFs(), dir), ErrNoClobberProbeUnstable)
	require.Equal(t, 2*noClobberProbeMaxAttempts, calls, "unstable rounds are never cached")
}

// TestNoClobberProbeCachedVerdictRevalidatedAtReturn: a remount between the
// identity sample and the cache hit must not serve the previous
// filesystem's verdict — the lookup restarts against the new identity and
// probes for the live mount (codex P2, PR #255).
func TestNoClobberProbeCachedVerdictRevalidatedAtReturn(t *testing.T) {
	dir := t.TempDir()
	ids := []string{"A", "A", "A", "B", "B", "B", "B"}
	idx := 0
	stubNoClobberDirID(t, func(string) string {
		v := ids[idx]
		if idx < len(ids)-1 {
			idx++
		}
		return v
	})
	refusal := fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM)
	calls := 0
	stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
		calls++
		if calls == 1 {
			return refusal // the old filesystem's verdict: incapable
		}
		return PublishNoReplace(fs, src, dst)
	})
	// Round one: A's verdict (refusal) is cached under A.
	require.ErrorIs(t, ProbeNoClobberPublish(afero.NewOsFs(), dir), refusal)
	require.Equal(t, 1, calls)
	// Round two: identity drifted to B by return time — the stale A verdict
	// must be discarded and the live mount probed (new filesystem: capable).
	// Without revalidation this call would incorrectly serve A's refusal.
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 2, calls)
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 2, calls, "B's verdict is cached")
}

// TestNoClobberProbeWaiterRevalidatesOnWake: a waiter that joined a flight
// keyed under one identity must not reuse the completed verdict when the
// filesystem identity changed while it waited — it re-enters the whole
// lookup against the new identity and probes (codex P2, PR #255).
// Deterministic via a pre-completed flight and a scripted identity stub.
func TestNoClobberProbeWaiterRevalidatesOnWake(t *testing.T) {
	dir := t.TempDir()
	// Sequence: first sample is A (joining the pre-armed A-flight), every
	// later sample is B — the wake-time revalidation sees the drift.
	seq := 0
	stubNoClobberDirID(t, func(string) string {
		seq++
		if seq == 1 {
			return "A"
		}
		return "B"
	})
	calls := 0
	stubNoClobberProbe(t, func(fs afero.Fs, src, dst string) error {
		calls++
		return PublishNoReplace(fs, src, dst)
	})
	// The flight's verdict was produced under identity A.
	flight := &noClobberFlight{done: make(chan struct{}), err: fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM), id: "A"}
	noClobberCacheMu.Lock()
	noClobberInflight[dir+"|A"] = flight
	noClobberCacheMu.Unlock()
	t.Cleanup(func() {
		noClobberCacheMu.Lock()
		delete(noClobberInflight, dir+"|A")
		noClobberCacheMu.Unlock()
	})
	close(flight.done) // simulate the flight completing while this caller waited

	// Without wake-time revalidation the waiter would return the flight's
	// refusal. Instead it notices the drift, re-probes under B and succeeds.
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 1, calls)
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	require.Equal(t, 1, calls, "B's verdict is cached")
}

// TestNoClobberProbeSustainedCacheDriftExitsIndeterminate: every
// sample/revalidation pair disagreeing across the bounded round loop must
// terminate with ErrNoClobberProbeUnstable — iteratively, without running a
// physical probe and without recursion (codex P2, PR #255).
func TestNoClobberProbeSustainedCacheDriftExitsIndeterminate(t *testing.T) {
	dir := t.TempDir()
	sample := 0
	stubNoClobberDirID(t, func(string) string {
		sample++
		// A,B / B,C / C,D: each round's sample differs from its revalidation,
		// so cached verdicts are never usable.
		return string(rune('A' + sample/2))
	})
	refusal := fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM)
	noClobberCacheMu.Lock()
	for _, id := range []string{"A", "B", "C"} {
		noClobberCache[dir+"|"+id] = refusal
	}
	noClobberCacheMu.Unlock()
	t.Cleanup(func() {
		noClobberCacheMu.Lock()
		for _, id := range []string{"A", "B", "C"} {
			delete(noClobberCache, dir+"|"+id)
		}
		noClobberCacheMu.Unlock()
	})
	calls := 0
	stubNoClobberProbe(t, func(afero.Fs, string, string) error { calls++; return nil })

	err := ProbeNoClobberPublish(afero.NewOsFs(), dir)
	require.ErrorIs(t, err, ErrNoClobberProbeUnstable)
	require.Zero(t, calls, "stale-identity hits never reach the physical probe")
}
func TestNoClobberProbeCopyRefusesBeforeOpeningSource(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source"), filepath.Join(dir, "out", "movie.mp4")
	// A directory cannot be streamed as a file: the preflight error must win
	// before opening/streaming the source, with no payload staging at all.
	require.NoError(t, os.Mkdir(src, 0o700))
	refusal := fmt.Errorf("%w: %w", ErrPublishNoReplaceUnsupported, syscall.EPERM)
	stubNoClobberProbe(t, func(afero.Fs, string, string) error { return refusal })
	require.ErrorIs(t, CopyFileNoReplace(afero.NewOsFs(), src, dst), refusal)
	entries, err := os.ReadDir(filepath.Dir(dst))
	require.NoError(t, err)
	require.Empty(t, entries)
	_, err = os.Stat(src)
	require.NoError(t, err)
}

func TestNoClobberProbePositiveCacheStillPublishes(t *testing.T) {
	dir := t.TempDir()
	src, dst := filepath.Join(dir, "source"), filepath.Join(dir, "out")
	require.NoError(t, os.WriteFile(src, []byte("payload"), 0o600))
	require.NoError(t, ProbeNoClobberPublish(afero.NewOsFs(), dir))
	hookNoReplacePlantSeams(t, func() { _ = os.WriteFile(dst, []byte("foreign"), 0o600) })
	require.True(t, errors.Is(CopyFileNoReplace(afero.NewOsFs(), src, dst), ErrPublishCollision))
	got, err := os.ReadFile(dst)
	require.NoError(t, err)
	require.Equal(t, "foreign", string(got))
}
