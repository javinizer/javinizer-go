//go:build linux

package fsutil

import (
	"fmt"
	"os"
	"strconv"
	"strings"
	"time"
)

// /proc/<pid>/stat reports starttime in clock ticks since boot. Linux's
// SC_CLK_TCK is 100 for the supported user-space ABI; keep the read behind a
// seam so the classifier can be exercised without depending on a real PID.
var replacementReadProcFile = os.ReadFile
var replacementLinuxClockTicks = func() int64 { return 100 }

func replacementProcessStartTimePlatform(pid int) *time.Time {
	if pid <= 0 {
		return nil
	}

	stat, err := replacementReadProcFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		return nil
	}
	startTicks, ok := parseReplacementLinuxProcStartTicks(string(stat))
	if !ok {
		return nil
	}

	procStat, err := replacementReadProcFile("/proc/stat")
	if err != nil {
		return nil
	}
	bootSeconds, ok := parseReplacementLinuxBootSeconds(string(procStat))
	if !ok {
		return nil
	}

	clockTicks := replacementLinuxClockTicks()
	if clockTicks <= 0 {
		return nil
	}
	seconds := startTicks / clockTicks
	remainingTicks := startTicks % clockTicks
	if seconds > int64((1<<63-1)/time.Second) {
		return nil
	}
	start := time.Unix(bootSeconds, 0).
		Add(time.Duration(seconds) * time.Second).
		Add(time.Duration(remainingTicks) * time.Second / time.Duration(clockTicks))
	return &start
}

func parseReplacementLinuxProcStartTicks(stat string) (int64, bool) {
	// The comm field may contain spaces and ')' characters. The final ')' is
	// the delimiter before field 3; field 22 is index 19 in the remainder.
	closeComm := strings.LastIndexByte(stat, ')')
	if closeComm < 0 || closeComm+1 >= len(stat) {
		return 0, false
	}
	fields := strings.Fields(stat[closeComm+1:])
	const startTimeIndex = 22 - 3
	if len(fields) <= startTimeIndex {
		return 0, false
	}
	startTicks, err := strconv.ParseInt(fields[startTimeIndex], 10, 64)
	if err != nil || startTicks < 0 {
		return 0, false
	}
	return startTicks, true
}

func parseReplacementLinuxBootSeconds(procStat string) (int64, bool) {
	for _, line := range strings.Split(procStat, "\n") {
		fields := strings.Fields(line)
		if len(fields) != 2 || fields[0] != "btime" {
			continue
		}
		seconds, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil {
			return 0, false
		}
		return seconds, true
	}
	return 0, false
}
