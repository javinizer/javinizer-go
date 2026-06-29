// Shared sessionStorage helpers for per-row manual inputs (D4b). Keyed by file
// path so a Back round-trip to /browse preserves them; /browse "Clear All" and
// /manual successful submit both clear the entry.
export const MANUAL_INPUTS_KEY = 'javinizer_manual_inputs';

export function loadManualInputs(): Record<string, string> {
	if (typeof sessionStorage === 'undefined') return {};
	try {
		return JSON.parse(sessionStorage.getItem(MANUAL_INPUTS_KEY) ?? '{}') as Record<string, string>;
	} catch {
		return {};
	}
}

export function persistManualInputs(map: Record<string, string>): void {
	if (typeof sessionStorage === 'undefined') return;
	if (Object.keys(map).length > 0) sessionStorage.setItem(MANUAL_INPUTS_KEY, JSON.stringify(map));
	else sessionStorage.removeItem(MANUAL_INPUTS_KEY);
}

export function clearManualInputs(): void {
	if (typeof sessionStorage !== 'undefined') sessionStorage.removeItem(MANUAL_INPUTS_KEY);
}
