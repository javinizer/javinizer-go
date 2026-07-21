import { describe, it, expect, vi, beforeEach } from 'vitest';
import {
	applyLocale,
	applySavedLocale,
	bootstrapLocale,
	currentLocaleChoice,
	LOCALE_CHOICE_KEY,
	LOCALE_STORAGE_KEY,
	reconcileWithConfig,
	resolveLocaleTag,
	selectLocale,
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

	it('auto config with an unpinned ja browser is steady via preferredLanguage', async () => {
		// No cached tag: Paraglide resolves ja directly from navigator.languages.
		mockGetLocale.mockImplementation(() => localStorage.getItem(LOCALE_STORAGE_KEY) ?? 'ja');
		stubLanguages(['ja-JP']);

		const result = await reconcileWithConfig({ language: 'auto' } as UIConfig);

		expect(result).toBe('ja');
		expect(mockSetLocale).not.toHaveBeenCalled();
	});

	it('auto config with an unsupported browser locale falls back to base and stays steady', async () => {
		// de-DE maps to no catalog: app and Paraglide both resolve baseLocale.
		mockGetLocale.mockImplementation(() => localStorage.getItem(LOCALE_STORAGE_KEY) ?? 'en');
		stubLanguages(['de-DE']);

		const result = await reconcileWithConfig({ language: 'auto' } as UIConfig);

		expect(result).toBe('en');
		expect(mockSetLocale).not.toHaveBeenCalled();
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

describe('selectLocale', () => {
	function stubLanguages(langs: string[]) {
		Object.defineProperty(window.navigator, 'languages', { value: langs, configurable: true });
	}

	beforeEach(() => {
		localStorage.clear();
		mockGetLocale.mockReset();
		mockSetLocale.mockReset();
		mockGetLocale.mockImplementation(() => localStorage.getItem(LOCALE_STORAGE_KEY) ?? 'en');
		mockSetLocale.mockImplementation(async (tag: string) => {
			localStorage.setItem(LOCALE_STORAGE_KEY, tag);
		});
		stubLanguages(['zh-CN']);
	});

	it("'auto' resolves the browser locale instead of forcing the base locale", async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'en');

		await selectLocale('auto');

		expect(mockSetLocale).toHaveBeenCalledWith('zh-Hans');
	});

	it('applies an explicit pick via setLocale when it differs from the rendered locale', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'en');

		await selectLocale('ja');

		expect(mockSetLocale).toHaveBeenCalledWith('ja');
	});

	it('re-pins the cache without setLocale when the pick is already rendered', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-Hans');

		await selectLocale('zh-Hans');

		expect(mockSetLocale).not.toHaveBeenCalled();
		expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh-Hans');
		expect(localStorage.getItem(LOCALE_CHOICE_KEY)).toBe('zh-Hans');
	});

	it('records the choice separately so auto does not read back as an explicit tag', async () => {
		// zh-CN browser already rendering zh-Hans (pinned by setLocale for rendering).
		localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-Hans');

		await selectLocale('auto');

		// No reload: the rendered locale already matches the browser preference.
		expect(mockSetLocale).not.toHaveBeenCalled();
		// The choice is recorded as 'auto', not the resolved concrete tag.
		expect(localStorage.getItem(LOCALE_CHOICE_KEY)).toBe('auto');
		expect(currentLocaleChoice()).toBe('auto');
	});

	it('pins the rendering cache for an explicit pick even when already rendered', async () => {
		// ja-JP browser: Paraglide resolves ja via preferredLanguage with no pin,
		// so the UI renders ja without a cached tag. Picking ja explicitly must
		// still pin the cache so a later browser-language change doesn't override
		// the explicit selection on the next bootstrap.
		mockGetLocale.mockImplementation(() => localStorage.getItem(LOCALE_STORAGE_KEY) ?? 'ja');
		stubLanguages(['ja-JP']);

		await selectLocale('ja');

		expect(mockSetLocale).not.toHaveBeenCalled();
		expect(localStorage.getItem(LOCALE_CHOICE_KEY)).toBe('ja');
		expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('ja');
	});

	it('falls back to the base locale for unsupported tags', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'ja');

		await selectLocale('fr');

		expect(mockSetLocale).toHaveBeenCalledWith('en');
	});
});

