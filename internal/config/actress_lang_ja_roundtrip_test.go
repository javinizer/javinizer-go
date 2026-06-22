package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOutputConfig_ActressLanguageJA_RoundTrip(t *testing.T) {
	dir := t.TempDir()
	// config_version: 3 avoids the legacy "wipe + regenerate" migration path.
	// In the refactored config layout, actress_language_ja lives under
	// metadata.nfo.format (it was relocated from output.* by the refactor).
	path := filepath.Join(dir, "config.yaml")
	yaml := "config_version: 3\nmetadata:\n  nfo:\n    actress_language_ja: true\noutput:\n  first_name_order: true\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if !cfg.Metadata.NFO.Format.ActressLanguageJA {
		t.Errorf("ActressLanguageJA = false, want true")
	}
	if !cfg.Output.Template.FirstNameOrder {
		t.Errorf("FirstNameOrder = false, want true (control case)")
	}

	// Default value when unspecified
	path2 := filepath.Join(dir, "config2.yaml")
	_ = os.WriteFile(path2, []byte("config_version: 3\noutput:\n  enabled: false\n"), 0o644)
	cfg2, err := Load(path2)
	if err != nil {
		t.Fatalf("Load path2: %v", err)
	}
	if cfg2.Metadata.NFO.Format.ActressLanguageJA {
		t.Errorf("ActressLanguageJA default = true, want false")
	}
}

// TestOutputConfig_LegacyDelimiterShim verifies that configs using the
// pre-rename `output.delimiter` key still carry its value into
// `ActressDelimiter` during Normalize, so users who set
// `delimiter: ' | '` before the rename don't silently lose it.
func TestOutputConfig_LegacyDelimiterShim(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	yaml := "config_version: 3\noutput:\n  delimiter: ' | '\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Output.Template.ActressDelimiter != " | " {
		t.Errorf("expected legacy 'delimiter: | ' to carry over to ActressDelimiter; got %q", cfg.Output.Template.ActressDelimiter)
	}
}

// TestOutputConfig_LegacyDelimiterDoesNotClobber verifies the shim doesn't
// clobber an explicitly-set actress_delimiter that happens to equal the
// default. Edge case worth covering since the shim has to be conservative.
func TestOutputConfig_LegacyDelimiterDoesNotClobber(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "config.yaml")
	// User has BOTH keys set and they differ — explicit actress_delimiter
	// should win (no surprise carryover).
	yaml := "config_version: 3\noutput:\n  delimiter: 'legacy-value'\n  actress_delimiter: 'explicit-value'\n"
	if err := os.WriteFile(path, []byte(yaml), 0o644); err != nil {
		t.Fatalf("write: %v", err)
	}
	cfg, err := LoadOrCreate(path)
	if err != nil {
		t.Fatalf("LoadOrCreate: %v", err)
	}
	if cfg.Output.Template.ActressDelimiter != "explicit-value" {
		t.Errorf("expected explicit actress_delimiter to win, got %q", cfg.Output.Template.ActressDelimiter)
	}
}
