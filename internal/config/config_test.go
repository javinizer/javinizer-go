package config

import (
	"os"
	"testing"
)

func TestLoadNewConfigFormat(t *testing.T) {
	// Test with new config format
	newConfig := `
metadata:
  actress_database:
    enabled: true
    auto_add: false
  genre_replacement:
    enabled: true
    auto_add: true
`

	// Write test config
	tmpFile := "/tmp/test_new_config.yaml"
	if err := os.WriteFile(tmpFile, []byte(newConfig), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	defer os.Remove(tmpFile)

	// Load config
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify actress_database loaded correctly
	if !cfg.Metadata.ActressDatabase.Enabled {
		t.Error("ActressDatabase.Enabled should be true")
	}
	if cfg.Metadata.ActressDatabase.AutoAdd {
		t.Error("ActressDatabase.AutoAdd should be false")
	}

	// Verify genre_replacement loaded correctly
	if !cfg.Metadata.GenreReplacement.Enabled {
		t.Error("GenreReplacement.Enabled should be true")
	}
	if !cfg.Metadata.GenreReplacement.AutoAdd {
		t.Error("GenreReplacement.AutoAdd should be true")
	}
}

func TestLoadOldConfigFormatFails(t *testing.T) {
	// Test with old config format (should not work anymore)
	oldConfig := `
metadata:
  thumb_csv:
    enabled: true
    auto_add: false
  genre_csv:
    enabled: true
    auto_add: true
`

	// Write test config
	tmpFile := "/tmp/test_old_config_fails.yaml"
	if err := os.WriteFile(tmpFile, []byte(oldConfig), 0644); err != nil {
		t.Fatalf("Failed to write config: %v", err)
	}
	defer os.Remove(tmpFile)

	// Load config - old field names should be ignored, defaults used
	cfg, err := Load(tmpFile)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Old config fields should be ignored, so we get defaults
	// Default has both enabled=true and auto_add=true
	if !cfg.Metadata.ActressDatabase.Enabled {
		t.Error("ActressDatabase.Enabled should use default (true)")
	}
	if !cfg.Metadata.ActressDatabase.AutoAdd {
		t.Error("ActressDatabase.AutoAdd should use default (true)")
	}
	if !cfg.Metadata.GenreReplacement.Enabled {
		t.Error("GenreReplacement.Enabled should use default (true)")
	}
	if !cfg.Metadata.GenreReplacement.AutoAdd {
		t.Error("GenreReplacement.AutoAdd should use default (true)")
	}
}
