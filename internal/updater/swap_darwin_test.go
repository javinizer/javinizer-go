//go:build darwin

package updater

import (
	"archive/zip"
	"context"
	"fmt"
	"os"
	"os/exec"
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

// --- Coverage-targeted tests for the remaining swap_darwin.go error paths ---

// withExecutableFuncErr injects a failing executableFunc seam for the duration
// of t. Covers the Target() error branch (executableFunc err) without relying
// on os.Executable actually failing.
func withExecutableFuncErr(t *testing.T) {
	t.Helper()
	prev := executableFunc
	executableFunc = func() (string, error) {
		return "", fmt.Errorf("synthetic os.Executable failure")
	}
	t.Cleanup(func() { executableFunc = prev })
}

func TestDarwinSwapper_Target_ExecutableError(t *testing.T) {
	withExecutableFuncErr(t)
	if _, err := NewDarwinSwapper().Target(); err == nil {
		t.Fatal("expected Target to fail when executableFunc errors")
	}
}

// TestDarwinSwapper_CanSwap_TargetError covers the CanSwap error branch that
// propagates a Target() failure (executableFunc err) before unix.Access runs.
func TestDarwinSwapper_CanSwap_TargetError(t *testing.T) {
	withExecutableFuncErr(t)
	if err := NewDarwinSwapper().CanSwap(); err == nil {
		t.Fatal("expected CanSwap to fail when Target fails")
	}
}

// TestDarwinSwapper_Stage_TargetError covers the Stage error branch that
// propagates a Target() failure (executableFunc err) before staging runs.
func TestDarwinSwapper_Stage_TargetError(t *testing.T) {
	dir := t.TempDir()
	zipPath := buildBundleZip(t, dir)
	withExecutableFuncErr(t)
	if _, err := NewDarwinSwapper().Stage(context.Background(), zipPath, "x.zip"); err == nil {
		t.Fatal("expected Stage to fail when Target fails")
	}
}

// TestDarwinSwapper_Stage_MkdirTempFailure covers the os.MkdirTemp failure
// branch by making the target's parent directory read-only so staging temp
// creation under it fails. Root bypasses UNIX perms, so skip under root.
func TestDarwinSwapper_Stage_MkdirTempFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	parent := t.TempDir()
	readonly := filepath.Join(parent, "apps")
	exe := buildFakeApp(t, readonly)
	// Make parent read-only so MkdirTemp under it fails.
	if err := os.Chmod(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })
	withExecutableFunc(t, exe)
	zipPath := filepath.Join(parent, "asset.zip")
	if err := os.WriteFile(zipPath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err := NewDarwinSwapper().Stage(context.Background(), zipPath, "x.zip")
	if err == nil {
		t.Fatal("expected Stage to fail when MkdirTemp fails")
	}
}

// TestDarwinSwapper_Stage_StagedMissing covers the staged-missing branch: a
// valid zip that does NOT contain Javinizer.app extracts fine but the post-
// extraction Stat fails the bundle-name check.
func TestDarwinSwapper_Stage_StagedMissing(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	// Build a zip with a different top-level entry name.
	zipPath := filepath.Join(dir, "wrong.zip")
	w, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	hdr := &zip.FileHeader{Name: "OtherApp/", Method: zip.Deflate}
	hdr.SetMode(0o755 | os.ModeDir)
	if _, err := zw.CreateHeader(hdr); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	w.Close()
	_, err = NewDarwinSwapper().Stage(context.Background(), zipPath, "x.zip")
	if err == nil {
		t.Fatal("expected Stage to fail when Javinizer.app missing after unzip")
	}
}

// TestDarwinSwapper_SwapAndRelaunch_StartError covers the cmd.Start failure
// branch by injecting a newSwapHelperCmd seam that returns a command pointing
// at a nonexistent interpreter, so Start fails with ENOENT.
func TestDarwinSwapper_SwapAndRelaunch_StartError(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	orig := newSwapHelperCmd
	newSwapHelperCmd = func(ctx context.Context, script string) *exec.Cmd {
		// Nonexistent interpreter path -> Start fails with ENOENT.
		return exec.CommandContext(ctx, "/this/interpreter/does/not/exist", "-c", script)
	}
	t.Cleanup(func() { newSwapHelperCmd = orig })
	if err := NewDarwinSwapper().SwapAndRelaunch(context.Background(), "/tmp/staged", 4242); err == nil {
		t.Fatal("expected cmd.Start error to be returned")
	}
}

