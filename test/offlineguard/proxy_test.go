package guardcheck

import (
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

// This proxy is a local recorder only: it never forwards a request anywhere.
// Thus the negative control is safe even if no verification overlay is loaded.
func TestHTTPExternalTargetBlockedBeforeProxy(t *testing.T) {
	var hits, selectors atomic.Int32
	proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits.Add(1); w.WriteHeader(http.StatusBadGateway) }))
	defer proxy.Close()
	u, err := url.Parse(proxy.URL)
	if err != nil {
		t.Fatal(err)
	}
	tr := &http.Transport{Proxy: func(r *http.Request) (*url.URL, error) { selectors.Add(1); return u, nil }}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: time.Second}
	for _, target := range []string{"http://synthetic.invalid/fixture", "https://synthetic.invalid/fixture", "http://192.0.2.1/fixture"} {
		resp, err := c.Get(target)
		if resp != nil {
			resp.Body.Close()
		}
		blocked(t, err)
	}
	if hits.Load() != 0 || selectors.Load() != 0 {
		t.Fatalf("proxy calls: hits=%d selectors=%d", hits.Load(), selectors.Load())
	}
}
func TestHTTPExternalTargetBlockedBeforeSOCKS(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var hits atomic.Int32
	done := make(chan struct{})
	go func() {
		defer close(done)
		for {
			c, e := l.Accept()
			if e != nil {
				return
			}
			hits.Add(1)
			c.Close()
		}
	}()
	u := &url.URL{Scheme: "socks5", Host: l.Addr().String()}
	tr := &http.Transport{Proxy: http.ProxyURL(u)}
	defer tr.CloseIdleConnections()
	c := &http.Client{Transport: tr, Timeout: time.Second}
	resp, err := c.Get("https://synthetic.invalid/fixture")
	if resp != nil {
		resp.Body.Close()
	}
	blocked(t, err)
	l.Close()
	<-done
	if hits.Load() != 0 {
		t.Fatalf("SOCKS connection attempts: %d", hits.Load())
	}
}
func TestInfrastructureProxyEndpointBlocked(t *testing.T) {
	l, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	t.Setenv("OFFLINE279_BLOCKED_PROXY_URLS", "http://"+l.Addr().String())
	c, err := net.DialTimeout("tcp", l.Addr().String(), time.Second)
	if c != nil {
		c.Close()
	}
	blocked(t, err)
	addr := l.Addr().(*net.TCPAddr)
	c2, err := net.DialTCP("tcp", nil, addr)
	if c2 != nil {
		c2.Close()
	}
	blocked(t, err)
}
func TestHTTPLoopbackAllowed(t *testing.T) {
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { io.WriteString(w, "local fixture") }))
	defer s.Close()
	resp, err := s.Client().Get(s.URL)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, err := io.ReadAll(resp.Body)
	if err != nil || string(b) != "local fixture" {
		t.Fatalf("body=%q err=%v", b, err)
	}
}
