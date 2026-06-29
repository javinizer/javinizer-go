// Shared sessionStorage helpers for per-row manual inputs (D4b). Persisted
// entries are scoped to a batchKey derived from the file set, so a later
// manual-scrape session can't silently reuse overrides from an abandoned
// session when the same path reappears in a different batch. A back
// round-trip to /browse preserves inputs for the SAME file set; /browse
// "Clear All" and /manual successful submit both clear the batch's entry.
export const MANUAL_INPUTS_KEY_PREFIX = 'javinizer_manual_inputs';

// batchKeyFromFiles derives a stable key from a file set so two sessions for
// the same files share overrides, while a different batch can't reuse them.
// This is a sessionStorage namespace, not a security boundary — a simple
// non-crypto hash suffices.
export function batchKeyFromFiles(files: string[]): string {
	const sorted = [...files].sort();
	let h = 0;
	for (const f of sorted) {
		for (let i = 0; i < f.length; i++) h = (Math.imul(31, h) + f.charCodeAt(i)) | 0;
		h = Math.imul(31, h) | 0; // separator between paths
	}
	return (h >>> 0).toString(36);
}

function keyFor(batchKey: string): string {
	return `${MANUAL_INPUTS_KEY_PREFIX}:${batchKey}`;
}

export function loadManualInputs(batchKey: string): Record<string, string> {
	if (typeof sessionStorage === 'undefined') return {};
	try {
		return JSON.parse(sessionStorage.getItem(keyFor(batchKey)) ?? '{}') as Record<string, string>;
	} catch {
		return {};
	}
}

export function persistManualInputs(batchKey: string, map: Record<string, string>): void {
	if (typeof sessionStorage === 'undefined') return;
	const key = keyFor(batchKey);
	if (Object.keys(map).length > 0) sessionStorage.setItem(key, JSON.stringify(map));
	else sessionStorage.removeItem(key);
}

// clearManualInputs drops a single batch's entry when given a batchKey, or all
// manual-input entries when called without one (used by /browse "Clear All",
// which discards the selection and any associated overrides).
export function clearManualInputs(batchKey?: string): void {
	if (typeof sessionStorage === 'undefined') return;
	if (batchKey !== undefined) {
		sessionStorage.removeItem(keyFor(batchKey));
		return;
	}
	for (let i = sessionStorage.length - 1; i >= 0; i--) {
		const k = sessionStorage.key(i);
		if (k && k.startsWith(`${MANUAL_INPUTS_KEY_PREFIX}:`)) sessionStorage.removeItem(k);
	}
}
