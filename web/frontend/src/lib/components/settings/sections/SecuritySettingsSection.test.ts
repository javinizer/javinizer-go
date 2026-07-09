import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import SecuritySettingsSection from './SecuritySettingsSection.svelte';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import type { SettingsConfig } from '$lib/api/types';
import { toastStore } from '$lib/stores/toast';

vi.mock('$lib/api/client', () => ({
	apiClient: {
		updateSecurityConfig: vi.fn(),
		getConfig: vi.fn(),
	},
}));

const mod = await import('$lib/api/client');
const mockUpdateSecurityConfig = vi.mocked(mod.apiClient.updateSecurityConfig);
const mockGetConfig = vi.mocked(mod.apiClient.getConfig);

if (!Element.prototype.animate) {
	// eslint-disable-next-line @typescript-eslint/no-explicit-any
	(Element.prototype as any).animate = function () {
		const anim = {
			onfinish: null as (() => void) | null,
			oncancel: null as (() => void) | null,
			effect: null as unknown,
			playState: 'finished' as const,
			currentTime: 0,
			cancel() {},
			finish() {
				anim.onfinish?.();
			},
			addEventListener() {},
			removeEventListener() {},
		};
		queueMicrotask(() => anim.onfinish?.());
		return anim;
	};
}

function makeConfig(overrides: Partial<SettingsConfig['api']['security']> = {}): SettingsConfig {
	return {
		server: { host: '0.0.0.0', port: 8080 },
		api: {
			security: {
				allowed_directories: [],
				denied_directories: [],
				max_files_per_scan: 0,
				scan_timeout_seconds: 0,
				allowed_origins: [],
				allow_unc: false,
				allowed_unc_servers: [],
				rate_limit: { requests_per_minute: 0 },
				trusted_proxies: [],
				force_secure_cookies: false,
				...overrides,
			},
		},
		system: { umask: '', version_check_enabled: false, version_check_interval_hours: 0, temp_dir: '' },
		scrapers: {} as never,
		metadata: {} as never,
		file_matching: { regex_enabled: false, regex_pattern: '' },
		output: {} as never,
		database: {} as never,
		logging: {} as never,
		performance: {} as never,
	} as unknown as SettingsConfig;
}

import { QueryClient } from '@tanstack/svelte-query';
function renderSection(config: SettingsConfig) {
	return render(
		SecuritySettingsSection,
		{ config, inputClass: 'w-full' },
		{
			wrapper: QueryClientWrapper,
			wrapperProps: { client: new QueryClient({ defaultOptions: { queries: { retry: false } } }) },
		},
	);
}

beforeEach(() => {
	vi.clearAllMocks();
	mockGetConfig.mockResolvedValue(makeConfig() as unknown as SettingsConfig);
});

afterEach(() => {
	vi.restoreAllMocks();
});

async function expandSection(container: HTMLElement): Promise<void> {
	const header = container.querySelector('button[aria-expanded="false"]') as HTMLButtonElement;
	if (header) {
		await fireEvent.click(header);
		await waitFor(() => expect(header.getAttribute('aria-expanded')).toBe('true'));
	}
}

