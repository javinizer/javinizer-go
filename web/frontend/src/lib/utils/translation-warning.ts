import * as m from '$lib/paraglide/messages';
import type { TranslationWarningCode } from '$lib/api/types';

/**
 * Resolve a translation warning code + raw warning pair to display text.
 * For `rate_limited` the raw backend string wins when present — it names the
 * actual provider+mode; the localized key is provider-neutral fallback for
 * slim (code-only) payloads. Other known codes render localized copy from the
 * paraglide catalogs; `unknown`, unrecognized, or missing codes fall back to
 * the raw `translation_warning` string.
 *
 * Returns null when neither a known code nor a raw string is available
 * (e.g. a Slim payload carrying code `unknown`), so callers can hide the UI.
 */
export function translationWarningMessage(
	code: TranslationWarningCode | string | undefined,
	raw: string | undefined
): string | null {
	if (code === 'rate_limited' && raw && raw.trim() !== '') {
		return raw;
	}
	switch (code) {
		case 'rate_limited':
			return m.translation_warning_rate_limited();
		case 'unauthorized':
			return m.translation_warning_unauthorized();
		case 'forbidden':
			return m.translation_warning_forbidden();
		case 'request_error':
			return m.translation_warning_request_error();
		case 'service_error':
			return m.translation_warning_service_error();
		case 'unavailable':
			return m.translation_warning_unavailable();
		case 'degraded':
			return m.translation_warning_degraded();
		default:
			break;
	}
	if (raw && raw.trim() !== '') {
		return raw;
	}
	return null;
}
