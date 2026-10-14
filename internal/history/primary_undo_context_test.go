package history

import (
	"bytes"
	"context"
	"errors"
	"io"
	"testing"

	"github.com/stretchr/testify/require"
)

type primaryUndoReaderFunc func([]byte) (int, error)

func (f primaryUndoReaderFunc) Read(p []byte) (int, error) { return f(p) }

type primaryUndoWriterFunc func([]byte) (int, error)

func (f primaryUndoWriterFunc) Write(p []byte) (int, error) { return f(p) }

type primaryUndoNoFastReader struct{ io.Reader }

func (primaryUndoNoFastReader) WriteTo(io.Writer) (int64, error) { panic("WriterTo bypass") }

type primaryUndoNoFastWriter struct{ io.Writer }

func (primaryUndoNoFastWriter) ReadFrom(io.Reader) (int64, error) { panic("ReaderFrom bypass") }

func TestPrimaryUndoContextStreamingBoundsAndErrors(t *testing.T) {
	for _, phase := range []string{"normal", "before-read", "after-read", "after-write", "eof", "read-error", "write-error", "short-write"} {
		t.Run(phase, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			payload := bytes.Repeat([]byte("x"), primaryUndoCopyChunk*3)
			source := bytes.NewReader(payload)
			var destination bytes.Buffer
			reads, writes := 0, 0
			fault := errors.New("synthetic underlying I/O error")
			reader := primaryUndoReaderFunc(func(p []byte) (int, error) {
				require.NoError(t, ctx.Err(), "no additional read after cancellation")
				require.LessOrEqual(t, len(p), primaryUndoCopyChunk)
				reads++
				if phase == "eof" || phase == "read-error" {
					cancel()
					if phase == "eof" {
						return 0, io.EOF
					}
					return 0, fault
				}
				n, err := source.Read(p)
				if phase == "after-read" {
					cancel()
				}
				return n, err
			})
			writer := primaryUndoWriterFunc(func(p []byte) (int, error) {
				require.NoError(t, ctx.Err(), "no write started after cancellation")
				writes++
				if phase == "short-write" {
					return 0, nil
				}
				if phase == "write-error" {
					cancel()
					return 0, fault
				}
				n, err := destination.Write(p)
				if phase == "after-write" {
					cancel()
				}
				return n, err
			})
			if phase == "before-read" {
				cancel()
			}
			n, err := primaryUndoCopyContext(ctx, primaryUndoNoFastWriter{writer}, primaryUndoNoFastReader{reader})
			switch phase {
			case "normal":
				require.NoError(t, err)
				require.Equal(t, int64(len(payload)), n)
				require.Equal(t, payload, destination.Bytes())
			case "short-write":
				require.ErrorIs(t, err, io.ErrShortWrite)
			default:
				require.ErrorIs(t, err, context.Canceled)
				require.LessOrEqual(t, reads, 1)
				require.LessOrEqual(t, writes, 1)
				if phase == "read-error" || phase == "write-error" {
					require.ErrorIs(t, err, fault)
				}
				if phase != "after-write" {
					require.Zero(t, destination.Len())
				}
			}
		})
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	n, err := (primaryUndoContextWriter{ctx: ctx, dst: io.Discard}).Write([]byte("must not write"))
	require.Zero(t, n)
	require.ErrorIs(t, err, context.Canceled)
}

func TestPrimaryUndoCanceledMutationStillSyncsAndPreservesErrors(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	original := errors.New("original failure")
	syncFailure := errors.New("sync failure")
	syncParent := primaryUndoSyncParent
	t.Cleanup(func() { primaryUndoSyncParent = syncParent })
	calls := 0
	primaryUndoSyncParent = func(path string) error {
		require.Equal(t, "synthetic-mutated-path", path)
		calls++
		return syncFailure
	}
	require.ErrorIs(t, primaryUndoCanceledMutation(ctx, "synthetic-mutated-path", original), original)
	cancel()
	require.ErrorIs(t, primaryUndoCanceledMutation(ctx, "", original), original)
	require.Zero(t, calls)
	err := primaryUndoCanceledMutation(ctx, "synthetic-mutated-path", original)
	require.ErrorIs(t, err, original)
	require.ErrorIs(t, err, context.Canceled)
	require.ErrorIs(t, err, syncFailure)
	require.Equal(t, 1, calls)
}
