package workflow

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/fsutil"
	"github.com/stretchr/testify/require"
)

func TestSharedArtifactCoordinatorIdenticalGenericPaths(t *testing.T) {
	for _, name := range []string{"movie.nfo", "poster.jpg", "trailer.mp4"} {
		t.Run(name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), name)
			coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
			first, err := coordinator.Claim(t.Context(), path, "same", "part-1")
			require.NoError(t, err)
			require.True(t, first.OwnsPublication())

			result := make(chan struct {
				claim SharedArtifactClaim
				err   error
			}, 1)
			go func() {
				claim, err := coordinator.Claim(t.Context(), path, "same", "part-2")
				result <- struct {
					claim SharedArtifactClaim
					err   error
				}{claim, err}
			}()
			coordinator.Complete(first, SharedArtifactPublished)
			got := <-result
			require.NoError(t, got.err)
			require.False(t, got.claim.OwnsPublication())
			coordinator.Done("part-1")
			coordinator.Done("part-2")
		})
	}
}

func TestSharedArtifactCoordinatorConflictFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fanart.jpg")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	first, err := coordinator.Claim(t.Context(), path, "first", "part-1")
	require.NoError(t, err)
	require.True(t, first.OwnsPublication())
	coordinator.Complete(first, SharedArtifactPublished)
	claim, err := coordinator.Claim(t.Context(), path, "different", "part-2")
	require.False(t, claim.OwnsPublication())
	require.ErrorContains(t, err, "content conflict")
}

func TestSharedArtifactCoordinatorPromotesAfterOwnerFailure(t *testing.T) {
	path := filepath.Join(t.TempDir(), "cover.jpg")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	first, err := coordinator.Claim(t.Context(), path, "same", "part-1")
	require.NoError(t, err)
	require.True(t, first.OwnsPublication())

	result := make(chan struct {
		claim SharedArtifactClaim
		err   error
	}, 1)
	go func() {
		claim, err := coordinator.Claim(t.Context(), path, "same", "part-2")
		result <- struct {
			claim SharedArtifactClaim
			err   error
		}{claim, err}
	}()
	coordinator.Complete(first, SharedArtifactSafeToPromote)
	coordinator.Done("part-1")
	got := <-result
	require.NoError(t, got.err)
	require.True(t, got.claim.OwnsPublication())
	coordinator.Complete(got.claim, SharedArtifactPublished)
}

func TestSharedArtifactCoordinatorPoisonFailsAllWaitersClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2", "part-3"})
	owner, err := coordinator.Claim(t.Context(), path, "same", "part-1")
	require.NoError(t, err)
	require.True(t, owner.OwnsPublication())

	waiter := make(chan error, 1)
	go func() {
		_, claimErr := coordinator.Claim(t.Context(), path, "same", "part-2")
		waiter <- claimErr
	}()
	coordinator.Complete(owner, SharedArtifactPoisoned)
	require.ErrorContains(t, <-waiter, "poisoned")

	_, err = coordinator.Claim(t.Context(), path, "same", "part-3")
	require.ErrorContains(t, err, "poisoned")
	coordinator.Done("part-1")
	coordinator.Done("part-2")
	coordinator.Done("part-3")
}

func TestSharedArtifactCoordinatorIncompleteOwnerPoisonsBucket(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	owner, err := coordinator.Claim(t.Context(), path, "same", "part-1")
	require.NoError(t, err)
	require.True(t, owner.OwnsPublication())
	coordinator.Done("part-1")

	_, err = coordinator.Claim(t.Context(), path, "same", "part-2")
	require.ErrorContains(t, err, "poisoned")
	coordinator.Done("part-2")
}

func TestSharedArtifactCoordinatorCancellationIsBounded(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	ctx, cancel := context.WithCancel(context.Background())
	started := make(chan struct{})
	result := make(chan error, 1)
	go func() {
		close(started)
		_, err := coordinator.Claim(ctx, path, "same", "part-2")
		result <- err
	}()
	<-started
	cancel()
	require.ErrorIs(t, <-result, context.Canceled)
	coordinator.Done("part-2")
	claim, err := coordinator.Claim(t.Context(), path, "same", "part-1")
	require.NoError(t, err)
	require.True(t, claim.OwnsPublication())
}

