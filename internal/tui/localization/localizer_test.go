package localization

import (
	"strings"
	"testing"
	"testing/fstest"
)

// testCatalogFS builds an in-memory fs.FS with a locales/ directory containing
// the provided locale catalog files (name -> JSON content). It lets tests
// construct arbitrary catalogs without touching the embed.FS.
func testCatalogFS(t *testing.T, files map[string]string) fstest.MapFS {
	t.Helper()
	m := fstest.MapFS{}
	for name, content := range files {
		m["locales/"+name] = &fstest.MapFile{Data: []byte(content)}
	}
	return m
}

func TestLocalizer_ExactLocaleAndFallback(t *testing.T) {
	fs := testCatalogFS(t, map[string]string{
		"active.en.json": `{"TUISettingsTitle":{"other":"Settings"},"TUIFilesFound":{"one":"Found {{.Count}} file","other":"Found {{.Count}} files"}}`,
	})

	l, err := NewFromFS(fs, "en")
	if err != nil {
		t.Fatalf("NewFromFS failed: %v", err)
	}
	if got := l.Localize("TUISettingsTitle"); got != "Settings" {
		t.Errorf("TUISettingsTitle = %q, want %q", got, "Settings")
	}
}

func TestLocalizer_EnglishFallbackForUnsupportedLocale(t *testing.T) {
	fs := testCatalogFS(t, map[string]string{
		"active.en.json": `{"TUISettingsTitle":{"other":"Settings"}}`,
	})
	// ja requested but no ja catalog exists -> English fallback.
	l, err := NewFromFS(fs, "ja")
	if err != nil {
		t.Fatalf("NewFromFS failed: %v", err)
	}
	if got := l.Localize("TUISettingsTitle"); got != "Settings" {
		t.Errorf("unsupported locale TUISettingsTitle = %q, want English %q", got, "Settings")
	}
}

func TestLocalizer_NamedParameterSubstitution(t *testing.T) {
	fs := testCatalogFS(t, map[string]string{
		"active.en.json": `{"TUIFilesFound":{"one":"Found {{.Count}} file","other":"Found {{.Count}} files"}}`,
	})
	l, err := NewFromFS(fs, "en")
	if err != nil {
		t.Fatalf("NewFromFS failed: %v", err)
	}
	got := l.Localize("TUIFilesFound", map[string]any{"Count": 5})
	if !strings.Contains(got, "5") || !strings.Contains(got, "files") {
		t.Errorf("substitution = %q, want to contain 5 and 'files'", got)
	}
}

func TestLocalizer_PluralForms(t *testing.T) {
	fs := testCatalogFS(t, map[string]string{
		"active.en.json": `{"TUIFilesProcessed":{"one":"Processed {{.Count}} file in {{.Elapsed}}","other":"Processed {{.Count}} files in {{.Elapsed}}"}}`,
	})
	l, err := NewFromFS(fs, "en")
	if err != nil {
		t.Fatalf("NewFromFS failed: %v", err)
	}

	singular := l.Plural("TUIFilesProcessed", 1, map[string]any{"Elapsed": "2s"})
	if !strings.Contains(singular, "1 file") {
		t.Errorf("plural one = %q, want singular form", singular)
	}
	plural := l.Plural("TUIFilesProcessed", 3, map[string]any{"Elapsed": "2s"})
	if !strings.Contains(plural, "3 files") {
		t.Errorf("plural other = %q, want plural form", plural)
	}
}

func TestLocalizer_NeverPanicsOnMissingMessage(t *testing.T) {
	fs := testCatalogFS(t, map[string]string{
		"active.en.json": `{"TUISettingsTitle":{"other":"Settings"}}`,
	})
	l, err := NewFromFS(fs, "en")
	if err != nil {
		t.Fatalf("NewFromFS failed: %v", err)
	}
	// Should not panic; returns the ID or a best-effort render.
	got := l.Localize("NonexistentMessageID")
	if got == "" {
		t.Error("missing message returned empty string; expected the ID as fallback")
	}
	if got != "NonexistentMessageID" {
		t.Logf("missing message returned %q (acceptable non-empty fallback)", got)
	}
}

func TestLocalizer_PluralMissingNeverPanics(t *testing.T) {
	fs := testCatalogFS(t, map[string]string{
		"active.en.json": `{}`,
	})
	l, err := NewFromFS(fs, "en")
	if err != nil {
		t.Fatalf("NewFromFS failed: %v", err)
	}
	got := l.Plural("Missing", 2)
	if got == "" {
		t.Error("missing plural returned empty string")
	}
}

// TestLocalizer_EmbeddedCatalogsLoad verifies the real embedded English catalog
// loads via the default New() constructor.
func TestLocalizer_EmbeddedCatalogsLoad(t *testing.T) {
	l, err := New("en")
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}
	if got := l.Localize("TUISettingsTitle"); got != "Settings" {
		t.Errorf("embedded TUISettingsTitle = %q, want %q", got, "Settings")
	}
	// Plural entry present in embedded catalog.
	if got := l.Plural("TUIFilesFound", 2, map[string]any{"Count": 2}); !strings.Contains(got, "files") {
		t.Errorf("embedded plural TUIFilesFound = %q, want plural form", got)
	}
}

func TestCatalogPath(t *testing.T) {
	if got := CatalogPath("en"); got != "locales/active.en.json" {
		t.Errorf("CatalogPath(en) = %q, want %q", got, "locales/active.en.json")
	}
}

func TestLocalizer_IndependentInstances(t *testing.T) {
	enFS := testCatalogFS(t, map[string]string{
		"active.en.json": `{"TUISettingsTitle":{"other":"Settings"}}`,
		"active.ja.json": `{"TUISettingsTitle":{"other":"設定"}}`,
	})
	lEn, err := NewFromFS(enFS, "en")
	if err != nil {
		t.Fatalf("NewFromFS en failed: %v", err)
	}
	lJa, err := NewFromFS(enFS, "ja")
	if err != nil {
		t.Fatalf("NewFromFS ja failed: %v", err)
	}
	if got := lEn.Localize("TUISettingsTitle"); got != "Settings" {
		t.Errorf("en instance = %q, want Settings", got)
	}
	if got := lJa.Localize("TUISettingsTitle"); got != "設定" {
		t.Errorf("ja instance = %q, want 設定", got)
	}
}
