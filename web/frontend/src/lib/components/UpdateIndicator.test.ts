import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import UpdateIndicator from './UpdateIndicator.svelte';
import QueryClientWrapper from './QueryClientWrapper.svelte';
import type { VersionStatusResponse } from '$lib/api/types';
import { toastStore } from '$lib/stores/toast';

// The component drives its state through createVersionStatusQuery() →
// apiClient.getVersionStatus() and a createMutation() → apiClient.checkVersion().
// Mock the api client so the query resolves with controlled fixtures and no
// network call is attempted under jsdom.
vi.mock('$lib/api/client', () => ({
	apiClient: {
		getVersionStatus: vi.fn(),
		checkVersion: vi.fn(),
		upgradeDesktop: vi.fn(),
	},
}));

const mod = await import('$lib/api/client');
const mockGetVersionStatus = vi.mocked(mod.apiClient.getVersionStatus);
const mockCheckVersion = vi.mocked(mod.apiClient.checkVersion);
const mockUpgradeDesktop = vi.mocked(mod.apiClient.upgradeDesktop);

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
		install_environment: 'cli',
		// Mirrors the real API on a macOS/Linux host: Homebrew only makes
		// sense there; Windows hosts get a Scoop row instead (the backend
		// gates the package-manager row on runtime.GOOS — no host offers
		// both). Docker/desktop fixtures override this per test.
		upgrade_commands: [
			{ key: 'cli_binary', command: 'javinizer upgrade' },
			{ key: 'homebrew', command: 'brew upgrade javinizer' },
		],
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

