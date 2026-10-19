package r18devdump

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUploadMultipartSpoolCreateFailure(t *testing.T) {
	h, dumpPath := newTestHandler(t)
	t.Cleanup(h.rt.Shutdown)
	buildTestDump(t, dumpPath, "118ipx00535", "IPX-535")
	original, err := os.ReadFile(dumpPath)
	require.NoError(t, err)
	seedStatusError(h, kindImport)
	h.uploadMaxMemory = 1
	srv := newUploadTestServer(t, h)

	spoolRoot := t.TempDir()
	blockedDir := filepath.Join(spoolRoot, "not-a-directory")
	require.NoError(t, os.WriteFile(blockedDir, []byte("keep"), 0o600))
	// Each OS uses one of these environment variables for os.TempDir.
	for _, key := range []string{"TMPDIR", "TMP", "TEMP"} {
		t.Setenv(key, blockedDir)
	}
	status, body, _ := srv.doUpload(t, buildUploadBody(t, bytes.Repeat([]byte("x"), 4096), "synthetic.db"))
	t.Logf("multipart create failure: HTTP %d %s", status, body)
	require.Equal(t, http.StatusInternalServerError, status)
	require.Contains(t, body, "stage upload")
	assertMultipartReceiveFailureClean(t, h, dumpPath, original)
	entries, err := os.ReadDir(spoolRoot)
	require.NoError(t, err)
	require.Len(t, entries, 1, "failed multipart create must not leave temporary files")
	require.Equal(t, "not-a-directory", entries[0].Name())
}

func assertMultipartReceiveFailureClean(t *testing.T, h *dumpHandler, dumpPath string, original []byte) {
	t.Helper()
	lastErr, kind, running := handlerState(h)
	require.Equal(t, "previous failure", lastErr, "synchronous failure must preserve status")
	require.Equal(t, kindImport, kind)
	require.False(t, running)
	require.Nil(t, h.done, "synchronous failure must not start an upload job")
	require.True(t, h.rt.WaitBackgroundTasks(time.Second))
	after, err := os.ReadFile(dumpPath)
	require.NoError(t, err)
	require.Equal(t, original, after, "working dump must remain byte-for-byte unchanged")
	assertNoUploadStages(t, dumpPath)
	require.True(t, h.tryAcquireDumpOp(), "receive failure must release the operation guard")
	h.releaseDumpOp()
}

func TestReceiveWrapperMultipartSpoolClassification(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
		want receiveDisposition
	}{
		{"create", &os.PathError{Op: "open", Path: "multipart-test", Err: os.ErrPermission}, dispSpoolFailure},
		{"write", &os.PathError{Op: "write", Path: "multipart-test", Err: errors.New("synthetic disk failure")}, dispSpoolFailure},
		{"close", &os.PathError{Op: "close", Path: "multipart-test", Err: os.ErrClosed}, dispSpoolFailure},
		{"short write", io.ErrShortWrite, dispSpoolFailure},
		{"truncated envelope", io.ErrUnexpectedEOF, dispEnvelope},
		{"multipart metadata limit", multipart.ErrMessageTooLarge, dispEnvelope},
		{"filesystem-looking text", errors.New("write multipart-test: no space left on device"), dispEnvelope},
		{"short-write-looking text", errors.New("short write"), dispEnvelope},
	} {
		t.Run(tc.name, func(t *testing.T) {
			for _, wrapped := range []bool{false, true} {
				t.Run(fmt.Sprintf("wrapped=%t", wrapped), func(t *testing.T) {
					err := tc.err
					if wrapped {
						err = fmt.Errorf("multipart: NextPart: %w", err)
					}
					w := newReceiveWrapper(io.NopCloser(bytes.NewReader(nil)), nil, nil)
					require.Equal(t, tc.want, w.classify(err))
					require.Equal(t, recvCompleted, w.terminalState())
				})
			}
		})
	}
}

func TestReceiveWrapperMultipartSpoolErrorPrecedence(t *testing.T) {
	for _, spoolErr := range []error{
		&os.PathError{Op: "write", Path: "multipart-test", Err: os.ErrPermission},
		io.ErrShortWrite,
	} {
		for _, tc := range []struct {
			name     string
			state    receiveState
			oversize bool
			want     receiveDisposition
		}{
			{"latched oversize", recvActive, true, dispOversize},
			{"watchdog", recvWatchdogAborted, false, dispStalled},
			{"server timeout", recvServerTimedOut, false, dispServerTimeout},
			{"oversize before watchdog", recvWatchdogAborted, true, dispOversize},
			{"oversize before server timeout", recvServerTimedOut, true, dispOversize},
		} {
			t.Run(fmt.Sprintf("%T/%s", spoolErr, tc.name), func(t *testing.T) {
				w := newReceiveWrapper(io.NopCloser(bytes.NewReader(nil)), nil, nil)
				w.state.Store(int32(tc.state))
				w.oversize.Store(tc.oversize)
				require.Equal(t, tc.want, w.classify(fmt.Errorf("multipart: %w", spoolErr)))
			})
		}
		t.Run(fmt.Sprintf("%T/oversize in error chain", spoolErr), func(t *testing.T) {
			w := newReceiveWrapper(io.NopCloser(bytes.NewReader(nil)), nil, nil)
			require.Equal(t, dispOversize, w.classify(errors.Join(spoolErr, &http.MaxBytesError{Limit: 1})))
		})
	}
}
