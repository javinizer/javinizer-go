package workflow

import (
	"regexp"

	"github.com/javinizer/javinizer-go/internal/fsutil"
)

var stagedBusyReservationPattern = regexp.MustCompile("(?i)" + regexp.QuoteMeta(fsutil.ReplacementBusySuffix) + `(?:(?:\.takeover-|\.quarantine-)[1-9][0-9]*-[0-9a-f]{1,16})?(?:\.vac\.[0-9a-f]{32})*$`)

func stagedBusyReservation(name string) bool {
	return stagedBusyReservationPattern.MatchString(name)
}
