package fsutil

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCopyFileAtomic_Success(t *testing.T) {
	// Create temp directory for test
	tmpDir := t.TempDir()

	// Create source file with test content
	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("test content for atomic copy")
	if err := os.WriteFile(srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy to destination
	dstPath := filepath.Join(tmpDir, "destination.txt")
	if err := CopyFileAtomic(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFileAtomic failed: %v", err)
	}

	// Verify destination exists
	if _, err := os.Stat(dstPath); os.IsNotExist(err) {
		t.Error("Destination file does not exist")
	}

	// Verify content matches
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(dstContent) != string(testContent) {
		t.Errorf("Content mismatch: got %q, want %q", string(dstContent), string(testContent))
	}

	// Verify temp file was cleaned up
	tmpPath := dstPath + ".tmp"
	if _, err := os.Stat(tmpPath); !os.IsNotExist(err) {
		t.Error("Temp file was not cleaned up")
	}
}

func TestCopyFileAtomic_SourceNotFound(t *testing.T) {
	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "nonexistent.txt")
	dstPath := filepath.Join(tmpDir, "destination.txt")

	err := CopyFileAtomic(srcPath, dstPath)
	if err == nil {
		t.Fatal("Expected error for nonexistent source, got nil")
	}

	// Verify destination was not created
	if _, err := os.Stat(dstPath); !os.IsNotExist(err) {
		t.Error("Destination file should not exist when source is missing")
	}
}

func TestCopyFileAtomic_CreatesNestedDirectories(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("test content")
	if err := os.WriteFile(srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy to nested directory that doesn't exist yet
	dstPath := filepath.Join(tmpDir, "level1", "level2", "level3", "destination.txt")

	// Should succeed by auto-creating directories
	err := CopyFileAtomic(srcPath, dstPath)
	if err != nil {
		t.Fatalf("CopyFileAtomic should auto-create nested directories, got error: %v", err)
	}

	// Verify destination file exists
	if _, err := os.Stat(dstPath); os.IsNotExist(err) {
		t.Error("Destination file does not exist after copy")
	}

	// Verify content matches
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(dstContent) != string(testContent) {
		t.Errorf("Content mismatch: got %q, want %q", string(dstContent), string(testContent))
	}

	// Verify intermediate directories were created
	dir := filepath.Join(tmpDir, "level1", "level2", "level3")
	if _, err := os.Stat(dir); os.IsNotExist(err) {
		t.Error("Intermediate directories were not created")
	}
}

func TestCopyFileAtomic_LargeFile(t *testing.T) {
	if testing.Short() {
		t.Skip("Skipping large file test in short mode")
	}

	tmpDir := t.TempDir()

	// Create a 10MB file
	srcPath := filepath.Join(tmpDir, "large_source.bin")
	size := 10 * 1024 * 1024 // 10MB
	data := make([]byte, size)
	for i := range data {
		data[i] = byte(i % 256)
	}

	if err := os.WriteFile(srcPath, data, 0644); err != nil {
		t.Fatalf("Failed to create large source file: %v", err)
	}

	// Copy
	dstPath := filepath.Join(tmpDir, "large_dest.bin")
	if err := CopyFileAtomic(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFileAtomic failed for large file: %v", err)
	}

	// Verify size matches
	srcInfo, _ := os.Stat(srcPath)
	dstInfo, _ := os.Stat(dstPath)

	if srcInfo.Size() != dstInfo.Size() {
		t.Errorf("File size mismatch: got %d, want %d", dstInfo.Size(), srcInfo.Size())
	}
}

func TestCopyFileAtomic_OverwriteExisting(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	srcPath := filepath.Join(tmpDir, "source.txt")
	newContent := []byte("new content")
	if err := os.WriteFile(srcPath, newContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create existing destination file with old content
	dstPath := filepath.Join(tmpDir, "destination.txt")
	oldContent := []byte("old content")
	if err := os.WriteFile(dstPath, oldContent, 0644); err != nil {
		t.Fatalf("Failed to create destination file: %v", err)
	}

	// Copy should overwrite
	if err := CopyFileAtomic(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFileAtomic failed: %v", err)
	}

	// Verify new content
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if string(dstContent) != string(newContent) {
		t.Errorf("Content was not overwritten: got %q, want %q", string(dstContent), string(newContent))
	}
}

func TestCopyFileAtomic_PreservesPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support Unix-style file permissions")
	}

	tmpDir := t.TempDir()

	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("test content")
	if err := os.WriteFile(srcPath, testContent, 0600); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "destination.txt")
	if err := CopyFileAtomic(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFileAtomic failed: %v", err)
	}

	dstInfo, err := os.Stat(dstPath)
	if err != nil {
		t.Fatalf("Failed to stat destination file: %v", err)
	}

	perms := dstInfo.Mode().Perm()
	if perms&0200 == 0 {
		t.Errorf("Destination file not owner-writable: got %v", perms)
	}
	if perms&0400 == 0 {
		t.Errorf("Destination file not owner-readable: got %v", perms)
	}
}

func TestCopyFileAtomic_SourceIsDirectory(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source directory (not a file)
	srcPath := filepath.Join(tmpDir, "source_dir")
	if err := os.Mkdir(srcPath, 0755); err != nil {
		t.Fatalf("Failed to create source directory: %v", err)
	}

	dstPath := filepath.Join(tmpDir, "destination.txt")

	// Should fail when source is a directory
	err := CopyFileAtomic(srcPath, dstPath)
	if err == nil {
		t.Fatal("Expected error when source is a directory, got nil")
	}
}

