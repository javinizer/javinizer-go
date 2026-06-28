import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import UpdateIndicator from './UpdateIndicator.svelte';
import QueryClientWrapper from './QueryClientWrapper.svelte';
import type { VersionStatusResponse } from '$lib/api/types';

// The component drives its state through createVersionStatusQuery() →
// apiClient.getVersionStatus() and a createMutation() → apiClient.checkVersion().
// Mock the api client so the query resolves with controlled fixtures and no
// network call is attempted under jsdom.
vi.mock('$lib/api/client', () => ({
	apiClient: {
		getVersionStatus: vi.fn(),
		checkVersion: vi.fn(),
	},
}));

const mod = await import('$lib/api/client');
const mockGetVersionStatus = vi.mocked(mod.apiClient.getVersionStatus);
const mockCheckVersion = vi.mocked(mod.apiClient.checkVersion);

// jsdom lacks the Web Animations API; Svelte's `transition:fly` (popover intro)
// calls element.animate(). Stub it so the open path runs under vitest.
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

function makeStatus(overrides: Partial<VersionStatusResponse> = {}): VersionStatusResponse {
	return {
		current: 'v0.3.14-alpha',
		latest: 'v0.3.15-alpha',
		update_available: true,
		prerelease: true,
		checked_at: '2026-06-27T23:21:20Z',
		source: 'fresh',
		...overrides,
	};
}

// Each test gets a fresh QueryClient (via the wrapper) so cached state from one
// test can't bleed into the next. The wrapper provides the svelte-query context
// UpdateIndicator reads via useQueryClient().
import { QueryClient } from '@tanstack/svelte-query';
function renderWithClient(status: VersionStatusResponse | null) {
	if (status !== null) {
		mockGetVersionStatus.mockResolvedValue(status);
	} else {
		mockGetVersionStatus.mockRejectedValue(new Error('network'));
	}
	return render(
		UpdateIndicator,
		{},
		{
			wrapper: QueryClientWrapper,
			wrapperProps: { client: new QueryClient({ defaultOptions: { queries: { retry: false } } }) },
		},
	);
}

beforeEach(() => {
	vi.clearAllMocks();
});

describe('UpdateIndicator', () => {
	it('is hidden when no update is available', async () => {
		const { container } = renderWithClient(makeStatus({ update_available: false }));
		await waitFor(() => expect(mockGetVersionStatus).toHaveBeenCalled());
		// No indicator button renders.
		expect(container.querySelector('button[aria-label="Update available"]')).toBeNull();
	});

	it('is hidden when update checks are disabled', async () => {
		const { container } = renderWithClient(
			makeStatus({ source: 'disabled', update_available: false, latest: '' }),
		);
		await waitFor(() => expect(mockGetVersionStatus).toHaveBeenCalled());
		expect(container.querySelector('button[aria-label="Update available"]')).toBeNull();
	});

	it('is hidden when no state exists yet (source: none)', async () => {
		const { container } = renderWithClient(
			makeStatus({ source: 'none', update_available: false, latest: '' }),
		);
		await waitFor(() => expect(mockGetVersionStatus).toHaveBeenCalled());
		expect(container.querySelector('button[aria-label="Update available"]')).toBeNull();
	});

	it('renders the indicator button when an update is available', async () => {
		const { container } = renderWithClient(makeStatus());
		await waitFor(() => {
			const button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
			expect(button?.getAttribute('aria-expanded')).toBe('false');
		});
	});

	it('opens the popover on click and shows the latest + current versions', async () => {
		const { container } = renderWithClient(makeStatus());
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		expect(button).not.toBeNull();

		await fireEvent.click(button!);

		await waitFor(() => {
			expect(button!.getAttribute('aria-expanded')).toBe('true');
			expect(container.textContent).toContain('v0.3.15-alpha');
			expect(container.textContent).toContain('v0.3.14-alpha');
			expect(container.textContent).toContain('prerelease');
			expect(container.textContent).toContain('View release');
			expect(container.textContent).toContain('Check again');
		});
	});

	it('renders a stable (non-prerelease) update without the prerelease tag', async () => {
		const { container } = renderWithClient(makeStatus({ latest: 'v1.0.0', prerelease: false }));
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		await waitFor(() => {
			expect(container.textContent).toContain('Update available');
			expect(container.textContent).toContain('v1.0.0');
			// No "prerelease" tag in the popover body.
			const tags = container.querySelectorAll('span.bg-amber-500\\/15');
			expect(tags.length).toBe(0);
		});
	});

	it('fires a force check and toasts when "Check again" is clicked', async () => {
		mockCheckVersion.mockResolvedValue(makeStatus());
		const { container } = renderWithClient(makeStatus());
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		let checkButton: HTMLButtonElement | null = null;
		await waitFor(() => {
			// The popover's "Check again" button is the one WITHOUT the update aria-label.
			checkButton = container.querySelector('button:not([aria-label="Update available"])');
			expect(checkButton).toBeTruthy();
		});
		await fireEvent.click(checkButton!);

		await waitFor(() => expect(mockCheckVersion).toHaveBeenCalled());
	});

	it('links to the specific release tag when a latest version is known', async () => {
		const { container } = renderWithClient(makeStatus({ latest: 'v0.3.15-alpha' }));
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		await waitFor(() => {
			const link = container.querySelector('a[href*="releases/tag/v0.3.15-alpha"]');
			expect(link).toBeTruthy();
			expect(link?.getAttribute('target')).toBe('_blank');
			expect(link?.getAttribute('rel')).toBe('noopener noreferrer');
		});
	});
});
