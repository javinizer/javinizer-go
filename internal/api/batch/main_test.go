package batch

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestMain(m *testing.M) {
	os.Exit(runBatchTests(m))
}

func runBatchTests(m *testing.M) (code int) {
	originalDir, err := os.Getwd()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	workDir, err := os.MkdirTemp("", "javinizer-batch-tests-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		if err := os.RemoveAll(workDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}()
	if err := os.Chdir(workDir); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	defer func() {
		if err := os.Chdir(originalDir); err != nil {
			fmt.Fprintln(os.Stderr, err)
			code = 1
		}
	}()
	return m.Run()
}

func TestBatchWorkingDirectoryIsolation(t *testing.T) {
	const receiptEnv = "JAVINIZER_BATCH_TEST_WORKDIR_RECEIPT"
	if receipt := os.Getenv(receiptEnv); receipt != "" {
		if _, err := os.Stat("stale-fixture"); !os.IsNotExist(err) {
			t.Fatalf("inherited stale fixture: %v", err)
		}
		cwd, err := os.Getwd()
		if err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(receipt, []byte(cwd), 0o600); err != nil {
			t.Fatal(err)
		}
		if err := os.MkdirAll("data/temp/posters/leaked-job", 0o755); err != nil {
			t.Fatal(err)
		}
		return
	}

	parentDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(parentDir, "stale-fixture"), []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	receipt := filepath.Join(parentDir, "receipt")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(binary, "-test.run=^TestBatchWorkingDirectoryIsolation$")
	cmd.Dir = parentDir
	cmd.Env = append(os.Environ(), receiptEnv+"="+receipt)
	if output, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("isolated subprocess failed: %v\n%s", err, output)
	}
	cwd, err := os.ReadFile(receipt)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(string(cwd)); !os.IsNotExist(err) {
		t.Fatalf("test working directory was not cleaned up: %s: %v", cwd, err)
	}
	if data, err := os.ReadFile(filepath.Join(parentDir, "stale-fixture")); err != nil || string(data) != "preserve" {
		t.Fatalf("original fixtures changed: %q: %v", data, err)
	}
}
