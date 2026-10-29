package metadatafetch_test

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	"github.com/javinizer/javinizer-go/internal/metadatafetch"
)

// Exercise the public constructor's system resolver rather than the package's
// injected DNS seam. A hostname resolving to loopback must never reach HTTP.
func TestDefaultResolverRejectsLoopbackHostname(t *testing.T) {
	ctx, cancel := context.WithTimeout(t.Context(), 10*time.Second)
	defer cancel()
	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", "localhost")
	if err != nil || len(ips) == 0 {
		t.Fatalf("system localhost resolution: ips=%v err=%v", ips, err)
	}
	for _, ip := range ips {
		if !ip.IsLoopback() {
			t.Fatalf("localhost unexpectedly resolved outside loopback: %v", ips)
		}
	}

	var requests, connections atomic.Int32
	server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		requests.Add(1)
		_, _ = w.Write([]byte("must not be fetched"))
	}))
	server.Config.ConnState = func(_ net.Conn, state http.ConnState) {
		if state == http.StateNew {
			connections.Add(1)
		}
	}
	server.Start()
	defer server.Close()
	_, port, err := net.SplitHostPort(server.Listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	client, err := metadatafetch.New(metadatafetch.Options{
		Route: metadatafetch.Direct,
		Limits: metadatafetch.Limits{
			RequestTimeout: 5 * time.Second, ConnectTimeout: 2 * time.Second,
			HeaderTimeout: time.Second, TLSHandshakeTimeout: time.Second,
			MaxHeaderBytes: 4096, MaxBodyBytes: 1024, MaxConcurrent: 1,
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	result, err := client.Fetch(ctx, "http://"+net.JoinHostPort("localhost", port)+"/metadata?token=private")
	if !errors.Is(err, metadatafetch.ErrTarget) {
		t.Fatalf("Fetch error = %v, want ErrTarget", err)
	}
	if !reflect.DeepEqual(result, metadatafetch.Result{}) {
		t.Fatalf("rejected fetch returned data: %#v", result)
	}
	if requests.Load() != 0 || connections.Load() != 0 {
		t.Fatalf("rejected hostname reached owned server: requests=%d connections=%d", requests.Load(), connections.Load())
	}
}
