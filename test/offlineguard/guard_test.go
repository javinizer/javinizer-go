package guardcheck

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/netip"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func blocked(t *testing.T, e error) {
	t.Helper()
	if e == nil || !strings.Contains(e.Error(), "offline279:") {
		t.Fatalf("not guard-blocked: %v", e)
	}
}
func TestExternalBeforeDNS(t *testing.T) {
	var calls atomic.Int32
	r := &net.Resolver{PreferGo: true, Dial: func(context.Context, string, string) (net.Conn, error) {
		calls.Add(1)
		return nil, errors.New("unexpected resolver call")
	}}
	// Cancellation plus the non-forwarding resolver keeps this canary safe
	// even when the overlay is absent or an insertion stops working.
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := r.LookupHost(ctx, "synthetic.invalid")
	blocked(t, err)
	_, err = r.LookupIPAddr(ctx, "synthetic.invalid")
	blocked(t, err)
	_, err = r.LookupIP(ctx, "ip", "synthetic.invalid")
	blocked(t, err)
	_, err = r.LookupNetIP(ctx, "ip", "synthetic.invalid")
	blocked(t, err)
	_, err = r.LookupCNAME(ctx, "synthetic.invalid")
	blocked(t, err)
	_, _, err = r.LookupSRV(ctx, "x", "tcp", "synthetic.invalid")
	blocked(t, err)
	_, err = r.LookupMX(ctx, "synthetic.invalid")
	blocked(t, err)
	_, err = r.LookupNS(ctx, "synthetic.invalid")
	blocked(t, err)
	_, err = r.LookupTXT(ctx, "synthetic.invalid")
	blocked(t, err)
	_, err = r.LookupAddr(ctx, "192.0.2.1")
	blocked(t, err)
	d := net.Dialer{Resolver: r, Timeout: time.Second}
	for _, n := range []string{"tcp", "tcp4", "udp", "udp4"} {
		for _, a := range []string{"synthetic.invalid:80", "192.0.2.1:80"} {
			c, e := d.DialContext(ctx, n, a)
			if c != nil {
				c.Close()
			}
			blocked(t, e)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("DNS resolver called %d times", calls.Load())
	}
}
func TestDirectDialAndUDPWrites(t *testing.T) {
	// Invalid ports fail sockaddr validation before connecting without the guard.
	// The instrumented entry points must still return their own guard errors.
	_, e := net.DialTCP("tcp", nil, &net.TCPAddr{IP: net.ParseIP("192.0.2.1"), Port: -1})
	blocked(t, e)
	_, e = net.DialUDP("udp", nil, &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: -1})
	blocked(t, e)
	c, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		t.Fatal(e)
	}
	// A closed owned socket cannot transmit even if a write hook is missing.
	if err := c.Close(); err != nil {
		t.Fatal(err)
	}
	a := &net.UDPAddr{IP: net.ParseIP("192.0.2.1"), Port: 80}
	ap := netip.MustParseAddrPort("192.0.2.1:80")
	_, e = c.WriteToUDP([]byte("x"), a)
	blocked(t, e)
	_, e = c.WriteToUDPAddrPort([]byte("x"), ap)
	blocked(t, e)
	_, _, e = c.WriteMsgUDP([]byte("x"), nil, a)
	blocked(t, e)
	_, _, e = c.WriteMsgUDPAddrPort([]byte("x"), nil, ap)
	blocked(t, e)
}
func TestLoopbackAllowed(t *testing.T) {
	l, e := net.Listen("tcp4", "127.0.0.1:0")
	if e != nil {
		t.Fatal(e)
	}
	defer l.Close()
	go func() {
		c, e := l.Accept()
		if e == nil {
			c.Close()
		}
	}()
	c, e := net.DialTimeout("tcp", l.Addr().String(), time.Second)
	if e != nil {
		t.Fatal(e)
	}
	c.Close()
	u, e := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if e != nil {
		t.Fatal(e)
	}
	defer u.Close()
	d, e := net.DialUDP("udp4", nil, u.LocalAddr().(*net.UDPAddr))
	if e != nil {
		t.Fatal(e)
	}
	defer d.Close()
	if _, e = d.Write([]byte("ok")); e != nil {
		t.Fatal(e)
	}
	u.SetReadDeadline(time.Now().Add(time.Second))
	b := make([]byte, 8)
	n, _, e := u.ReadFromUDP(b)
	if e != nil || string(b[:n]) != "ok" {
		t.Fatal(fmt.Sprint(n, e))
	}
	a, e := net.LookupHost("localhost")
	if e != nil || len(a) == 0 {
		t.Fatal(a, e)
	}
}
