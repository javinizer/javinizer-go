package fsutil

// Codex P2 (PR #239): dirTierKey must normalize the directory path BEFORE the
// "\x00dir" tier suffix is appended. With the suffix already attached,
// filepath.Clean sees it as a new trailing path component and can no longer
// strip a trailing separator or a terminal "."/".." — '/dst' and '/dst/'
// derived distinct keys, so a directory rename could overlap a child write.
// These tables pin that every equivalent spelling of one directory derives
// ONE tier key (the same normalization the file tier gets) and that the
// spellings serialize against each other in both shared/exclusive directions.

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

var dirTierEquivalentSpellings = []struct {
	name     string
	spelling string
}{
	{"canonical", "/beta/dst"},
	{"trailing slash", "/beta/dst/"},
	{"double slash", "/beta//dst//"},
	{"terminal dot", "/beta/dst/."},
	{"terminal dot then slash", "/beta/dst/./"},
	{"interior dotdot", "/beta/other/../dst"},
	{"terminal dotdot", "/beta/dst/x/.."},
	{"terminal dotdot with slash", "/beta/dst/x/../"},
	{"case variant", "/BETA/DsT"},
	{"backslash spelling", `\beta\dst\`},
}

func TestKeyedLock_DirTier_EquivalentSpellingsDeriveOneKey(t *testing.T) {
	previous := PathBackslashesAreSeparators
	PathBackslashesAreSeparators = true
	t.Cleanup(func() { PathBackslashesAreSeparators = previous })

	want := foldKeyedLock(dirTierKey("/beta/dst"))
	for _, tc := range dirTierEquivalentSpellings {
		t.Run(tc.name, func(t *testing.T) {
			require.Equal(t, want, foldKeyedLock(dirTierKey(tc.spelling)),
				"equivalent directory spellings must derive one tier key")
		})
	}
}

func TestKeyedLock_DirTier_EquivalentSpellingsSerialize(t *testing.T) {
	previous := PathBackslashesAreSeparators
	PathBackslashesAreSeparators = true
	t.Cleanup(func() { PathBackslashesAreSeparators = previous })

	for _, tc := range dirTierEquivalentSpellings {
		t.Run(tc.name, func(t *testing.T) {
			t.Run("rename waits for child write", func(t *testing.T) {
				r := NewKeyedLockRegistry()
				child := r.AcquireDirShared(tc.spelling)
				rename := make(chan func(), 1)
				go func() { rename <- r.AcquireDirExclusive("/beta/dst") }()
				select {
				case rel := <-rename:
					rel()
					t.Fatalf("exclusive hold must block behind a shared hold on equivalent spelling %q", tc.spelling)
				case <-time.After(50 * time.Millisecond):
				}
				child()
				select {
				case rel := <-rename:
					rel()
				case <-time.After(2 * time.Second):
					t.Fatal("exclusive acquisition never granted after shared release")
				}
			})
			t.Run("child write waits for rename", func(t *testing.T) {
				r := NewKeyedLockRegistry()
				rename := r.AcquireDirExclusive("/beta/dst")
				child := make(chan func(), 1)
				go func() { child <- r.AcquireDirShared(tc.spelling) }()
				select {
				case rel := <-child:
					rel()
					t.Fatalf("shared hold on equivalent spelling %q must block behind an exclusive hold", tc.spelling)
				case <-time.After(50 * time.Millisecond):
				}
				rename()
				select {
				case rel := <-child:
					rel()
				case <-time.After(2 * time.Second):
					t.Fatal("shared acquisition never granted after exclusive release")
				}
			})
		})
	}
}
