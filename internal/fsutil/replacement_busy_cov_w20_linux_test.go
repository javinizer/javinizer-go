//go:build linux

package fsutil

import (
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestReplacementBusyW20_LinuxProcessStartTimeConversion(t *testing.T) {
	oldRead := replacementReadProcFile
	oldClock := replacementLinuxClockTicks
	t.Cleanup(func() {
		replacementReadProcFile = oldRead
		replacementLinuxClockTicks = oldClock
	})

	fields := make([]string, 20)
	for i := range fields {
		fields[i] = "0"
	}
	fields[0] = "S"
	fields[19] = "250"
	procPIDStat := "123 (poster) " + strings.Join(fields, " ")
	replacementReadProcFile = func(path string) ([]byte, error) {
		switch path {
		case "/proc/123/stat":
			return []byte(procPIDStat), nil
		case "/proc/stat":
			return []byte("cpu 1 2 3\nbtime 1000\n"), nil
		default:
			return nil, errors.New("unexpected proc path")
		}
	}
	replacementLinuxClockTicks = func() int64 { return 100 }

	start := replacementProcessStartTimePlatform(123)
	require.NotNil(t, start)
	require.Equal(t, time.Unix(1002, 500*int64(time.Millisecond)), *start)
}

func TestReplacementBusyW20_LinuxProcessStartTimeFailureFallsBack(t *testing.T) {
	oldRead := replacementReadProcFile
	oldClock := replacementLinuxClockTicks
	t.Cleanup(func() {
		replacementReadProcFile = oldRead
		replacementLinuxClockTicks = oldClock
	})

	validFields := make([]string, 20)
	for i := range validFields {
		validFields[i] = "0"
	}
	validFields[0] = "S"
	validFields[19] = "250"
	validStat := []byte("123 (poster) " + strings.Join(validFields, " "))
	validBoot := []byte("btime 1000\n")

	tests := []struct {
		name  string
		read  func(string) ([]byte, error)
		clock int64
	}{
		{
			name:  "pid stat read error",
			read:  func(string) ([]byte, error) { return nil, errors.New("stat unavailable") },
			clock: 100,
		},
		{
			name: "pid stat parse error",
			read: func(path string) ([]byte, error) {
				if path == "/proc/123/stat" {
					return []byte("malformed"), nil
				}
				return validBoot, nil
			},
			clock: 100,
		},
		{
			name: "boot read error",
			read: func(path string) ([]byte, error) {
				if path == "/proc/123/stat" {
					return validStat, nil
				}
				return nil, errors.New("boot unavailable")
			},
			clock: 100,
		},
		{
			name: "boot parse error",
			read: func(path string) ([]byte, error) {
				if path == "/proc/123/stat" {
					return validStat, nil
				}
				return []byte("btime not-a-number\n"), nil
			},
			clock: 100,
		},
		{
			name: "clock ticks unavailable",
			read: func(path string) ([]byte, error) {
				if path == "/proc/123/stat" {
					return validStat, nil
				}
				return validBoot, nil
			},
			clock: 0,
		},
		{
			name: "duration overflow",
			read: func(path string) ([]byte, error) {
				if path == "/proc/123/stat" {
					fields := make([]string, 20)
					for i := range fields {
						fields[i] = "0"
					}
					fields[0] = "S"
					fields[19] = fmt.Sprint(int64(^uint64(0) >> 1))
					return []byte("123 (poster) " + strings.Join(fields, " ")), nil
				}
				return validBoot, nil
			},
			clock: 100,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			replacementReadProcFile = tc.read
			replacementLinuxClockTicks = func() int64 { return tc.clock }
			require.Nil(t, replacementProcessStartTimePlatform(123))
		})
	}
	require.Nil(t, replacementProcessStartTimePlatform(0))
}
