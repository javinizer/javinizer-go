import type { BatchJobResponse, FileResult } from '$lib/api/types';

const hasRevision = (revision: number | undefined): revision is number =>
	Number.isSafeInteger(revision) && revision! >= 0;

/** Result identity and revision own the entire Movie/provenance pair, including absence. */
export function acceptsResultSnapshot(
	current: FileResult | undefined,
	incoming: FileResult,
): boolean {
	if (!current) return true;
	if (current.result_id && incoming.result_id !== current.result_id) return false;
	if (hasRevision(current.revision)) {
		return hasRevision(incoming.revision) && incoming.revision >= current.revision;
	}
	return true; // Historical payloads without any revision retain legacy behavior.
}

/** Same-job delayed polls/bulk responses may never downgrade an already observed result. */
export function reconcileJobSnapshot(
	current: BatchJobResponse | null,
	incoming: BatchJobResponse | null,
): BatchJobResponse | null {
	if (!current || !incoming || current.id !== incoming.id) return incoming;
	const results = { ...incoming.results };
	for (const [path, previous] of Object.entries(current.results ?? {})) {
		const next = results[path];
		// Preserve full-snapshot removal semantics; fence only existing same-path pairs.
		if (next && !acceptsResultSnapshot(previous, next)) results[path] = previous;
	}
	return { ...incoming, results };
}