func TestSharedArtifactCoordinatorStressExactlyOneOwner(t *testing.T) {
	for iteration := 0; iteration < 100; iteration++ {
		const count = 32
		contenders := make([]string, count)
		for i := range contenders {
			contenders[i] = fmt.Sprintf("part-%03d", i)
		}
		coordinator := NewSharedArtifactCoordinator(contenders)
		path := filepath.Join(t.TempDir(), "screens", "shared.jpg")
		var owners atomic.Int64
		var wg sync.WaitGroup
		start := make(chan struct{})
		for _, contender := range contenders {
			contender := contender
			wg.Add(1)
			go func() {
				defer wg.Done()
				defer coordinator.Done(contender)
				<-start
				claim, err := coordinator.Claim(t.Context(), path, "same", contender)
				require.NoError(t, err)
				if claim.OwnsPublication() {
					owners.Add(1)
					coordinator.Complete(claim, SharedArtifactPublished)
				}
			}()
		}
		close(start)
		wg.Wait()
		require.Equal(t, int64(1), owners.Load())
		require.Empty(t, coordinator.paths)
		require.Empty(t, coordinator.rank)
		require.Empty(t, coordinator.done)
		require.Nil(t, coordinator.resolver)
	}
}

func TestSharedArtifactCoordinatorDefensiveLifecycle(t *testing.T) {
	var nilCoordinator *SharedArtifactCoordinator
	claim, err := nilCoordinator.Claim(t.Context(), "unused", "digest", "part")
	require.NoError(t, err)
	require.True(t, claim.OwnsPublication())
	nilCoordinator.Complete(claim, SharedArtifactPublished)
	nilCoordinator.Done("part")

	path := filepath.Join(t.TempDir(), "movie.nfo")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	claim, err = coordinator.Claim(t.Context(), path, "first", "unknown")
	require.False(t, claim.OwnsPublication())
	require.ErrorContains(t, err, "not registered")

	absFailure := NewSharedArtifactCoordinator([]string{"part"})
	absFailure.absolute = func(string) (string, error) { return "", errors.New("cwd unavailable") }
	_, err = absFailure.Claim(t.Context(), path, "first", "part")
	require.ErrorContains(t, err, "canonicalize shared artifact destination")

	first, err := coordinator.Claim(t.Context(), path, "first", "part-1")
	require.NoError(t, err)
	require.True(t, first.OwnsPublication())
	claim, err = coordinator.Claim(t.Context(), path, "first", "part-1")
	require.NoError(t, err)
	require.True(t, claim.OwnsPublication())
	claim, err = coordinator.Claim(t.Context(), path, "changed", "part-1")
	require.False(t, claim.OwnsPublication())
	require.ErrorContains(t, err, "changed content")

	coordinator.Complete(SharedArtifactClaim{}, SharedArtifactPublished)
	coordinator.Complete(first, SharedArtifactPublished)
	coordinator.Complete(first, SharedArtifactPublished) // stale completion cannot republish
	coordinator.Done("unknown")
	coordinator.Done("part-1")
	claim, err = coordinator.Claim(t.Context(), path, "first", "part-1")
	require.ErrorContains(t, err, "already done")
	coordinator.Done("part-1")
	second, err := coordinator.Claim(t.Context(), path, "first", "part-2")
	require.NoError(t, err)
	require.False(t, second.OwnsPublication())
	claim, err = coordinator.Claim(t.Context(), path, "first", "part-2")
	require.NoError(t, err)
	require.False(t, claim.OwnsPublication())
}

