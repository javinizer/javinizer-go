package history

import (
	"context"
	"errors"
	"io"
)

const primaryUndoCopyChunk = 256 * 1024

// These wrappers intentionally expose neither WriterTo nor ReaderFrom, so a
// fast path cannot bypass cancellation checks or read an unbounded file at once.
// Cancellation cannot interrupt an already-running underlying Read or Write;
// it prevents the next chunk and discards a read completed after cancellation.
type primaryUndoContextReader struct {
	ctx context.Context
	src io.Reader
}

func (r primaryUndoContextReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := r.src.Read(p)
	if canceled := r.ctx.Err(); canceled != nil {
		return 0, errors.Join(err, canceled)
	}
	return n, err
}

type primaryUndoContextWriter struct {
	ctx context.Context
	dst io.Writer
}

func (w primaryUndoContextWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	n, err := w.dst.Write(p)
	return n, errors.Join(err, w.ctx.Err())
}

func primaryUndoCopyContext(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	return io.CopyBuffer(primaryUndoContextWriter{ctx: ctx, dst: dst}, primaryUndoContextReader{ctx: ctx, src: src}, make([]byte, primaryUndoCopyChunk))
}

// Cancellation stops new work, but a completed namespace mutation still needs
// its directory-sync tail. Retain all errors and the durable recovery journal;
// this does not grant permission to continue hashing, taking claims, or unlinking.
func primaryUndoCanceledMutation(ctx context.Context, path string, err error) error {
	if path == "" || ctx.Err() == nil {
		return err
	}
	return errors.Join(err, ctx.Err(), primaryUndoSyncParent(path))
}
