package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/spf13/afero"
	"github.com/stretchr/testify/assert"
)

func TestMoveFile_SameDevice(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("move test content")
	if err := os.WriteFile(srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "destination.txt")
	if err := MoveFile(srcPath, dstPath); err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Error("Source file should not exist after move")
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	assert.Equal(t, testContent, dstContent)
}

func TestMoveFile_ToSubdirectory(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("move to subdir content")
	if err := os.WriteFile(srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "subdir", "destination.txt")
	if err := MoveFile(srcPath, dstPath); err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Error("Source file should not exist after move")
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	assert.Equal(t, testContent, dstContent)
}

func TestMoveFile_SourceNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "nonexistent.txt")
	dstPath := filepath.Join(tmpDir, "destination.txt")

	err := MoveFile(srcPath, dstPath)
	assert.Error(t, err)
}

func TestMoveFile_OverwriteExisting(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "source.txt")
	newContent := []byte("new content")
	if err := os.WriteFile(srcPath, newContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "destination.txt")
	oldContent := []byte("old content")
	if err := os.WriteFile(dstPath, oldContent, 0644); err != nil {
		t.Fatalf("Failed to create destination file: %v", err)
	}

	if err := MoveFile(srcPath, dstPath); err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	assert.Equal(t, newContent, dstContent)
}

func TestMoveFile_PreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support Unix-style file permissions")
	}

	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("permissions test")
	if err := os.WriteFile(srcPath, testContent, 0755); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "subdir", "destination.txt")
	if err := MoveFile(srcPath, dstPath); err != nil {
		t.Fatalf("MoveFile failed: %v", err)
	}

	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("Failed to stat destination: %v", err)
	}
	assert.Equal(t, os.FileMode(0755), dstInfo.Mode().Perm())
}

func TestMoveFileFs_SameDevice(t *testing.T) {
	fs := afero.NewOsFs()
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("afero move test")
	if err := afero.WriteFile(fs, srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "subdir", "destination.txt")
	if err := MoveFileFs(fs, srcPath, dstPath); err != nil {
		t.Fatalf("MoveFileFs failed: %v", err)
	}

	exists, _ := afero.Exists(fs, srcPath)
	assert.False(t, exists, "Source file should not exist after move")

	dstContent, err := afero.ReadFile(fs, dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}
	assert.Equal(t, testContent, dstContent)
}

func TestMoveFileFs_SourceNotFound(t *testing.T) {
	fs := afero.NewOsFs()
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "nonexistent.txt")
	dstPath := filepath.Join(tmpDir, "destination.txt")

	err := MoveFileFs(fs, srcPath, dstPath)
	assert.Error(t, err)
}

func TestMoveFileFs_EmptyFile(t *testing.T) {
	fs := afero.NewOsFs()
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "empty.txt")
	if err := afero.WriteFile(fs, srcPath, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create empty source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "subdir", "destination.txt")
	if err := MoveFileFs(fs, srcPath, dstPath); err != nil {
		t.Fatalf("MoveFileFs failed: %v", err)
	}

	dstContent, err := afero.ReadFile(fs, dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination: %v", err)
	}
	assert.Empty(t, dstContent)
}

func TestIsCrossDeviceError(t *testing.T) {
	testCases := []struct {
		name      string
		err       error
		wantCross bool
	}{
		{"nil_error", nil, false},
		{"generic_error", os.ErrNotExist, false},
	}
	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.Equal(t, tc.wantCross, isCrossDeviceError(tc.err))
		})
	}
}

func TestCopyFileDataFs_Basic(t *testing.T) {
	fs := afero.NewOsFs()
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("copy data fs test")
	if err := afero.WriteFile(fs, srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "destination.txt")
	if err := copyFileDataFs(fs, srcPath, dstPath); err != nil {
		t.Fatalf("copyFileDataFs failed: %v", err)
	}

	dstContent, err := afero.ReadFile(fs, dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination: %v", err)
	}
	assert.Equal(t, testContent, dstContent)
}

func TestCrossDeviceMove_Success(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("cross-device content")
	if err := os.WriteFile(srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "destination.txt")
	if err := crossDeviceMove(srcPath, dstPath); err != nil {
		t.Fatalf("crossDeviceMove failed: %v", err)
	}

	if _, err := os.Stat(srcPath); !os.IsNotExist(err) {
		t.Error("Source file should not exist after cross-device move")
	}

	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination: %v", err)
	}
	assert.Equal(t, testContent, dstContent)
}

func TestCrossDeviceMove_SourceRemovalFails(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "source.txt")
	if err := os.WriteFile(srcPath, []byte("data"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "destination.txt")
	if err := crossDeviceMove(srcPath, dstPath); err != nil {
		t.Fatalf("crossDeviceMove should succeed on same device: %v", err)
	}
}

func TestCrossDeviceMoveFs_Success(t *testing.T) {
	fs := afero.NewMemMapFs()
	tmpDir := "/tmp"

	srcPath := filepath.Join(tmpDir, "source.txt")
	if err := afero.WriteFile(fs, srcPath, []byte("memfs cross-device"), 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "destination.txt")
	if err := crossDeviceMoveFs(fs, srcPath, dstPath); err != nil {
		t.Fatalf("crossDeviceMoveFs failed: %v", err)
	}

	exists, _ := afero.Exists(fs, srcPath)
	assert.False(t, exists, "Source file should not exist after cross-device move")

	dstContent, err := afero.ReadFile(fs, dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination: %v", err)
	}
	assert.Equal(t, []byte("memfs cross-device"), dstContent)
}

func TestCrossDeviceMoveFs_SourceRemovalFails(t *testing.T) {
	fs := afero.NewReadOnlyFs(afero.NewMemMapFs())

	err := crossDeviceMoveFs(fs, "/tmp/source.txt", "/tmp/destination.txt")
	assert.Error(t, err)
}

func TestCrossDeviceMove_CopyFails(t *testing.T) {
	err := crossDeviceMove("/nonexistent/source.txt", "/tmp/destination.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy file across devices")
}

func TestCrossDeviceMoveFs_CopyFails(t *testing.T) {
	fs := afero.NewMemMapFs()
	err := crossDeviceMoveFs(fs, "/nonexistent/source.txt", "/tmp/destination.txt")
	assert.Error(t, err)
	assert.Contains(t, err.Error(), "failed to copy file across devices")
}
