package actresscache

import (
	"context"
	"errors"
	"io"
	"net"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

type spyDialer struct {
	calls   []string
	failFor string
}

func (s *spyDialer) dial(_ context.Context, _ string, addr string) (net.Conn, error) {
	s.calls = append(s.calls, addr)
	if s.failFor == addr {
		return nil, io.EOF
	}
	return &net.TCPConn{}, nil // not a real conn — callers must not read
}

func TestHostIPLiteralZoneStripping(t *testing.T) {
	assert.NotNil(t, hostIPLiteral("fe80::250:56ff:fec0:dead%eth0"))
	assert.NotNil(t, hostIPLiteral("fe80::1%eno1"))   // zone stripped
	assert.NotNil(t, hostIPLiteral("[fe80::1%eno1]")) // brackets + zone stripped
	assert.Nil(t, hostIPLiteral("example.com"))       // hostname
	assert.NotNil(t, hostIPLiteral("127.0.0.1"))
}

func TestIsBlockedIPBranches(t *testing.T) {
	cases := map[string]bool{
		"127.0.0.1":            true,
		"10.0.0.1":             true,
		"169.254.169.254":      true,
		"100.100.100.200":      true,
		"8.8.8.8":              false,
		"2606:4700:4700::1111": false,
		"2002:0a00:0001::1":    true,  // 6to4 embedding 10.0.0.1
		"2001:4860:4860::8888": false, // Google DNS v6
		"2001:db8::1":          true,  // documentation space
	}
	for addr, want := range cases {
		assert.Equalf(t, want, isBlockedIP(net.ParseIP(addr)), "addr %s", addr)
	}
}

func TestCheckFetchTargetVariants(t *testing.T) {
	f := mustFetcher(NewFetcher(nil, 0, "test"))
	assert.Error(t, f.checkFetchTarget(context.Background(), "https", "127.0.0.1", ""))
	assert.Error(t, f.checkFetchTarget(context.Background(), "", "", ""), "empty host is blocked lexically")
}

func TestGuardedDialContextBranches(t *testing.T) {
	spy := &spyDialer{}
	fallback := spy.dial
	prev := lookupIP
	defer func() { lookupIP = prev }()

	_, err := guardedDialContext(context.Background(), "tcp", "no-port", fallback)
	require.Error(t, err)

	_, err = guardedDialContext(context.Background(), "tcp", "127.0.0.1:443", fallback)
	require.Error(t, err)

	lookupIP = func(context.Context, string, string) ([]net.IP, error) { return nil, errors.New("nxdomain") }
	_, err = guardedDialContext(context.Background(), "tcp", "nope.invalid:443", fallback)
	require.ErrorContains(t, err, "nxdomain")

	lookupIP = func(context.Context, string, string) ([]net.IP, error) { return nil, nil }
	_, err = guardedDialContext(context.Background(), "tcp", "empty.invalid:443", fallback)
	require.ErrorContains(t, err, "no addresses")

	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("192.168.1.1")}, nil
	}
	_, err = guardedDialContext(context.Background(), "tcp", "internal.home:443", fallback)
	var blockedErr *BlockedFetchError
	require.ErrorAs(t, err, &blockedErr)

	lookupIP = func(context.Context, string, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("1.1.1.1"), net.ParseIP("8.8.8.8")}, nil
	}
	spy.failFor = "1.1.1.1:443" // IPv4 unbracketed
	conn, err := guardedDialContext(context.Background(), "tcp", "dual.example:443", spy.dial)
	require.NoError(t, err)
	assert.NotNil(t, conn)
	assert.Contains(t, spy.calls, "1.1.1.1:443")
	assert.Contains(t, spy.calls, "8.8.8.8:443")

	spy.failFor = "8.8.8.8:443"
	lookupIP = func(context.Context, string, string) ([]net.IP, error) { return []net.IP{net.ParseIP("8.8.8.8")}, nil }
	_, err = guardedDialContext(context.Background(), "tcp", "dead.example:443", spy.dial)
	require.Error(t, err)
}
