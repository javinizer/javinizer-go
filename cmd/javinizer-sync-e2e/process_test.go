package main

import (
	"bufio"
	"bytes"
	"fmt"
	"github.com/stretchr/testify/require"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

func TestFixtureChildProcess(t *testing.T) {
	if os.Getenv("JAVINIZER_SYNC_TEST_CHILD") != "1" {
		return
	}
	os.Args = []string{"fixture"}
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Exit(0)
}
func processPort(t *testing.T) string {
	t.Helper()
	lc := net.ListenConfig{}
	l, err := lc.Listen(t.Context(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	port := strconv.Itoa(l.Addr().(*net.TCPAddr).Port)
	require.NoError(t, l.Close())
	return port
}
func startFixtureProcess(t *testing.T, root, mode, port string) func() {
	t.Helper()
	cmd := exec.CommandContext(t.Context(), os.Args[0], "-test.run=^TestFixtureChildProcess$")
	cmd.Env = append(os.Environ(), "JAVINIZER_SYNC_TEST_CHILD=1", "JAVINIZER_SYNC_E2E_ROOT="+root, "JAVINIZER_SYNC_E2E_MODE="+mode, "JAVINIZER_SYNC_E2E_PORT="+port, "JAVINIZER_SYNC_E2E_PAUSE_AFTER=-1")
	stdout, err := cmd.StdoutPipe()
	require.NoError(t, err)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	require.NoError(t, cmd.Start())
	done := make(chan error, 1)
	ready := make(chan struct{})
	go func() {
		scanner := bufio.NewScanner(stdout)
		once := false
		for scanner.Scan() {
			if strings.Contains(scanner.Text(), "javinizer-sync-e2e: ready") && !once {
				close(ready)
				once = true
			}
		}
	}()
	go func() { done <- cmd.Wait() }()
	stopped := false
	stop := func() {
		if stopped {
			return
		}
		stopped = true
		_ = cmd.Process.Signal(syscall.SIGTERM)
		select {
		case err := <-done:
			require.NoError(t, err, stderr.String())
		case <-time.After(20 * time.Second):
			_ = cmd.Process.Kill()
			<-done
			t.Fatal("fixture failed graceful shutdown")
		}
	}
	t.Cleanup(stop)
	select {
	case <-ready:
	case err := <-done:
		stopped = true
		t.Fatalf("fixture exited: %v %s", err, stderr.String())
	case <-time.After(60 * time.Second):
		_ = cmd.Process.Kill()
		<-done
		stopped = true
		t.Fatal("fixture readiness timed out")
	}
	return stop
}
func fixtureHTTP(t *testing.T, client *http.Client, method, url, body string) []byte {
	t.Helper()
	req, err := http.NewRequestWithContext(t.Context(), method, url, strings.NewReader(body))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	resp, err := client.Do(req)
	require.NoError(t, err)
	defer func() { _ = resp.Body.Close() }()
	b, err := io.ReadAll(resp.Body)
	require.NoError(t, err)
	require.Equal(t, http.StatusOK, resp.StatusCode, string(b))
	return b
}
func TestRealServerReadinessAuthAndRestart(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "darwin" {
		t.Skip("native publication and POSIX signals required")
	}
	for _, mode := range []string{"ready", "disabled", "missing", "stale", "future"} {
		t.Run(mode, func(t *testing.T) {
			root := testRoot(t)
			port := processPort(t)
			stop := startFixtureProcess(t, root, mode, port)
			base := "http://127.0.0.1:" + port
			jar, err := cookiejar.New(nil)
			require.NoError(t, err)
			client := &http.Client{Jar: jar, Timeout: 10 * time.Second}
			fixtureHTTP(t, client, http.MethodPost, base+"/api/v1/auth/login", `{"username":"admin","password":"adminpassword123","remember_me":true}`)
			status := fixtureHTTP(t, client, http.MethodGet, base+"/api/v1/actress-name-sync/status", "")
			require.Contains(t, string(status), `"enabled":`)
			manifest, err := os.ReadFile(filepath.Join(root, "fixture.json"))
			require.NoError(t, err)
			stop()
			if mode == "ready" {
				stop2 := startFixtureProcess(t, root, mode, port)
				defer stop2()
				fixtureHTTP(t, client, http.MethodGet, base+"/api/v1/actress-name-sync/status", "")
				after, err := os.ReadFile(filepath.Join(root, "fixture.json"))
				require.NoError(t, err)
				require.Equal(t, manifest, after)
			}
		})
	}
}
