import { describe, it, expect } from 'vitest';
import { getScraperProxyMode, isTestValid, isProxyConfigDirty } from './proxy-logic';

// Regression tests for issues found in code review rounds 1-6
describe('Regression: Code Review Issues', () => {
	// Round 1: Direct mode mismatch
	it('treats disabled scraper as direct mode', () => {
		expect(getScraperProxyMode(true, { enabled: false })).toBe('direct');
		expect(getScraperProxyMode(true, { enabled: false, profile: 'backup' })).toBe('direct');
	});

	// Round 2: Nil override semantics
	it('treats nil override as inherit mode', () => {
		expect(getScraperProxyMode(true, undefined)).toBe('inherit');
	});

	// Round 3: Inherit mode validation (contract test - no direct validation in frontend)
	// Covered by backend TestRegression_ValidationAcceptsInheritMode

	// Round 4: Test result invalidation on config change
	it('invalidates test when config changes', () => {
		const config = { url: 'http://original.com' };
		const baseline = JSON.stringify(config);
		const result = {
			success: true,
			timestamp: Date.now(),
			configSnapshot: baseline,
		};

		// Test is valid initially
		expect(isTestValid(result, config, 300000)).toBe(true);

		// After config change, test is invalid
		const modifiedConfig = { url: 'http://modified.com' };
		expect(isTestValid(result, modifiedConfig, 300000)).toBe(false);
	});

	// Round 5: Mode mismatch for partial configs
	it('requires enabled===true for specific/inherit modes', () => {
		// Partial config (no enabled) → direct
		expect(getScraperProxyMode(true, { profile: 'backup' })).toBe('direct');
		expect(getScraperProxyMode(true, {})).toBe('direct');

		// Explicit enabled=true → specific/inherit
		expect(getScraperProxyMode(true, { enabled: true, profile: 'backup' })).toBe('specific');
		expect(getScraperProxyMode(true, { enabled: true })).toBe('inherit');
	});

	// Round 6: Dirty state tracking
	it('detects proxy config as dirty when changed', () => {
		const baseline = JSON.stringify({ enabled: true, default_profile: 'main' });

		// Same config → not dirty
		expect(isProxyConfigDirty({ enabled: true, default_profile: 'main' }, baseline)).toBe(false);

		// Modified config → dirty
		expect(isProxyConfigDirty({ enabled: true, default_profile: 'backup' }, baseline)).toBe(true);
		expect(isProxyConfigDirty({ enabled: false, default_profile: 'main' }, baseline)).toBe(true);
	});
});
