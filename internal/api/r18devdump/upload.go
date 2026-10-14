package r18devdump

import (
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path"
	"regexp"
	"strings"
	"sync/atomic"
)

// Receive-phase classification sentinels. They are exported typed errors so
// they survive the multipart parser's %w wrapping; the handler matches them
// with errors.Is and never samples wrapper state piecemeal.
var (
	ErrReceiveStalled = errors.New("receive stalled: no data arrived within the stall timeout")
	ErrReceiveTimeout = errors.New("server receive timeout before the request deadline was lifted")
)

type receiveState int32

const (
	recvActive receiveState = iota
	recvCompleted
	recvWatchdogAborted
	recvServerTimedOut
)

// receiveDisposition is the terminal classification of a receive attempt.
type receiveDisposition int

const (
	dispStaging       receiveDisposition = iota // parse succeeded; proceed to staging
	dispEnvelope                                // parse failed for non-timeout envelope reasons → 400
	dispOversize                                // 413
	dispStalled                                 // 408 watchdog stall
	dispServerTimeout                           // 408 server read-timer (pre-lift window)
)

// receiveWrapper instruments the request body: counts bytes (for progress),
// pings the stall watchdog, latches oversize when MaxBytesReader trips, and
// linearizes watchdog-vs-server-timer-vs-completion races through an atomic
// state machine. All classification decisions are owned here; the handler
// calls classify() exactly once per ParseMultipartForm return.
type receiveWrapper struct {
	inner    io.ReadCloser // MaxBytesReader
	watchdog *stallWatchdog
	onBytes  func(n int64) // progress hook (request bytes only)
	state    atomic.Int32
	oversize atomic.Bool
}

func newReceiveWrapper(inner io.ReadCloser, watchdog *stallWatchdog, onBytes func(n int64)) *receiveWrapper {
	w := &receiveWrapper{inner: inner, watchdog: watchdog, onBytes: onBytes}
	w.state.Store(int32(recvActive))
	return w
}

func (w *receiveWrapper) Read(p []byte) (int, error) {
	n, err := w.inner.Read(p)
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		w.oversize.Store(true)
		return n, err
	}
	if errors.Is(err, os.ErrDeadlineExceeded) {
		// Linearize inside Read: whoever CASes first owns the classification.
		if w.state.CompareAndSwap(int32(recvActive), int32(recvServerTimedOut)) {
			return n, fmt.Errorf("%w: %v", ErrReceiveTimeout, err)
		}
		if receiveState(w.state.Load()) == recvWatchdogAborted {
			return n, fmt.Errorf("%w: %v", ErrReceiveStalled, err)
		}
		return n, err
	}
	if n > 0 {
		if w.watchdog != nil {
			w.watchdog.Ping()
		}
		if w.onBytes != nil {
			w.onBytes(int64(n))
		}
	}
	return n, err
}

func (w *receiveWrapper) Close() error { return w.inner.Close() }

// onStall is the watchdog fire callback: claim watchdogAborted, and only when
// the claim wins, interrupt blocked reads via an already-expired deadline.
// A lost claim (completed or server-timed-out already decided) is inert.
func (w *receiveWrapper) onStall(setDeadline func()) {
	if w.state.CompareAndSwap(int32(recvActive), int32(recvWatchdogAborted)) {
		setDeadline()
	}
}

// watchdogAbortedNow reports whether the watchdog claim won (used by the
// handler to keep the expired deadline in place for the 408 teardown).
func (w *receiveWrapper) terminalState() receiveState {
	return receiveState(w.state.Load())
}

// classify is the single completion/classification entry point; it runs for
// every ParseMultipartForm return, success or error, in exact precedence:
// oversize (latched or in the error chain) > completed-CAS > terminal state.
func (w *receiveWrapper) classify(parseErr error) receiveDisposition {
	var mbe *http.MaxBytesError
	if w.oversize.Load() || errors.As(parseErr, &mbe) {
		return dispOversize
	}
	if w.state.CompareAndSwap(int32(recvActive), int32(recvCompleted)) {
		if parseErr == nil {
			return dispStaging
		}
		return dispEnvelope
	}
	switch w.terminalState() {
	case recvWatchdogAborted:
		return dispStalled
	case recvServerTimedOut:
		return dispServerTimeout
	}
	// Unreachable: state is only ever written to a terminal value once.
	return dispServerTimeout
}

var controlChars = regexp.MustCompile(`[\x00-\x1f\x7f]`)

const maxProvenanceLen = 255

type provenanceCapture struct {
	raw     string
	tooLong bool
}

// captureProvenance stores the raw multipart filename parameter verbatim
// (bounded; never interpreted). Receive-time handling does not sanitize or
// reject: the payload kind is unknown until the job sniffs content.
func captureProvenance(filenameParam string) provenanceCapture {
	if controlChars.MatchString(filenameParam) {
		filenameParam = controlChars.ReplaceAllString(filenameParam, ".")
	}
	if len(filenameParam) > maxProvenanceLen {
		return provenanceCapture{tooLong: true}
	}
	return provenanceCapture{raw: filenameParam}
}

// errProvenance is returned for unusable raw-form provenance tokens.
var errProvenance = errors.New("upload provenance rejection")

// token computes a safe provenance display token from the raw captured
// filename for gzip payloads. URL-shaped values are rejected before basename;
// degenerate or over-long values are rejected outright.
func (c provenanceCapture) token() (string, error) {
	if c.tooLong {
		return "", fmt.Errorf("%w: filename exceeds %d bytes", errProvenance, maxProvenanceLen)
	}
	s := c.raw
	if s == "" {
		return "", fmt.Errorf("%w: empty filename", errProvenance)
	}
	if strings.Contains(s, "://") {
		return "", fmt.Errorf("%w: filename must not be a URL", errProvenance)
	}
	s = strings.ReplaceAll(s, "\x5c", "/")
	base := path.Base(s)
	switch base {
	case "", ".", "/", "..", "\\":
		return "", fmt.Errorf("%w: degenerate filename %q", errProvenance, s)
	}
	return base, nil
}