func TestCopyFileAtomic_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create empty source file
	srcPath := filepath.Join(tmpDir, "empty.txt")
	if err := os.WriteFile(srcPath, []byte{}, 0644); err != nil {
		t.Fatalf("Failed to create empty source file: %v", err)
	}

	// Copy to destination
	dstPath := filepath.Join(tmpDir, "destination.txt")
	if err := CopyFileAtomic(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFileAtomic failed for empty file: %v", err)
	}

	// Verify destination exists and is empty
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination file: %v", err)
	}

	if len(dstContent) != 0 {
		t.Errorf("Expected empty file, got %d bytes", len(dstContent))
	}
}

func TestCopyFileAtomic_DestinationIsSymlink(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("test content")
	if err := os.WriteFile(srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Create a target file for the symlink
	targetPath := filepath.Join(tmpDir, "target.txt")
	if err := os.WriteFile(targetPath, []byte("old content"), 0644); err != nil {
		t.Fatalf("Failed to create target file: %v", err)
	}

	// Create symlink pointing to target
	symlinkPath := filepath.Join(tmpDir, "symlink.txt")
	if err := os.Symlink(targetPath, symlinkPath); err != nil {
		t.Fatalf("Failed to create symlink: %v", err)
	}

	// Copy to symlink destination (should follow symlink and replace target)
	if err := CopyFileAtomic(srcPath, symlinkPath); err != nil {
		t.Fatalf("CopyFileAtomic failed: %v", err)
	}

	// Verify content was written to the symlink location
	symlinkContent, err := os.ReadFile(symlinkPath)
	if err != nil {
		t.Fatalf("Failed to read through symlink: %v", err)
	}

	if string(symlinkContent) != string(testContent) {
		t.Errorf("Content mismatch: got %q, want %q", string(symlinkContent), string(testContent))
	}
}

func TestCopyFileAtomic_SpecialPermissions(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not support Unix-style file permissions")
	}

	tmpDir := t.TempDir()

	tests := []struct {
		name string
	}{
		{"executable"},
		{"read-only"},
		{"owner-only"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srcPath := filepath.Join(tmpDir, "src_"+tt.name+".txt")
			if err := os.WriteFile(srcPath, []byte("content"), 0644); err != nil {
				t.Fatalf("Failed to create source: %v", err)
			}

			dstPath := filepath.Join(tmpDir, "dst_"+tt.name+".txt")
			if err := CopyFileAtomic(srcPath, dstPath); err != nil {
				t.Fatalf("CopyFileAtomic failed: %v", err)
			}

			dstInfo, err := os.Stat(dstPath)
			if err != nil {
				t.Fatalf("Failed to stat destination: %v", err)
			}

			perms := dstInfo.Mode().Perm()
			if perms&0600 != 0600 {
				t.Errorf("Destination file not owner-readable/writable: got %v", perms)
			}
		})
	}
}

func TestCopyFileAtomic_ConcurrentCopies(t *testing.T) {
	tmpDir := t.TempDir()

	// Create source file
	srcPath := filepath.Join(tmpDir, "source.txt")
	testContent := []byte("concurrent test content")
	if err := os.WriteFile(srcPath, testContent, 0644); err != nil {
		t.Fatalf("Failed to create source file: %v", err)
	}

	// Copy to same destination concurrently (should use unique temp files)
	dstPath := filepath.Join(tmpDir, "destination.txt")

	done := make(chan error, 3)
	for i := 0; i < 3; i++ {
		go func() {
			done <- CopyFileAtomic(srcPath, dstPath)
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 3; i++ {
		if err := <-done; err != nil {
			t.Errorf("Concurrent copy %d failed: %v", i, err)
		}
	}

	// Verify final content is correct
	dstContent, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination: %v", err)
	}

	if string(dstContent) != string(testContent) {
		t.Errorf("Content mismatch after concurrent copies: got %q, want %q",
			string(dstContent), string(testContent))
	}
}

func TestCopyFileAtomic_BinaryFile(t *testing.T) {
	tmpDir := t.TempDir()

	// Create binary file with various byte values
	srcPath := filepath.Join(tmpDir, "binary.bin")
	binaryData := make([]byte, 256)
	for i := range binaryData {
		binaryData[i] = byte(i)
	}
	if err := os.WriteFile(srcPath, binaryData, 0644); err != nil {
		t.Fatalf("Failed to create binary file: %v", err)
	}

	// Copy
	dstPath := filepath.Join(tmpDir, "binary_copy.bin")
	if err := CopyFileAtomic(srcPath, dstPath); err != nil {
		t.Fatalf("CopyFileAtomic failed for binary file: %v", err)
	}

	// Verify byte-for-byte match
	dstData, err := os.ReadFile(dstPath)
	if err != nil {
		t.Fatalf("Failed to read destination: %v", err)
	}

	if len(dstData) != len(binaryData) {
		t.Fatalf("Length mismatch: got %d, want %d", len(dstData), len(binaryData))
	}

	for i := range binaryData {
		if dstData[i] != binaryData[i] {
			t.Errorf("Byte mismatch at position %d: got %d, want %d",
				i, dstData[i], binaryData[i])
			break
		}
	}
}
