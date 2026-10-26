package metadatafetch

import (
	"context"
	"errors"
	"net"
	"sync"
	"testing"
)

func TestConnectionOwnerClosesLateDialBeforeReturning(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	owner := &connectionOwner{cancel: cancel}
	entered, release, rawClosed := make(chan struct{}), make(chan struct{}), make(chan struct{})
	client, server := net.Pipe()
	defer server.Close()
	defer client.Close()
	connected := make(chan error, 1)
	go func() {
		_, err := owner.connect(ctx, "tcp", "80", []net.IP{net.ParseIP("203.0.113.17")}, func(context.Context, string, string) (net.Conn, error) {
			close(entered)
			// Model a successful socket racing with cancellation. No network is
			// touched; cleanup must wait until this late socket is closed.
			<-release
			return &closeObservedConn{Conn: client, closed: rawClosed}, nil
		})
		connected <- err
	}()
	<-entered
	cleanup := make(chan struct{})
	go func() { owner.close(); close(cleanup) }()
	<-ctx.Done()
	select {
	case <-cleanup:
		t.Fatal("cleanup escaped while dial was in flight")
	default:
	}
	close(release)
	if err := <-connected; !errors.Is(err, ErrConnect) {
		t.Fatal(err)
	}
	<-cleanup
	select {
	case <-rawClosed:
	default:
		t.Fatal("late connection not closed")
	}
	var group sync.WaitGroup
	for range 20 {
		group.Go(func() {
			_, err := owner.connect(t.Context(), "tcp", "80", []net.IP{net.ParseIP("203.0.113.17")}, func(context.Context, string, string) (net.Conn, error) {
				t.Error("dial admitted after cleanup")
				return nil, errors.New("unexpected dial")
			})
			if !errors.Is(err, ErrConnect) {
				t.Errorf("closed owner err=%v", err)
			}
			owner.close()
		})
	}
	group.Wait()
}

func TestConnectionOwnerRejectsUnexpectedSecondConnection(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	owner := &connectionOwner{cancel: cancel}
	defer owner.close()
	first, firstPeer := net.Pipe()
	defer firstPeer.Close()
	second, secondPeer := net.Pipe()
	defer secondPeer.Close()
	firstClosed, secondClosed := make(chan struct{}), make(chan struct{})
	ips := []net.IP{net.ParseIP("203.0.113.17")}
	if _, err := owner.connect(ctx, "tcp", "80", ips, func(context.Context, string, string) (net.Conn, error) {
		return &closeObservedConn{Conn: first, closed: firstClosed}, nil
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := owner.connect(ctx, "tcp", "80", ips, func(context.Context, string, string) (net.Conn, error) {
		return &closeObservedConn{Conn: second, closed: secondClosed}, nil
	}); !errors.Is(err, ErrConnect) {
		t.Fatal(err)
	}
	select {
	case <-secondClosed:
	default:
		t.Fatal("unexpected socket not closed")
	}
	owner.close()
	select {
	case <-firstClosed:
	default:
		t.Fatal("owned socket not closed")
	}
}
