package httpclient

import (
	"errors"
	"io"
	"strings"
	"testing"
)

type errReader struct {
	err error
}

func (e *errReader) Read(p []byte) (int, error) {
	return 0, e.err
}

type errCloser struct {
	closed bool
	err    error
}

func (e *errCloser) Close() error {
	e.closed = true
	return e.err
}

type readCloser struct {
	reader io.Reader
	closer *errCloser
}

func (rc *readCloser) Read(p []byte) (int, error) {
	return rc.reader.Read(p)
}

func (rc *readCloser) Close() error {
	return rc.closer.Close()
}

func TestDrainAndClose(t *testing.T) {
	testCases := []struct {
		name     string
		body     io.ReadCloser
		wantErr  bool
		errMatch string
	}{
		{
			name:    "nil body returns nil",
			body:    nil,
			wantErr: false,
		},
		{
			name:    "successful drain and close",
			body:    io.NopCloser(strings.NewReader("hello")),
			wantErr: false,
		},
		{
			name: "drain failure still attempts close",
			body: &readCloser{
				reader: &errReader{err: errors.New("drain error")},
				closer: &errCloser{err: nil},
			},
			wantErr:  true,
			errMatch: "drain error",
		},
		{
			name: "close failure takes precedence over drain error",
			body: &readCloser{
				reader: &errReader{err: errors.New("drain error")},
				closer: &errCloser{err: errors.New("close error")},
			},
			wantErr:  true,
			errMatch: "close error",
		},
		{
			name: "close error returned when drain succeeds",
			body: &readCloser{
				reader: strings.NewReader("data"),
				closer: &errCloser{err: errors.New("close error")},
			},
			wantErr:  true,
			errMatch: "close error",
		},
		{
			name:    "partial drain body larger than 1MB limit",
			body:    io.NopCloser(io.LimitReader(strings.NewReader(""), 2*1024*1024)),
			wantErr: false,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			err := DrainAndClose(tc.body)
			if tc.wantErr {
				if err == nil {
					t.Errorf("DrainAndClose() expected error containing %q, got nil", tc.errMatch)
				} else if tc.errMatch != "" && !strings.Contains(err.Error(), tc.errMatch) {
					t.Errorf("DrainAndClose() error = %v, want containing %q", err, tc.errMatch)
				}
			} else {
				if err != nil {
					t.Errorf("DrainAndClose() unexpected error: %v", err)
				}
			}
		})
	}
}
