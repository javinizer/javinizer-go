package updater

import "testing"

func TestAppImageArch(t *testing.T) {
	cases := []struct {
		goarch, want string
		ok           bool
	}{
		{"amd64", "x86_64", true},
		{"arm64", "aarch64", true},
		{"386", "", false},
		{"mips", "", false},
	}
	for _, c := range cases {
		got, ok := appImageArch(c.goarch)
		if got != c.want || ok != c.ok {
			t.Errorf("appImageArch(%q) = (%q, %v), want (%q, %v)", c.goarch, got, ok, c.want, c.ok)
		}
	}
}
