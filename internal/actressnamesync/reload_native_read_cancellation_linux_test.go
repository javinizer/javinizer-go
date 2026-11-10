package actressnamesync

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/unix"
)

const reloadLeaseHelperPath = "JAVINIZER_TEST_RELOAD_LEASE_PATH"
const reloadLeaseHelperTimeoutSeconds = 30

func TestReloadCancellationDuringNativeArtifactRead(t *testing.T) {
	if path := os.Getenv(reloadLeaseHelperPath); path != "" {
		holdReloadArtifactLease(t, path)
		return
	}

	f := newServiceFixture(t)
	s, _ := f.service(t, f.repo)
	s.initialize()
	old := s.generation
	review := f.review(t, s)
	ticket := s.tickets[review.ReviewID]
	before := reloadDatabaseState(t, f.db)
	next := f.publish(t, "更新名")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()

	var reloadFinished chan struct{}
	t.Cleanup(func() {
		cancel()
		if reloadFinished != nil {
			awaitServiceSignal(t, reloadFinished)
		}
	})
	events, releaseLease := startNativeArtifactReadLease(t, filepath.Join(f.directory, next.Digest()+".ndjson"))
	type reloadResult struct {
		info SourceInfo
		err  error
	}
	result := make(chan reloadResult, 1)
	reloadFinished = make(chan struct{})
	go func() {
		defer close(reloadFinished)
		info, err := s.Reload(ctx)
		result <- reloadResult{info, err}
	}()
	// The lease-break notification proves Reload reached the candidate's native
	// open. With the verified lease-break timeout longer than the helper's
	// lifetime, the open stays blocked until lease release or helper termination.
	require.Equal(t, "reader blocked", awaitServiceSignal(t, events))
	cancel()
	releaseLease()
	got := awaitServiceSignal(t, result)
	require.ErrorIs(t, got.err, context.Canceled)
	require.Equal(t, "reload_cancelled", got.info.ReloadProblemCode)
	require.Equal(t, old.snapshot.Digest(), got.info.ArtifactDigest)
	require.Same(t, old, s.generation)
	require.Equal(t, ticket, s.tickets[review.ReviewID])
	require.Empty(t, s.reload)
	require.Empty(t, s.confirm)
	page, err := s.SourcesAt(t.Context(), "", 0, 100, review.ArtifactDigest)
	require.NoError(t, err)
	require.Equal(t, review.ArtifactDigest, page.ArtifactDigest)
	require.Equal(t, before, reloadDatabaseState(t, f.db))
	requireServiceCounts(t, f.db, 0, 0)

	// Cancellation releases admission: a fresh request can publish the same
	// unchanged candidate and invalidate the old review in the usual way.
	info, err := s.Reload(t.Context())
	require.NoError(t, err)
	require.Equal(t, next.Digest(), info.ArtifactDigest)
	require.Empty(t, info.ReloadProblemCode)
	_, err = s.Confirm(t.Context(), fixturePrincipal, review.ReviewID, true)
	require.ErrorIs(t, err, ErrReviewExpired)
	fresh := f.review(t, s)
	require.Equal(t, next.Digest(), fresh.ArtifactDigest)
	require.Equal(t, "更新名", fresh.Proposals[0].AfterName)
	require.Equal(t, before, reloadDatabaseState(t, f.db))
	requireServiceCounts(t, f.db, 0, 0)
}

// startNativeArtifactReadLease runs the signal handler in a separate process.
// The caller must register its reader cleanup first, so a failure releases this
// lease before trying to join a reader blocked in a native open.
func startNativeArtifactReadLease(t *testing.T, path string) (<-chan string, func()) {
	t.Helper()
	executable, err := os.Executable()
	require.NoError(t, err)
	command := exec.CommandContext(t.Context(), executable, "-test.run=^TestReloadCancellationDuringNativeArtifactRead$", fmt.Sprintf("-test.timeout=%ds", reloadLeaseHelperTimeoutSeconds))
	command.Env = append(os.Environ(), reloadLeaseHelperPath+"="+path)
	var stderr bytes.Buffer
	command.Stderr = &stderr
	output, err := command.StdoutPipe()
	require.NoError(t, err)
	release, err := command.StdinPipe()
	require.NoError(t, err)
	require.NoError(t, command.Start())
	finished := make(chan struct{})
	var commandErr error
	var helperOutput bytes.Buffer
	events := make(chan string, 2)
	go func() {
		defer close(finished)
		defer close(events)
		scanner := bufio.NewScanner(output)
		for scanner.Scan() {
			fmt.Fprintln(&helperOutput, scanner.Text())
			if event := scanner.Text(); event == "leased" || event == "reader blocked" {
				events <- event
			}
		}
		commandErr = command.Wait()
		if err := scanner.Err(); err != nil {
			commandErr = errors.Join(commandErr, fmt.Errorf("read lease helper output: %w", err))
		}
	}()
	t.Cleanup(func() {
		_ = release.Close()
		select {
		case <-finished:
		default:
			_ = command.Process.Kill()
			awaitServiceSignal(t, finished)
		}
		if t.Failed() {
			t.Logf("lease helper: %v\n%s%s", commandErr, helperOutput.String(), stderr.String())
		}
	})
	require.Equal(t, "leased", awaitServiceSignal(t, events))
	return events, func() {
		t.Helper()
		_, err := fmt.Fprintln(release, "release")
		require.NoError(t, err)
		awaitServiceSignal(t, finished)
		require.NoError(t, commandErr, stderr.String())
	}
}

func holdReloadArtifactLease(t *testing.T, path string) {
	t.Helper()
	breakTime, err := os.ReadFile("/proc/sys/fs/lease-break-time")
	require.NoError(t, err, "unsupported environment: cannot verify the Linux lease-break timeout")
	breakSeconds, err := strconv.Atoi(strings.TrimSpace(string(breakTime)))
	require.NoError(t, err, "unsupported environment: invalid Linux lease-break timeout")
	require.Greater(t, breakSeconds, reloadLeaseHelperTimeoutSeconds,
		"unsupported environment: Linux lease-break timeout must exceed the helper lifetime; host settings are never changed")
	file, err := os.OpenFile(path, os.O_RDWR, 0)
	require.NoError(t, err)
	defer file.Close()
	notifications := make(chan os.Signal, 1)
	signal.Notify(notifications, unix.SIGIO)
	defer signal.Stop(notifications)
	_, err = unix.FcntlInt(file.Fd(), unix.F_SETOWN, os.Getpid())
	require.NoError(t, err)
	_, err = unix.FcntlInt(file.Fd(), unix.F_SETLEASE, unix.F_WRLCK)
	// Unsupported leases are a failure, never a silent skip or coverage claim.
	require.NoError(t, err, "native artifact-read cancellation requires Linux file leases")
	fmt.Println("leased")
	awaitServiceSignal(t, notifications)
	lease, err := unix.FcntlInt(file.Fd(), unix.F_GETLEASE, 0)
	require.NoError(t, err)
	require.Equal(t, unix.F_RDLCK, lease, "the pending read-open must have requested a lease downgrade")
	fmt.Println("reader blocked")
	line, err := bufio.NewReader(os.Stdin).ReadString('\n')
	require.NoError(t, err)
	require.Equal(t, "release\n", line)
	_, err = unix.FcntlInt(file.Fd(), unix.F_SETLEASE, unix.F_UNLCK)
	require.NoError(t, err)
}
