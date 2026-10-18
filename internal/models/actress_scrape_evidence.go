package models

// actressScrapeEvidence retains the exact highest-priority source record, not
// the canonicalized or lower-priority-enriched actress. Unexported state cannot
// be supplied by JSON clients or persisted as part of a canonical actress.
type actressScrapeEvidence struct {
	source   string
	priority int
	info     ActressInfo
}

// SetScrapeEvidence records an aggregation-selected source actor before rename.
func (a *Actress) SetScrapeEvidence(source string, info ActressInfo, priority int) {
	a.scrapeEvidence = actressScrapeEvidence{source: source, info: info, priority: priority}
}

// ScrapeEvidence returns the selected raw record, if this actress came directly
// from aggregation. Legacy and reconstructed actresses have no such evidence.
func (a Actress) ScrapeEvidence() (string, ActressInfo, int) {
	return a.scrapeEvidence.source, a.scrapeEvidence.info, a.scrapeEvidence.priority
}
