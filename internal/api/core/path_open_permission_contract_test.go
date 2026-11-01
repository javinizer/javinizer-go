//go:build linux || darwin

package core_test

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/javinizer/javinizer-go/internal/api/apperrors"
	"github.com/javinizer/javinizer-go/internal/api/core"
)

// A searchable directory need not be readable. Validation may establish its
// identity and policy membership, but an operational caller must not receive a
// handle unless the native open succeeds. No filesystem changes race validation.
func TestValidateAndOpenPathUnreadableDirectoryContract(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("requires unprivileged native directory read permissions")
	}
	for _, browse := range []bool{false, true} {
		name := "scan"
		if browse {
			name = "browse"
		}
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			dir := filepath.Join(root, "searchable-not-readable")
			if err := os.Mkdir(dir, 0700); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(filepath.Join(dir, "retained.txt"), []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				if err := os.Chmod(dir, 0700); err != nil {
					t.Errorf("restore owned directory permissions: %v", err)
				}
			})
			if err := os.Chmod(dir, 0111); err != nil {
				t.Fatal(err)
			}
			probe, err := os.Open(dir)
			if err == nil {
				_ = probe.Close()
				t.Skip("filesystem or process privilege does not enforce directory read permissions")
			}
			if !errors.Is(err, os.ErrPermission) {
				t.Fatalf("native open must fail specifically for permissions: %v", err)
			}

			cfg := &core.SecurityNarrowConfig{AllowedDirectories: []string{root}}
			validate := core.ValidateScanPath
			open := core.ValidateAndOpenPath
			if browse {
				cfg.AllowedDirectories = nil // Configure-scope browsing needs no allowlist.
				validate = core.ValidateBrowsePath
				open = core.ValidateAndOpenBrowsePath
			}
			canonical, err := validate(dir, cfg)
			if err != nil || canonical == "" {
				t.Fatalf("searchable directory should pass path validation: path=%q error=%v", canonical, err)
			}
			f, openedPath, err := open(dir, cfg)
			if f != nil {
				_ = f.Close()
				t.Fatal("denied native open returned a file handle")
			}
			if openedPath != "" || !errors.Is(err, apperrors.ErrPathInvalid) {
				t.Fatalf("denied open must return empty path and ErrPathInvalid: path=%q error=%v", openedPath, err)
			}

			if err := os.Chmod(dir, 0700); err != nil {
				t.Fatal(err)
			}
			f, openedPath, err = open(dir, cfg)
			if err != nil || f == nil {
				t.Fatalf("restoring permissions should permit the same public operation: %v", err)
			}
			defer func() {
				if err := f.Close(); err != nil {
					t.Errorf("close recovered directory handle: %v", err)
				}
			}()
			if openedPath != canonical {
				t.Fatalf("recovered operation changed canonical identity: got %q, want %q", openedPath, canonical)
			}
			entries, err := f.ReadDir(-1)
			if err != nil || len(entries) != 1 || entries[0].Name() != "retained.txt" {
				t.Fatalf("recovered handle must read retained directory entries: entries=%v error=%v", entries, err)
			}
			data, err := os.ReadFile(filepath.Join(dir, "retained.txt"))
			if err != nil || string(data) != "unchanged" {
				t.Fatalf("failed open must not mutate contents: data=%q error=%v", data, err)
			}
		})
	}
}
