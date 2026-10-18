//go:build linux

package fuseintegration

import "testing"

func TestMountIdentityParser(t *testing.T) {
	for _, tc := range []struct{ name, data, path, want string }{
		{"one exact mount", "42 1 0:5 / /tmp/owned rw - fuse.test synthetic rw\n", "/tmp/owned", "42"},
		{"missing mount", "42 1 0:5 / /tmp/other rw - fuse.test synthetic rw\n", "/tmp/owned", ""},
		{"child does not own parent", "42 1 0:5 / /tmp/owned/child rw - fuse.test synthetic rw\n", "/tmp/owned", ""},
		{"stacked mount refuses", "42 1 0:5 / /tmp/owned rw - fuse.test first rw\n43 42 0:6 / /tmp/owned rw - fuse.test second rw\n", "/tmp/owned", ""},
		{"escaped path", "42 1 0:5 / /tmp/owned\\040space rw - fuse.test synthetic rw\n", "/tmp/owned space", "42"},
		{"malformed line", "42 1 0:5\n", "/tmp/owned", ""},
	} {
		if got := mountIDFrom([]byte(tc.data), tc.path); got != tc.want {
			t.Errorf("%s: got %q, want %q", tc.name, got, tc.want)
		}
	}
}
