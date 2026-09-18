package tui

import (
	"testing"
)

func TestFinalLanguagePresentationAndPreference(t *testing.T) {
	choices := supportedLanguages()
	if len(choices) != 5 || choices[0] != autoSettingValue || choices[2] != "ja" {
		t.Fatalf("catalog choices: %v", choices)
	}
	view := newSettingsView()
	if view.language != autoSettingValue {
		t.Fatalf("default language: %q", view.language)
	}
	for _, tc := range []struct{ input, want string }{{"en", "English"}, {"ja", "日本語"}, {"zh-Hans", "简体中文"}, {"zh-Hant", "繁體中文"}, {"xx", "xx"}} {
		if got := view.languageDisplayName(tc.input); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.input, got, tc.want)
		}
	}
	sm := newSettingsManager(settingsManagerDeps{}, false, false)
	sm.setLanguage(" ja ")
	if sm.languageValue() != "ja" {
		t.Fatalf("preference: %q", sm.languageValue())
	}
	if got := resolveLanguagePreferences(" ja "); len(got) != 1 || got[0] != "ja" {
		t.Fatalf("explicit preferences: %v", got)
	}
	sm.setLanguage(" AUTO ")
	if sm.languageValue() != autoSettingValue {
		t.Fatalf("auto preference: %q", sm.languageValue())
	}
	sm.setLanguage("   ")
	if sm.languageValue() != autoSettingValue {
		t.Fatalf("empty preference: %q", sm.languageValue())
	}
	if got := view.languageDisplayName(" auto "); got != view.loc("TUISettingsLanguageAuto") {
		t.Fatalf("auto display: %q", got)
	}
	if got := resolveLanguagePreferences(autoSettingValue); len(got) == 0 {
		t.Fatal("auto must resolve OS preferences")
	}
	model := New(TUIModelConfig{Language: autoSettingValue})
	if model.Localizer() == nil {
		t.Fatal("automatic language model must initialize localizer")
	}
	if got := resolveLanguagePreferences(model.settingsMgr.languageValue()); len(got) == 0 {
		t.Fatal("model auto preference must resolve OS locale")
	}
}