afterEach(() => {
	vi.restoreAllMocks();
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

	it('renders one labeled row per CLI install method, each with its own copy button', async () => {
		const { container } = renderWithClient(makeStatus());
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		await waitFor(() => {
			const rows = container.querySelectorAll('[data-upgrade-command]');
			expect(rows.length).toBe(2);
			const keys = Array.from(rows).map((row) => row.getAttribute('data-upgrade-command'));
			expect(keys).toEqual(['cli_binary', 'homebrew']);
			// Each row carries its own copy affordance, labeled with its command.
			const copyButtons = container.querySelectorAll(
				'[data-upgrade-command] button[aria-label^="Copy"]',
			);
			expect(copyButtons.length).toBe(2);
			// The prose blob and its fake `sh` header are gone.
			expect(container.querySelector('pre')).toBeNull();
			expect(container.textContent).toContain('Update with one of these commands');
		});
	});

	it("copies exactly the clicked row's command (not the whole guidance)", async () => {
		const writeText = vi.fn().mockResolvedValue(undefined);
		Object.defineProperty(navigator, 'clipboard', {
			value: { writeText },
			configurable: true,
		});
		const { container } = renderWithClient(makeStatus());
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		let copyButton: HTMLButtonElement | null = null;
		await waitFor(() => {
			copyButton = container.querySelector('[data-upgrade-command="homebrew"] button');
			expect(copyButton).toBeTruthy();
		});
		await fireEvent.click(copyButton!);

		await waitFor(() => {
			expect(writeText).toHaveBeenCalledTimes(1);
			expect(writeText).toHaveBeenCalledWith('brew upgrade javinizer');
		});
		// Swap to the check icon confirms the copy landed.
		await waitFor(() => {
			expect(copyButton!.querySelector('svg.text-emerald-500')).toBeTruthy();
		});
	});

	it('toasts instead of crashing when the clipboard is unavailable', async () => {
		Object.defineProperty(navigator, 'clipboard', {
			value: { writeText: vi.fn().mockRejectedValue(new Error('denied')) },
			configurable: true,
		});
		const errorSpy = vi.spyOn(toastStore, 'error');
		const { container } = renderWithClient(makeStatus());
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		let copyButton: HTMLButtonElement | null = null;
		await waitFor(() => {
			copyButton = container.querySelector('[data-upgrade-command="cli_binary"] button');
			expect(copyButton).toBeTruthy();
		});
		await fireEvent.click(copyButton!);

		await waitFor(() => {
			expect(errorSpy).toHaveBeenCalledWith(expect.stringContaining('clipboard'));
		});
	});

	it('shows pull + compose command rows for Docker installs', async () => {
		// Docker guidance used to be hidden entirely ("docker users already
		// know to docker pull") because the old UI rendered it as a noisy prose
		// blob. As discrete, copyable command rows it earns its space: what you
		// copy is exactly what you paste.
		const { container } = renderWithClient(
			makeStatus({
				install_environment: 'docker',
				upgrade_commands: [
					{ key: 'docker_pull', command: 'docker pull ghcr.io/javinizer/javinizer-go:latest' },
					{ key: 'docker_compose', command: 'docker compose pull && docker compose up -d' },
				],
				prerelease: false,
				latest: 'v1.1.0',
			}),
		);
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		await waitFor(() => {
			// Environment badge still labels the install type.
			expect(container.textContent).toContain('Running in Docker');
			const pullRow = container.querySelector('[data-upgrade-command="docker_pull"]');
			const composeRow = container.querySelector('[data-upgrade-command="docker_compose"]');
			expect(pullRow).toBeTruthy();
			expect(composeRow).toBeTruthy();
			expect(pullRow!.textContent).toContain('docker pull ghcr.io/javinizer/javinizer-go:latest');
			expect(composeRow!.textContent).toContain('docker compose pull && docker compose up -d');
			// No prose instructions block / no fake shell snippet.
			expect(container.querySelector('pre')).toBeNull();
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
			// Pin by label: with per-row copy buttons also in the popover, the
			// old "first button without the aria-label" heuristic no longer
			// lands on "Check again".
			checkButton =
				Array.from(container.querySelectorAll<HTMLButtonElement>('[role="dialog"] button')).find(
					(b) => b.textContent?.includes('Check again'),
				) ?? null;
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

	it('shows an "Update & restart" button for desktop installs (instead of View release)', async () => {
		const { container } = renderWithClient(
			makeStatus({ install_environment: 'desktop', prerelease: false, latest: 'v1.2.0' }),
		);
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		await waitFor(() => {
			// Desktop replaces the releases-link CTA with an in-app upgrade button.
			const upgradeBtn = container.querySelector('button[aria-label="Update and restart"]');
			expect(upgradeBtn).toBeTruthy();
			expect(container.textContent).toContain('Update & restart');
			expect(container.querySelector('a[href*="releases"]')).toBeNull();
			// No terminal commands for desktop: the button IS the self-upgrade.
			expect(container.querySelector('[data-upgrade-commands]')).toBeNull();
		});
	});

	it('calls upgradeDesktop and enters a Restarting… state on click', async () => {
		mockUpgradeDesktop.mockResolvedValue({ status: 'relaunching', version: 'v1.2.0' });
		const { container } = renderWithClient(
			makeStatus({ install_environment: 'desktop', prerelease: false, latest: 'v1.2.0' }),
		);
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		let upgradeBtn: HTMLButtonElement | null = null;
		await waitFor(() => {
			upgradeBtn = container.querySelector('button[aria-label="Update and restart"]');
			expect(upgradeBtn).toBeTruthy();
		});
		await fireEvent.click(upgradeBtn!);

		await waitFor(() => {
			expect(mockUpgradeDesktop).toHaveBeenCalledWith({ force: false });
			// Spinner state: label flips to "Restarting…" and the button is disabled.
			expect(container.textContent).toContain('Restarting');
			expect(upgradeBtn!.hasAttribute('disabled')).toBe(true);
		});
		// On 200 the relaunch takes over — the spinner state is held, no revert.
		expect(mockUpgradeDesktop).toHaveBeenCalledTimes(1);
		expect(container.textContent).toContain('Restarting');
	});

	it('toasts the error and reverts to the button when the upgrade fails', async () => {
		mockUpgradeDesktop.mockRejectedValue(new Error('a bundle upgrade is already in progress'));
		const errorSpy = vi.spyOn(toastStore, 'error');
		const { container } = renderWithClient(
			makeStatus({ install_environment: 'desktop', prerelease: false, latest: 'v1.2.0' }),
		);
		let button: HTMLButtonElement | null = null;
		await waitFor(() => {
			button = container.querySelector('button[aria-label="Update available"]');
			expect(button).toBeTruthy();
		});
		await fireEvent.click(button!);

		let upgradeBtn: HTMLButtonElement | null = null;
		await waitFor(() => {
			upgradeBtn = container.querySelector('button[aria-label="Update and restart"]');
			expect(upgradeBtn).toBeTruthy();
		});
		await fireEvent.click(upgradeBtn!);

		await waitFor(() => {
			expect(mockUpgradeDesktop).toHaveBeenCalledWith({ force: false });
			expect(errorSpy).toHaveBeenCalled();
		});
		// Reverted: label is back to "Update & restart" and the button is re-enabled.
		await waitFor(() => {
			expect(container.textContent).toContain('Update & restart');
			expect(upgradeBtn!.hasAttribute('disabled')).toBe(false);
		});
		expect(errorSpy).toHaveBeenCalledWith(
			expect.stringContaining('a bundle upgrade is already in progress'),
		);
	});
});