// --- unzipTo / extractZip error-path coverage ---

// writeZip builds a zip at path with the given entries and returns its path.
type zipEntry struct {
	name string
	data string
	mode os.FileMode
}

func writeZip(t *testing.T, path string, entries []zipEntry) {
	t.Helper()
	w, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	zw := zip.NewWriter(w)
	for _, e := range entries {
		hdr := &zip.FileHeader{Name: e.name, Method: zip.Deflate}
		hdr.SetMode(e.mode)
		fw, err := zw.CreateHeader(hdr)
		if err != nil {
			t.Fatalf("create %s: %v", e.name, err)
		}
		if e.data != "" {
			if _, err := fw.Write([]byte(e.data)); err != nil {
				t.Fatalf("write %s: %v", e.name, err)
			}
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
}

// TestExtractZipFile_EscapeDestDir covers the isWithinDir guard: a zip entry
// whose name contains ".." resolves outside destDir and is rejected.
func TestExtractZipFile_EscapeDestDir(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "slip.zip")
	writeZip(t, zipPath, []zipEntry{
		{name: "../escape.txt", data: "evil", mode: 0o644},
	})
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected error for zip entry escaping dest dir")
	}
	if _, err := os.Stat(filepath.Join(dir, "escape.txt")); err == nil {
		t.Fatal("escape file must not be created outside dest dir")
	}
}

// TestUnzipTo_OpenReaderFailure covers zip.OpenReader failing on a path that
// does not exist.
func TestUnzipTo_OpenReaderFailure(t *testing.T) {
	dest := t.TempDir()
	if err := unzipTo(filepath.Join(dest, "nope.zip"), dest); err == nil {
		t.Fatal("expected error opening nonexistent zip")
	}
}

// TestUnzipTo_ExtractFailure propagates an extractZipFile error (escape guard)
// through the unzipTo loop's return-err branch (line 185-187).
func TestUnzipTo_ExtractFailure(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "slip.zip")
	writeZip(t, zipPath, []zipEntry{
		{name: "../escape.txt", data: "evil", mode: 0o644},
	})
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected unzipTo to surface extractZipFile error")
	}
}

// TestExtractZipFile_DirMkdirAllFailure covers the directory-entry MkdirAll
// failure branch by making destDir's parent read-only so MkdirAll on a nested
// entry fails. Root bypasses perms, so skip under root.
func TestExtractZipFile_DirMkdirAllFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	parent := t.TempDir()
	readonly := filepath.Join(parent, "ro")
	if err := os.MkdirAll(readonly, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(readonly, "out")
	// Build zip with a nested directory entry; MkdirAll under read-only dest fails.
	zipPath := filepath.Join(parent, "dirs.zip")
	writeZip(t, zipPath, []zipEntry{
		{name: "a/b/c/", data: "", mode: 0o755 | os.ModeDir},
	})
	if err := os.Chmod(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected MkdirAll failure for directory entry under read-only parent")
	}
}

// TestExtractZipRegular_MkdirAllFailure covers extractZipRegular's MkdirAll
// (filepath.Dir(name)) failure under a read-only parent. Skip under root.
func TestExtractZipRegular_MkdirAllFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	parent := t.TempDir()
	readonly := filepath.Join(parent, "ro")
	if err := os.MkdirAll(readonly, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(readonly, "out")
	zipPath := filepath.Join(parent, "reg.zip")
	writeZip(t, zipPath, []zipEntry{
		{name: "sub/file.txt", data: "x", mode: 0o644},
	})
	if err := os.Chmod(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected MkdirAll failure for regular entry under read-only parent")
	}
}

// TestExtractZipRegular_OpenFileFailure covers os.OpenFile failure on a
// regular entry by pre-creating a directory at the entry's target path, so
// O_CREATE|O_WRONLY on a path occupied by a directory fails (EISDIR).
func TestExtractZipRegular_OpenFileFailure(t *testing.T) {
	dir := t.TempDir()
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	// Pre-create a directory where the regular file would land; OpenFile with
	// O_WRONLY|O_CREATE|O_TRUNC on an existing directory fails with EISDIR.
	if err := os.MkdirAll(filepath.Join(dest, "file.txt"), 0o755); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(dir, "reg.zip")
	writeZip(t, zipPath, []zipEntry{
		{name: "file.txt", data: "x", mode: 0o644},
	})
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected OpenFile failure when target path is a directory")
	}
}

