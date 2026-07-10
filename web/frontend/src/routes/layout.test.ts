import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import Layout from './+layout.svelte';
import { tick } from 'svelte';

vi.mock('$lib/api/client', () => ({
	apiClient: {
		getAuthStatus: vi.fn(),
		setupAuth: vi.fn(),
		loginAuth: vi.fn(),
		logoutAuth: vi.fn(),
		getConfig: vi.fn(),
		updateSecurityConfig: vi.fn(),
		getScrapers: vi.fn(),
		getCurrentWorkingDirectory: vi.fn(),
		request: vi.fn(),
	},
}));

vi.mock('$lib/stores/websocket', () => ({
	websocketStore: { connect: vi.fn(), disconnect: vi.fn() },
}));

vi.mock('$lib/stores/background-job.svelte', () => ({
	getBackgroundJobState: () => ({ jobId: null, showModal: false }),
	reopenModal: vi.fn(),
	dismiss: vi.fn(),
	closeModal: vi.fn(),
}));

vi.mock('$lib/stores/theme.svelte', () => ({
	getThemeStore: () => ({ initTheme: vi.fn(), destroyTheme: vi.fn() }),
}));

import { toastStore } from '$lib/stores/toast';

const mod = await import('$lib/api/client');
const apiClient = vi.mocked(mod.apiClient);

if (!Element.prototype.animate) {
	Element.prototype.animate = function () {
		const anim = {
			onfinish: null as (() => void) | null,
			oncancel: null as (() => void) | null,
			cancel() {},
			finish() {
				anim.onfinish?.();
			},
			addEventListener() {},
			removeEventListener() {},
		};
		queueMicrotask(() => anim.onfinish?.());
		return anim as unknown as Animation;
	};
}

// jsdom has no ResizeObserver; the wizard's stage-height measurement effect
// uses one, so polyfill it to avoid uncaught exceptions during render.
if (typeof globalThis.ResizeObserver === 'undefined') {
	globalThis.ResizeObserver = class {
		observe() {}
		unobserve() {}
		disconnect() {}
	} as unknown as typeof ResizeObserver;
}

function uninitializedStatus() {
	return { initialized: false, authenticated: false, username: null } as unknown as Awaited<
		ReturnType<typeof apiClient.getAuthStatus>
	>;
}

function authenticatedStatus() {
	return { initialized: true, authenticated: true, username: 'admin' } as unknown as Awaited<
		ReturnType<typeof apiClient.getAuthStatus>
	>;
}

function freshConfig(allowed: string[] = [], overrides: Record<string, unknown> = {}) {
	return {
		scrapers: { priority: [] },
		api: {
			security: {
				allowed_directories: allowed,
				denied_directories: [],
				allow_unc: false,
				allowed_unc_servers: [],
				...overrides,
			},
		},
	} as unknown as Awaited<ReturnType<typeof apiClient.getConfig>>;
}

function scrapersResponse() {
	return [
		{ name: 'r18', display_title: 'R18', enabled: true, options: {} },
		{ name: 'javlibrary', display_title: 'JavLibrary', enabled: true, options: {} },
	] as unknown as Awaited<ReturnType<typeof apiClient.getScrapers>>;
}

function findButton(container: HTMLElement, text: string): HTMLButtonElement | undefined {
	return Array.from(container.querySelectorAll('button')).find((b) =>
		b.textContent?.includes(text),
	) as HTMLButtonElement;
}

async function fillCredentials(container: HTMLElement) {
	const username = container.querySelector('input[placeholder="admin"]') as HTMLInputElement;
	const password = container.querySelector(
		'input[placeholder="At least 8 characters"]',
	) as HTMLInputElement;
	const confirm = container.querySelector(
		'input[placeholder="Re-enter password"]',
	) as HTMLInputElement;
	await fireEvent.input(username, { target: { value: 'admin' } });
	await fireEvent.input(password, { target: { value: 'password123' } });
	await fireEvent.input(confirm, { target: { value: 'password123' } });
	return { username, password, confirm };
}

// Register the admin account and dismiss the credentials-confirmation
// interstitial, landing on the directories step. Registration is now a
// two-click flow: Create admin account → Continue to library setup
// (acknowledges and advances).
async function proceedToDirectories(container: HTMLElement) {
	await fillCredentials(container);
	await fireEvent.click(findButton(container, 'Create admin account')!);
	await waitFor(() => expect(apiClient.setupAuth).toHaveBeenCalledTimes(1));
	await waitFor(() =>
		expect(container.textContent).toContain('Your admin account is secured'),
	);
	await fireEvent.click(findButton(container, 'Continue to library setup')!);
	await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));
}

