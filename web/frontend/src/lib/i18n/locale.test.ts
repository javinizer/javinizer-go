import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
	applyLocale,
	LOCALE_STORAGE_KEY,
	reconcileWithConfig,
	resolveLocaleTag,
	SUPPORTED_LOCALES
} from './locale';
import type { UIConfig } from '$lib/api/types';

vi.mock('$app/environment', () => ({ browser: true }));

const mockGetLocale = vi.fn();
const mockSetLocale = vi.fn();

vi.mock('$lib/paraglide/runtime', () => ({
	baseLocale: 'en',
	locales: ['en', 'en-XA', 'ja', 'zh-Hans', 'zh-Hant'],
	getLocale: (...args: unknown[]) => mockGetLocale(...args),
	setLocale: (...args: unknown[]) => mockSetLocale(...args)
}));

describe('resolveLocaleTag', () => {
	it('resolves canonical supported tags', () => {
		expect(resolveLocaleTag('en')).toBe('en');
		expect(resolveLocaleTag('ja')).toBe('ja');
		expect(resolveLocaleTag('zh-Hans')).toBe('zh-Hans');
		expect(resolveLocaleTag('zh-Hant')).toBe('zh-Hant');
	});

	it('canonicalizes language casing (JA -> ja)', () => {
		expect(resolveLocaleTag('JA')).toBe('ja');
		expect(resolveLocaleTag('JA-JP')).toBe('ja');
	});

	it('canonicalizes script casing (zh-hans -> zh-Hans)', () => {
		expect(resolveLocaleTag('zh-hans')).toBe('zh-Hans');
		expect(resolveLocaleTag('ZH-hans')).toBe('zh-Hans');
		expect(resolveLocaleTag('zh-HANS')).toBe('zh-Hans');
	});

	it('canonicalizes region casing and maps to script (zh-cn -> zh-Hans)', () => {
		expect(resolveLocaleTag('zh-cn')).toBe('zh-Hans');
		expect(resolveLocaleTag('zh-CN')).toBe('zh-Hans');
		expect(resolveLocaleTag('ZH-cn')).toBe('zh-Hans');
		expect(resolveLocaleTag('zh-tw')).toBe('zh-Hant');
		expect(resolveLocaleTag('zh-TW')).toBe('zh-Hant');
		expect(resolveLocaleTag('zh-hk')).toBe('zh-Hant');
	});

	it('strips region from script+region tags (zh-Hans-CN -> zh-Hans)', () => {
		expect(resolveLocaleTag('zh-Hans-CN')).toBe('zh-Hans');
		expect(resolveLocaleTag('zh-hans-CN')).toBe('zh-Hans');
		expect(resolveLocaleTag('zh-Hant-TW')).toBe('zh-Hant');
	});

	it('normalizes underscores to hyphens', () => {
		expect(resolveLocaleTag('zh_Hans')).toBe('zh-Hans');
		expect(resolveLocaleTag('zh_CN')).toBe('zh-Hans');
		expect(resolveLocaleTag('ja_JP')).toBe('ja');
	});

	it('returns null for unsupported locales', () => {
		expect(resolveLocaleTag('fr')).toBeNull();
		expect(resolveLocaleTag('de-DE')).toBeNull();
		expect(resolveLocaleTag('')).toBeNull();
	});

	it('every supported shipped tag resolves to itself', () => {
		for (const locale of SUPPORTED_LOCALES) {
			if (locale.tag === 'auto') continue;
			expect(resolveLocaleTag(locale.tag)).toBe(locale.tag);
		}
	});
});

describe('reconcileWithConfig / applyLocale reload guards (#164)', () => {
	function stubLanguages(langs: string[]) {
		Object.defineProperty(window.navigator, 'languages', { value: langs, configurable: true });
	}

	beforeEach(() => {
		localStorage.clear();
		mockGetLocale.mockReset();
		mockSetLocale.mockReset();
		// Simulate Paraglide's resolution for a zh-CN browser: the localStorage
		// strategy hits, but preferredLanguage cannot map zh-CN -> zh-Hans
		// (exact and base-tag matching only), so it falls back to baseLocale.
		mockGetLocale.mockImplementation(() => localStorage.getItem(LOCALE_STORAGE_KEY) ?? 'en');
		mockSetLocale.mockImplementation(async (tag: string) => {
			localStorage.setItem(LOCALE_STORAGE_KEY, tag);
		});
		stubLanguages(['zh-CN']);
	});

	it('auto config in steady state does not reapply (setLocale reloads by default)', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-Hans');

		const result = await reconcileWithConfig({ language: 'auto' } as UIConfig);

		expect(result).toBe('zh-Hans');
		expect(mockSetLocale).not.toHaveBeenCalled();
		expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh-Hans');
	});

	it('auto config corrects a stale cached tag exactly once, then stays stable', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'en');

		const first = await reconcileWithConfig({ language: 'auto' } as UIConfig);

		expect(first).toBe('zh-Hans');
		expect(mockSetLocale).toHaveBeenCalledTimes(1);
		expect(mockSetLocale).toHaveBeenCalledWith('zh-Hans');

		// Second pass simulates the next page load after setLocale's reload:
		// without the steady-state guard this re-applied and reloaded forever.
		const second = await reconcileWithConfig({ language: 'auto' } as UIConfig);

		expect(second).toBe('zh-Hans');
		expect(mockSetLocale).toHaveBeenCalledTimes(1);
	});

	it('explicit configured locale is cached; the fresh cache makes reapply a no-op', async () => {
		stubLanguages(['en-US']);

		const result = await reconcileWithConfig({ language: 'zh-Hant' } as UIConfig);

		expect(result).toBe('zh-Hant');
		expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh-Hant');
		// The cache write above already makes getLocale() resolve the tag, so
		// applyLocale's guard skips setLocale — same as before the fix, where
		// setLocale's own no-reload guard saw the fresh cache and did nothing.
		expect(mockSetLocale).not.toHaveBeenCalled();
	});

	it('applyLocale skips setLocale when the locale already matches', async () => {
		mockGetLocale.mockReturnValue('zh-Hans');

		await applyLocale('zh-Hans');

		expect(mockSetLocale).not.toHaveBeenCalled();
		expect(document.documentElement.lang).toBe('zh-Hans');
	});

	it('applyLocale delegates to setLocale on an actual change', async () => {
		mockGetLocale.mockReturnValue('en');

		await applyLocale('zh-Hans');

		expect(mockSetLocale).toHaveBeenCalledWith('zh-Hans');
	});
});
