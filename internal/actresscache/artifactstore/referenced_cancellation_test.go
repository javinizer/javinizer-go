package artifactstore

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/actresscache"
	"github.com/stretchr/testify/require"
)

// referencedReadContext preserves a real parent's cancellation semantics while
// permitting a test to pause one Err observation. Zero pauseAt just counts.
type referencedReadContext struct {
	context.Context
	calls   atomic.Int32
	pauseAt int32
	reached chan struct{}
	release chan struct{}
}

func (ctx *referencedReadContext) Err() error {
	if ctx.calls.Add(1) == ctx.pauseAt {
		close(ctx.reached)
		<-ctx.release
	}
	return ctx.Context.Err()
}

func cancelReferencedReadAt(t *testing.T, s *Store, digest string, pauseAt int32) (*actresscache.Snapshot, error) {
	t.Helper()
	parent, cancel := context.WithCancel(t.Context())
	ctx := &referencedReadContext{Context: parent, pauseAt: pauseAt, reached: make(chan struct{}), release: make(chan struct{})}
	finished := make(chan struct{})
	var snapshot *actresscache.Snapshot
	var loadErr error
	var once sync.Once
	release := func() { once.Do(func() { close(ctx.release) }) }
	join := func() {
		t.Helper()
		select {
		case <-finished:
		case <-time.After(5 * time.Second):
			t.Fatal("referenced read did not finish after cancellation")
		}
	}
	go func() {
		defer close(finished)
		snapshot, loadErr = s.LoadReferencedSnapshot(ctx, digest)
	}()
	defer func() { cancel(); release(); join() }()
	select {
	case <-ctx.reached:
	case <-finished:
		t.Fatal("referenced read returned before its cancellation boundary")
	case <-time.After(5 * time.Second):
		t.Fatal("referenced read did not reach its cancellation boundary")
	}
	cancel()
	release()
	join()
	require.ErrorIs(t, parent.Err(), context.Canceled)
	return snapshot, loadErr
}

func referencedDirectoryBytes(t *testing.T, path string) map[string][]byte {
	t.Helper()
	entries, err := os.ReadDir(path)
	require.NoError(t, err)
	files := make(map[string][]byte, len(entries))
	for _, entry := range entries {
		data, err := os.ReadFile(filepath.Join(path, entry.Name()))
		require.NoError(t, err)
		files[entry.Name()] = data
	}
	return files
}

func TestReferencedSnapshotCancellationDuringSourceValidation(t *testing.T) {
	s, path := openStore(t)
	data, _ := evidence(t, "Pinned job evidence")
	seed, err := actresscache.Decode(t.Context(), bytes.NewReader(data), limits().Artifact)
	require.NoError(t, err)
	records := seed.Records()
	for i := 1; i <= 2; i++ {
		record := records[0]
		record.SourceID = fmt.Sprintf("pinned-%d", i)
		records = append(records, record)
	}
	want, err := actresscache.Build(t.Context(), records, limits().Artifact)
	require.NoError(t, err)
	digest := writeReferencedObject(t, path, want.Bytes())
	currentData, receipt := evidence(t, "Different current generation")
	current := writeSlot(t, path, currentManifest, currentData, receipt)
	require.NotEqual(t, digest, current.ArtifactSHA256)
	before := referencedDirectoryBytes(t, path)

	// Calibrate only the successful native read and decode phases, not the
	// source-validation loop under test. Its first and final checks can then
	// be paused without hard-coded platform-specific read counts or sleeps.
	prefix := &referencedReadContext{Context: t.Context()}
	require.NoError(t, prefix.Err()) // initial request check
	raw, err := s.readRegular(prefix, digest+".ndjson", s.limits.Artifact.MaxBytes)
	require.NoError(t, err)
	decoded, err := actresscache.Decode(prefix, bytes.NewReader(raw), s.limits.Artifact)
	require.NoError(t, err)
	require.Equal(t, digest, decoded.Digest())
	require.Equal(t, raw, decoded.Bytes())
	prefixCalls := prefix.calls.Load()
	baseline := &referencedReadContext{Context: t.Context()}
	complete, err := s.LoadReferencedSnapshot(baseline, digest)
	require.NoError(t, err)
	require.Equal(t, want.Bytes(), complete.Bytes())
	require.Equal(t, prefixCalls+int32(len(records)), baseline.calls.Load())

	native := s.ops.openRead
	var opened atomic.Int32
	s.ops.openRead = func(root *os.Root, name string) (*os.File, error) {
		opened.Add(1)
		if name != digest+".ndjson" {
			return nil, fmt.Errorf("read outside admitted reference: %s", name)
		}
		return native(root, name)
	}
	for _, record := range []int{1, len(records)} {
		t.Run(fmt.Sprintf("before_record_%d", record), func(t *testing.T) {
			opened.Store(0)
			got, err := cancelReferencedReadAt(t, s, digest, prefixCalls+int32(record))
			require.ErrorIs(t, err, context.Canceled)
			require.Nil(t, got, "partially source-validated evidence must not escape")
			require.Equal(t, int32(1), opened.Load(), "cancellation must not retry or read the current manifest")
			require.Equal(t, before, referencedDirectoryBytes(t, path), "failed reads must not modify objects, manifests or create locks")

			// A live retry must still resolve precisely the admitted object,
			// rather than adopting the different current generation.
			retried, err := s.LoadReferencedSnapshot(t.Context(), digest)
			require.NoError(t, err)
			require.Equal(t, digest, retried.Digest())
			require.Equal(t, want.Bytes(), retried.Bytes())
			require.Equal(t, records, retried.Records())
			require.Equal(t, int32(2), opened.Load())
			require.Equal(t, before, referencedDirectoryBytes(t, path))
		})
	}
}
