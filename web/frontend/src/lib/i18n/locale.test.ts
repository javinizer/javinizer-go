import { describe, it, expect } from 'vitest';
import { resolveLocaleTag, SUPPORTED_LOCALES } from './locale';

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
