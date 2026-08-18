package fsutil

// POSTER-WRITE-HARDENING codex PR#215 wave-19 (P2) — PublishRefusal is the
// shared classifier history and the downloader consult before deciding a
// journaled backup name is UNOWNED: both typed occupied-name classes (and
// their wrapped forms) classify; anything else keeps the caller's plain
// warn-only posture.

import (
	"errors"
	"fmt"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestPublishRefusalW19_Classification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"plain sentinel", errors.New("staging wedged"), false},
		{"os.PathError without the class", &os.PathError{Op: "rename", Path: "/x", Err: os.ErrPermission}, false},
		{"collision", ErrPublishCollision, true},
		{"wrapped collision", fmt.Errorf("re-arm install backup /x: %w", ErrPublishCollision), true},
		{"double-wrapped collision", fmt.Errorf("outer: %w", fmt.Errorf("inner: %w", ErrPublishCollision)), true},
		{"no-replace unsupported", ErrPublishNoReplaceUnsupported, true},
		{"wrapped unsupported", fmt.Errorf("no-replace publish /a -> /b: %w: %w", ErrPublishNoReplaceUnsupported, errors.New("link EPERM")), true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, tc.want, PublishRefusal(tc.err))
		})
	}
}
