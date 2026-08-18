//go:build linux

package fsutil

import (
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// W21: close the linux-file coverage gaps the W20 suite leaves open — the
// package-default replacementLinuxClockTicks closure body (every other test
// swaps the seam for a stub) and the parser boundary branches that no
// platform-level path in the W20 table drives to its true side.

// makeW21ProcStat builds a synthetic /proc/<pid>/stat line whose field-22
// (start time in clock ticks) is field22; comm may itself contain ')' and
// spaces, matching the kernel's quoting rules.
func makeW21ProcStat(comm, field22 string) string {
	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[19] = field22
	return "123 (" + comm + ") " + strings.Join(fields, " ")
}

// TestReplacementBusyW21_LinuxDefaultClockTicksSeam invokes the genuine
// package-default clock seam. Mutating tests restore the var via t.Cleanup,
// so at test entry the var still holds the real closure declared on
// replacement_busy_pid_linux.go's var line — its body only fires here.
func TestReplacementBusyW21_LinuxDefaultClockTicksSeam(t *testing.T) {
	require.Equal(t, int64(100), replacementLinuxClockTicks())
}

// TestReplacementBusyW21_LinuxProcStartTicksParserBounds drives each
// parseReplacementLinuxProcStartTicks failure/success boundary directly so
// both sides of every comparison are exercised.
func TestReplacementBusyW21_LinuxProcStartTicksParserBounds(t *testing.T) {
	t.Run("close paren is final byte", func(t *testing.T) {
		// closeComm >= 0 but closeComm+1 >= len(stat): second operand of the
		// guard, which "malformed" (closeComm == -1) never evaluates to true.
		_, ok := parseReplacementLinuxProcStartTicks("123 (comm)")
		require.False(t, ok)
	})
	t.Run("too few fields", func(t *testing.T) {
		_, ok := parseReplacementLinuxProcStartTicks("123 (comm) S 1 2 3")
		require.False(t, ok)
	})
	t.Run("start field not numeric", func(t *testing.T) {
		_, ok := parseReplacementLinuxProcStartTicks(makeW21ProcStat("comm", "not-a-number"))
		require.False(t, ok)
	})
	t.Run("start field negative", func(t *testing.T) {
		// ParseInt succeeds, so the startTicks < 0 operand is the one that fires.
		_, ok := parseReplacementLinuxProcStartTicks(makeW21ProcStat("comm", "-1"))
		require.False(t, ok)
	})
	t.Run("comm with inner paren parses via last paren", func(t *testing.T) {
		ticks, ok := parseReplacementLinuxProcStartTicks(makeW21ProcStat("weird) name", "250"))
		require.True(t, ok)
		require.Equal(t, int64(250), ticks)
	})
}

// TestReplacementBusyW21_LinuxBootSecondsParserBounds drives the
// parseReplacementLinuxBootSeconds scan branches: a well-formed two-field
// line that is not btime (second operand of the field-shape guard), scan
// exhaustion with no btime line at all, and the bad-btime-value arm.
func TestReplacementBusyW21_LinuxBootSecondsParserBounds(t *testing.T) {
	t.Run("two-field non-btime line skipped", func(t *testing.T) {
		seconds, ok := parseReplacementLinuxBootSeconds("intr 9\nbtime 1234\n")
		require.True(t, ok)
		require.Equal(t, int64(1234), seconds)
	})
	t.Run("no btime line at all", func(t *testing.T) {
		_, ok := parseReplacementLinuxBootSeconds("cpu 1 2 3\nintr 9\n")
		require.False(t, ok)
	})
	t.Run("btime value not numeric", func(t *testing.T) {
		_, ok := parseReplacementLinuxBootSeconds("cpu 1 2 3\nbtime nope\n")
		require.False(t, ok)
	})
}

// TestReplacementBusyW21_LinuxStartTimeParserArmsThroughSeam routes the new
// parser failure arms through the injected proc-read seam so the platform
// function's parse-fallback path is exercised beyond the W20 "malformed" arm,
// and re-asserts the success-arm tick math with a non-trivial clock rate.
func TestReplacementBusyW21_LinuxStartTimeParserArmsThroughSeam(t *testing.T) {
	oldRead := replacementReadProcFile
	oldClock := replacementLinuxClockTicks
	t.Cleanup(func() {
		replacementReadProcFile = oldRead
		replacementLinuxClockTicks = oldClock
	})

	validBoot := []byte("cpu 0 0 0\nbtime 1000\n")
	setRead := func(stat []byte) {
		replacementReadProcFile = func(path string) ([]byte, error) {
			if path == "/proc/321/stat" {
				return stat, nil
			}
			return validBoot, nil
		}
	}
	replacementLinuxClockTicks = func() int64 { return 4 }

	t.Run("trailing paren stat falls back", func(t *testing.T) {
		setRead([]byte("321 (comm)"))
		require.Nil(t, replacementProcessStartTimePlatform(321))
	})
	t.Run("short stat falls back", func(t *testing.T) {
		setRead([]byte("321 (comm) S 1 2 3"))
		require.Nil(t, replacementProcessStartTimePlatform(321))
	})
	t.Run("negative start field falls back", func(t *testing.T) {
		setRead([]byte(makeW21ProcStat("comm", "-1")))
		require.Nil(t, replacementProcessStartTimePlatform(321))
	})
	t.Run("success with fractional remainder", func(t *testing.T) {
		setRead([]byte(makeW21ProcStat("comm", "123")))
		start := replacementProcessStartTimePlatform(321)
		require.NotNil(t, start)
		// 123 ticks at 4 Hz since boot 1000 = 30s + 3/4s.
		require.Equal(t, time.Unix(1000, 0).Add(30*time.Second+750*time.Millisecond), *start)
	})
}
