package ssrf

import (
	"context"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/url"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPublicChecksRejectLinkLocalMulticastBeforeDial(t *testing.T) {
	// The package-global resolver seam requires serial tests and cleanup.
	for _, address := range []string{"224.0.0.1", "ff02::1"} {
		t.Run(address, func(t *testing.T) {
			useTestLookup(t, map[string][]net.IP{"multicast.test": {net.ParseIP(address)}})
			want := "SSRF blocked: multicast.test resolves to private/internal IP " + address
			if err := CheckURL("https://multicast.test/resource"); err == nil || err.Error() != want {
				t.Fatalf("CheckURL error = %v, want %q", err, want)
			}

			dials := 0
			transport := &http.Transport{DialContext: func(context.Context, string, string) (net.Conn, error) {
				dials++
				return nil, errors.New("unexpected multicast dial")
			}}
			t.Cleanup(transport.CloseIdleConnections)
			WrapTransportWithSSRFCheck(transport)
			conn, err := transport.DialContext(t.Context(), "tcp", "multicast.test:443")
			if conn != nil || err == nil || err.Error() != want {
				t.Errorf("wrapped dial = (%v, %v), want (nil, %q)", conn, err, want)
			}
			if dials != 0 {
				t.Errorf("multicast rejection reached the underlying dialer %d times", dials)
			}
		})
	}
}

func TestCheckURLRejectsMalformedURLBeforeDNS(t *testing.T) {
	lookups := 0
	t.Cleanup(setLookupIPForTest(func(string) ([]net.IP, error) {
		lookups++
		return nil, errors.New("unexpected malformed-URL lookup")
	}))
	const rawURL = "https://public.test/%zz"
	err := CheckURL(rawURL)
	if err == nil || !strings.HasPrefix(err.Error(), "invalid URL: ") {
		t.Fatalf("CheckURL error = %v, want invalid URL wrapper", err)
	}
	var parseErr *url.Error
	if !errors.As(err, &parseErr) || parseErr.Op != "parse" || parseErr.URL != rawURL {
		t.Errorf("URL parse cause not preserved: %v", err)
	}
	if lookups != 0 {
		t.Errorf("malformed URL triggered %d DNS lookups", lookups)
	}
}

func TestCallerBudgetCountsOnlyCompatibleFailoverAddresses(t *testing.T) {
	for _, tc := range []struct {
		network string
		answers []net.IP
		want    []string
	}{
		{
			network: "tcp4",
			answers: []net.IP{net.ParseIP("203.0.113.1"), net.ParseIP("2001:db8::1"), net.ParseIP("203.0.113.2")},
			want:    []string{"203.0.113.1:443", "203.0.113.2:443"},
		},
		{
			network: "tcp6",
			answers: []net.IP{net.ParseIP("2001:db8::1"), net.ParseIP("203.0.113.1"), net.ParseIP("2001:db8::2")},
			want:    []string{"[2001:db8::1]:443", "[2001:db8::2]:443"},
		},
	} {
		t.Run(tc.network, func(t *testing.T) {
			// A large budget makes deadline comparisons insensitive to ordinary
			// scheduling delays; the fixture never waits for a timeout.
			const budget = time.Hour
			var overallDeadline time.Time
			lookups := 0
			before := time.Now()
			t.Cleanup(setLookupIPContextForTest(func(ctx context.Context, host string) ([]net.IP, error) {
				lookups++
				if host != "public.test" {
					return nil, fmt.Errorf("unexpected test DNS lookup: %s", host)
				}
				observed := time.Now()
				var ok bool
				overallDeadline, ok = ctx.Deadline()
				if !ok || overallDeadline.Before(before.Add(budget)) || overallDeadline.After(observed.Add(budget)) {
					t.Errorf("DNS deadline = %v, want caller-owned %v budget", overallDeadline, budget)
				}
				return tc.answers, nil
			}))

			client, server := net.Pipe()
			t.Cleanup(func() { client.Close() })
			t.Cleanup(func() { server.Close() })
			var addresses []string
			var firstAttempt context.Context
			marker := errors.New("first eligible address unavailable")
			transport := &http.Transport{DialContext: func(ctx context.Context, network, address string) (net.Conn, error) {
				addresses = append(addresses, address)
				if network != tc.network {
					t.Errorf("network = %q, want %q", network, tc.network)
				}
				deadline, ok := ctx.Deadline()
				if !ok || ctx.Err() != nil {
					t.Fatalf("attempt lacks a live deadline: deadline=%v err=%v", deadline, ctx.Err())
				}
				switch len(addresses) {
				case 1:
					firstAttempt = ctx
					now := time.Now()
					share := float64(deadline.Sub(now)) / float64(overallDeadline.Sub(now))
					if share < 0.45 || share > 0.55 {
						t.Errorf("first attempt has %.3f of the remaining budget, want half for two compatible addresses (not one third)", share)
					}
					return nil, marker
				case 2:
					if !errors.Is(firstAttempt.Err(), context.Canceled) {
						t.Errorf("failed attempt context was not canceled before failover: %v", firstAttempt.Err())
					}
					if !deadline.Equal(overallDeadline) {
						t.Errorf("final deadline = %v, want original overall deadline %v", deadline, overallDeadline)
					}
					return client, nil
				default:
					return nil, errors.New("unexpected extra dial")
				}
			}}
			t.Cleanup(transport.CloseIdleConnections)
			if WrapTransportWithSSRFCheck(transport, budget) != transport {
				t.Fatal("wrapper replaced the caller-owned transport")
			}
			conn, err := transport.DialContext(t.Context(), tc.network, "public.test:443")
			if err != nil || conn != client {
				t.Errorf("failover = (%v, %v), want the successful net.Pipe connection", conn, err)
			}
			if lookups != 1 {
				t.Errorf("DNS lookups = %d, want one lookup shared by all attempts", lookups)
			}
			if !reflect.DeepEqual(addresses, tc.want) {
				t.Errorf("dialed %v, want only compatible pinned addresses %v in order", addresses, tc.want)
			}
		})
	}
}