beforeEach(() => {
	vi.clearAllMocks();
	localStorage.clear();
	apiClient.getAuthStatus.mockReset();
	apiClient.getAuthStatus.mockResolvedValue(uninitializedStatus());
	apiClient.getScrapers.mockResolvedValue(scrapersResponse());
	apiClient.getConfig.mockResolvedValue(freshConfig());
	apiClient.getCurrentWorkingDirectory.mockResolvedValue({ path: '' });
	apiClient.updateSecurityConfig.mockResolvedValue({ security: { allowed_directories: [] } } as unknown as Awaited<ReturnType<typeof apiClient.updateSecurityConfig>>);
	apiClient.request.mockResolvedValue({ message: 'ok' });
	toastStore.clear();
	vi.spyOn(toastStore, 'success');
	vi.spyOn(toastStore, 'info');
});

afterEach(() => {
	vi.restoreAllMocks();
	localStorage.clear();
});

describe('first-run setup wizard', () => {
	it('shows the credentials step before auth is initialized', async () => {
		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		expect(container.textContent).toContain('Admin Account');
		expect(findButton(container, 'Create admin account')).toBeTruthy();
	});

	it('creates the admin account and advances to the directories step', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);
		// directories/scrapers are staged — no config/security calls yet at step 1
		expect(apiClient.getConfig).not.toHaveBeenCalled();
		expect(apiClient.updateSecurityConfig).not.toHaveBeenCalled();
	});

	it('hides the Back button on the directories step (credentials are committed)', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);
		expect(findButton(container, 'Back')).toBeUndefined();
	});

	it('stages directories and advances to the scrapers step without committing', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);

		const dirInput = container.querySelector(
			'input[placeholder*="Add a directory"]',
		) as HTMLInputElement;
		await fireEvent.input(dirInput, { target: { value: '/mnt/videos' } });
		await fireEvent.click(
			container.querySelector('button[title="Add allowed directory"]') as HTMLButtonElement,
		);
		await tick();

		await fireEvent.click(findButton(container, 'Continue')!);

		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		// staged — nothing committed yet
		expect(apiClient.updateSecurityConfig).not.toHaveBeenCalled();
	});

	it('commits staged directories and scraper selection together on Finish', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getConfig.mockResolvedValue({
			scrapers: { priority: [] },
			api: {
				security: {
					allowed_directories: [],
					denied_directories: ['/sensitive'],
					allow_unc: true,
					allowed_unc_servers: ['\\\\srv\\share'],
				},
			},
		} as unknown as Awaited<ReturnType<typeof apiClient.getConfig>>);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);

		const dirInput = container.querySelector(
			'input[placeholder*="Add a directory"]',
		) as HTMLInputElement;
		await fireEvent.input(dirInput, { target: { value: '/mnt/videos' } });
		await fireEvent.click(
			container.querySelector('button[title="Add allowed directory"]') as HTMLButtonElement,
		);
		await tick();

		await fireEvent.click(findButton(container, 'Continue')!);

		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		await waitFor(() => expect(apiClient.getScrapers).toHaveBeenCalledTimes(1));

		await fireEvent.click(findButton(container, 'Finish Setup')!);

		await waitFor(() => expect(apiClient.updateSecurityConfig).toHaveBeenCalledTimes(1));
		expect(apiClient.updateSecurityConfig).toHaveBeenCalledWith(
			expect.objectContaining({
				allowed_directories: ['/mnt/videos'],
				denied_directories: ['/sensitive'],
				allow_unc: true,
				allowed_unc_servers: ['\\\\srv\\share'],
			}),
		);
		await waitFor(() => expect(apiClient.request).toHaveBeenCalledTimes(1));
		expect(apiClient.request).toHaveBeenCalledWith(
			'/api/v1/config',
			expect.objectContaining({ method: 'PUT' }),
		);
		const calls = (apiClient.request as unknown as { mock: { calls: unknown[][] } }).mock.calls;
		const payload = JSON.parse((calls[0][1] as { body: string }).body);
		expect(payload.scrapers.priority).toEqual(['r18', 'javlibrary']);
		expect(payload.scrapers.r18.enabled).toBe(true);
		expect(payload.scrapers.javlibrary.enabled).toBe(true);
		await waitFor(() => expect(vi.mocked(toastStore.success)).toHaveBeenCalled());
	});

	it('commits a typed directory on Enter before finishing', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);

		const dirInput = container.querySelector(
			'input[placeholder*="Add a directory"]',
		) as HTMLInputElement;
		await fireEvent.input(dirInput, { target: { value: '/mnt/videos' } });
		await fireEvent.keyDown(dirInput, { key: 'Enter' });
		await tick();

		await fireEvent.click(findButton(container, 'Continue')!);
		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		await fireEvent.click(findButton(container, 'Finish Setup')!);

		await waitFor(() => expect(apiClient.updateSecurityConfig).toHaveBeenCalledTimes(1));
		expect(apiClient.updateSecurityConfig).toHaveBeenCalledWith(
			expect.objectContaining({ allowed_directories: ['/mnt/videos'] }),
		);
	});

	it('skips the directories step and still advances to scrapers', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);

		await fireEvent.click(findButton(container, 'Skip for now')!);

		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		expect(apiClient.updateSecurityConfig).not.toHaveBeenCalled();
	});

	it('shows a Back button on the scrapers step that returns to directories', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);
		await fireEvent.click(findButton(container, 'Continue')!);

		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		const back = findButton(container, 'Back');
		expect(back).toBeTruthy();
		await fireEvent.click(back!);

		await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));
	});

	it('pre-fills the directories list with a sensible default path after registration', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getCurrentWorkingDirectory.mockResolvedValue({ path: '/home/test/Videos' });

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);

		await waitFor(() =>
			expect(container.textContent).toContain('/home/test/Videos'),
		);
		// The pre-filled path is staged as the default (first) allowed directory.
		expect(apiClient.getCurrentWorkingDirectory).toHaveBeenCalledTimes(1);
	});

	it('commits the pre-filled default directory on Finish', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getCurrentWorkingDirectory.mockResolvedValue({ path: '/home/test/Videos' });

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);
		await waitFor(() =>
			expect(container.textContent).toContain('/home/test/Videos'),
		);

		await fireEvent.click(findButton(container, 'Continue')!);
		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		await waitFor(() => expect(apiClient.getScrapers).toHaveBeenCalledTimes(1));
		await fireEvent.click(findButton(container, 'Finish Setup')!);

		await waitFor(() => expect(apiClient.updateSecurityConfig).toHaveBeenCalledTimes(1));
		expect(apiClient.updateSecurityConfig).toHaveBeenCalledWith(
			expect.objectContaining({ allowed_directories: ['/home/test/Videos'] }),
		);
	});

	it('leaves the directories list empty when /cwd returns an empty path', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getCurrentWorkingDirectory.mockResolvedValue({ path: '' });

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await proceedToDirectories(container);

		await waitFor(() => expect(apiClient.getCurrentWorkingDirectory).toHaveBeenCalledTimes(1));
		expect(container.textContent).toContain('No directories added yet');
		expect(container.textContent).not.toContain('/home');
	});

	it('clears all localStorage and cookies when the wizard is shown (first-run)', async () => {
		localStorage.setItem('javinizer_test_stale', 'stale');
		localStorage.setItem('javinizer_input_path', '/old/path');
		localStorage.setItem('javinizer_session', 'old-session-id');
		expect(localStorage.getItem('javinizer_test_stale')).toBe('stale');

		document.cookie = 'javinizer_test=stale; path=/';
		expect(document.cookie).toContain('javinizer_test');

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));

		expect(localStorage.getItem('javinizer_test_stale')).toBeNull();
		expect(localStorage.getItem('javinizer_input_path')).toBeNull();
		expect(localStorage.getItem('javinizer_session')).toBeNull();
		expect(localStorage.length).toBe(0);
		expect(document.cookie).not.toContain('javinizer_test');
	});

	it('does not clear localStorage when the app is already initialized', async () => {
		apiClient.getAuthStatus.mockResolvedValue({ initialized: true, authenticated: false, username: null } as unknown as Awaited<ReturnType<typeof apiClient.getAuthStatus>>);
		localStorage.setItem('javinizer_test_keep', 'keep');

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Login Required'));

		expect(localStorage.getItem('javinizer_test_keep')).toBe('keep');
	});
});

	it('creates the admin account when submitting the credentials form', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await fillCredentials(container);

		const form = container.querySelector('#credentials-form') as HTMLFormElement;
		await fireEvent.submit(form);

		await waitFor(() => expect(apiClient.setupAuth).toHaveBeenCalledTimes(1));
		await waitFor(() =>
			expect(container.textContent).toContain('Your admin account is secured'),
		);
	});