// TestExtractZipRegular_IOCopyFailure covers the io.Copy error branch by
// crafting a Deflate entry with a corrupted compressed body so the flate
// reader returns a decompression error mid-copy.
func TestExtractZipRegular_IOCopyFailure(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "corrupt.zip")
	w, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	hdr := &zip.FileHeader{Name: "big.txt", Method: zip.Deflate}
	hdr.SetMode(0o644)
	fw, err := zw.CreateHeader(hdr)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fw.Write([]byte("hello world payload")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	// Corrupt the compressed payload bytes (after the local file header).
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 35; i < len(data)-22 && i < 45; i++ {
		data[i] = 0xFF
	}
	if err := os.WriteFile(zipPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected io.Copy decompression error to be surfaced")
	}
}

// corruptLocalHeader writes a zip with one entry then flips a byte in the
// local file header so the entry opens (OpenReader reads the central
// directory) but f.Open fails reading the local header (zip: not a valid zip
// file). Used to cover the f.Open error branches in extractZipRegular and
// extractZipSymlink.
func corruptLocalHeader(t *testing.T, path string) {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	// Local file header starts at offset 0 with 'PK\x03\x04'; corrupt the
	// 3rd signature byte so findBodyOffset rejects the local header.
	data[2] = 0x00
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestExtractZipRegular_CloseFailure covers the defensive out.Close()
// error branch (line 233-235) by injecting a closeZipEntry seam that always
// fails. The default close on a just-flushed regular file in a temp dir does
// not error, so the seam is the cleanest deterministic trigger (mirrors
// closeTempFile in engine.go).
func TestExtractZipRegular_CloseFailure(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "ok.zip")
	writeZip(t, zipPath, []zipEntry{
		{name: "file.txt", data: "hello", mode: 0o644},
	})
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := closeZipEntry
	closeZipEntry = func(f *os.File) error {
		_ = f.Close()
		return fmt.Errorf("synthetic close failure")
	}
	t.Cleanup(func() { closeZipEntry = orig })
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected close error to be surfaced")
	}
}

// TestExtractZipRegular_ChmodFailure covers the defensive os.Chmod error
// branch (line 236-238) by injecting a chmodZipEntry seam that always fails.
// chmod on a just-created file in a writable temp dir does not error, so the
// seam is the cleanest deterministic trigger.
func TestExtractZipRegular_ChmodFailure(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "ok.zip")
	writeZip(t, zipPath, []zipEntry{
		{name: "file.txt", data: "hello", mode: 0o644},
	})
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	orig := chmodZipEntry
	chmodZipEntry = func(name string, mode os.FileMode) error {
		return fmt.Errorf("synthetic chmod failure")
	}
	t.Cleanup(func() { chmodZipEntry = orig })
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected chmod error to be surfaced")
	}
}

// TestExtractZipRegular_OpenEntryFailure covers the f.Open error branch
// (line 220-223) by corrupting the local file header so the entry cannot be
// opened, while the central directory (read by OpenReader) is still intact.
func TestExtractZipRegular_OpenEntryFailure(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "badhdr.zip")
	writeZip(t, zipPath, []zipEntry{
		{name: "file.txt", data: "hello", mode: 0o644},
	})
	corruptLocalHeader(t, zipPath)
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected f.Open error for corrupted local header")
	}
}

// TestExtractZipSymlink_OpenEntryFailure covers the f.Open error branch in
// extractZipSymlink (line 243-245) via a corrupted local header.
func TestExtractZipSymlink_OpenEntryFailure(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "symlink-bad.zip")
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
	corruptLocalHeader(t, zipPath)
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected f.Open error for symlink with corrupted local header")
	}
}

