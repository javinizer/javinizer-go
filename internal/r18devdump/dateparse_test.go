package r18devdump

import "testing"

func TestParseFilenameSourceDate(t *testing.T) {
	cases := []struct {
		name string
		in   string
		want string
	}{
		{"canonical", "r18dotdev_dump_2026-09-20.sql.gz", "2026-09-20"},
		{"no marker", "r18dotdev.sql.gz", ""},
		{"invalid calendar date", "r18dotdev_dump_2026-13-99.sql.gz", ""},
		{"uppercase marker", "r18dotdev_DUMP_2026-01-01.sql.gz", ""},
		{"end of token", "r18dotdev_dump_2026-09-20", "2026-09-20"},
		{"dot terminator any extension", "r18dotdev_dump_2026-09-20.foo", "2026-09-20"},
		{"letter after date", "r18dotdev_dump_2026-09-20abc", ""},
		{"digit after date", "r18dotdev_dump_2026-09-201.sql.gz", ""},
		{"query after dot", "r18dotdev_dump_2026-09-20.sql.gz?x=1", "2026-09-20"},
		{"short date", "r18dotdev_dump_2026-09-2.sql.gz", ""},
		{"marker only", "_dump_", ""},
		{"marker at start", "_dump_2026-09-20.sql.gz", "2026-09-20"},
		{"feb 29 leap", "r18dotdev_dump_2024-02-29.sql.gz", "2024-02-29"},
		{"feb 29 non-leap", "r18dotdev_dump_2025-02-29.sql.gz", ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := ParseFilenameSourceDate(tc.in); got != tc.want {
				t.Errorf("ParseFilenameSourceDate(%q) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}
