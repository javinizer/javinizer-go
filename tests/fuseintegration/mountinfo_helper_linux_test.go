//go:build linux

package fuseintegration

import "strings"

func mountInfoPath(path string) string {
	return strings.NewReplacer("\\", "\\134", " ", "\\040", "\t", "\\011", "\n", "\\012").Replace(path)
}

// mountIDFrom accepts exactly one entry at the exact mountpoint. Stacked
// overmounts can leave the old entry first, so duplicates are ambiguous and
// confer no authority to unmount either filesystem.
func mountIDFrom(data []byte, path string) string {
	escaped, id := mountInfoPath(path), ""
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) > 4 && fields[4] == escaped {
			if id != "" {
				return ""
			}
			id = fields[0]
		}
	}
	return id
}
