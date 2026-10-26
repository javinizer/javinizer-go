package metadatafetch

import (
	"bufio"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"errors"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptrace"
	"strings"
	"sync"
	"testing"
	"time"
)

type closeObservedConn struct {
	net.Conn
	once   sync.Once
	closed chan struct{}
}

func (c *closeObservedConn) Close() error {
	var err error
	c.once.Do(func() { err = c.Conn.Close(); close(c.closed) })
	return err
}

func TestTLSClosesRawConnectionBeforeReleasingSlot(t *testing.T) {
	for _, status := range []string{"500 Error", "200 OK"} {
		t.Run(status, func(t *testing.T) {
			key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
			if err != nil {
				t.Fatal(err)
			}
			template := &x509.Certificate{SerialNumber: big.NewInt(1), DNSNames: []string{"localhost"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}}
			der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
			if err != nil {
				t.Fatal(err)
			}
			cert, err := x509.ParseCertificate(der)
			if err != nil {
				t.Fatal(err)
			}
			options := networkOptions()
			options.Limits.RequestTimeout = 100 * time.Millisecond
			options.Limits.MaxConcurrent = 1
			c := networkClient(t, options)
			c.rootCAs = x509.NewCertPool()
			c.rootCAs.AddCert(cert)
			closed, written := make(chan struct{}), make(chan struct{})
			c.dial = func(context.Context, string, string) (net.Conn, error) {
				raw := pipeResponse(t, func(raw net.Conn) {
					server := tls.Server(raw, &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12})
					if err := server.HandshakeContext(t.Context()); err != nil {
						t.Error(err)
						return
					}
					if _, err := http.ReadRequest(bufio.NewReader(server)); err != nil {
						t.Error(err)
						return
					}
					// Avoid relying on a race with an unfinished request writer, which can
					// cause TLS.Close to skip close_notify and hide the teardown problem.
					<-written
					if status == "200 OK" {
						io.WriteString(server, "HTTP/1.1 200 OK\r\nConnection: keep-alive\r\nContent-Length: 2\r\n\r\nok")
					} else {
						io.WriteString(server, "HTTP/1.1 500 Error\r\nConnection: keep-alive\r\nContent-Length: 200000\r\n\r\n")
					}
					// Block writing instead of reading the client's TLS close_notify.
					io.WriteString(server, strings.Repeat("x", 200000))
				})
				return &closeObservedConn{Conn: raw, closed: closed}, nil
			}
			ctx := httptrace.WithClientTrace(t.Context(), &httptrace.ClientTrace{WroteRequest: func(httptrace.WroteRequestInfo) { close(written) }})
			result, err := c.Fetch(ctx, "https://localhost/")
			if status == "200 OK" {
				if err != nil || string(result.Bytes) != "ok" {
					t.Fatalf("result=%+v err=%v", result, err)
				}
			} else if !errors.Is(err, ErrResponse) || result.Bytes != nil {
				t.Fatalf("result=%+v err=%v", result, err)
			}
			select {
			case <-closed:
			default:
				t.Fatal("raw TLS connection still open after fetch returned")
			}
			if len(c.slots) != 0 {
				t.Fatal("slot retained after raw socket cleanup")
			}
		})
	}
}
