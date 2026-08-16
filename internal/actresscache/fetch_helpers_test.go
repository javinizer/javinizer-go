package actresscache

import (
	"bufio"
	"context"
	"io"
	"net"
	"net/http"
)

// mustFetcher unwraps a fetcher construction result, panicking on failure
// (test-only template.Must-style helper).
func mustFetcher(f *Fetcher, err error) *Fetcher {
	if err != nil {
		panic(err)
	}
	return f
}

// serveOnce returns a DialContext stub that serves a canned HTTP response
// over an in-memory pipe, so guard tests can use a pinnable *http.Transport
// without any real network access.
func serveOnce(response string) func(context.Context, string, string) (net.Conn, error) {
	return func(context.Context, string, string) (net.Conn, error) {
		clientSide, serverSide := net.Pipe()
		go func() {
			defer func() { _ = serverSide.Close() }()
			if _, err := http.ReadRequest(bufio.NewReader(serverSide)); err != nil {
				return
			}
			_, _ = io.WriteString(serverSide, response)
		}()
		return clientSide, nil
	}
}