func TestSharedArtifactCoordinatorRetiredWaitFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	first, err := coordinator.Claim(t.Context(), path, "same", "part-1")
	require.NoError(t, err)
	require.True(t, first.OwnsPublication())

	result := make(chan error, 1)
	go func() {
		_, claimErr := coordinator.Claim(t.Context(), path, "same", "part-2")
		result <- claimErr
	}()
	require.Eventually(t, func() bool {
		coordinator.mu.Lock()
		defer coordinator.mu.Unlock()
		bucket := coordinator.paths[first.key]
		return bucket != nil && bucket.candidates["part-2"] == "same"
	}, time.Second, time.Millisecond)

	coordinator.mu.Lock()
	delete(coordinator.paths, first.key)
	close(coordinator.changed)
	coordinator.changed = make(chan struct{})
	coordinator.mu.Unlock()

	require.ErrorContains(t, <-result, "retired before completion")
	coordinator.Done("part-1")
	coordinator.Done("part-2")
}

func TestSharedArtifactCoordinatorTransientProbeRecoveryKeepsClaimStable(t *testing.T) {
	root := t.TempDir()
	path := filepath.Join(root, "poster.jpg")

	previous := fsutil.CaseSensitiveProbe
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = previous
		fsutil.ResetCaseSensitivityCache()
	})
	fsutil.ResetCaseSensitivityCache()

	calls := 0
	fsutil.CaseSensitiveProbe = func(string) (bool, error) {
		calls++
		if calls == 1 {
			return false, errors.New("transient posture probe failure")
		}
		return false, nil
	}

	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	first, err := coordinator.Claim(t.Context(), path, "same", "part-1")
	require.NoError(t, err)
	require.True(t, first.OwnsPublication())

	_ = fsutil.DestKey(path) // recover process posture after this job froze its fallback
	coordinator.Complete(first, SharedArtifactPublished)
	coordinator.Done("part-1")

	second, err := coordinator.Claim(t.Context(), path, "same", "part-2")
	require.NoError(t, err)
	require.False(t, second.OwnsPublication(), "completion must use the first claim's immutable key")
	require.Equal(t, 2, calls, "the coordinator never re-probes its frozen root posture")
	coordinator.Done("part-2")
	require.Empty(t, coordinator.paths)
	require.Empty(t, coordinator.rank)
	require.Empty(t, coordinator.done)
	require.Nil(t, coordinator.resolver, "job completion releases the resolver posture cache")
}

func TestSharedArtifactCoordinatorRelativeAbsoluteAndDotAliasesShareBucket(t *testing.T) {
	cwd, err := os.Getwd()
	require.NoError(t, err)
	root, err := os.MkdirTemp(".", ".shared-artifact-coordinator-")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, os.RemoveAll(root)) })

	absolute, err := filepath.Abs(filepath.Join(root, "movie.nfo"))
	require.NoError(t, err)
	relative, err := filepath.Rel(cwd, absolute)
	require.NoError(t, err)
	dotted := filepath.Dir(absolute) + string(filepath.Separator) + "." + string(filepath.Separator) +
		"sub" + string(filepath.Separator) + ".." + string(filepath.Separator) + "movie.nfo"

	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2", "part-3"})
	first, err := coordinator.Claim(t.Context(), relative, "same", "part-1")
	require.NoError(t, err)
	require.True(t, first.OwnsPublication())
	coordinator.Complete(first, SharedArtifactPublished)
	coordinator.Done("part-1")

	for contender, alias := range map[string]string{"part-2": absolute, "part-3": dotted} {
		claim, claimErr := coordinator.Claim(t.Context(), alias, "same", contender)
		require.NoError(t, claimErr)
		require.False(t, claim.OwnsPublication(), "alias %q must consume the existing publication", alias)
		coordinator.Done(contender)
	}
}

