package r18devdump

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestUploadShutdownRejectsAdmission(t *testing.T) {
	h, path, srv := newUploadHandler(t)
	h.rt.Shutdown()
	status, body, _ := srv.doUpload(t, buildUploadBody(t, []byte("synthetic"), "dump.db"))
	require.Equal(t, http.StatusServiceUnavailable, status)
	require.Contains(t, body, "server is shutting down")
	require.Nil(t, h.done)
	require.False(t, h.running)
	require.NoFileExists(t, path+".upload")
}

func TestUploadShutdownInterruptsReceive(t *testing.T) {
	h, path, srv := newUploadHandler(t)
	h.stallTimeout = time.Hour
	reading := make(chan struct{})
	var readOnce sync.Once
	h.broadcastProgressFn = func(phase string, _, _ int64, _ string) {
		if phase == "downloading" {
			readOnce.Do(func() { close(reading) })
		}
	}
	conn, err := net.Dial("tcp", srv.srv.Listener.Addr().String())
	require.NoError(t, err)
	defer conn.Close()
	_, err = fmt.Fprintf(conn, "POST /api/v1/r18dev/dump/upload HTTP/1.1\r\nHost: localhost\r\nContent-Type: multipart/form-data; boundary=synthetic\r\nContent-Length: 10000\r\n\r\n--synthetic\r\n")
	require.NoError(t, err)
	select {
	case <-reading:
	case <-time.After(5 * time.Second):
		t.Fatal("receive did not start")
	}
	stopped := make(chan struct{})
	go func() { h.rt.Shutdown(); close(stopped) }()
	select {
	case <-stopped:
	case <-time.After(5 * time.Second):
		t.Fatal("shutdown did not join receive")
	}
	_, _, running := handlerState(h)
	require.False(t, running)
	require.NoFileExists(t, path+".upload")
	require.True(t, h.rt.WaitBackgroundTasks(time.Second))
}

func TestUploadShutdownJoinsJobAndRequestIsDetached(t *testing.T) {
	for _, shutdown := range []bool{false, true} {
		t.Run(fmt.Sprint(shutdown), func(t *testing.T) {
			h, path, srv := newUploadHandler(t)
			started, release := make(chan struct{}), make(chan struct{})
			var once sync.Once
			unblock := func() { once.Do(func() { close(release) }) }
			defer func() { unblock(); h.rt.Shutdown() }()
			h.broadcastProgressFn = func(phase string, _, _ int64, _ string) {
				if phase == "importing" {
					close(started)
					<-release
				}
			}
			payload := buildUploadBody(t, gzBytes(t, uploadOneRowDump), "synthetic.sql.gz")
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			req, err := http.NewRequestWithContext(ctx, http.MethodPost, srv.srv.URL+"/api/v1/r18dev/dump/upload", bytes.NewReader(payload.body))
			require.NoError(t, err)
			req.Header.Set("Content-Type", payload.contentType)
			resp, err := srv.client.Do(req)
			require.NoError(t, err)
			require.Equal(t, http.StatusAccepted, resp.StatusCode)
			resp.Body.Close()
			<-started
			cancel()
			require.False(t, h.rt.WaitBackgroundTasks(time.Millisecond))
			stopped := make(chan struct{})
			if shutdown {
				go func() { h.rt.Shutdown(); close(stopped) }()
				require.Eventually(t, func() bool { return h.rt.ServerCtx().Err() != nil }, time.Second, time.Millisecond)
				select {
				case <-stopped:
					t.Fatal("shutdown returned before worker cleanup")
				default:
				}
			}
			unblock()
			awaitDone(t, h, 5*time.Second)
			if shutdown {
				select {
				case <-stopped:
				case <-time.After(5 * time.Second):
					t.Fatal("shutdown failed to join upload")
				}
				require.Contains(t, h.lastError, "context canceled")
				require.NoFileExists(t, path)
			} else {
				require.Empty(t, h.lastError)
				require.FileExists(t, path)
			}
			require.NoFileExists(t, path+".upload")
			for _, suffix := range []string{".tmp", ".tmp-wal", ".tmp-shm"} {
				_, err := os.Stat(path + suffix)
				require.True(t, os.IsNotExist(err))
			}
			require.True(t, h.rt.WaitBackgroundTasks(time.Second))
		})
	}
}
