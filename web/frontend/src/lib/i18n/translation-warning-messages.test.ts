import { describe, it, expect } from 'vitest';
import {
	translation_warning_rate_limited,
	translation_warning_unauthorized,
	translation_warning_forbidden,
	translation_warning_request_error,
	translation_warning_service_error,
	translation_warning_unavailable,
	translation_warning_degraded,
} from '$lib/paraglide/messages';
import { translationWarningMessage } from '$lib/utils/translation-warning';

const locales = ['en', 'en-XA', 'ja', 'zh-Hans', 'zh-Hant'] as const;

describe('translation_warning_* localized messages across all locales', () => {
	it('translation_warning_rate_limited is provider-neutral fallback copy in every locale', () => {
		expect(translation_warning_rate_limited({}, { locale: 'en' })).toBe(
			'Translation was rate limited by the translation provider. Retry later, switch translation provider, or configure paid mode with an API key in Settings.'
		);
		for (const locale of locales) {
			const msg = translation_warning_rate_limited({}, { locale });
			expect(msg).toBeTruthy();
			expect(msg).not.toBe('translation_warning_rate_limited');
		}
	});

	it('every key is present in every locale (no fallback leakage)', () => {
		for (const locale of locales) {
			expect(translation_warning_rate_limited({}, { locale })).toBeTruthy();
			expect(translation_warning_unauthorized({}, { locale })).toBeTruthy();
			expect(translation_warning_forbidden({}, { locale })).toBeTruthy();
			expect(translation_warning_request_error({}, { locale })).toBeTruthy();
			expect(translation_warning_service_error({}, { locale })).toBeTruthy();
			expect(translation_warning_unavailable({}, { locale })).toBeTruthy();
			expect(translation_warning_degraded({}, { locale })).toBeTruthy();
		}
	});
});

describe('translationWarningMessage code mapping', () => {
	it('known codes render localized copy', () => {
		expect(translationWarningMessage('degraded', 'raw')).toBe(translation_warning_degraded());
	});

	it('rate_limited prefers the raw backend string (names provider+mode) when present', () => {
		expect(
			translationWarningMessage('rate_limited', 'Translation (openai): rate limited - retry later')
		).toBe('Translation (openai): rate limited - retry later');
	});

	it('rate_limited falls back to the localized key on slim (code-only) payloads', () => {
		expect(translationWarningMessage('rate_limited', undefined)).toBe(
			translation_warning_rate_limited()
		);
		expect(translationWarningMessage('rate_limited', '   ')).toBe(
			translation_warning_rate_limited()
		);
	});

	it('unknown code falls back to the raw warning string', () => {
		expect(translationWarningMessage('unknown', 'Translation (google): partial detail')).toBe(
			'Translation (google): partial detail'
		);
	});

	it('missing code falls back to the raw warning string', () => {
		expect(translationWarningMessage(undefined, 'raw warning')).toBe('raw warning');
	});

	it('unrecognized code falls back to the raw warning string', () => {
		expect(translationWarningMessage('some_future_code', 'raw warning')).toBe('raw warning');
	});

	it('returns null when there is nothing to display', () => {
		expect(translationWarningMessage('unknown', undefined)).toBeNull();
		expect(translationWarningMessage(undefined, undefined)).toBeNull();
		expect(translationWarningMessage(undefined, '  ')).toBeNull();
	});
});