// TestExtractZipSymlink_ReadAllFailure covers the io.ReadAll error branch
// (line 249-251) by corrupting the compressed body of a Deflate symlink entry
// so the flate reader returns a decompression error mid-read.
func TestExtractZipSymlink_ReadAllFailure(t *testing.T) {
	dir := t.TempDir()
	zipPath := filepath.Join(dir, "sym-corrupt.zip")
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
	if _, err := fw.Write([]byte("a-longish-symlink-target-path")); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	w.Close()
	data, err := os.ReadFile(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	for i := 35; i < len(data)-22 && i < 45; i++ {
		data[i] = 0xFF
	}
	if err := os.WriteFile(zipPath, data, 0o644); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(dir, "out")
	if err := os.MkdirAll(dest, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected io.ReadAll decompression error for symlink entry")
	}
}

// TestNewSwapHelperCmd_Default covers the default (non-injected) body of the
// newSwapHelperCmd seam by invoking it directly. The command is not started,
// so no real helper is spawned; we only assert it builds a non-nil Cmd with
// the configured SysProcAttr.
func TestNewSwapHelperCmd_Default(t *testing.T) {
	cmd := newSwapHelperCmd(context.Background(), "true")
	if cmd == nil {
		t.Fatal("newSwapHelperCmd returned nil")
	}
	if cmd.Path == "" {
		t.Fatal("cmd.Path empty")
	}
	if cmd.SysProcAttr == nil {
		t.Fatal("SysProcAttr not set on default cmd")
	}
}

// TestDarwinSwapper_SwapAndRelaunch_HappyPath covers the cmd.Start success
// branch (line 126 `return nil`) by injecting a newSwapHelperCmd seam that
// runs a trivial, immediately-exiting command (`true`). The detached helper
// exits before the test ends, leaving no lingering process.
func TestDarwinSwapper_SwapAndRelaunch_HappyPath(t *testing.T) {
	dir := t.TempDir()
	exe := buildFakeApp(t, dir)
	withExecutableFunc(t, exe)
	orig := newSwapHelperCmd
	newSwapHelperCmd = func(ctx context.Context, script string) *exec.Cmd {
		return exec.CommandContext(ctx, "true")
	}
	t.Cleanup(func() { newSwapHelperCmd = orig })
	if err := NewDarwinSwapper().SwapAndRelaunch(context.Background(), "/tmp/staged", 4242); err != nil {
		t.Fatalf("expected success on cmd.Start, got %v", err)
	}
}

// TestExtractZipSymlink_MkdirAllFailure covers the symlink branch's MkdirAll
// (filepath.Dir(name)) failure under a read-only parent. Skip under root.
func TestExtractZipSymlink_MkdirAllFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root bypasses permission checks")
	}
	parent := t.TempDir()
	readonly := filepath.Join(parent, "ro")
	if err := os.MkdirAll(readonly, 0o755); err != nil {
		t.Fatal(err)
	}
	dest := filepath.Join(readonly, "out")
	zipPath := filepath.Join(parent, "sym.zip")
	w, err := os.Create(zipPath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(w)
	hdr := &zip.FileHeader{Name: "sub/link", Method: zip.Deflate}
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
	if err := os.Chmod(readonly, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(readonly, 0o755) })
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected MkdirAll failure for symlink entry under read-only parent")
	}
}

// TestExtractZipSymlink_SymlinkFailure covers os.Symlink failure by making the
// symlink entry's name collide with an existing directory (Symlink over a
// dir fails with EEXIST/EISDIR on darwin). The os.Remove(name) in the function
// only removes a file/symlink, not a non-empty dir, so Symlink fails.
func TestExtractZipSymlink_SymlinkFailure(t *testing.T) {
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
	// Pre-create a non-empty directory at the symlink target path; os.Remove
	// fails on a non-empty dir, then os.Symlink fails because the path exists.
	linkPath := filepath.Join(dest, "link")
	if err := os.MkdirAll(linkPath, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(linkPath, "blocker"), []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := unzipTo(zipPath, dest); err == nil {
		t.Fatal("expected os.Symlink failure when target path is a non-empty dir")
	}
}

// TestIsWithinDir_RelError covers the filepath.Rel error branch (line 257-259).
// filepath.Rel returns an error when path and dir are on different volumes on
// darwin (e.g. /Volumes/A vs /Volumes/B), which is unreachable from a single
// temp dir. We instead call isWithinDir with an empty dir to force a Rel
// error indirectly — but filepath.Rel("", "/a") does not error. The Rel-error
// branch is therefore defensive: archive/zip always joins destDir with f.Name,
// so path is always under a cleanable prefix of dir; Rel only errors on
// cross-volume inputs, which a single destDir cannot produce. This test pins
// the happy-path contract (no panic) for the documented-unreachable branch.
func TestIsWithinDir_NoPanicOnDegenerate(t *testing.T) {
	// Empty dir + absolute path: filepath.Rel returns a non-error relative.
	_ = isWithinDir("/a/b", "")
	// Cross-volume-style inputs that DO error: Rel can error when one path is
	// absolute and the other is relative — exercise to confirm false is returned
	// (not a panic) on the Rel-error path.
	if got := isWithinDir("rel", "/abs"); got != false {
		t.Errorf("isWithinDir(rel, abs) = %v, want false", got)
	}
}
