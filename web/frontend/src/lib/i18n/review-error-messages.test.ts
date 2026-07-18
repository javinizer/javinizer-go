import { describe, it, expect } from 'vitest';
import {
	review_error_blocked,
	review_error_not_found,
	review_error_rate_limited,
	review_error_unavailable,
} from '$lib/paraglide/messages';

const locales = ['en', 'en-XA', 'ja', 'zh-Hans', 'zh-Hant'] as const;

describe('review_error_* localized messages across all locales', () => {
	it('review_error_unavailable returns localized strings in every locale', () => {
		expect(review_error_unavailable({}, { locale: 'en' })).toBe('Source temporarily unavailable');
		expect(review_error_unavailable({}, { locale: 'en-XA' })).toBe('[ Šöürcë tëmpöråríly üñåvåílåblë ]');
		expect(review_error_unavailable({}, { locale: 'ja' })).toBe('ソースは一時的に利用できません');
		expect(review_error_unavailable({}, { locale: 'zh-Hans' })).toBe('来源暂时不可用');
		expect(review_error_unavailable({}, { locale: 'zh-Hant' })).toBe('來源暫時無法使用');
	});

	it('review_error_not_found returns localized strings in every locale', () => {
		expect(review_error_not_found({}, { locale: 'en' })).toBe('Movie not found');
		expect(review_error_not_found({}, { locale: 'en-XA' })).toBe('[ Mövíë ñöt föüñd ]');
		expect(review_error_not_found({}, { locale: 'ja' })).toBe('動画が見つかりません');
		expect(review_error_not_found({}, { locale: 'zh-Hans' })).toBe('未找到影片');
		expect(review_error_not_found({}, { locale: 'zh-Hant' })).toBe('找不到影片');
	});

	it('review_error_rate_limited returns localized strings in every locale', () => {
		expect(review_error_rate_limited({}, { locale: 'en' })).toBe('Rate limited, try again later');
		expect(review_error_rate_limited({}, { locale: 'en-XA' })).toBe('[ Råtë límítëd, try ågåíñ låtër ]');
		expect(review_error_rate_limited({}, { locale: 'ja' })).toBe('レート制限中です。後で再試行してください');
		expect(review_error_rate_limited({}, { locale: 'zh-Hans' })).toBe('请求频率受限，请稍后重试');
		expect(review_error_rate_limited({}, { locale: 'zh-Hant' })).toBe('請求頻率受限，請稍後重試');
	});

	it('review_error_blocked returns localized strings in every locale', () => {
		expect(review_error_blocked({}, { locale: 'en' })).toBe('Blocked by source');
		expect(review_error_blocked({}, { locale: 'en-XA' })).toBe('[ Blöçkëd by söürcë ]');
		expect(review_error_blocked({}, { locale: 'ja' })).toBe('ソースによってブロックされました');
		expect(review_error_blocked({}, { locale: 'zh-Hans' })).toBe('被来源屏蔽');
		expect(review_error_blocked({}, { locale: 'zh-Hant' })).toBe('被來源封鎖');
	});

	it('every key is present in every locale (no fallback leakage)', () => {
		for (const locale of locales) {
			expect(review_error_unavailable({}, { locale })).toBeTruthy();
			expect(review_error_not_found({}, { locale })).toBeTruthy();
			expect(review_error_rate_limited({}, { locale })).toBeTruthy();
			expect(review_error_blocked({}, { locale })).toBeTruthy();
		}
	});
});
