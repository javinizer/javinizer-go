package scrape

import (
	"github.com/javinizer/javinizer-go/internal/catalogname"
	"github.com/javinizer/javinizer-go/internal/models"
	"strings"
	"unicode"
)

type catalogNameDecision struct {
	Index       int    `json:"index"`
	SourceID    string `json:"source_id,omitempty"`
	DisplayName string `json:"display_name,omitempty"`
	Applied     bool   `json:"applied"`
	Confidence  string `json:"confidence"`
	Reason      string `json:"reason"`
	CatalogID   uint   `json:"catalog_id,omitempty"`
}

type catalogNameOutput struct {
	Mode      string                `json:"mode"`
	Raw       *models.ScraperResult `json:"raw"`
	Enriched  *models.ScraperResult `json:"enriched"`
	Decisions []catalogNameDecision `json:"decisions"`
}

// enrichCatalogNames changes a copy for display/export only. It never changes
// identity identifiers, native names, source evidence, or persisted credits.
func enrichCatalogNames(raw *models.ScraperResult, catalog *catalogname.Snapshot) catalogNameOutput {
	out := catalogNameOutput{Mode: "catalog_names", Raw: raw, Enriched: raw.Clone(), Decisions: make([]catalogNameDecision, 0, len(raw.Actresses))}
	for i, actor := range raw.Actresses {
		d := catalogNameDecision{Index: i, Confidence: "none", Reason: "missing_source_evidence"}
		if raw.Source != "javlibrary" || len(raw.CastNameEvidence) != len(raw.Actresses) {
			out.Decisions = append(out.Decisions, d)
			continue
		}
		evidence := raw.CastNameEvidence[i]
		d.SourceID, d.DisplayName = evidence.SourceID, evidence.DisplayName
		switch {
		case evidence.ManualOverride:
			d.Confidence, d.Reason = "explicit_override", "manual_override_preserved"
		case actor.JapaneseName != "" || strings.ContainsFunc(evidence.DisplayName, func(r rune) bool { return unicode.In(r, unicode.Han, unicode.Hiragana, unicode.Katakana) }):
			d.Reason = "native_name_preserved"
		case evidence.SourceID == "" || evidence.DisplayName == "":
			// Do not turn repaired or absent source metadata into trusted evidence.
		default:
			resolved := catalog.Resolve(evidence.DisplayName)
			d.Confidence, d.Reason = resolved.Confidence, resolved.Reason
			if resolved.Resolved {
				d.CatalogID = resolved.CatalogID
				d.Applied = actor.FirstName != resolved.FirstName || actor.LastName != resolved.LastName
				out.Enriched.Actresses[i].FirstName = resolved.FirstName
				out.Enriched.Actresses[i].LastName = resolved.LastName
			}
		}
		out.Decisions = append(out.Decisions, d)
	}
	return out
}
