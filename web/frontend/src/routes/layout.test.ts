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

function findButton(container: HTMLElement, text: string): HTMLButtonElement {
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

beforeEach(() => {
	vi.clearAllMocks();
	apiClient.getAuthStatus.mockReset();
	apiClient.getAuthStatus.mockResolvedValue(uninitializedStatus());
	apiClient.getScrapers.mockResolvedValue(scrapersResponse());
	apiClient.getConfig.mockResolvedValue(freshConfig());
	apiClient.updateSecurityConfig.mockResolvedValue({ security: { allowed_directories: [] } } as unknown as Awaited<ReturnType<typeof apiClient.updateSecurityConfig>>);
	apiClient.request.mockResolvedValue({ message: 'ok' });
	toastStore.clear();
	vi.spyOn(toastStore, 'success');
	vi.spyOn(toastStore, 'info');
});

afterEach(() => {
	vi.restoreAllMocks();
});

describe('first-run setup wizard', () => {
	it('shows the credentials step before auth is initialized', async () => {
		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		expect(container.textContent).toContain('Admin Account');
		expect(findButton(container, 'Continue')).toBeTruthy();
	});

	it('creates the admin account and advances to the directories step', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await fillCredentials(container);

		await fireEvent.click(findButton(container, 'Continue'));

		await waitFor(() => expect(apiClient.setupAuth).toHaveBeenCalledTimes(1));
		await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));
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
		await fillCredentials(container);
		await fireEvent.click(findButton(container, 'Continue'));

		await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));
		expect(findButton(container, 'Back')).toBeUndefined();
	});

	it('stages directories and advances to the scrapers step without committing', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await fillCredentials(container);
		await fireEvent.click(findButton(container, 'Continue'));

		await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));

		const dirInput = container.querySelector(
			'input[placeholder*="Add a directory"]',
		) as HTMLInputElement;
		await fireEvent.input(dirInput, { target: { value: '/mnt/videos' } });
		await fireEvent.click(
			container.querySelector('button[title="Add allowed directory"]') as HTMLButtonElement,
		);
		await tick();

		await fireEvent.click(findButton(container, 'Continue'));

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
		await fillCredentials(container);
		await fireEvent.click(findButton(container, 'Continue'));

		await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));

		const dirInput = container.querySelector(
			'input[placeholder*="Add a directory"]',
		) as HTMLInputElement;
		await fireEvent.input(dirInput, { target: { value: '/mnt/videos' } });
		await fireEvent.click(
			container.querySelector('button[title="Add allowed directory"]') as HTMLButtonElement,
		);
		await tick();

		await fireEvent.click(findButton(container, 'Continue'));

		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		await waitFor(() => expect(apiClient.getScrapers).toHaveBeenCalledTimes(1));

		await fireEvent.click(findButton(container, 'Finish Setup'));

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
		await fillCredentials(container);
		await fireEvent.click(findButton(container, 'Continue'));

		await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));

		const dirInput = container.querySelector(
			'input[placeholder*="Add a directory"]',
		) as HTMLInputElement;
		await fireEvent.input(dirInput, { target: { value: '/mnt/videos' } });
		await fireEvent.keyDown(dirInput, { key: 'Enter' });
		await tick();

		await fireEvent.click(findButton(container, 'Continue'));
		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		await fireEvent.click(findButton(container, 'Finish Setup'));

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
		await fillCredentials(container);
		await fireEvent.click(findButton(container, 'Continue'));

		await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));

		await fireEvent.click(findButton(container, 'Skip for now'));

		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		expect(apiClient.updateSecurityConfig).not.toHaveBeenCalled();
	});

	it('shows a Back button on the scrapers step that returns to directories', async () => {
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('Create your admin account'));
		await fillCredentials(container);
		await fireEvent.click(findButton(container, 'Continue'));

		await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));
		await fireEvent.click(findButton(container, 'Continue'));

		await waitFor(() => expect(container.textContent).toContain('Choose your metadata sources'));
		const back = findButton(container, 'Back');
		expect(back).toBeTruthy();
		await fireEvent.click(back);

		await waitFor(() => expect(container.textContent).toContain('Point Javinizer at your library'));
	});
});
