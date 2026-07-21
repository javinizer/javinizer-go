import { browser } from '$app/environment';
import { baseLocale, getLocale, locales, setLocale } from '$lib/paraglide/runtime';
import type { UIConfig } from '$lib/api/types';

// Mirror key for early bootstrap reads before the protected full-config
// request finishes. Paraglide's own strategy also reads localStorage via the
// configured localStorageKey; this mirror is a fast-path cache that survives
// across reloads and clearClientStorage-tolerant flows.
export const LOCALE_STORAGE_KEY = 'javinizer-locale';

// Records the user's selector choice ('auto' or an explicit tag), kept separate
// from LOCALE_STORAGE_KEY: that key is also Paraglide's rendering cache
// (setLocale writes the resolved tag there for catalogs that preferredLanguage
// can't derive, e.g. zh-CN -> zh-Hans), so it cannot on its own distinguish an
// explicit pick from an auto-resolved one. Without this, choosing Automatic
// would come back as a concrete tag after reload (#165 P2).
export const LOCALE_CHOICE_KEY = 'javinizer-locale-choice';

// currentLocaleChoice returns the selector value for the recorded choice:
// 'auto' (browser preference) or an explicit supported tag. Pre-auth there is
// no config, so this is the source of truth for the dropdown display.
export function currentLocaleChoice(): string {
	if (!browser) return 'auto';
	const choice = localStorage.getItem(LOCALE_CHOICE_KEY);
	if (choice === 'auto' || (choice !== null && isSupported(choice))) return choice;
	return 'auto';
}

// Supported locales that ship a catalog. Currently English only; future
// reviewed locales are added here. Use self-names so the selector stays usable
// when the current locale is wrong.
export interface SupportedLocale {
	tag: string;
	selfName: string;
	dir: 'ltr' | 'rtl';
}

export const SUPPORTED_LOCALES: SupportedLocale[] = [
	{ tag: 'auto', selfName: 'Automatic (browser)', dir: 'ltr' },
	{ tag: 'en', selfName: 'English', dir: 'ltr' },
	{ tag: 'ja', selfName: '日本語', dir: 'ltr' },
	{ tag: 'zh-Hans', selfName: '简体中文', dir: 'ltr' },
	{ tag: 'zh-Hant', selfName: '繁體中文', dir: 'ltr' }
];

function canonicalizeTag(tag: string): string {
	const parts = tag.split('-');
	return parts
		.map((part, i) => {
			if (i === 0) return part.toLowerCase();
			if (part.length === 4) return part.charAt(0).toUpperCase() + part.slice(1).toLowerCase();
			if (part.length === 2 || part.length === 3) return part.toUpperCase();
			return part.toLowerCase();
		})
		.join('-');
}

function isSupported(tag: string): boolean {
	return locales.includes(tag as typeof locales[number]);
}

// resolveLocaleTag canonicalizes/resolve a BCP 47 tag against the compiled
// locale set. It reuses the browser locale mapping (region -> script) and the
// script-subtag-stripping logic so a configured variant like `ja-JP` or
// `zh-Hans-CN` resolves to the supported base/script tag. Returns null when no
// supported locale can be derived.
export function resolveLocaleTag(raw: string): string | null {
	if (!raw) return null;
	const tag = canonicalizeTag(raw.replace(/_/g, '-'));
	const mapped = mapBrowserLocale(tag);
	if (isSupported(mapped)) return mapped;
	const parts = mapped.split('-');
	if (parts.length >= 3) {
		const scriptOnly = parts[0] + '-' + parts[1];
		if (isSupported(scriptOnly)) return scriptOnly;
	}
	const base = mapped.split('-')[0];
	if (base && isSupported(base)) return base;
	return null;
}

// resolveBrowserLocale matches navigator.languages against compiled locales
// and returns the first supported tag, falling back to the base locale.
// Browsers send region-based tags (zh-CN, zh-TW); map to script-based tags
// (zh-Hans, zh-Hant) which are what our catalogs use.
export function resolveBrowserLocale(): string {
	if (!browser) return baseLocale;
	const candidates = navigator.languages ?? [navigator.language];
	for (const raw of candidates) {
		if (!raw) continue;
		const resolved = resolveLocaleTag(raw);
		if (resolved) return resolved;
	}
	return baseLocale;
}

function mapBrowserLocale(tag: string): string {
	const canonical = canonicalizeTag(tag);
	const lower = canonical.toLowerCase();
	if (lower === 'zh-cn' || lower === 'zh-sg') return 'zh-Hans';
	if (lower === 'zh-tw' || lower === 'zh-hk' || lower === 'zh-mo') return 'zh-Hant';
	return canonical;
}

// readCachedLocale returns a valid cached locale mirror or null when
// absent/invalid/stale.
function readCachedLocale(): string | null {
	if (!browser) return null;
	const cached = localStorage.getItem(LOCALE_STORAGE_KEY);
	if (!cached) return null;
	if (isSupported(cached)) return cached;
	return null;
}

// bootstrapLocale resolves the effective locale before the protected full-config
// request finishes. For an 'auto' choice it always re-resolves the browser
// preference — the concrete tag pinned by setLocale (for catalogs
// preferredLanguage can't derive, e.g. zh-CN -> zh-Hans) is just a rendering
// cache and must not pin the language across browser-language changes. For an
// explicit choice (or none recorded yet) it trusts the cached pin and falls
// back to the browser. Sets document.documentElement.lang and dir.
export async function bootstrapLocale(): Promise<string> {
	if (!browser) return baseLocale;

	let effective: string;
	if (localStorage.getItem(LOCALE_CHOICE_KEY) === 'auto') {
		effective = resolveBrowserLocale();
	} else {
		effective = readCachedLocale() ?? resolveBrowserLocale();
	}

	await applyLocale(effective);
	return effective;
}

