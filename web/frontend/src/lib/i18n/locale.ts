import { browser } from '$app/environment';
import { baseLocale, getLocale, locales, setLocale } from '$lib/paraglide/runtime';
import type { UIConfig } from '$lib/api/types';

// Mirror key for early bootstrap reads before the protected full-config
// request finishes. Paraglide's own strategy also reads localStorage via the
// configured localStorageKey; this mirror is a fast-path cache that survives
// across reloads and clearClientStorage-tolerant flows.
export const LOCALE_STORAGE_KEY = 'javinizer-locale';

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

function isSupported(tag: string): boolean {
	return locales.includes(tag as typeof locales[number]);
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
		const tag = raw.replace(/_/g, '-');
		const mapped = mapBrowserLocale(tag);
		if (isSupported(mapped)) return mapped;
		const parts = mapped.split('-');
		if (parts.length >= 3) {
			const scriptOnly = parts[0] + '-' + parts[1];
			if (isSupported(scriptOnly)) return scriptOnly;
		}
		const base = mapped.split('-')[0];
		if (base && isSupported(base)) return base;
	}
	return baseLocale;
}

function mapBrowserLocale(tag: string): string {
	const lower = tag.toLowerCase();
	if (lower === 'zh-cn' || lower === 'zh-sg') return 'zh-Hans';
	if (lower === 'zh-tw' || lower === 'zh-hk' || lower === 'zh-mo') return 'zh-Hant';
	return tag;
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
// request finishes. Order: valid localStorage mirror -> browser preference ->
// base locale. Sets document.documentElement.lang and dir. Returns the resolved
// locale tag.
export async function bootstrapLocale(): Promise<string> {
	if (!browser) return baseLocale;

	let effective = readCachedLocale();
	if (!effective) {
		effective = resolveBrowserLocale();
	}

	await applyLocale(effective);
	return effective;
}

// applyLocale sets the Paraglide locale and updates <html lang> and <html dir>.
export async function applyLocale(tag: string): Promise<void> {
	if (!browser) return;
	const resolved = isSupported(tag) ? tag : baseLocale;
	await setLocale(resolved as typeof locales[number]);
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
	if (configured === '' || configured.toLowerCase() === 'auto') {
		localStorage.removeItem(LOCALE_STORAGE_KEY);
		const browserLocale = resolveBrowserLocale();
		await applyLocale(browserLocale);
		return browserLocale;
	}

	if (isSupported(configured)) {
		localStorage.setItem(LOCALE_STORAGE_KEY, configured);
		await applyLocale(configured);
		return configured;
	}

	// Valid but unsupported configured tag: render English, do not cache.
	await applyLocale(baseLocale);
	return baseLocale;
}

// selectLocale persists an explicit choice, applies it, and reloads so stores
// and non-component modules re-evaluate against the new locale.
export async function selectLocale(tag: string): Promise<void> {
	if (!browser) return;
	if (tag !== 'auto' && isSupported(tag)) {
		localStorage.setItem(LOCALE_STORAGE_KEY, tag);
	}
	await applyLocale(isSupported(tag) ? tag : baseLocale);
}
