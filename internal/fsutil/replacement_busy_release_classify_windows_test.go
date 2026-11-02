//go:build windows

package fsutil

import (
	"errors"
	"fmt"
	"testing"

	"github.com/stretchr/testify/require"
	"golang.org/x/sys/windows"
)

func TestReplacementBusyReleaseSharingClassification(t *testing.T) {
	for _, tc := range []struct {
		name    string
		err     error
		allowed bool
	}{
		{"native", windows.ERROR_SHARING_VIOLATION, true},
		{"wrapped", fmt.Errorf("take-aside: %w", windows.ERROR_SHARING_VIOLATION), true},
		{"nil", nil, false},
		{"access_denied", windows.ERROR_ACCESS_DENIED, false},
		{"lookalike", errors.New(windows.ERROR_SHARING_VIOLATION.Error()), false},
		{"foreign_precedence", errors.Join(windows.ERROR_SHARING_VIOLATION, ErrTakeAsideForeign), false},
		{"vanished_precedence", errors.Join(windows.ERROR_SHARING_VIOLATION, ErrTakeAsideVanished), false},
		{"restore_failed_precedence", errors.Join(windows.ERROR_SHARING_VIOLATION, ErrTakeAsideRestoreFailed), false},
	} {
		t.Run(tc.name, func(t *testing.T) { require.Equal(t, tc.allowed, replacementBusyCanMarkReleased(tc.err)) })
	}
}
