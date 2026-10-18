package r18devdump

import (
	"context"
	"io"
)

// copyUploadStage checks shutdown between bounded staging reads and writes.
// Wrapping both ends prevents WriterTo/ReaderFrom from bypassing these checks.
// Cancellation cannot interrupt a filesystem syscall already in progress;
// callers must still wait for it before closing handles and removing the stage.
func copyUploadStage(ctx context.Context, dst io.Writer, src io.Reader) (int64, error) {
	n, err := io.Copy(uploadStageWriter{ctx, dst}, uploadStageReader{ctx, src})
	if err != nil {
		return n, err
	}
	return n, ctx.Err()
}

type uploadStageReader struct {
	ctx    context.Context
	reader io.Reader
}

func (r uploadStageReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	return r.reader.Read(p)
}

type uploadStageWriter struct {
	ctx    context.Context
	writer io.Writer
}

func (w uploadStageWriter) Write(p []byte) (int, error) {
	if err := w.ctx.Err(); err != nil {
		return 0, err
	}
	return w.writer.Write(p)
}
