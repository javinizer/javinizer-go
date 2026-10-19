package r18devdump

import (
	"bytes"
	"context"
	"errors"
	"io"
	"mime/multipart"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type stageReadFunc func([]byte) (int, error)

func (f stageReadFunc) Read(p []byte) (int, error) { return f(p) }

type stageWriteFunc func([]byte) (int, error)

func (f stageWriteFunc) Write(p []byte) (int, error) { return f(p) }

// These fast paths must never be used: they would bypass context checks.
type stageFastReader struct{ io.Reader }

func (stageFastReader) WriteTo(io.Writer) (int64, error) { panic("WriterTo bypassed cancellation") }

type stageFastWriter struct{ io.Writer }

func (stageFastWriter) ReadFrom(io.Reader) (int64, error) { panic("ReaderFrom bypassed cancellation") }

func TestUploadStageCooperativeCancellation(t *testing.T) {
	t.Run("success disables fast paths", func(t *testing.T) {
		var dst bytes.Buffer
		n, err := copyUploadStage(context.Background(), stageFastWriter{&dst}, stageFastReader{bytes.NewBufferString("tiny")})
		require.NoError(t, err)
		require.EqualValues(t, 4, n)
		require.Equal(t, "tiny", dst.String())
	})
	t.Run("already canceled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		_, err := copyUploadStage(ctx, io.Discard, stageReadFunc(func([]byte) (int, error) { t.Fatal("read after cancellation"); return 0, nil }))
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("cancellation during read prevents write", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, err := copyUploadStage(ctx, stageWriteFunc(func([]byte) (int, error) { t.Fatal("write after cancellation"); return 0, nil }), stageReadFunc(func(p []byte) (int, error) { cancel(); return copy(p, "tiny"), io.EOF }))
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("cancellation during write stops next read", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		reads := 0
		n, err := copyUploadStage(ctx, stageWriteFunc(func(p []byte) (int, error) { cancel(); return len(p), nil }), stageReadFunc(func(p []byte) (int, error) { reads++; return copy(p, "tiny"), nil }))
		require.ErrorIs(t, err, context.Canceled)
		require.Equal(t, 1, reads)
		require.EqualValues(t, 4, n)
	})
	t.Run("final write cancellation is returned", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		_, err := copyUploadStage(ctx, stageWriteFunc(func(p []byte) (int, error) { cancel(); return len(p), nil }), stageReadFunc(func(p []byte) (int, error) { return copy(p, "tiny"), io.EOF }))
		require.ErrorIs(t, err, context.Canceled)
	})
	t.Run("storage errors preserved", func(t *testing.T) {
		fault := errors.New("synthetic staging write failure")
		_, err := copyUploadStage(context.Background(), stageWriteFunc(func([]byte) (int, error) { return 0, fault }), bytes.NewBufferString("tiny"))
		require.ErrorIs(t, err, fault)
	})
}

type stageShutdownFile struct {
	*bytes.Reader
	entered, release chan struct{}
	closed           bool
}

func (f *stageShutdownFile) Read(p []byte) (int, error) {
	close(f.entered)
	<-f.release
	return f.Reader.Read(p)
}
func (f *stageShutdownFile) Close() error { f.closed = true; return nil }

func TestUploadShutdownDuringStageCleansAndJoins(t *testing.T) {
	h, path, srv := newUploadHandler(t)
	src := &stageShutdownFile{Reader: bytes.NewReader([]byte("tiny")), entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(src.release) }) }
	defer func() { unblock(); h.rt.Shutdown() }()
	h.openPartFn = func(*multipart.FileHeader) (multipart.File, error) { return src, nil }
	response := make(chan struct{})
	var status int
	var body string
	go func() {
		status, body, _ = srv.doUpload(t, buildUploadBody(t, []byte("tiny"), "synthetic.db"))
		close(response)
	}()
	select {
	case <-src.entered:
	case <-time.After(5 * time.Second):
		t.Fatal("stage read not entered")
	}
	stopped := make(chan struct{})
	go func() { h.rt.Shutdown(); close(stopped) }()
	require.Eventually(t, func() bool { return h.rt.ServerCtx().Err() != nil }, time.Second, time.Millisecond)
	select {
	case <-stopped:
		t.Fatal("shutdown returned before synchronous staging I/O completed")
	default:
	}
	unblock()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join staging cleanup")
	}
	select {
	case <-response:
	case <-time.After(5 * time.Second):
		t.Fatal("handler did not finish")
	}
	require.Equal(t, http.StatusInternalServerError, status)
	require.Contains(t, body, "stage upload: context canceled")
	require.True(t, src.closed)
	require.Nil(t, h.done, "canceled stage must not hand off a job")
	require.False(t, h.running)
	require.NoFileExists(t, path+".upload")
	assertNoUploadStages(t, path)
	require.NoFileExists(t, path)
	require.True(t, h.rt.WaitBackgroundTasks(time.Second))
}
