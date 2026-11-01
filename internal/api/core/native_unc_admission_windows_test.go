//go:build windows

package core

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/spf13/afero"
)

// These observers fail the test before an unexpected remote path can reach
// Windows filesystem APIs. They never manufacture a validation error, and
// delegate local operations unchanged to the real filesystem.
type nativeUNCAdmissionFS struct {
	afero.Fs
	observe func(string, string)
}

func (fs nativeUNCAdmissionFS) Stat(name string) (os.FileInfo, error) {
	fs.observe("Stat", name)
	return fs.Fs.Stat(name)
}

func (fs nativeUNCAdmissionFS) LstatIfPossible(name string) (os.FileInfo, bool, error) {
	fs.observe("Lstat", name)
	return fs.Fs.(afero.Lstater).LstatIfPossible(name)
}

func (fs nativeUNCAdmissionFS) Open(name string) (afero.File, error) {
	fs.observe("Open", name)
	return fs.Fs.Open(name)
}

func (fs nativeUNCAdmissionFS) OpenFile(name string, flag int, perm os.FileMode) (afero.File, error) {
	fs.observe("OpenFile", name)
	return fs.Fs.OpenFile(name, flag, perm)
}

func TestNativeWindowsUNCAdmissionContracts(t *testing.T) {
	// Do not parallelize: the real resolver seams are temporarily observed.
	root := t.TempDir()
	// Require a local drive fixture, not an SMB-backed temporary directory.
	if volume := filepath.VolumeName(root); len(volume) != 2 || volume[1] != ':' {
		t.Fatalf("test requires a local drive temporary directory, got %q", root)
	}
	localFile := filepath.Join(root, "owned.txt")
	if err := os.WriteFile(localFile, []byte("owned local control"), 0600); err != nil {
		t.Fatal(err)
	}

	for _, policy := range []struct {
		name    string
		enabled bool
		servers []string
	}{
		{"disabled_even_for_listed_server", false, []string{"coverage-untrusted.invalid"}},
		{"enabled_but_server_unlisted", true, []string{"coverage-trusted.invalid"}},
	} {
		t.Run(policy.name, func(t *testing.T) {
			for _, target := range []struct {
				name   string
				local  string
				remote string
				call   func(*PathValidator, string) (string, error)
			}{
				{"directory", root, `\\coverage-untrusted.invalid\media`, (*PathValidator).ValidateDir},
				{"file", localFile, `\\coverage-untrusted.invalid\media\movie.mp4`, (*PathValidator).ValidateFile},
			} {
				t.Run(target.name, func(t *testing.T) {
					observed := make(map[string][]string)
					observe := func(operation, path string) {
						t.Helper()
						observed[operation] = append(observed[operation], path)
						// Independent of production isUNCPath/normalization so a
						// regression in those helpers cannot disable this guard.
						p := strings.ReplaceAll(path, "/", `\`)
						if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, `\??\`) {
							t.Fatalf("%s reached a network/device path before admission rejected it: %q", operation, path)
						}
					}
					originalEval, originalShort := evalSymlinksFunc, resolveShortPathNameFunc
					t.Cleanup(func() {
						evalSymlinksFunc, resolveShortPathNameFunc = originalEval, originalShort
					})
					evalSymlinksFunc = func(path string) (string, error) {
						observe("EvalSymlinks", path)
						return originalEval(path)
					}
					resolveShortPathNameFunc = func(path string) string {
						observe("GetLongPathName", path)
						return originalShort(path)
					}
					fs := nativeUNCAdmissionFS{Fs: afero.NewOsFs(), observe: observe}
					validator := NewPathValidatorWithUNC(fs, []string{root}, nil, policy.enabled, policy.servers)

					got, err := target.call(validator, target.remote)
					if got != "" || !errors.Is(err, apperrors.ErrUNCPathBlocked) {
						t.Fatalf("remote validation = (%q, %v), want empty path and ErrUNCPathBlocked", got, err)
					}
					// The allowlist must actually be usable, rather than an
					// earlier invalid-configuration gate rejecting every input.
					got, err = target.call(validator, target.local)
					if err != nil {
						t.Fatalf("local control rejected: %v", err)
					}
					actual, err := fs.Stat(got)
					if err != nil {
						t.Fatal(err)
					}
					expected, err := fs.Stat(target.local)
					if err != nil {
						t.Fatal(err)
					}
					if !filepath.IsAbs(got) || !os.SameFile(expected, actual) {
						t.Fatalf("local control returned %q, not the owned fixture %q", got, target.local)
					}
					for _, operation := range []string{"EvalSymlinks", "GetLongPathName", "Stat"} {
						if len(observed[operation]) == 0 {
							t.Errorf("local control did not exercise %s observer", operation)
						}
					}
				})
			}
		})
	}
}
