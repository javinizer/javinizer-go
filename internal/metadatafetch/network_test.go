package metadatafetch

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func networkOptions() Options {
	return Options{Route: Direct, Limits: Limits{
		RequestTimeout: 2 * time.Second, ConnectTimeout: time.Second,
		HeaderTimeout: time.Second, TLSHandshakeTimeout: time.Second,
		MaxHeaderBytes: 1024, MaxBodyBytes: 128, MaxConcurrent: 2,
	}, Redirects: RedirectPolicy{MaxHops: 3}}
}

func networkClient(t *testing.T, options Options) *Client {
	t.Helper()
	c, err := New(options)
	if err != nil {
		t.Fatal(err)
	}
	c.resolve = func(_ context.Context, host string) ([]net.IP, error) {
		if host != "localhost" && host != "proxy.test" {
			t.Errorf("unexpected DNS lookup: %q", host)
			return nil, errors.New("unregistered lookup")
		}
		return []net.IP{net.ParseIP("203.0.113.17")}, nil
	}
	c.dial = func(_ context.Context, _, addr string) (net.Conn, error) {
		t.Errorf("unexpected dial: %q", addr)
		return nil, errors.New("unregistered dial")
	}
	return c
}

func pipeResponse(t *testing.T, serve func(net.Conn)) net.Conn {
	t.Helper()
	client, server := net.Pipe()
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer server.Close()
		serve(server)
	}()
	t.Cleanup(func() { client.Close(); server.Close(); <-done })
	return client
}

func TestDirectPinningHostAndEnvironmentIsolation(t *testing.T) {
	t.Setenv("HTTP_PROXY", "http://secret:password@localhost:3128")
	t.Setenv("HTTPS_PROXY", "http://secret:password@localhost:3128")
	t.Setenv("ALL_PROXY", "socks5://secret:password@localhost:1080")
	c := networkClient(t, networkOptions())
	lookups, dials := 0, 0
	c.resolve = func(_ context.Context, host string) ([]net.IP, error) {
		lookups++
		if lookups != 1 || host != "localhost" {
			return []net.IP{net.ParseIP("127.0.0.1")}, nil
		}
		return []net.IP{net.ParseIP("203.0.113.17")}, nil
	}
	c.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
		dials++
		if address != "203.0.113.17:80" || network != "tcp" {
			t.Errorf("unpinned target: %s %s", network, address)
		}
		if _, ok := ctx.Deadline(); !ok {
			t.Error("missing connection deadline")
		}
		return pipeResponse(t, func(conn net.Conn) {
			req, err := http.ReadRequest(bufio.NewReader(conn))
			if err != nil {
				t.Error(err)
				return
			}
			if req.Host != "localhost" || req.URL.String() != "/metadata?token=secret" || req.Header.Get("Accept-Encoding") != "identity" || req.Header.Get("Authorization") != "" || req.Header.Get("Proxy-Authorization") != "" {
				t.Errorf("incorrect request: %s %s %v", req.Host, req.URL, req.Header)
			}
			io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 2\r\n\r\nok")
		}), nil
	}
	result, err := c.Fetch(t.Context(), "http://localhost/metadata?token=secret")
	if err != nil || string(result.Bytes) != "ok" || lookups != 1 || dials != 1 {
		t.Fatalf("result=%+v err=%v lookups=%d dials=%d", result, err, lookups, dials)
	}
}

func TestTLSVerifiesOriginalHostnameAndSNI(t *testing.T) {
	for _, mode := range []string{"trusted", "wrong-host", "untrusted"} {
		t.Run(mode, func(t *testing.T) {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			name := "localhost"
			if mode == "wrong-host" {
				name = "different.test"
			}
			cert := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{name}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			der, err := x509.CreateCertificate(rand.Reader, cert, cert, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			var requests atomic.Int32
			server := httptest.NewUnstartedServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				requests.Add(1)
				if r.Host != "localhost" || r.TLS.ServerName != "localhost" {
					t.Errorf("host=%q SNI=%q", r.Host, r.TLS.ServerName)
				}
				io.WriteString(w, "tls")
			}))
			server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
			server.StartTLS()
			defer server.Close()
			c := networkClient(t, networkOptions())
			if mode != "untrusted" {
				c.rootCAs = x509.NewCertPool()
				c.rootCAs.AddCert(server.Certificate())
			}
			c.dial = func(ctx context.Context, network, address string) (net.Conn, error) {
				if address != "203.0.113.17:443" {
					t.Errorf("unvalidated dial: %q", address)
				}
				return (&net.Dialer{}).DialContext(ctx, network, server.Listener.Addr().String())
			}
			result, err := c.Fetch(t.Context(), "https://localhost/")
			if mode == "trusted" {
				if err != nil || string(result.Bytes) != "tls" || requests.Load() != 1 {
					t.Fatalf("result=%+v err=%v requests=%d", result, err, requests.Load())
				}
			} else if err == nil || result.Bytes != nil || requests.Load() != 0 {
				t.Fatalf("accepted invalid certificate: %+v %v requests=%d", result, err, requests.Load())
			}
		})
	}
}