describe('SecuritySettingsSection', () => {
	it('renders the empty-state hint when no allowed directories are configured', async () => {
		const { container } = renderSection(makeConfig());
		await expandSection(container);
		expect(container.textContent).toContain('No allowed directories configured');
	});

	it('lists configured allowed directories', async () => {
		const { container } = renderSection(
			makeConfig({ allowed_directories: ['/mnt/videos', '/mnt/media'] }),
		);
		await expandSection(container);
		expect(container.textContent).toContain('/mnt/videos');
		expect(container.textContent).toContain('/mnt/media');
	});

	it('marks the first allowed directory as the default scan path', async () => {
		const { container } = renderSection(
			makeConfig({ allowed_directories: ['/mnt/videos', '/mnt/media'] }),
		);
		await expandSection(container);
		expect(container.textContent).toContain('Default');
	});

	it('adds an allowed directory from the path input', async () => {
		const { container } = renderSection(makeConfig());
		await expandSection(container);
		const input = container.querySelector('input[placeholder*="Add a directory"]') as HTMLInputElement;
		expect(input).toBeTruthy();
		await fireEvent.input(input, { target: { value: '/new/dir' } });
		const addBtn = container.querySelector('button[title="Add allowed directory"]') as HTMLButtonElement;
		await fireEvent.click(addBtn);
		expect(container.textContent).toContain('/new/dir');
	});

	it('disables the Save button until the draft is dirty', async () => {
		const { container } = renderSection(
			makeConfig({ allowed_directories: ['/existing'] }),
		);
		await expandSection(container);
		const buttons = Array.from(container.querySelectorAll('button'));
		const save = buttons.find((b) => b.textContent?.includes('Save Security')) as HTMLButtonElement;
		expect(save).toBeTruthy();
		expect(save.hasAttribute('disabled')).toBe(true);
	});

	it('enables Save and persists the security block via the dedicated endpoint', async () => {
		const { container } = renderSection(makeConfig({ allowed_directories: ['/existing'] }));
		await expandSection(container);
		const buttons = Array.from(container.querySelectorAll('button'));
		let save = buttons.find((b) => b.textContent?.includes('Save Security')) as HTMLButtonElement;
		expect(save).toBeTruthy();
		expect(save.hasAttribute('disabled')).toBe(true);

		const removeBtn = container.querySelector('button[aria-label^="Remove allowed directory"]') as HTMLButtonElement;
		expect(removeBtn).toBeTruthy();
		await fireEvent.click(removeBtn);

		save = Array.from(container.querySelectorAll('button')).find(
			(b) => b.textContent?.includes('Save Security'),
		) as HTMLButtonElement;
		await waitFor(() => expect(save.hasAttribute('disabled')).toBe(false));

		mockUpdateSecurityConfig.mockResolvedValue({
			security: {
				allowed_directories: [],
				denied_directories: [],
				max_files_per_scan: 0,
				scan_timeout_seconds: 0,
				allowed_origins: [],
				allow_unc: false,
				allowed_unc_servers: [],
				rate_limit: { requests_per_minute: 0 },
				trusted_proxies: [],
				force_secure_cookies: false,
			},
		});
		const successSpy = vi.spyOn(toastStore, 'success');
		await fireEvent.click(save);

		await waitFor(() => expect(mockUpdateSecurityConfig).toHaveBeenCalledTimes(1));
		expect(mockUpdateSecurityConfig).toHaveBeenCalledWith(
			expect.objectContaining({ allowed_directories: [] }),
		);
		await waitFor(() => expect(successSpy).toHaveBeenCalled());
	});

	it('toasts an error when the save fails', async () => {
		mockUpdateSecurityConfig.mockRejectedValue(new Error('server refused'));
		const errorSpy = vi.spyOn(toastStore, 'error');
		const { container } = renderSection(makeConfig({ allowed_directories: ['/existing'] }));
		await expandSection(container);

		const removeBtn = container.querySelector('button[aria-label^="Remove allowed directory"]') as HTMLButtonElement;
		await fireEvent.click(removeBtn);

		const save = Array.from(container.querySelectorAll('button')).find(
			(b) => b.textContent?.includes('Save Security'),
		) as HTMLButtonElement;
		await waitFor(() => expect(save.hasAttribute('disabled')).toBe(false));
		await fireEvent.click(save);

		await waitFor(() => expect(errorSpy).toHaveBeenCalled());
		expect(mockUpdateSecurityConfig).toHaveBeenCalledTimes(1);
	});

	it('rehydrates the draft from the fresh config query after a successful save', async () => {
		mockUpdateSecurityConfig.mockResolvedValue({
			security: {
				allowed_directories: ['/persisted'],
				denied_directories: [],
				max_files_per_scan: 0,
				scan_timeout_seconds: 0,
				allowed_origins: [],
				allow_unc: true,
				allowed_unc_servers: ['\\\\srv\\share'],
				rate_limit: { requests_per_minute: 0 },
				trusted_proxies: [],
				force_secure_cookies: false,
			},
		});
		mockGetConfig.mockResolvedValue(
			makeConfig({
				allowed_directories: ['/persisted'],
				allow_unc: true,
				allowed_unc_servers: ['\\\\srv\\share'],
			}) as unknown as SettingsConfig,
		);

		const { container } = renderSection(makeConfig({ allowed_directories: ['/existing'] }));
		await expandSection(container);

		const removeBtn = container.querySelector('button[aria-label^="Remove allowed directory"]') as HTMLButtonElement;
		await fireEvent.click(removeBtn);

		const save = Array.from(container.querySelectorAll('button')).find(
			(b) => b.textContent?.includes('Save Security'),
		) as HTMLButtonElement;
		await waitFor(() => expect(save.hasAttribute('disabled')).toBe(false));
		await fireEvent.click(save);

		await waitFor(() => expect(mockGetConfig).toHaveBeenCalledTimes(1));
		await waitFor(() => expect(container.textContent).toContain('/persisted'));
		await waitFor(() => expect(save.hasAttribute('disabled')).toBe(true));
	});

	it('propagates draft edits into the shared config so the top-level Save Changes persists them', async () => {
		// Regression: the section kept a local draft that was never written back
		// to the parent config, so the whole-config PUT ("Save Changes" button)
		// would clobber a just-added directory with the stale value. The sync
		// effect must mirror draft edits into config.api.security.
		const config = makeConfig({ allowed_directories: [] });
		const { container } = renderSection(config);
		await expandSection(container);

		const input = container.querySelector('input[placeholder*="Add a directory"]') as HTMLInputElement;
		expect(input).toBeTruthy();
		await fireEvent.input(input, { target: { value: '/mnt/videos' } });
		const addBtn = container.querySelector('button[title="Add allowed directory"]') as HTMLButtonElement;
		await fireEvent.click(addBtn);

		await waitFor(() =>
			expect(config.api.security.allowed_directories).toEqual(['/mnt/videos']),
		);
	});

});
