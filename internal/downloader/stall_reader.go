package downloader

import (
	"context"
	"errors"
	"io"
	"net"
	neturl "net/url"
	"sync"
	"sync/atomic"
	"time"
)

type downloadStalledError struct{}

func (downloadStalledError) Error() string {
	return "download stalled: no bytes received within idle timeout"
}
func (downloadStalledError) Timeout() bool   { return true }
func (downloadStalledError) Temporary() bool { return true }

var errDownloadStalled = downloadStalledError{}
var _ net.Error = downloadStalledError{}

// StallReader wraps an io.ReadCloser with a stall-based idle watchdog.
type StallReader struct {
	body         io.ReadCloser
	idleTimeout  time.Duration
	ctx          context.Context
	stalled      atomic.Bool
	timer        *time.Timer
	watchdogOnce sync.Once
	stop         chan struct{}
	closeOnce    sync.Once
}

// NewStallReader wraps body with a stall watchdog. If idleTimeout <= 0, returns a no-op wrapper.
func NewStallReader(body io.ReadCloser, idleTimeout time.Duration, ctx context.Context) *StallReader {
	return &StallReader{
		body:        body,
		idleTimeout: idleTimeout,
		ctx:         ctx,
		stop:        make(chan struct{}),
	}
}

func (r *StallReader) start() {
	if r.idleTimeout <= 0 {
		return
	}
	r.timer = time.NewTimer(r.idleTimeout)
	go r.watchdog()
}

func (r *StallReader) watchdog() {
	select {
	case <-r.timer.C:
		r.stalled.Store(true)
		_ = r.body.Close()
	case <-r.stop:
		r.timer.Stop()
	}
}

func (r *StallReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}

	r.watchdogOnce.Do(r.start)

	if r.stalled.Load() {
		return 0, errDownloadStalled
	}

	n, err := r.body.Read(p)

	if n > 0 && r.timer != nil {
		r.timer.Reset(r.idleTimeout)
	}

	if err != nil && r.stalled.Load() {
		return n, errDownloadStalled
	}

	return n, err
}

// Disarm stops the stall watchdog without closing the underlying body.
func (r *StallReader) Disarm() {
	r.watchdogOnce.Do(func() {})
	select {
	case <-r.stop:
	default:
		close(r.stop)
	}
}

// Close stops the watchdog and closes the underlying body.
func (r *StallReader) Close() error {
	r.watchdogOnce.Do(func() {})

	var err error
	r.closeOnce.Do(func() {
		select {
		case <-r.stop:
		default:
			close(r.stop)
		}
		err = r.body.Close()
	})
	return err
}

var _ io.ReadCloser = (*StallReader)(nil)

// IsDownloadStalled returns true if the error is or wraps errDownloadStalled.
func IsDownloadStalled(err error) bool {
	return errors.Is(err, errDownloadStalled)
}

type downloadTruncatedError struct{}

func (downloadTruncatedError) Error() string   { return "download truncated" }
func (downloadTruncatedError) Timeout() bool   { return true }
func (downloadTruncatedError) Temporary() bool { return true }

var errDownloadTruncated = downloadTruncatedError{}
var _ net.Error = downloadTruncatedError{}

type downloadEmptyError struct{}

func (downloadEmptyError) Error() string   { return "downloaded 0 bytes" }
func (downloadEmptyError) Timeout() bool   { return true }
func (downloadEmptyError) Temporary() bool { return true }

var errDownloadEmpty = downloadEmptyError{}
var _ net.Error = downloadEmptyError{}

// IsDownloadTruncated returns true if the error is or wraps errDownloadTruncated.
func IsDownloadTruncated(err error) bool {
	return errors.Is(err, errDownloadTruncated)
}

// IsDownloadEmpty returns true if the error is or wraps errDownloadEmpty.
func IsDownloadEmpty(err error) bool {
	return errors.Is(err, errDownloadEmpty)
}

func redactURL(rawURL string) string {
	parsed, err := neturl.Parse(rawURL)
	if err != nil {
		return rawURL
	}
	parsed.User = nil
	parsed.RawQuery = ""
	parsed.Fragment = ""
	return parsed.String()
}