func TestSharedArtifactCoordinatorWindowsExistingCaseAndSeparatorAliasesShareBucket(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("requires the Windows runtime and its case-insensitive temporary filesystem")
	}

	root := t.TempDir()
	directory := filepath.Join(root, "Library")
	require.NoError(t, os.Mkdir(directory, 0o755))
	firstPath := filepath.Join(directory, "Poster.jpg")
	require.NoError(t, os.WriteFile(firstPath, []byte("poster"), 0o600))
	secondPath := filepath.ToSlash(filepath.Join(root, "library", "poster.JPG"))
	_, err := os.Stat(secondPath)
	require.NoError(t, err, "both spellings must resolve to the existing file before testing coordination")

	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	first, err := coordinator.Claim(t.Context(), firstPath, "same", "part-1")
	require.NoError(t, err)
	require.True(t, first.OwnsPublication())
	coordinator.Complete(first, SharedArtifactPublished)
	coordinator.Done("part-1")
	require.Len(t, coordinator.paths, 1)

	second, err := coordinator.Claim(t.Context(), secondPath, "same", "part-2")
	require.NoError(t, err)
	require.False(t, second.OwnsPublication(), "the case-and-separator alias must consume the existing publication")
	require.Equal(t, first.key, second.key, "the default resolver must derive one destination bucket")
	require.Len(t, coordinator.paths, 1)
	coordinator.Done("part-2")
	require.Empty(t, coordinator.paths)
}

func TestSharedArtifactCoordinatorPOSIXExistingPathSemantics(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("requires POSIX path semantics")
	}

	t.Run("literal backslash is a distinct filename", func(t *testing.T) {
		root := t.TempDir()
		directory := filepath.Join(root, "library")
		require.NoError(t, os.Mkdir(directory, 0o755))
		slashPath := filepath.Join(directory, "poster.jpg")
		backslashPath := filepath.Join(root, `library\poster.jpg`)
		require.NoError(t, os.WriteFile(slashPath, []byte("slash"), 0o600))
		require.NoError(t, os.WriteFile(backslashPath, []byte("backslash"), 0o600))

		coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
		first, err := coordinator.Claim(t.Context(), slashPath, "same", "part-1")
		require.NoError(t, err)
		require.True(t, first.OwnsPublication())
		coordinator.Complete(first, SharedArtifactPublished)
		coordinator.Done("part-1")

		second, err := coordinator.Claim(t.Context(), backslashPath, "same", "part-2")
		require.NoError(t, err)
		require.True(t, second.OwnsPublication(), "a POSIX literal backslash must not become a separator")
		require.NotEqual(t, first.key, second.key)
		require.Len(t, coordinator.paths, 2)
		coordinator.Complete(second, SharedArtifactPublished)
		coordinator.Done("part-2")
	})

	t.Run("case-distinct files use distinct buckets", func(t *testing.T) {
		root := t.TempDir()
		upper := filepath.Join(root, "Poster.jpg")
		lower := filepath.Join(root, "poster.jpg")
		require.NoError(t, os.WriteFile(upper, []byte("upper"), 0o600))
		file, err := os.OpenFile(lower, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			t.Skip("the POSIX host filesystem is case-insensitive; Windows-style alias coverage exercises that posture")
		}
		require.NoError(t, err)
		require.NoError(t, file.Close())

		coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
		first, err := coordinator.Claim(t.Context(), upper, "same", "part-1")
		require.NoError(t, err)
		require.True(t, first.OwnsPublication())
		coordinator.Complete(first, SharedArtifactPublished)
		coordinator.Done("part-1")

		second, err := coordinator.Claim(t.Context(), lower, "same", "part-2")
		require.NoError(t, err)
		require.True(t, second.OwnsPublication(), "two existing case-distinct files need separate publications")
		require.NotEqual(t, first.key, second.key)
		require.Len(t, coordinator.paths, 2)
		coordinator.Complete(second, SharedArtifactPublished)
		coordinator.Done("part-2")
	})
}

