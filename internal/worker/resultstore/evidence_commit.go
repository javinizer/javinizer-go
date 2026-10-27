package resultstore

import "fmt"

// AtomicResultCommitter is an optional capability: an evidence-bearing rescrape
// requires one store-lock CAS for both result and provenance. Keeping it optional
// preserves compatibility with legacy stores, which cannot publish evidence.
type AtomicResultCommitter interface {
	CommitResultAndProvenance(string, *MovieResult, uint64, *ProvenanceData) error
}

// ScrapeReplacementProvenance retains legacy attribution when a successful
// scrape has none, while replacing evidence as a whole (nil clears opt-out).
// The returned data is owned and safe to publish under the result store lock.
func ScrapeReplacementProvenance(current, incoming *ProvenanceData) *ProvenanceData {
	if incoming != nil && (incoming.FieldSources != nil || incoming.ActressSources != nil || incoming.ScraperResults != nil) {
		return incoming.Clone()
	}
	out := current.Clone()
	if incoming != nil && incoming.OfflineEvidence != nil {
		if out == nil {
			out = &ProvenanceData{}
		}
		out.OfflineEvidence = incoming.OfflineEvidence.Clone()
	} else if out != nil {
		out.OfflineEvidence = nil
	}
	return out
}

func (ru *resultUpdater) CommitResultAndProvenance(filePath string, result *MovieResult, expectedRevision uint64, prov *ProvenanceData) error {
	ru.mu.Lock()
	defer ru.mu.Unlock()
	if result == nil {
		return fmt.Errorf("cannot commit nil result")
	}
	current := ru.Results[filePath]
	var revision uint64
	if current != nil {
		revision = current.Revision
	}
	if revision != expectedRevision {
		return fmt.Errorf("conflict: expected revision %d, got %d", expectedRevision, revision)
	}
	owned := result.Clone()
	if info, ok := ru.FileMatchInfo[filePath]; ok {
		owned.FileMatchInfo = info
	}
	owned.Revision = expectedRevision + 1
	if current != nil && current.ResultID != "" {
		owned.ResultID = current.ResultID
	}
	ownedProv := ScrapeReplacementProvenance(ru.Provenance[filePath], prov)
	stateReindexFilePathLocked(ru.resultTrackerState, filePath, current, owned)
	ru.Results[filePath] = owned
	if ownedProv != nil {
		if ru.Provenance == nil {
			ru.Provenance = make(map[string]*ProvenanceData)
		}
		ru.Provenance[filePath] = ownedProv
	}
	stateRecalculateProgress(ru.resultTrackerState)
	// The existing rescrape response echoes the exact landing revision from its
	// candidate. Only these scalar values are shared; stored payloads are owned.
	result.Revision, result.ResultID, result.FileMatchInfo = owned.Revision, owned.ResultID, owned.FileMatchInfo
	return nil
}
