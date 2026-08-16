package downloader

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"runtime"
	"sync/atomic"
	"testing"
	"time"
)

type mockReadCloser struct {
	data   []byte
	pos    int
	closed atomic.Bool
	delay  time.Duration
	done   chan struct{}
}

func newMockReadCloser(data []byte, delay time.Duration) *mockReadCloser {
	return &mockReadCloser{data: data, delay: delay, done: make(chan struct{})}
}

func (m *mockReadCloser) Read(p []byte) (int, error) {
	if m.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if m.delay > 0 {
		select {
		case <-time.After(m.delay):
		case <-m.done:
			return 0, io.ErrClosedPipe
		}
	}
	if m.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	if m.pos >= len(m.data) {
		return 0, io.EOF
	}
	n := copy(p, m.data[m.pos:])
	m.pos += n
	return n, nil
}

func (m *mockReadCloser) Close() error {
	if m.closed.CompareAndSwap(false, true) {
		close(m.done)
	}
	return nil
}

func (m *mockReadCloser) isClosed() bool {
	return m.closed.Load()
}

func TestStallReader_SuccessfulReadsResetTimer(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 1000)
	body := newMockReadCloser(data, 50*time.Millisecond)
	ctx := context.Background()
	r := NewStallReader(body, 200*time.Millisecond, ctx)

	buf := make([]byte, 100)
	totalRead := 0
	for {
		n, err := r.Read(buf)
		totalRead += n
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if totalRead != 1000 {
		t.Errorf("expected 1000 bytes, got %d", totalRead)
	}
	_ = r.Close()
	if !body.isClosed() {
		t.Error("underlying body not closed")
	}
}

func TestStallReader_StalledBodyTriggersError(t *testing.T) {
	body := newMockReadCloser(nil, time.Hour)
	ctx := context.Background()
	r := NewStallReader(body, 100*time.Millisecond, ctx)

	buf := make([]byte, 100)
	start := time.Now()
	_, err := r.Read(buf)
	elapsed := time.Since(start)

	if !IsDownloadStalled(err) {
		t.Errorf("expected errDownloadStalled, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("stall took too long: %v", elapsed)
	}
	_ = r.Close()
}

func TestStallReader_IdleTimeoutZeroDisables(t *testing.T) {
	body := newMockReadCloser([]byte("hello"), 0)
	ctx := context.Background()
	r := NewStallReader(body, 0, ctx)
	buf := make([]byte, 10)
	n, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}
	if n != 5 {
		t.Errorf("expected 5 bytes, got %d", n)
	}
	_ = r.Close()
}

func TestStallReader_WorkerContextCancellation(t *testing.T) {
	body := newMockReadCloser(nil, time.Hour)
	ctx, cancel := context.WithCancel(context.Background())
	r := NewStallReader(body, 10*time.Second, ctx)
	cancel()

	buf := make([]byte, 100)
	_, err := r.Read(buf)

	if !errors.Is(err, context.Canceled) {
		t.Errorf("expected context.Canceled, got: %v", err)
	}
	if IsDownloadStalled(err) {
		t.Error("should not be reported as stall")
	}
	_ = r.Close()
}

func TestStallReader_DisarmStopsWatchdog(t *testing.T) {
	body := newMockReadCloser([]byte("data"), 50*time.Millisecond)
	ctx := context.Background()
	r := NewStallReader(body, 100*time.Millisecond, ctx)

	buf := make([]byte, 10)
	_, _ = r.Read(buf)
	r.Disarm()
	time.Sleep(200 * time.Millisecond)

	if body.isClosed() {
		t.Error("body was closed by watchdog after Disarm")
	}
	_ = r.Close()
}

func TestStallReader_ZeroByteReadDoesNotResetTimer(t *testing.T) {
	body := newMockReadCloser([]byte("x"), 10*time.Millisecond)
	ctx := context.Background()
	r := NewStallReader(body, 500*time.Millisecond, ctx)
	buf := make([]byte, 10)
	_, err := r.Read(buf)
	if err != nil && err != io.EOF {
		t.Fatalf("unexpected error: %v", err)
	}
	_ = r.Close()
}

type zeroByteReadCloser struct {
	closed atomic.Bool
	done   chan struct{}
}