func TestSOCKSOnlySendsPinnedLiteralAndKeepsCredentialsAtProxy(t *testing.T) {
	for _, ip := range []string{"203.0.113.17", "2001:db8::17"} {
		t.Run(ip, func(t *testing.T) {
			options := networkOptions()
			options.Route = SOCKS5
			options.ProxyURL = "socks5://user:password@proxy.test:1080"
			c := networkClient(t, options)
			var mu sync.Mutex
			var lookups []string
			c.resolve = func(_ context.Context, host string) ([]net.IP, error) {
				mu.Lock()
				lookups = append(lookups, host)
				mu.Unlock()
				if host == "localhost" {
					return []net.IP{net.ParseIP(ip)}, nil
				}
				if host == "proxy.test" {
					return []net.IP{net.ParseIP("203.0.113.99")}, nil
				}
				return nil, errors.New("unregistered lookup")
			}
			c.dial = func(_ context.Context, network, address string) (net.Conn, error) {
				if network != "tcp" || address != "203.0.113.99:1080" {
					t.Errorf("direct fallback or unsafe proxy: %s %s", network, address)
				}
				return pipeResponse(t, func(conn net.Conn) {
					var greeting [2]byte
					if _, err := io.ReadFull(conn, greeting[:]); err != nil {
						t.Error(err)
						return
					}
					methods := make([]byte, int(greeting[1]))
					io.ReadFull(conn, methods)
					if greeting[0] != 5 {
						t.Error("not SOCKS5")
					}
					conn.Write([]byte{5, 2})
					var auth [2]byte
					io.ReadFull(conn, auth[:])
					username := make([]byte, int(auth[1]))
					io.ReadFull(conn, username)
					var length [1]byte
					io.ReadFull(conn, length[:])
					password := make([]byte, int(length[0]))
					io.ReadFull(conn, password)
					if auth[0] != 1 || string(username) != "user" || string(password) != "password" {
						t.Errorf("auth not preserved")
					}
					conn.Write([]byte{1, 0})
					var header [4]byte
					io.ReadFull(conn, header[:])
					lengthIP := 4
					if net.ParseIP(ip).To4() == nil {
						lengthIP = 16
					}
					wantType := byte(1)
					if lengthIP == 16 {
						wantType = 4
					}
					if header != [4]byte{5, 1, 0, wantType} {
						t.Errorf("SOCKS unpinned target: %v", header)
						return
					}
					target := make([]byte, lengthIP+2)
					io.ReadFull(conn, target)
					if !net.IP(target[:lengthIP]).Equal(net.ParseIP(ip)) || binary.BigEndian.Uint16(target[lengthIP:]) != 80 {
						t.Errorf("wrong SOCKS target: %v", target)
					}
					conn.Write([]byte{5, 0, 0, 1, 0, 0, 0, 0, 0, 0})
					req, err := http.ReadRequest(bufio.NewReader(conn))
					if err != nil {
						t.Error(err)
						return
					}
					if req.Host != "localhost" || req.Header.Get("Proxy-Authorization") != "" || req.Header.Get("Authorization") != "" {
						t.Errorf("credential or host leak: %v", req)
					}
					io.WriteString(conn, "HTTP/1.1 200 OK\r\nContent-Length: 5\r\n\r\nsocks")
				}), nil
			}
			result, err := c.Fetch(t.Context(), "http://localhost/")
			mu.Lock()
			gotLookups := append([]string(nil), lookups...)
			mu.Unlock()
			if err != nil || string(result.Bytes) != "socks" || !reflect.DeepEqual(gotLookups, []string{"localhost", "proxy.test"}) {
				t.Fatalf("result=%+v err=%v DNS=%v", result, err, gotLookups)
			}
		})
	}
}

func TestSOCKSFailureNeverFallsBackAndRedacts(t *testing.T) {
	options := networkOptions()
	options.Route = SOCKS5
	options.ProxyURL = "socks5://user:secretpass@proxy.test:1080"
	c := networkClient(t, options)
	dials := 0
	c.dial = func(_ context.Context, _, address string) (net.Conn, error) {
		dials++
		if address != "203.0.113.17:1080" {
			t.Errorf("direct fallback: %q", address)
		}
		return nil, errors.New("secretpass token=private full sensitive upstream error")
	}
	result, err := c.Fetch(t.Context(), "http://localhost/?token=private")
	if !errors.Is(err, ErrConnect) || result.Bytes != nil || dials != 1 {
		t.Fatalf("result=%+v err=%v dials=%d", result, err, dials)
	}
	for _, secret := range []string{"secretpass", "private", "user", "localhost", "proxy.test"} {
		if strings.Contains(fmt.Sprint(err), secret) {
			t.Errorf("error exposes %q: %v", secret, err)
		}
	}
}
