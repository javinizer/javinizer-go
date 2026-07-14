package config_test

import (
	"encoding/json"
	"strings"
	"testing"

	config "github.com/javinizer/javinizer-go/internal/config"
	"gopkg.in/yaml.v3"
)

func TestUIConfig_DefaultLanguage(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	if cfg.UI.Language != "auto" {
		t.Errorf("DefaultConfig UI.Language = %q, want %q", cfg.UI.Language, "auto")
	}
}

func TestUIConfig_YAMLRoundTrip(t *testing.T) {
	in := "ui:\n    language: ja\n"
	var cfg config.Config
	if err := yaml.Unmarshal([]byte(in), &cfg); err != nil {
		t.Fatalf("yaml.Unmarshal failed: %v", err)
	}
	if cfg.UI.Language != "ja" {
		t.Errorf("UI.Language = %q, want %q", cfg.UI.Language, "ja")
	}

	out, err := yaml.Marshal(&cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal failed: %v", err)
	}
	if !strings.Contains(string(out), "ui:") {
		t.Errorf("YAML output missing ui block: %s", string(out))
	}
	if !strings.Contains(string(out), "language: ja") {
		t.Errorf("YAML output missing language: %s", string(out))
	}
}

func TestUIConfig_JSONRoundTrip(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.UI.Language = "zh-Hans"

	data, err := json.Marshal(cfg)
	if err != nil {
		t.Fatalf("json.Marshal failed: %v", err)
	}
	jsonStr := string(data)
	if !strings.Contains(jsonStr, `"ui"`) {
		t.Errorf("JSON output missing ui field: %s", jsonStr)
	}
	if !strings.Contains(jsonStr, `"language":"zh-Hans"`) {
		t.Errorf("JSON output missing language value: %s", jsonStr)
	}

	var reloaded config.Config
	if err := json.Unmarshal(data, &reloaded); err != nil {
		t.Fatalf("json.Unmarshal failed: %v", err)
	}
	if reloaded.UI.Language != "zh-Hans" {
		t.Errorf("reloaded UI.Language = %q, want %q", reloaded.UI.Language, "zh-Hans")
	}
}

// TestUIConfig_LegacyConfigNoUIBlock verifies an old config without a ui
// block loads cleanly. yaml.v3 leaves the field at its zero value (empty
// string); ValidateConfig treats empty as "auto".
func TestUIConfig_LegacyConfigNoUIBlock(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	// Simulate a legacy config that predates the ui block: clear the field.
	cfg.UI.Language = ""
	if err := cfg.Validate(); err != nil {
		t.Errorf("Validate rejected empty UI.Language (should be treated as auto): %v", err)
	}
}

func TestUIConfig_CloneIndependence(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	cfg.UI.Language = "pt-BR"
	cp := cfg.Clone()
	if cp.UI.Language != "pt-BR" {
		t.Errorf("clone UI.Language = %q, want %q", cp.UI.Language, "pt-BR")
	}
	cp.UI.Language = "en"
	if cfg.UI.Language != "pt-BR" {
		t.Errorf("original mutated after clone: UI.Language = %q, want %q", cfg.UI.Language, "pt-BR")
	}
}

func TestUIConfig_ValidateAcceptsValidBCP47(t *testing.T) {
	cases := []string{
		"auto", "AUTO", "Auto",
		"", // empty treated as auto
		"en", "ja", "zh-Hans", "zh-Hant", "pt-BR",
		"de-DE", "fr", "ko",
	}
	for _, c := range cases {
		cfg := config.DefaultConfig(nil, nil)
		cfg.UI.Language = c
		if err := cfg.Validate(); err != nil {
			t.Errorf("Validate rejected valid ui.language %q: %v", c, err)
		}
	}
}

func TestUIConfig_ValidateRejectsInvalid(t *testing.T) {
	cases := []string{
		"english", "zh_", "123", "en-US-x", "a b c",
	}
	for _, c := range cases {
		cfg := config.DefaultConfig(nil, nil)
		cfg.UI.Language = c
		if err := cfg.Validate(); err == nil {
			t.Errorf("Validate accepted invalid ui.language %q (expected error)", c)
		}
	}
}

func TestUIConfig_MarshalYAMLIncludesUI(t *testing.T) {
	cfg := config.DefaultConfig(nil, nil)
	out, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatalf("yaml.Marshal failed: %v", err)
	}
	if !strings.Contains(string(out), "ui:") {
		t.Errorf("MarshalYAML did not include ui block: %s", string(out))
	}
}