describe('applySavedLocale', () => {
	function stubLanguages(langs: string[]) {
		Object.defineProperty(window.navigator, 'languages', { value: langs, configurable: true });
	}

	beforeEach(() => {
		localStorage.clear();
		mockGetLocale.mockReset();
		mockSetLocale.mockReset();
		mockGetLocale.mockImplementation(() => localStorage.getItem(LOCALE_STORAGE_KEY) ?? 'en');
		mockSetLocale.mockImplementation(async (tag: string) => {
			localStorage.setItem(LOCALE_STORAGE_KEY, tag);
		});
		stubLanguages(['zh-CN']);
	});

	it('reloads via setLocale when the saved locale differs from the rendered one', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-Hans');

		await applySavedLocale('ja');

		expect(mockSetLocale).toHaveBeenCalledWith('ja');
		expect(localStorage.getItem(LOCALE_CHOICE_KEY)).toBe('ja');
	});

	it('syncs the selector choice to auto for an auto save', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-Hans');

		await applySavedLocale('auto');

		expect(localStorage.getItem(LOCALE_CHOICE_KEY)).toBe('auto');
	});

	it('resolves auto to the browser preference', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'ja');

		await applySavedLocale('auto');

		expect(mockSetLocale).toHaveBeenCalledWith('zh-Hans');
	});

	it('maps config variants onto shipped catalogs', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'en');

		await applySavedLocale('zh-Hans-CN');

		expect(mockSetLocale).toHaveBeenCalledWith('zh-Hans');
	});

	it('does not pin a concrete tag when auto is already rendered', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-Hans');

		await applySavedLocale('auto');

		expect(mockSetLocale).not.toHaveBeenCalled();
		// The rendering cache stays as-is; no conflation with an explicit pick.
		expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh-Hans');
		// The choice is synced to the saved 'auto' so bootstrap honors it.
		expect(localStorage.getItem(LOCALE_CHOICE_KEY)).toBe('auto');
	});

	it('re-pins without setLocale when an explicit saved locale is already rendered', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-Hans');

		await applySavedLocale('zh-Hans');

		expect(mockSetLocale).not.toHaveBeenCalled();
		expect(localStorage.getItem(LOCALE_STORAGE_KEY)).toBe('zh-Hans');
	});

	it('falls back to the base locale for valid but unsupported config tags', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'ja');

		await applySavedLocale('pt-BR');

		expect(mockSetLocale).toHaveBeenCalledWith('en');
	});
});

describe('bootstrapLocale', () => {
	function stubLanguages(langs: string[]) {
		Object.defineProperty(window.navigator, 'languages', { value: langs, configurable: true });
	}

	beforeEach(() => {
		localStorage.clear();
		mockGetLocale.mockReset();
		mockSetLocale.mockReset();
		mockGetLocale.mockImplementation(() => localStorage.getItem(LOCALE_STORAGE_KEY) ?? 'en');
		mockSetLocale.mockImplementation(async (tag: string) => {
			localStorage.setItem(LOCALE_STORAGE_KEY, tag);
		});
	});

	it('re-resolves the browser locale for an auto choice, ignoring a stale pin', async () => {
		// A prior setLocale pinned zh-Hans for a zh-CN browser; the user later
		// switches the browser to ja-JP and chose 'auto'. bootstrap must follow
		// the browser, not the stale pin.
		localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-Hans');
		localStorage.setItem(LOCALE_CHOICE_KEY, 'auto');
		stubLanguages(['ja-JP']);

		const effective = await bootstrapLocale();

		expect(effective).toBe('ja');
		// applyLocale sees the stale pin != ja and delegates to setLocale.
		expect(mockSetLocale).toHaveBeenCalledWith('ja');
	});

	it('trusts the cached pin for an explicit choice', async () => {
		localStorage.setItem(LOCALE_STORAGE_KEY, 'ja');
		localStorage.setItem(LOCALE_CHOICE_KEY, 'ja');
		stubLanguages(['zh-CN']);

		const effective = await bootstrapLocale();

		expect(effective).toBe('ja');
		expect(mockSetLocale).not.toHaveBeenCalled();
	});

	it('honors an explicit choice even when the rendering pin is absent', async () => {
		// Edge case: localStorage was unavailable when setLocale tried to write
		// the pin, so the choice is recorded but the mirror key is not. The
		// explicit choice must win over the browser locale, not revert to it.
		localStorage.setItem(LOCALE_CHOICE_KEY, 'ja');
		stubLanguages(['en-US']);

		const effective = await bootstrapLocale();

		expect(effective).toBe('ja');
		expect(mockSetLocale).toHaveBeenCalledWith('ja');
	});

	it('falls back to the browser preference when no choice is recorded', async () => {
		stubLanguages(['zh-CN']);

		const effective = await bootstrapLocale();

		expect(effective).toBe('zh-Hans');
	});
});

describe('currentLocaleChoice', () => {
	beforeEach(() => {
		localStorage.clear();
	});

	it('returns the recorded explicit choice', () => {
		localStorage.setItem(LOCALE_CHOICE_KEY, 'ja');
		expect(currentLocaleChoice()).toBe('ja');
	});

	it('returns auto when the choice is auto', () => {
		localStorage.setItem(LOCALE_CHOICE_KEY, 'auto');
		// A concrete tag may still be pinned for rendering; the choice stays auto.
		localStorage.setItem(LOCALE_STORAGE_KEY, 'zh-Hans');
		expect(currentLocaleChoice()).toBe('auto');
	});

	it('falls back to auto when no choice is recorded', () => {
		expect(currentLocaleChoice()).toBe('auto');
	});

	it('falls back to auto for an invalid recorded choice', () => {
		localStorage.setItem(LOCALE_CHOICE_KEY, 'klingon');
		expect(currentLocaleChoice()).toBe('auto');
	});
});