// applyLocale sets the Paraglide locale and updates <html lang> and <html dir>.
// setLocale reloads the page by default, so skip it when Paraglide already
// renders the target: its own no-reload guard compares against getLocale(),
// which cannot map region tags like zh-CN onto script locales (zh-Hans) and
// reports baseLocale instead — callers that clear the cached tag before
// applying (the 'auto' flows) would otherwise reload on every pass (#164).
export async function applyLocale(tag: string): Promise<void> {
	if (!browser) return;
	const resolved = isSupported(tag) ? tag : baseLocale;
	if (getLocale() !== resolved) {
		await setLocale(resolved as typeof locales[number]);
	}
	document.documentElement.lang = resolved;
	document.documentElement.dir = localeDir(resolved);
}

function localeDir(tag: string): 'ltr' | 'rtl' {
	const entry = SUPPORTED_LOCALES.find((l) => l.tag === tag);
	if (entry) return entry.dir;
	// Heuristic for RTL base languages not yet in the supported list.
	const base = tag.split('-')[0];
	return base === 'ar' || base === 'he' || base === 'fa' || base === 'ur' ? 'rtl' : 'ltr';
}

// reconcileWithConfig applies the configured ui.language after the full config
// loads. An explicit supported tag wins and updates the cache. 'auto' clears a
// stale explicit cache so the current browser preference is used. A valid but
// unsupported configured tag is retained for diagnostics but renders English.
export async function reconcileWithConfig(ui?: UIConfig | null): Promise<string> {
	if (!browser) return getLocale() ?? baseLocale;

	const configured = ui?.language?.trim() ?? '';
	if (configured === '' || canonicalizeTag(configured) === 'auto') {
		const browserLocale = resolveBrowserLocale();
		// Steady state: Paraglide already renders the browser-preferred locale.
		// Clearing the cached tag and reapplying on every reconcile (which runs
		// on each page load post-auth) makes getLocale() fall back to baseLocale
		// for script-based locales (zh-Hans/zh-Hant), so setLocale's reload
		// guard misfires and the page reloads forever (#164).
		if (getLocale() === browserLocale) {
			return browserLocale;
		}
		localStorage.removeItem(LOCALE_STORAGE_KEY);
		await applyLocale(browserLocale);
		return browserLocale;
	}

	const resolved = resolveLocaleTag(configured);
	if (resolved) {
		localStorage.setItem(LOCALE_STORAGE_KEY, resolved);
		await applyLocale(resolved);
		return resolved;
	}

	// Valid but unsupported configured tag: render English, do not cache.
	await applyLocale(baseLocale);
	return baseLocale;
}

// selectLocale applies a locale picked from a selector. 'auto' resolves the
// browser preference (mapped onto the shipped catalogs). The choice is recorded
// under LOCALE_CHOICE_KEY separately from Paraglide's rendering cache
// (LOCALE_STORAGE_KEY): when the effective locale differs from the rendered
// one, setLocale pins the resolved tag for rendering and reloads so compiled
// messages re-render. For an explicit pick that already matches the rendered
// locale, the rendering cache is still pinned so a later browser-language
// change doesn't override the explicit selection on the next bootstrap; 'auto'
// never pins, so the browser preference stays authoritative.
export async function selectLocale(tag: string): Promise<void> {
	if (!browser) return;
	const resolved = tag === 'auto' ? resolveBrowserLocale() : isSupported(tag) ? tag : baseLocale;
	localStorage.setItem(LOCALE_CHOICE_KEY, tag);
	if (getLocale() !== resolved) {
		await setLocale(resolved as typeof locales[number]);
		return;
	}
	if (tag !== 'auto' && isSupported(tag)) {
		localStorage.setItem(LOCALE_STORAGE_KEY, resolved);
	}
	document.documentElement.lang = resolved;
	document.documentElement.dir = localeDir(resolved);
}

// applySavedLocale applies a just-persisted ui.language after a settings
// save. Unlike reconcileWithConfig (which never reloads in steady state and
// pins before applying, so it can leave the UI rendering the old locale),
// this forces a reload when the saved locale differs from the rendered one —
// otherwise a language change saved in settings would not re-render the page.
// Config values go through resolveLocaleTag so variants like zh-Hans-CN map
// onto shipped catalogs; 'auto' resolves the browser preference.
export async function applySavedLocale(language?: string | null): Promise<void> {
	if (!browser) return;
	const configured = language?.trim() ?? '';
	const isAuto = configured === '' || canonicalizeTag(configured) === 'auto';
	const resolved = isAuto ? resolveBrowserLocale() : (resolveLocaleTag(configured) ?? baseLocale);
	if (getLocale() === resolved) {
		// Already rendering the target. Don't pin a concrete tag for 'auto' —
		// that would conflate the rendering cache with an explicit choice and
		// mask later browser-preference changes.
		if (!isAuto) localStorage.setItem(LOCALE_STORAGE_KEY, resolved);
		return;
	}
	await setLocale(resolved as typeof locales[number]);
}
