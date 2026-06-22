package scanner

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/spf13/afero"
)

func TestScannerInterface(t *testing.T) {
	tmpDir := t.TempDir()
	for _, name := range []string{"movie1.mp4", "movie2.mkv", "ignore.txt"} {
		if err := os.WriteFile(filepath.Join(tmpDir, name), []byte("test"), 0o644); err != nil {
			t.Fatalf("create file: %v", err)
		}
	}

	ifaceCfg := &Config{Extensions: []string{".mp4", ".mkv"}}
	var iface ScannerInterface = NewScanner(afero.NewOsFs(), ifaceCfg)

	result, err := iface.Scan(tmpDir)
	if err != nil {
		t.Fatalf("Scan via interface failed: %v", err)
	}
	if len(result.Files) != 2 {
		t.Fatalf("expected 2 files from Scan, got %d", len(result.Files))
	}

	filtered, err := iface.ScanWithFilter(context.Background(), tmpDir, 1, "movie")
	if err != nil {
		t.Fatalf("ScanWithFilter via interface failed: %v", err)
	}
	if len(filtered.Files) != 1 {
		t.Fatalf("expected 1 file from ScanWithFilter, got %d", len(filtered.Files))
	}

	single, err := iface.ScanSingle(filepath.Join(tmpDir, "movie1.mp4"))
	if err != nil {
		t.Fatalf("ScanSingle via interface failed: %v", err)
	}
	if len(single.Files) != 1 {
		t.Fatalf("expected 1 file from ScanSingle, got %d", len(single.Files))
	}
}
