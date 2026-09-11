import { describe, it, expect, vi } from 'vitest';
import { createProxyStore } from './proxy-store.svelte';
import { apiClient } from '$lib/api/client';
import type { Config } from '$lib/api/types';

vi.mock('$lib/api/client', () => ({ apiClient: { testProxy: vi.fn() } }));
vi.mock('$lib/stores/toast', () => ({ toastStore: { success: vi.fn(), error: vi.fn() } }));

describe('unsaved proxy profile tests', () => {
	it.each(['main', 'backup'])('tests the selected %s profile and enables saving', async (name) => {
		const profiles = {
			main: { url: 'http://127.0.0.1:7890', username: '', password: '' },
			backup: { url: 'http://127.0.0.1:7891', username: '', password: '' },
		};
		const config = {
			scrapers: { proxy: { enabled: true, default_profile: 'main', profiles } },
		} as Config;
		vi.mocked(apiClient.testProxy).mockResolvedValue({
			success: true,
			mode: 'direct',
			target_url: 'http://example.com',
			status_code: 200,
			duration_ms: 1,
			message: 'ok',
			verification_token: 'verified',
		});
		const store = createProxyStore({
			getConfig: () => config,
			setConfig: vi.fn(),
			getError: () => null,
			setError: vi.fn(),
			getScrapers: () => [],
			setScrapers: vi.fn(),
			getScraperConfigNames: () => [],
			ensureProxyProfilesInitialized: vi.fn(),
		});
		await store.runNamedProxyProfileTest(name);
		expect(apiClient.testProxy).toHaveBeenLastCalledWith({
			mode: 'direct',
			proxy: {
				enabled: true,
				profile: name,
				profiles: name === 'main' ? profiles : { backup: profiles.backup },
			},
		});
		expect(store.canSaveProfile(name)).toBe(true);
		expect(store.verificationTokens['global']).toBe(name === 'main' ? 'verified' : undefined);
		if (name === 'main') {
			const globalResult = store.globalProxyTestResult;
			vi.mocked(apiClient.testProxy).mockResolvedValueOnce({
				success: true,
				mode: 'direct',
				target_url: 'http://example.com',
				status_code: 200,
				duration_ms: 1,
				message: 'ok',
				verification_token: 'partial-profile-token',
			});
			await store.runNamedProxyProfileTest('backup');
			expect(store.canSaveProfile('backup')).toBe(true);
			expect(store.verificationTokens['global']).toBe('verified');
			expect(store.globalProxyTestResult).toEqual(globalResult);
		}
	});
});
