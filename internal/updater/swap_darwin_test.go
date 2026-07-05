//go:build darwin

package updater

import (
	"archive/zip"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withExecutableFunc swaps the package-level executable seam to exePath for
// the duration of t and restores it on cleanup. Tests are sequential (no
// t.Parallel) so the global swap is race-free under -race.
func withExecutableFunc(t *testing.T, exePath string) {
	t.Helper()
	prev := executableFunc
	executableFunc = func() (string, error) { return exePath, nil }
	t.Cleanup(func() { executableFunc = prev })
}

// buildFakeApp creates a synthetic .app bundle layout under dir and returns
// the inner executable path (.../Javinizer.app/Contents/MacOS/Javinizer).
func buildFakeApp(t *testing.T, dir string) string {
	t.Helper()
	exe := filepath.Join(dir, "Javinizer.app", "Contents", "MacOS", "Javinizer")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(exe, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write exec: %v", err)
	}
	return exe
}

func TestAppBundlePath_Canonical(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	want := filepath.Join(dir, "Javinizer.app")
	got, err := appBundlePath(exe)
	if err != nil {
		t.Fatalf("appBundlePath: %v", err)
	}
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAppBundlePath_FallbackWalkUp(t *testing.T) {
	// Non-canonical layout: extra dir between MacOS and the exec, so the
	// three-level walk-up misses; the *.app ancestor walk-up must catch it.
	dir := t.TempDir()
	exe := filepath.Join(dir, "Foo.app", "Contents", "MacOS", "Resources", "helper")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	got, err := appBundlePath(exe)
	if err != nil {
		t.Fatalf("appBundlePath: %v", err)
	}
	want := filepath.Join(dir, "Foo.app")
	if got != want {
		t.Fatalf("got %q, want %q", got, want)
	}
}

func TestAppBundlePath_NotInBundle(t *testing.T) {
	dir := t.TempDir()
	exe := filepath.Join(dir, "bin", "javinizer")
	if err := os.MkdirAll(filepath.Dir(exe), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := appBundlePath(exe); err == nil {
		t.Fatal("expected error for executable not in .app bundle")
	}
}

func TestDarwinSwapper_Target(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	got, err := NewDarwinSwapper().Target()
	if err != nil {
		t.Fatalf("Target: %v", err)
	}
	want := filepath.Join(dir, "Javinizer.app")
	if got != want {
		t.Fatalf("Target = %q, want %q", got, want)
	}
}

func TestDarwinSwapper_CanSwap_Writable(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	if err := NewDarwinSwapper().CanSwap(); err != nil {
		t.Fatalf("CanSwap on writable dir: unexpected error: %v", err)
	}
}

func TestDarwinSwapper_CanSwap_ReadOnly(t *testing.T) {
	parent := t.TempDir()
	readonly := filepath.Join(parent, "apps")
	exe := buildFakeApp(t, readonly)
	if err := os.Chmod(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })
	withExecutableFunc(t, exe)
	if err := NewDarwinSwapper().CanSwap(); err == nil {
		t.Fatal("expected CanSwap to fail on read-only parent")
	}
}

// buildBundleZip writes a zip whose top-level entry is Javinizer.app/... into a
// temp file and returns its path. Mirrors the `ditto -c -k --keepParent` layout.
func buildBundleZip(t *testing.T, dir string) string {
	t.Helper()
	zipPath := filepath.Join(dir, "asset.zip")
	w, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	zw := zip.NewWriter(w)
	files := []struct {
		name string
		data string
		mode os.FileMode
	}{
		{"Javinizer.app/", "", 0o755 | os.ModeDir},
		{"Javinizer.app/Contents/", "", 0o755 | os.ModeDir},
		{"Javinizer.app/Contents/MacOS/", "", 0o755 | os.ModeDir},
		{"Javinizer.app/Contents/Info.plist", "<?xml version=\"1.0\"?>\n", 0o644},
		{"Javinizer.app/Contents/MacOS/Javinizer", "#!/bin/sh\necho hi\n", 0o755},
	}
	for _, f := range files {
		hdr := &zip.FileHeader{Name: f.name, Method: zip.Deflate}
		hdr.SetMode(f.mode)
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("create %s: %v", f.name, err)
		}
		if f.data != "" {
			if _, err := fw.Write([]byte(f.data)); err != nil {
				t.Fatalf("write %s: %v", f.name, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return zipPath
}

func TestDarwinSwapper_Stage(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	zipPath := buildBundleZip(t, dir)

	staged, err := NewDarwinSwapper().Stage(context.Background(), zipPath, "Javinizer-macos-universal.zip")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	if filepath.Base(staged) != macAppBundleName {
		t.Fatalf("staged base = %q, want %q", filepath.Base(staged), macAppBundleName)
	}
	if staged == exe {
		t.Fatal("staged path must differ from the running executable")
	}
	info, err := os.Stat(staged)
	if err != nil {
		t.Fatalf("stat staged: %v", err)
	}
	if !info.IsDir() {
		t.Fatal("staged bundle is not a directory")
	}
	innerExec := filepath.Join(staged, "Contents", "MacOS", "Javinizer")
	if fi, err := os.Stat(innerExec); err != nil {
		t.Fatalf("staged exec missing: %v", err)
	} else if fi.Mode()&0o100 == 0 {
		t.Errorf("staged exec not executable: %v", fi.Mode())
	}
	if _, err := os.Stat(filepath.Join(staged, "Contents", "Info.plist")); err != nil {
		t.Fatalf("staged Info.plist missing: %v", err)
	}
}

func TestDarwinSwapper_Stage_SameVolumeAsTarget(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	zipPath := buildBundleZip(t, dir)

	staged, err := NewDarwinSwapper().Stage(context.Background(), zipPath, "Javinizer-macos-universal.zip")
	if err != nil {
		t.Fatalf("Stage: %v", err)
	}
	// Staging dir must be a sibling of the target .app (same filesystem), so
	// the swap rename cannot fail with EXDEV.
	if filepath.Dir(staged) == dir {
		t.Fatalf("staged dir %q should be a temp subdir of %q, not the dir itself", filepath.Dir(staged), dir)
	}
	if !strings.HasPrefix(filepath.Dir(staged), dir+string(os.PathSeparator)) {
		t.Fatalf("staged dir %q is not adjacent to target parent %q", filepath.Dir(staged), dir)
	}
}

func TestDarwinSwapper_Stage_CancelledContext(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	zipPath := buildBundleZip(t, dir)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := NewDarwinSwapper().Stage(ctx, zipPath, "Javinizer-macos-universal.zip"); err == nil {
		t.Fatal("expected error on cancelled context")
	}
}

func TestDarwinSwapper_Stage_BadZip(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	bad := filepath.Join(dir, "not-a-zip")
	if err := os.WriteFile(bad, []byte("definitely not a zip"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewDarwinSwapper().Stage(context.Background(), bad, "Javinizer-macos-universal.zip"); err == nil {
		t.Fatal("expected error for non-zip asset")
	}
}

func TestDarwinSwapScript_Contents(t *testing.T) {
	app := "/Applications/Javinizer.app"
	staged := "/tmp/stage/Javinizer.app"
	got := darwinSwapScript(app, staged, 4242)
	for _, want := range []string{
		"kill -0 4242",
		"sleep 0.2",
		"timed out waiting for process exit",
		"exit 1",
		"if mv '/Applications/Javinizer.app' '/Applications/Javinizer.app.old'; then",
		"  if mv '/tmp/stage/Javinizer.app' '/Applications/Javinizer.app'; then",
		"    xattr -cr '/Applications/Javinizer.app'",
		"    open -n '/Applications/Javinizer.app'",
		"    rm -rf '/Applications/Javinizer.app.old'",
		"    rmdir '/tmp/stage'",
		"  else",
		"    rm -rf '/Applications/Javinizer.app'",
		"    mv '/Applications/Javinizer.app.old' '/Applications/Javinizer.app'",
		"    echo 'javinizer: swap failed; restored previous bundle'",
		"  fi",
		"else",
		"  echo 'javinizer: could not move current bundle aside; aborting swap'",
		"fi",
	} {
		if !strings.Contains(got, want) {
			t.Errorf("script missing %q\nscript:\n%s", want, got)
		}
	}
}

// TestDarwinSwapScript_RollbackOnSwapFailure asserts the script fails safe:
// the catastrophic path (rm -rf of .old after a failed swap) must NOT run,
// and the previous bundle must be restored from .old. Without this guard a
// failed staged->app move would brick the install (the only good copy deleted).
func TestDarwinSwapScript_RollbackOnSwapFailure(t *testing.T) {
	app := "/Applications/Javinizer.app"
	staged := "/tmp/stage/Javinizer.app"
	got := darwinSwapScript(app, staged, 4242)

	// The rm -rf of .old must be INSIDE the success branch (after `if mv`),
	// not unconditional. Confirm it follows the `then`, not the failed-swap path.
	if !strings.Contains(got, "then") || !strings.Contains(got, "else") {
		t.Fatal("script must guard the swap with if/then/else for rollback")
	}
	thenIdx := strings.Index(got, "then")
	elseIdx := strings.Index(got, "else")
	rmOld := "rm -rf '/Applications/Javinizer.app.old'"
	mvAppToOldIdx := strings.Index(got, "mv '/Applications/Javinizer.app' '/Applications/Javinizer.app.old'")
	restoreIdx := strings.Index(got, "mv '/Applications/Javinizer.app.old' '/Applications/Javinizer.app'")

	// A stale .old from a prior swap would make `mv app appOld` nest or fail,
	// breaking rollback. The script must pre-clear .old before the first mv.
	preClearIdx := strings.Index(got, rmOld)
	if !(preClearIdx < mvAppToOldIdx) {
		t.Errorf("pre-clear rm -rf .old must precede mv app->.old; got preClear=%d mv=%d\n%s", preClearIdx, mvAppToOldIdx, got)
	}

	// The success-branch rm -rf .old (the second occurrence, after then) must
	// still be between then and else.
	successRmIdx := strings.Index(got[thenIdx:], rmOld) + thenIdx
	if !(thenIdx < successRmIdx && successRmIdx < elseIdx) {
		t.Errorf("rm -rf .old must be in the success branch (between then and else); got then=%d rm=%d else=%d\n%s", thenIdx, successRmIdx, elseIdx, got)
	}
	if !(elseIdx < restoreIdx) {
		t.Errorf("restore-from-.old must be in the else branch (after else); got else=%d restore=%d\n%s", elseIdx, restoreIdx, got)
	}
}

func TestDarwinSwapScript_QuotingSpaces(t *testing.T) {
	app := "/Users/me/My Apps/Javinizer.app"
	staged := "/tmp/stage dir/Javinizer.app"
	got := darwinSwapScript(app, staged, 1)
	if !strings.Contains(got, "'/Users/me/My Apps/Javinizer.app'") {
		t.Errorf("script does not quote spaced app path:\n%s", got)
	}
	if !strings.Contains(got, "'/tmp/stage dir/Javinizer.app'") {
		t.Errorf("script does not quote spaced staged path:\n%s", got)
	}
}

func TestShellQuote_SingleQuote(t *testing.T) {
	got := shellQuote("a'b")
	if got != `'a'\''b'` {
		t.Fatalf("shellQuote(`a'b`) = %q", got)
	}
}

func TestDarwinSwapper_SwapAndRelaunch_InvalidPID(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	if err := NewDarwinSwapper().SwapAndRelaunch(context.Background(), "/tmp/staged", 0); err == nil {
		t.Fatal("expected error for oldPID=0")
	}
	if err := NewDarwinSwapper().SwapAndRelaunch(context.Background(), "/tmp/staged", -1); err == nil {
		t.Fatal("expected error for oldPID=-1")
	}
}

func TestDarwinSwapper_SwapAndRelaunch_TargetError(t *testing.T) {
	// Executable not inside an .app -> Target fails before any spawn.
	dir := t.TempDir()
	exe := filepath.Join(dir, "javinizer")
	if err := os.WriteFile(exe, []byte("x"), 0o755); err != nil {
		t.Fatal(err)
	}
	withExecutableFunc(t, exe)
	if err := NewDarwinSwapper().SwapAndRelaunch(context.Background(), "/tmp/staged", 9999); err == nil {
		t.Fatal("expected Target error to prevent spawn")
	}
}

func TestUnzipTo_Symlink(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "sym.zip")
	w, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	hdr := &zip.FileHeader{Name: "link", Method: zip.Deflate}
	hdr.SetMode(0o777 | os.ModeSymlink)
	fw, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("target.txt")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	w.Close()

	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zipPath, dest); err != nil {
		t.Fatalf("unzipTo: %v", err)
	}
	got, err := os.Readlink(filepath.Join(dest, "link"))
	if err != nil {
		t.Fatalf("Readlink: %v", err)
	}
	if got != "target.txt" {
		t.Fatalf("symlink target = %q, want target.txt", got)
	}
}

func TestIsWithinDir(t *testing.T) {
	cases := []struct {
		path, dir string
		want      bool
	}{
		{"/a/b/c", "/a/b", true},
		{"/a/b", "/a/b", true},
		{"/a/b/c", "/a/b/", true},
		{"/a/c", "/a/b", false},
		{"/a", "/a/b", false},
		{"/elsewhere", "/a/b", false},
	}
	for _, c := range cases {
		if got := isWithinDir(c.path, c.dir); got != c.want {
			t.Errorf("isWithinDir(%q,%q) = %v, want %v", c.path, c.dir, got, c.want)
		}
	}
}