func TestSharedArtifactCoordinatorDefaultResolverWiresInsensitiveCaseFold(t *testing.T) {
	root := t.TempDir()
	upper := filepath.Join(root, "Poster.jpg")
	lower := filepath.Join(root, "poster.jpg")
	require.NoError(t, os.WriteFile(upper, []byte("upper"), 0o600))
	require.NoError(t, os.WriteFile(lower, []byte("lower"), 0o600))

	previous := fsutil.CaseSensitiveProbe
	t.Cleanup(func() {
		fsutil.CaseSensitiveProbe = previous
		fsutil.ResetCaseSensitivityCache()
	})
	fsutil.ResetCaseSensitivityCache()
	fsutil.CaseSensitiveProbe = func(string) (bool, error) { return false, nil }

	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	first, err := coordinator.Claim(t.Context(), upper, "same", "part-1")
	require.NoError(t, err)
	require.True(t, first.OwnsPublication())
	coordinator.Complete(first, SharedArtifactPublished)
	coordinator.Done("part-1")

	second, err := coordinator.Claim(t.Context(), lower, "same", "part-2")
	require.NoError(t, err)
	require.False(t, second.OwnsPublication(), "the coordinator default must use DestKeyResolver's case fold")
	require.Equal(t, first.key, second.key)
	require.Len(t, coordinator.paths, 1)
	coordinator.Done("part-2")
}
func TestSharedArtifactCoordinatorFailedOwnerStillPinsDigest(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	first, err := coordinator.Claim(t.Context(), path, "first", "part-1")
	require.NoError(t, err)
	require.True(t, first.OwnsPublication())
	coordinator.Complete(first, SharedArtifactSafeToPromote)

	claim, err := coordinator.Claim(t.Context(), path, "different", "part-2")
	require.False(t, claim.OwnsPublication())
	require.ErrorContains(t, err, "content conflict")
}

func TestSharedArtifactCoordinatorDirectFilesystemDispositionProbe(t *testing.T) {
	t.Run("verified rollback permits exactly one second publication", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "movie.nfo")
		require.NoError(t, os.WriteFile(path, []byte("preclaim"), 0o600))
		coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
		owner, err := coordinator.Claim(t.Context(), path, "same", "part-1")
		require.NoError(t, err)
		require.NoError(t, os.WriteFile(path, []byte("owner-published"), 0o600))
		require.NoError(t, os.WriteFile(path, []byte("preclaim"), 0o600))
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "preclaim", string(got))
		coordinator.Complete(owner, SharedArtifactSafeToPromote)
		coordinator.Done("part-1")
		promoted, err := coordinator.Claim(t.Context(), path, "same", "part-2")
		require.NoError(t, err)
		require.True(t, promoted.OwnsPublication())
		require.NoError(t, os.WriteFile(path, []byte("promoted-published"), 0o600))
		coordinator.Complete(promoted, SharedArtifactPublished)
		coordinator.Done("part-2")
		got, err = os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "promoted-published", string(got))
		require.Empty(t, coordinator.paths)
		require.Empty(t, coordinator.rank)
		require.Empty(t, coordinator.done)
	})

	t.Run("uncertain rollback preserves bytes and blocks every digest", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "movie.nfo")
		require.NoError(t, os.WriteFile(path, []byte("owner-survived"), 0o600))
		coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2", "part-3"})
		owner, err := coordinator.Claim(t.Context(), path, "same", "part-1")
		require.NoError(t, err)
		coordinator.Complete(owner, SharedArtifactPoisoned)
		coordinator.Done("part-1")
		_, err = coordinator.Claim(t.Context(), path, "same", "part-2")
		require.ErrorContains(t, err, "poisoned")
		_, err = coordinator.Claim(t.Context(), path, "different", "part-3")
		require.Error(t, err)
		got, err := os.ReadFile(path)
		require.NoError(t, err)
		require.Equal(t, "owner-survived", string(got))
		coordinator.Done("part-2")
		coordinator.Done("part-3")
		require.Empty(t, coordinator.paths)
	})
}

func TestSharedArtifactCoordinatorUnknownDispositionFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "movie.nfo")
	coordinator := NewSharedArtifactCoordinator([]string{"part-1", "part-2"})
	owner, err := coordinator.Claim(t.Context(), path, "same", "part-1")
	require.NoError(t, err)
	coordinator.Complete(owner, SharedArtifactCompletionDisposition(255))
	coordinator.Done("part-1")
	_, err = coordinator.Claim(t.Context(), path, "same", "part-2")
	require.ErrorContains(t, err, "poisoned")
	coordinator.Done("part-2")
}