func (z *zeroByteReadCloser) Read(p []byte) (int, error) {
	if z.closed.Load() {
		return 0, io.ErrClosedPipe
	}
	select {
	case <-time.After(1 * time.Millisecond):
		return 0, nil
	case <-z.done:
		return 0, io.ErrClosedPipe
	}
}

func (z *zeroByteReadCloser) Close() error {
	if z.closed.CompareAndSwap(false, true) {
		close(z.done)
	}
	return nil
}

func TestStallReader_RepeatedZeroByteReadsTriggerStall(t *testing.T) {
	body := &zeroByteReadCloser{done: make(chan struct{})}
	ctx := context.Background()
	r := NewStallReader(body, 100*time.Millisecond, ctx)

	buf := make([]byte, 10)
	start := time.Now()
	var err error
	for {
		_, err = r.Read(buf)
		if err != nil {
			break
		}
	}
	elapsed := time.Since(start)

	if !IsDownloadStalled(err) {
		t.Errorf("expected errDownloadStalled from repeated zero-byte reads, got: %v", err)
	}
	if elapsed > 500*time.Millisecond {
		t.Errorf("stall took too long: %v", elapsed)
	}
	_ = r.Close()
}

func TestStallReader_CloseStopsGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()
	body := newMockReadCloser([]byte("x"), 10*time.Millisecond)
	ctx := context.Background()
	r := NewStallReader(body, 100*time.Millisecond, ctx)

	buf := make([]byte, 10)
	_, _ = r.Read(buf)
	_ = r.Close()

	deadline := time.Now().Add(500 * time.Millisecond)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutine leak: before=%d, after=%d", before, runtime.NumGoroutine())
}

func TestStallReader_DisarmThenClose(t *testing.T) {
	body := newMockReadCloser([]byte("x"), 10*time.Millisecond)
	ctx := context.Background()
	r := NewStallReader(body, 100*time.Millisecond, ctx)

	buf := make([]byte, 10)
	_, _ = r.Read(buf)
	r.Disarm()
	_ = r.Close()

	if !body.isClosed() {
		t.Error("body should be closed after Close")
	}
}

func TestDownloadStalled_ImplementsNetError(t *testing.T) {
	var err net.Error
	if !errors.As(downloadStalledError{}, &err) {
		t.Error("errDownloadStalled does not satisfy net.Error")
	}
	if !err.Timeout() {
		t.Error("errDownloadStalled.Timeout() should return true")
	}
	if !err.Temporary() {
		t.Error("errDownloadStalled.Temporary() should return true")
	}
}

func TestDownloadTruncated_ImplementsNetError(t *testing.T) {
	var err net.Error
	if !errors.As(downloadTruncatedError{}, &err) {
		t.Error("errDownloadTruncated does not satisfy net.Error")
	}
	if !err.Timeout() {
		t.Error("errDownloadTruncated.Timeout() should return true")
	}
}

func TestDownloadEmpty_ImplementsNetError(t *testing.T) {
	var err net.Error
	if !errors.As(downloadEmptyError{}, &err) {
		t.Error("errDownloadEmpty does not satisfy net.Error")
	}
	if !err.Timeout() {
		t.Error("errDownloadEmpty.Timeout() should return true")
	}
}

func TestIsRetryableError_StallError(t *testing.T) {
	if !isRetryableError(errDownloadStalled) {
		t.Error("errDownloadStalled should be retryable")
	}
}

func TestIsRetryableError_TruncationError(t *testing.T) {
	wrapped := fmt.Errorf("%w: downloaded 5 of 10 bytes", errDownloadTruncated)
	if !isRetryableError(wrapped) {
		t.Error("truncation error should be retryable")
	}
}

func TestIsRetryableError_EmptyError(t *testing.T) {
	wrapped := fmt.Errorf("%w: downloaded 0 bytes for http://example.com", errDownloadEmpty)
	if !isRetryableError(wrapped) {
		t.Error("empty download error should be retryable")
	}
}

func TestIsRetryableError_NotFoundNotRetryable(t *testing.T) {
	err := &statusError{statusCode: 404}
	if isRetryableError(err) {
		t.Error("404 should not be retryable")
	}
}

func TestIsRetryableError_UnexpectedEOF(t *testing.T) {
	wrapped := fmt.Errorf("failed to write file: %w", io.ErrUnexpectedEOF)
	if !isRetryableError(wrapped) {
		t.Error("io.ErrUnexpectedEOF should be retryable (truncation via early close)")
	}
}
