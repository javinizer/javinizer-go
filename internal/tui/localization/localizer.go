package localization

import (
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"path/filepath"
	"strings"

	goi18n "github.com/nicksnyder/go-i18n/v2/i18n"
	"golang.org/x/text/language"
)

//go:embed locales/active.*.json
var catalogs embed.FS

// localesFS returns the embedded filesystem restricted to the locales
// directory so it can be passed to LoadMessageFileFS.
func localesFS() fs.FS {
	sub, err := fs.Sub(catalogs, "locales")
	if err != nil {
		// Should never happen with a compiled-in embed.FS.
		panic(fmt.Sprintf("localization: cannot access embedded locales: %v", err))
	}
	return sub
}

// Localizer is a narrow, non-panicking wrapper around go-i18n that loads the
// embedded JSON catalogs and resolves messages for the requested locale(s).
// English is always available as the fallback. Instances are independent —
// there is no package-global locale state, so concurrent models/tests may use
// different locales safely.
type Localizer struct {
	inner *goi18n.Localizer
}

// New constructs a Localizer from the embedded catalogs. preferences is the
// BCP 47 tag preference list (e.g. the resolved ui.language or detected OS
// locale). When a preference has no exact catalog match, go-i18n falls back to
// English. A nil/empty preference list yields English.
func New(preferences ...string) (*Localizer, error) {
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	root := localesFS()
	entries, err := fs.ReadDir(root, ".")
	if err != nil {
		return nil, fmt.Errorf("localization: cannot read embedded locales: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "active.") || !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, err := bundle.LoadMessageFileFS(root, name); err != nil {
			return nil, fmt.Errorf("localization: cannot load catalog %q: %w", name, err)
		}
	}

	return &Localizer{inner: goi18n.NewLocalizer(bundle, preferences...)}, nil
}

// NewFromFS constructs a Localizer from an arbitrary filesystem (for tests or
// custom catalogs). Catalog files must live at locales/active.*.json under root.
func NewFromFS(catalogs fs.FS, preferences ...string) (*Localizer, error) {
	bundle := goi18n.NewBundle(language.English)
	bundle.RegisterUnmarshalFunc("json", json.Unmarshal)

	localesDir, err := fs.Sub(catalogs, "locales")
	if err != nil {
		return nil, fmt.Errorf("localization: catalogs must contain a locales/ directory: %w", err)
	}
	entries, err := fs.ReadDir(localesDir, ".")
	if err != nil {
		return nil, fmt.Errorf("localization: cannot read locales dir: %w", err)
	}
	for _, entry := range entries {
		name := entry.Name()
		if !strings.HasPrefix(name, "active.") || !strings.HasSuffix(name, ".json") {
			continue
		}
		if _, err := bundle.LoadMessageFileFS(localesDir, name); err != nil {
			return nil, fmt.Errorf("localization: cannot load catalog %q: %w", name, err)
		}
	}

	return &Localizer{inner: goi18n.NewLocalizer(bundle, preferences...)}, nil
}

// Localize returns the translated message for messageID. When template is
// provided, its keys are substituted as named placeholders. It never panics:
// a missing message falls back to messageID itself so the TUI keeps rendering.
func (l *Localizer) Localize(messageID string, template ...map[string]any) string {
	cfg := &goi18n.LocalizeConfig{
		MessageID: messageID,
	}
	if len(template) > 0 && template[0] != nil {
		cfg.TemplateData = template[0]
	}
	msg, err := l.inner.Localize(cfg)
	if err != nil {
		// go-i18n returns an error for unknown message IDs. Fall back to the ID
		// (or a best-effort render) rather than panicking or returning blank.
		if fallback := l.fallbackMessage(messageID, template...); fallback != "" {
			return fallback
		}
		return messageID
	}
	return msg
}

// Plural returns a count-aware translation for messageID. count drives CLDR
// plural-rule selection (one/other/...). It never panics: a missing message
// falls back to a rendered ID + count.
func (l *Localizer) Plural(messageID string, count interface{}, template ...map[string]any) string {
	data := map[string]any{"PluralCount": count, "Count": count}
	if len(template) > 0 && template[0] != nil {
		for k, v := range template[0] {
			data[k] = v
		}
	}
	cfg := &goi18n.LocalizeConfig{
		MessageID:    messageID,
		PluralCount:  count,
		TemplateData: data,
	}
	msg, err := l.inner.Localize(cfg)
	if err != nil {
		if fallback := l.fallbackMessage(messageID, map[string]any{"Count": count}); fallback != "" {
			return fallback
		}
		return fmt.Sprintf("%s (%v)", messageID, count)
	}
	return msg
}

// fallbackMessage attempts a best-effort render of the English "other" form
// from the loaded bundle when the primary Localize call fails (e.g. when a
// preference locale has no catalog entry). Returns "" if no fallback is
// available so callers can fall back to the raw messageID.
func (l *Localizer) fallbackMessage(messageID string, template ...map[string]any) string {
	cfg := &goi18n.LocalizeConfig{
		MessageID: messageID,
		DefaultMessage: &goi18n.Message{
			ID: messageID,
		},
		TemplateData: nil,
	}
	if len(template) > 0 && template[0] != nil {
		cfg.TemplateData = template[0]
	}
	msg, err := l.inner.Localize(cfg)
	if err != nil {
		return ""
	}
	return msg
}

// CatalogPath returns the embedded path for a locale's catalog file, e.g.
// locales/active.en.json. Exposed for diagnostics and tooling.
func CatalogPath(locale string) string {
	return filepath.Join("locales", "active."+locale+".json")
}
