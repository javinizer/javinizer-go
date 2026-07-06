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

function freshConfig(allowed: string[] = []) {
	return {
		api: {
			security: {
				allowed_directories: allowed,
				denied_directories: [],
				allow_unc: false,
				allowed_unc_servers: [],
			},
		},
	} as unknown as Awaited<ReturnType<typeof apiClient.getConfig>>;
}

function securityResponse(allowed: string[] = [], overrides: Record<string, unknown> = {}) {
	return {
		security: {
			allowed_directories: allowed,
			denied_directories: [],
			allow_unc: false,
			allowed_unc_servers: [],
			...overrides,
		},
	} as unknown as Awaited<ReturnType<typeof apiClient.updateSecurityConfig>>;
}

beforeEach(() => {
	vi.clearAllMocks();
	apiClient.getAuthStatus.mockResolvedValue(uninitializedStatus());
	toastStore.clear();
	vi.spyOn(toastStore, 'success');
	vi.spyOn(toastStore, 'info');
});

afterEach(() => {
	vi.restoreAllMocks();
});

describe('first-run setup allowed directories step', () => {
	it('shows the credentials form before auth is initialized', async () => {
		const { container, getByText } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('First-Time Setup'));
		expect(container.textContent).toContain('Create the default username and password');
		expect(getByText('Create Credentials')).toBeTruthy();
	});

	it('transitions to the allowed directories step after credentials are created', async () => {
		apiClient.getAuthStatus
			.mockResolvedValueOnce(uninitializedStatus())
			.mockResolvedValueOnce(authenticatedStatus());
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getConfig.mockResolvedValue(freshConfig());
		apiClient.updateSecurityConfig.mockResolvedValue(securityResponse());

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('First-Time Setup'));

		const username = container.querySelector('#setup-username') as HTMLInputElement;
		const password = container.querySelector('#setup-password') as HTMLInputElement;
		const confirm = container.querySelector('#setup-password-confirm') as HTMLInputElement;
		await fireEvent.input(username, { target: { value: 'admin' } });
		await fireEvent.input(password, { target: { value: 'password123' } });
		await fireEvent.input(confirm, { target: { value: 'password123' } });

		const form = username.closest('form') as HTMLFormElement;
		await fireEvent.submit(form);

		await waitFor(() => expect(container.textContent).toContain('Add Allowed Directories'));
		expect(container.textContent).toContain('you can change this later in Settings');
		expect(apiClient.getConfig).toHaveBeenCalledTimes(1);
		expect(apiClient.updateSecurityConfig).not.toHaveBeenCalled();
	});

	it('persists allowed directories via the security endpoint on submit', async () => {
		apiClient.getAuthStatus
			.mockResolvedValueOnce(uninitializedStatus())
			.mockResolvedValueOnce(authenticatedStatus());
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getConfig.mockResolvedValue(freshConfig());
		apiClient.updateSecurityConfig.mockResolvedValue(securityResponse(['/mnt/videos']));

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('First-Time Setup'));

		const username = container.querySelector('#setup-username') as HTMLInputElement;
		const password = container.querySelector('#setup-password') as HTMLInputElement;
		const confirm = container.querySelector('#setup-password-confirm') as HTMLInputElement;
		await fireEvent.input(username, { target: { value: 'admin' } });
		await fireEvent.input(password, { target: { value: 'password123' } });
		await fireEvent.input(confirm, { target: { value: 'password123' } });
		await fireEvent.submit(username.closest('form') as HTMLFormElement);

		await waitFor(() => expect(container.textContent).toContain('Add Allowed Directories'));

		const dirInput = container.querySelector(
			'input[placeholder*="Add a directory"]',
		) as HTMLInputElement;
		expect(dirInput).toBeTruthy();
		await fireEvent.input(dirInput, { target: { value: '/mnt/videos' } });
		const addBtn = container.querySelector(
			'button[title="Add allowed directory"]',
		) as HTMLButtonElement;
		await fireEvent.click(addBtn);
		await tick();

		const save = Array.from(container.querySelectorAll('button')).find((b) =>
			b.textContent?.includes('Save & Continue'),
		) as HTMLButtonElement;
		expect(save).toBeTruthy();
		await fireEvent.click(save);

		await waitFor(() => expect(apiClient.updateSecurityConfig).toHaveBeenCalledTimes(1));
		expect(apiClient.updateSecurityConfig).toHaveBeenCalledWith(
			expect.objectContaining({
				allowed_directories: ['/mnt/videos'],
				denied_directories: [],
				allow_unc: false,
				allowed_unc_servers: [],
			}),
		);
		await waitFor(() => expect(vi.mocked(toastStore.success)).toHaveBeenCalled());
	});

	it('commits a typed directory on Enter without clicking the add button', async () => {
		apiClient.getAuthStatus
			.mockResolvedValueOnce(uninitializedStatus())
			.mockResolvedValueOnce(authenticatedStatus());
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getConfig.mockResolvedValue(freshConfig());
		apiClient.updateSecurityConfig.mockResolvedValue(securityResponse(['/mnt/videos']));

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('First-Time Setup'));

		const username = container.querySelector('#setup-username') as HTMLInputElement;
		const password = container.querySelector('#setup-password') as HTMLInputElement;
		const confirm = container.querySelector('#setup-password-confirm') as HTMLInputElement;
		await fireEvent.input(username, { target: { value: 'admin' } });
		await fireEvent.input(password, { target: { value: 'password123' } });
		await fireEvent.input(confirm, { target: { value: 'password123' } });
		await fireEvent.submit(username.closest('form') as HTMLFormElement);

		await waitFor(() => expect(container.textContent).toContain('Add Allowed Directories'));

		const dirInput = container.querySelector(
			'input[placeholder*="Add a directory"]',
		) as HTMLInputElement;
		expect(dirInput).toBeTruthy();
		await fireEvent.input(dirInput, { target: { value: '/mnt/videos' } });
		await fireEvent.keyDown(dirInput, { key: 'Enter' });
		await tick();

		const save = Array.from(container.querySelectorAll('button')).find((b) =>
			b.textContent?.includes('Save & Continue'),
		) as HTMLButtonElement;
		await fireEvent.click(save);

		await waitFor(() =>
			expect(apiClient.updateSecurityConfig).toHaveBeenCalledWith(
				expect.objectContaining({ allowed_directories: ['/mnt/videos'] }),
			),
		);
	});

	it('sends all four security fields including current defaults', async () => {
		apiClient.getAuthStatus
			.mockResolvedValueOnce(uninitializedStatus())
			.mockResolvedValueOnce(authenticatedStatus());
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getConfig.mockResolvedValue({
			api: {
				security: {
					allowed_directories: [],
					denied_directories: ['/sensitive'],
					allow_unc: true,
					allowed_unc_servers: ['\\\\srv\\share'],
				},
			},
		} as unknown as Awaited<ReturnType<typeof apiClient.getConfig>>);
		apiClient.updateSecurityConfig.mockResolvedValue(
			securityResponse(['/mnt/videos'], {
				denied_directories: ['/sensitive'],
				allow_unc: true,
				allowed_unc_servers: ['\\\\srv\\share'],
			}),
		);

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('First-Time Setup'));

		const username = container.querySelector('#setup-username') as HTMLInputElement;
		const password = container.querySelector('#setup-password') as HTMLInputElement;
		const confirm = container.querySelector('#setup-password-confirm') as HTMLInputElement;
		await fireEvent.input(username, { target: { value: 'admin' } });
		await fireEvent.input(password, { target: { value: 'password123' } });
		await fireEvent.input(confirm, { target: { value: 'password123' } });
		await fireEvent.submit(username.closest('form') as HTMLFormElement);

		await waitFor(() => expect(container.textContent).toContain('Add Allowed Directories'));

		const dirInput = container.querySelector(
			'input[placeholder*="Add a directory"]',
		) as HTMLInputElement;
		await fireEvent.input(dirInput, { target: { value: '/mnt/videos' } });
		await fireEvent.click(
			container.querySelector('button[title="Add allowed directory"]') as HTMLButtonElement,
		);
		await tick();

		const save = Array.from(container.querySelectorAll('button')).find((b) =>
			b.textContent?.includes('Save & Continue'),
		) as HTMLButtonElement;
		await fireEvent.click(save);

		await waitFor(() => expect(apiClient.updateSecurityConfig).toHaveBeenCalledTimes(1));
		expect(apiClient.updateSecurityConfig).toHaveBeenCalledWith(
			expect.objectContaining({
				allowed_directories: ['/mnt/videos'],
				denied_directories: ['/sensitive'],
				allow_unc: true,
				allowed_unc_servers: ['\\\\srv\\share'],
			}),
		);
	});

	it('skips the step without calling the security endpoint and toasts guidance', async () => {
		apiClient.getAuthStatus
			.mockResolvedValueOnce(uninitializedStatus())
			.mockResolvedValueOnce(authenticatedStatus());
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getConfig.mockResolvedValue(freshConfig());

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('First-Time Setup'));

		const username = container.querySelector('#setup-username') as HTMLInputElement;
		const password = container.querySelector('#setup-password') as HTMLInputElement;
		const confirm = container.querySelector('#setup-password-confirm') as HTMLInputElement;
		await fireEvent.input(username, { target: { value: 'admin' } });
		await fireEvent.input(password, { target: { value: 'password123' } });
		await fireEvent.input(confirm, { target: { value: 'password123' } });
		await fireEvent.submit(username.closest('form') as HTMLFormElement);

		await waitFor(() => expect(container.textContent).toContain('Add Allowed Directories'));

		const skipBtn = Array.from(container.querySelectorAll('button')).find((b) =>
			b.textContent?.includes('Skip for now'),
		) as HTMLButtonElement;
		expect(skipBtn).toBeTruthy();
		await fireEvent.click(skipBtn);

		await waitFor(() =>
			expect(vi.mocked(toastStore.info)).toHaveBeenCalledWith(
				expect.stringContaining('Settings → Security'),
				expect.any(Number),
			),
		);
		await waitFor(() => expect(apiClient.updateSecurityConfig).not.toHaveBeenCalled());
		expect(vi.mocked(toastStore.info)).toHaveBeenCalled();
	});

	it('surfaces a visible error when config loading fails but keeps the dirs gate stable', async () => {
		apiClient.getAuthStatus
			.mockResolvedValueOnce(uninitializedStatus())
			.mockResolvedValueOnce(authenticatedStatus());
		apiClient.setupAuth.mockResolvedValue(
			authenticatedStatus() as unknown as Awaited<ReturnType<typeof apiClient.setupAuth>>,
		);
		apiClient.getConfig.mockRejectedValue(new Error('network down'));

		const { container } = render(Layout);
		await waitFor(() => expect(container.textContent).toContain('First-Time Setup'));

		const username = container.querySelector('#setup-username') as HTMLInputElement;
		const password = container.querySelector('#setup-password') as HTMLInputElement;
		const confirm = container.querySelector('#setup-password-confirm') as HTMLInputElement;
		await fireEvent.input(username, { target: { value: 'admin' } });
		await fireEvent.input(password, { target: { value: 'password123' } });
		await fireEvent.input(confirm, { target: { value: 'password123' } });
		await fireEvent.submit(username.closest('form') as HTMLFormElement);

		await waitFor(() => expect(container.textContent).toContain('Add Allowed Directories'));
		expect(container.textContent).toContain('Failed to load current security settings');
		expect(container.textContent).toContain('network down');
		expect(apiClient.getConfig).toHaveBeenCalledTimes(1);
		expect(apiClient.updateSecurityConfig).not.toHaveBeenCalled();
	});
});
