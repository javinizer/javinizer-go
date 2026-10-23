package main

import (
	"bytes"
	"context"
	"debug/pe"
	"encoding/binary"
	"flag"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// Child processes inherit Go's coverage environment, so normal go test
// -coverprofile includes the actual main/os.Exit paths without an exit seam.
func TestGeniconProcessContract(t *testing.T) {
	if os.Getenv("JAVINIZER_GENICON_HELPER") == "1" {
		for i, arg := range os.Args {
			if arg == "--" {
				os.Args = append([]string{"genicon"}, os.Args[i+1:]...)
				break
			}
		}
		flag.CommandLine = flag.NewFlagSet("genicon", flag.ExitOnError)
		main()
		os.Exit(0)
	}
	bad := filepath.Join(t.TempDir(), "bad.ico")
	require.NoError(t, os.WriteFile(bad, []byte("not an icon"), 0600))
	type processCase struct {
		name    string
		args    []string
		code    int
		want    string
		output  string
		machine uint16
	}
	cases := []processCase{
		{name: "usage", code: 2, want: "usage: genicon -ico FILE.ico -o FILE.syso [-arch amd64]\n"},
		{name: "missing output", args: []string{"-ico", "../javinizer.ico"}, code: 2, want: "usage: genicon"},
		{name: "flag error", args: []string{"-unknown"}, code: 2, want: "flag provided but not defined: -unknown\nUsage of genicon:"},
		{name: "missing flag value", args: []string{"-ico"}, code: 2, want: "flag needs an argument: -ico"},
		{name: "help", args: []string{"-h"}, want: "Usage of genicon:"},
		{name: "open failure", args: []string{"-ico", filepath.Join(t.TempDir(), "missing.ico"), "-o", filepath.Join(t.TempDir(), "out.syso")}, code: 1, want: "genicon: open ico:"},
		{name: "malformed icon", args: []string{"-ico", bad, "-o", filepath.Join(t.TempDir(), "out.syso")}, code: 1, want: "genicon: load ico:"},
		{name: "unsupported architecture", args: []string{"-ico", "../javinizer.ico", "-o", filepath.Join(t.TempDir(), "out.syso"), "-arch", "mips"}, code: 2, want: "genicon: unsupported arch \"mips\" (want 386, amd64, or arm64)\n"},
		{name: "output directory", args: []string{"-ico", "../javinizer.ico", "-o", t.TempDir()}, code: 1, want: "genicon: create output:"},
	}
	for _, target := range []struct {
		name, arch string
		machine    uint16
	}{{"default", "", pe.IMAGE_FILE_MACHINE_AMD64}, {"386", "386", pe.IMAGE_FILE_MACHINE_I386}, {"amd64", "amd64", pe.IMAGE_FILE_MACHINE_AMD64}, {"arm64", "arm64", pe.IMAGE_FILE_MACHINE_ARM64}} {
		out := filepath.Join(t.TempDir(), "icon.syso")
		args := []string{"-ico", "../javinizer.ico", "-o", out}
		if target.arch != "" {
			args = append(args, "-arch", target.arch)
		}
		cases = append(cases, processCase{name: target.name, args: args, output: out, machine: target.machine})
	}
	if runtime.GOOS == "linux" {
		// Linux's nonpersistent kernel ENOSPC sink opens successfully and rejects writes.
		if _, err := os.Stat("/dev/full"); err == nil {
			cases = append(cases, processCase{name: "actual write failure", args: []string{"-ico", "../javinizer.ico", "-o", "/dev/full"}, code: 1, want: "genicon: write syso:"})
		} else {
			t.Logf("Linux kernel ENOSPC fixture unavailable: %v", err)
		}
	} else {
		t.Log("ENOSPC kernel-device case is Linux-only; other CLI cases run on this platform")
	}
	originalFlags := flag.CommandLine
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			executable, err := os.Executable()
			require.NoError(t, err)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, executable, append([]string{"-test.run=^TestGeniconProcessContract$", "--"}, tc.args...)...)
			cmd.Env = append(os.Environ(), "JAVINIZER_GENICON_HELPER=1")
			var stdout, stderr bytes.Buffer
			cmd.Stdout = &stdout
			cmd.Stderr = &stderr
			err = cmd.Run()
			if tc.code == 0 {
				require.NoError(t, err)
			} else {
				var exit *exec.ExitError
				require.ErrorAs(t, err, &exit)
				require.Equal(t, tc.code, exit.ExitCode())
			}
			require.Empty(t, stdout.String())
			if tc.want == "" {
				require.Empty(t, stderr.String())
			} else {
				require.Contains(t, stderr.String(), tc.want)
			}
			if tc.name == "actual write failure" {
				require.Contains(t, stderr.String(), "no space left on device")
			}
			require.Same(t, originalFlags, flag.CommandLine)
			require.Nil(t, flag.CommandLine.Lookup("ico"))
			if tc.output != "" {
				assertIconObject(t, tc.output, tc.machine)
			}
		})
	}
}

func assertIconObject(t *testing.T, path string, machine uint16) {
	t.Helper()
	object, err := pe.Open(path)
	require.NoError(t, err)
	defer object.Close()
	require.Equal(t, machine, object.Machine)
	section := object.Section(".rsrc")
	require.NotNil(t, section)
	data, err := section.Data()
	require.NoError(t, err)
	require.GreaterOrEqual(t, len(data), 16)
	entries := int(binary.LittleEndian.Uint16(data[12:14])) + int(binary.LittleEndian.Uint16(data[14:16]))
	found := false
	for i := 0; i < entries; i++ {
		off := 16 + i*8
		require.GreaterOrEqual(t, len(data), off+8)
		if binary.LittleEndian.Uint32(data[off:off+4]) != 14 {
			continue
		} // RT_GROUP_ICON
		directory := int(binary.LittleEndian.Uint32(data[off+4:off+8]) & 0x7fffffff)
		require.GreaterOrEqual(t, len(data), directory+24)
		require.Equal(t, uint16(0), binary.LittleEndian.Uint16(data[directory+12:directory+14]))
		require.Equal(t, uint16(1), binary.LittleEndian.Uint16(data[directory+14:directory+16]))
		require.Equal(t, uint32(3), binary.LittleEndian.Uint32(data[directory+16:directory+20]), "Wails requests icon resource ID3")
		found = true
	}
	require.True(t, found, "COFF resources must contain the Wails group icon")
}
