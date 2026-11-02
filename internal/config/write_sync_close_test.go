package config

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
)

type protocolFailure struct{ operation string }

func (e *protocolFailure) Error() string { return e.operation + " failed" }

type protocolHandle struct {
	trace                       []string
	offered                     [][]byte
	writeErr, syncErr, closeErr error
	partial                     bool
}

func (f *protocolHandle) Write(p []byte) (int, error) {
	f.trace = append(f.trace, "write")
	f.offered = append(f.offered, append([]byte(nil), p...))
	if f.writeErr != nil {
		if f.partial {
			return len(p) / 2, f.writeErr
		}
		return 0, f.writeErr
	}
	return len(p), nil
}
func (f *protocolHandle) Sync() error  { f.trace = append(f.trace, "sync"); return f.syncErr }
func (f *protocolHandle) Close() error { f.trace = append(f.trace, "close"); return f.closeErr }

func TestWriteSyncClose_ProtocolAndErrorPriority(t *testing.T) {
	writeFailure := &protocolFailure{"write"}
	syncFailure := &protocolFailure{"sync"}
	closeFailure := &protocolFailure{"close"}
	for _, description := range []string{"temp config file", "config lock"} {
		t.Run(description, func(t *testing.T) {
			for _, tc := range []struct {
				name                        string
				writeErr, syncErr, closeErr error
				partial                     bool
				primary                     *protocolFailure
				trace                       []string
			}{
				{name: "success", trace: []string{"write", "sync", "close"}},
				{name: "write", writeErr: writeFailure, primary: writeFailure, trace: []string{"write", "close"}},
				{name: "partial write and close", writeErr: writeFailure, closeErr: closeFailure, partial: true, primary: writeFailure, trace: []string{"write", "close"}},
				{name: "write prevents sync", writeErr: writeFailure, syncErr: syncFailure, closeErr: closeFailure, primary: writeFailure, trace: []string{"write", "close"}},
				{name: "sync", syncErr: syncFailure, primary: syncFailure, trace: []string{"write", "sync", "close"}},
				{name: "sync and close", syncErr: syncFailure, closeErr: closeFailure, primary: syncFailure, trace: []string{"write", "sync", "close"}},
				{name: "close", closeErr: closeFailure, primary: closeFailure, trace: []string{"write", "sync", "close"}},
			} {
				t.Run(tc.name, func(t *testing.T) {
					payload := []byte("configuration or lock token")
					f := &protocolHandle{writeErr: tc.writeErr, syncErr: tc.syncErr, closeErr: tc.closeErr, partial: tc.partial}
					err := writeSyncClose(f, payload, description)
					require.Equal(t, tc.trace, f.trace, "exactly one close; no retry or sync after write failure")
					require.Equal(t, [][]byte{payload}, f.offered, "write receives the complete payload once")
					if tc.primary == nil {
						require.NoError(t, err)
						return
					}
					require.ErrorIs(t, err, tc.primary)
					var typed *protocolFailure
					require.ErrorAs(t, err, &typed)
					require.Same(t, tc.primary, typed)
					require.EqualError(t, err, fmt.Sprintf("failed to %s %s: %s", tc.primary.operation, description, tc.primary))
					for _, other := range []*protocolFailure{writeFailure, syncFailure, closeFailure} {
						if other != tc.primary {
							require.False(t, errors.Is(err, other), "secondary/unexecuted errors do not replace or join the primary")
						}
					}
				})
			}
		})
	}
}
