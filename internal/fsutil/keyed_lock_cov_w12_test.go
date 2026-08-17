package fsutil

import (
	"runtime"
	"testing"

	"github.com/stretchr/testify/require"
)

func w12SetPathBackslashPolicy(t *testing.T, separators bool) {
	t.Helper()
	previous := PathBackslashesAreSeparators
	PathBackslashesAreSeparators = separators
	t.Cleanup(func() { PathBackslashesAreSeparators = previous })
}

func w12SetCasePolicy(t *testing.T, sensitive bool) {
	t.Helper()
	previous := CaseSensitiveProbe
	CaseSensitiveProbe = func(string) (bool, error) { return sensitive, nil }
	ResetCaseSensitivityCache()
	t.Cleanup(func() {
		CaseSensitiveProbe = previous
		ResetCaseSensitivityCache()
	})
}

func TestDestKey_W12POSIXPreservesLiteralBackslashes(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX literal-backslash semantics require a host where filepath treats backslashes as name characters")
	}
	w12SetPathBackslashPolicy(t, false)
	w12SetCasePolicy(t, true)

	literal := `/media/poster\old.jpg`
	separator := `/media/poster/old.jpg`
	require.NotEqual(t, DestKeyForRoot(`/media`, literal), DestKeyForRoot(`/media`, separator))
	require.NotEqual(t, foldKeyedLock(literal), foldKeyedLock(separator), "POSIX lock keys must keep distinct filename shapes distinct")
}

func TestDestKey_W12WindowsNormalizesBackslashes(t *testing.T) {
	w12SetPathBackslashPolicy(t, true)
	w12SetCasePolicy(t, true)

	backslash := `C:\Media\poster.jpg`
	slash := `C:/Media/poster.jpg`
	require.Equal(t, DestKeyForRoot(`C:/Media`, backslash), DestKeyForRoot(`C:/Media`, slash), "Windows separator forms identify one destination")
}

func TestDestKey_W12SeparatorAndCasePoliciesCompose(t *testing.T) {
	previousPathPolicy := PathBackslashesAreSeparators
	previousCaseProbe := CaseSensitiveProbe
	t.Cleanup(func() {
		PathBackslashesAreSeparators = previousPathPolicy
		CaseSensitiveProbe = previousCaseProbe
		ResetCaseSensitivityCache()
	})

	root := t.TempDir()
	literal := root + `/Poster\Old.jpg`
	separator := root + `/poster/old.jpg`
	cases := []struct {
		name          string
		separators    bool
		caseSensitive bool
		equal         bool
	}{
		{name: "posix-sensitive", separators: false, caseSensitive: true, equal: false},
		{name: "posix-insensitive", separators: false, caseSensitive: false, equal: false},
		{name: "windows-sensitive", separators: true, caseSensitive: true, equal: false},
		{name: "windows-insensitive", separators: true, caseSensitive: false, equal: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if runtime.GOOS == "windows" && !tc.separators {
				t.Skip("POSIX separator expectations are not meaningful on a Windows filepath host")
			}
			PathBackslashesAreSeparators = tc.separators
			CaseSensitiveProbe = func(string) (bool, error) { return tc.caseSensitive, nil }
			ResetCaseSensitivityCache()
			if tc.equal {
				require.Equal(t, DestKeyForRoot(root, literal), DestKeyForRoot(root, separator))
				return
			}
			require.NotEqual(t, DestKeyForRoot(root, literal), DestKeyForRoot(root, separator))
		})
	}
}
