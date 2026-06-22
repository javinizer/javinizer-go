import { describe, it, expect } from 'vitest';
import {
	getScraperProxyMode,
	isProxyConfigDirty,
	isTestValid,
	type ScraperProxyOverride,
} from './proxy-logic';

describe('getScraperProxyMode', () => {
	it('returns direct when global is disabled (circuit breaker)', () => {
		expect(getScraperProxyMode(false, undefined)).toBe('direct');
		expect(getScraperProxyMode(false, { enabled: true, profile: 'backup' })).toBe('direct');
	});

	it('returns inherit when no override present (nil override)', () => {
		expect(getScraperProxyMode(true, undefined)).toBe('inherit');
	});

	it('returns direct when override.enabled is false', () => {
		expect(getScraperProxyMode(true, { enabled: false })).toBe('direct');
		expect(getScraperProxyMode(true, { enabled: false, profile: 'backup' })).toBe('direct');
	});

	it('returns direct when override.enabled is undefined (not explicitly true)', () => {
		expect(getScraperProxyMode(true, {})).toBe('direct');
		expect(getScraperProxyMode(true, { profile: 'backup' })).toBe('direct');
	});

	it('returns inherit when enabled=true but no profile', () => {
		expect(getScraperProxyMode(true, { enabled: true })).toBe('inherit');
		expect(getScraperProxyMode(true, { enabled: true, profile: '' })).toBe('inherit');
	});

	it('returns specific when enabled=true and profile is set', () => {
		expect(getScraperProxyMode(true, { enabled: true, profile: 'backup' })).toBe('specific');
	});
});

describe('isProxyConfigDirty', () => {
	it('returns false when config matches baseline', () => {
		const config = { enabled: true, default_profile: 'main' };
		const baseline = JSON.stringify(config);
		expect(isProxyConfigDirty(config, baseline)).toBe(false);
	});

	it('returns true when config differs from baseline', () => {
		const config = { enabled: true, default_profile: 'main' };
		const baseline = JSON.stringify({ enabled: true, default_profile: 'backup' });
		expect(isProxyConfigDirty(config, baseline)).toBe(true);
	});

	it('treats undefined config as dirty when compared to any baseline', () => {
		// undefined stringifies to the primitive undefined
		// Any string baseline will be !== undefined, so it's dirty
		expect(isProxyConfigDirty(undefined, '{}')).toBe(true);
		expect(isProxyConfigDirty(undefined, 'null')).toBe(true);
		expect(isProxyConfigDirty(undefined, '')).toBe(true);
	});
});

describe('isTestValid', () => {
	const TEST_VALIDITY_MS = 5 * 60 * 1000;

	it('returns false for null/undefined result', () => {
		expect(isTestValid(null, {}, TEST_VALIDITY_MS)).toBe(false);
		expect(isTestValid(undefined, {}, TEST_VALIDITY_MS)).toBe(false);
	});

	it('returns false for failed test', () => {
		expect(isTestValid({ success: false, timestamp: Date.now() }, {}, TEST_VALIDITY_MS)).toBe(
			false,
		);
	});

	it('returns false for expired test', () => {
		const oldTimestamp = Date.now() - TEST_VALIDITY_MS - 1000;
		expect(isTestValid({ success: true, timestamp: oldTimestamp }, {}, TEST_VALIDITY_MS)).toBe(
			false,
		);
	});

	it('returns false when config changed since test', () => {
		const result = {
			success: true,
			timestamp: Date.now(),
			configSnapshot: JSON.stringify({ url: 'http://old.com' }),
		};
		expect(isTestValid(result, { url: 'http://new.com' }, TEST_VALIDITY_MS)).toBe(false);
	});

	it('returns true for valid, fresh, matching test', () => {
		const config = { url: 'http://test.com' };
		const result = {
			success: true,
			timestamp: Date.now(),
			configSnapshot: JSON.stringify(config),
		};
		expect(isTestValid(result, config, TEST_VALIDITY_MS)).toBe(true);
	});
});
