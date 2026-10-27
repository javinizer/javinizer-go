import * as m from '$lib/paraglide/messages';

/** Presentation only: never infer identity, fetch or recompute historical freshness. */
export function offlineEvidenceOutcome(outcome: string): string {
	switch (outcome) {
		case 'evidence_available':
			return m.offline_evidence_available();
		case 'disabled':
			return m.offline_evidence_disabled();
		case 'busy':
			return m.offline_evidence_busy();
		case 'no_source_reference':
			return m.offline_evidence_no_reference();
		case 'unsupported_identity':
			return m.offline_evidence_unsupported();
		case 'stale':
			return m.offline_evidence_stale();
		case 'future':
			return m.offline_evidence_future();
		case 'unmatched':
			return m.offline_evidence_unmatched();
		case 'ambiguous':
			return m.offline_evidence_ambiguous();
		case 'provider_failed':
			return m.offline_evidence_provider_failed();
		case 'cancelled':
			return m.offline_evidence_cancelled();
		case 'budget_exceeded':
			return m.offline_evidence_budget();
		default:
			return m.offline_evidence_unavailable();
	}
}
