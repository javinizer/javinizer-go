import { describe, it, expect, vi, beforeEach, afterEach, afterAll, beforeAll } from 'vitest';
import { cleanup, render, fireEvent, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import BrowsePage from './+page.svelte';
import pageSource from './+page.svelte?raw';
import { readFileSync } from 'node:fs';

const appStyles = readFileSync('src/app.css', 'utf8');
import { BrowseBootstrapCookie, decodeBrowseBootstrap, type BrowseBootstrap } from '$lib/browse-bootstrap';

function createStorage(): Storage {
	let store = new Map<string, string>();
	return {
		get length() { return store.size; },
		clear: () => { store.clear(); },
		getItem: (k: string) => (store.has(k) ? store.get(k) as string : null),
		key: (i: number) => Array.from(store.keys())[i] ?? null,
		removeItem: (k: string) => { store.delete(k); },
		setItem: (k: string, v: string) => { store.set(k, String(v)); }
	};
}

const savedSession = (globalThis as { sessionStorage?: Storage }).sessionStorage;
const savedLocal = (globalThis as { localStorage?: Storage }).localStorage;

beforeAll(() => {
	(globalThis as { sessionStorage?: Storage }).sessionStorage = createStorage();
	(globalThis as { localStorage?: Storage }).localStorage = createStorage();
});

// jsdom lacks matchMedia — Svelte's MediaQuery (used for prefers-reduced-motion) needs it.
if (!window.matchMedia) {
	window.matchMedia = (q: string) => ({ matches: false, media: q, onchange: null, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false });
}
const savedAnimate = window.Element.prototype.animate;
window.Element.prototype.animate = function () {
	return {
		onfinish: null as ((this: unknown, ev: AnimationPlaybackEvent) => unknown) | null,
		oncancel: null as ((this: unknown, ev: AnimationPlaybackEvent) => unknown) | null,
		cancel() {},
		finish() {},
		play() {},
		pause() {},
		currentTime: 0
	} as unknown as Animation;
};

afterAll(() => {
	window.Element.prototype.animate = savedAnimate;
	(globalThis as { sessionStorage?: Storage }).sessionStorage = savedSession;
	(globalThis as { localStorage?: Storage }).localStorage = savedLocal;
});

vi.mock('$lib/api/client', () => ({
	apiClient: {
		getConfig: vi.fn(),
		getScrapers: vi.fn(),
		getCurrentWorkingDirectory: vi.fn(),
		browse: vi.fn(),
		batchScrape: vi.fn()
	}
}));

vi.mock('$lib/stores/background-job.svelte', () => ({
	startJob: vi.fn()
}));

vi.mock('$lib/stores/pending-scrape.svelte', () => ({
	setPendingScrape: vi.fn(),
	clearPendingScrape: vi.fn(),
	buildPendingScrapeSnapshot: vi.fn((input: Record<string, unknown>) => ({ ...input, update: false }))
}));

const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
const mod = await import('$lib/api/client');
const apiClient = vi.mocked(mod.apiClient);

function renderPage(props: Record<string, unknown> = {}) {
	return render(BrowsePage, props, {
		wrapper: QueryClientWrapper,
		wrapperProps: { client }
	});
}

afterEach(() => cleanup());

beforeEach(() => {
	vi.clearAllMocks();
	apiClient.getConfig.mockResolvedValue({
		output: { folder_format: '', file_format: '', subfolder_format: [], operation_mode: 'organize' },
		api: { security: { allowed_directories: [] } }
	} as never);
	apiClient.getScrapers.mockResolvedValue([] as never);
	apiClient.getCurrentWorkingDirectory.mockResolvedValue({ path: '/library' } as never);
	apiClient.browse.mockResolvedValue({ current_path: '/library', parent_path: '', items: [] } as never);
	apiClient.batchScrape.mockResolvedValue({ job_id: 'job-1' } as never);
	sessionStorage.clear();
	localStorage.clear();
	document.cookie = `${BrowseBootstrapCookie}=; Path=/; Max-Age=0`;
});

describe('/browse action plan — decision workflow structure', () => {
	it('renders the plan as a numbered rail ending in a live tear-off brief', async () => {
		const { container, findByRole, getByText } = renderPage();
		await findByRole('radio', { name: /Organize into another location/ });
		for (const step of ['01', '02', '03']) {
			expect(getByText(step)).toBeTruthy();
		}
		expect(container.querySelectorAll('div[class*="sm:grid-cols"]').length).toBeGreaterThanOrEqual(3);
		const brief = container.querySelector('[aria-live="polite"]');
		expect(brief).toBeTruthy();
		expect(brief?.className).toContain('border-dashed');
		expect(brief?.querySelector('ul')?.className).toContain('font-mono');
	});

	it('shows the destination step only for organize and the merge step only for leave-in-place', async () => {
		const { findByRole, getByText, queryByText, getByLabelText } = renderPage();
		await findByRole('radio', { name: /Organize into another location/ });
		expect(getByLabelText('Output Destination')).toBeTruthy();
		expect(queryByText('Existing metadata merge')).toBeNull();

		await fireEvent.click(await findByRole('radio', { name: /Leave video files in place/ }));
		await waitFor(() => expect(getByText('Existing metadata merge')).toBeTruthy());
		expect(queryByText('Output Destination')).toBeNull();
	});

	it('offers metadata-artwork as a distinct non-merge operation', async () => {
		const { findByRole, getByText, queryByText } = renderPage();
		await fireEvent.click(await findByRole('radio', { name: /Metadata and artwork only/ }));
		await waitFor(() => expect(getByText('Update metadata and artwork only')).toBeTruthy());
		expect(queryByText('Existing metadata merge')).toBeNull();
	});

	it('drives NFO and media policies through native segmented radio groups', async () => {
		const { findByRole, getByRole, getByText } = renderPage();
		await findByRole('radio', { name: /Organize into another location/ });
		const skipNfo = getByRole('radio', { name: 'Skip NFO' }) as HTMLInputElement;
		expect(skipNfo.type).toBe('radio');
		expect(skipNfo.name).toBe('nfo-output');
		await fireEvent.click(skipNfo);
		await waitFor(() => expect(getByText('Skip NFO output')).toBeTruthy());
	});

	it('gates Replace existing behind leave-in-place and marks it destructive in the brief', async () => {
		const { findByRole, getByRole, queryByRole, getByText } = renderPage();
		await findByRole('radio', { name: /Organize into another location/ });
		expect(queryByRole('radio', { name: 'Replace existing' })).toBeNull();

		await fireEvent.click(getByRole('radio', { name: /Leave video files in place/ }));
		const replace = (await findByRole('radio', { name: 'Replace existing' })) as HTMLInputElement;
		expect(replace.name).toBe('media-policy');
		await fireEvent.click(replace);
		await waitFor(() => expect(getByText('Replace existing enabled media (destructive)', { exact: true })).toBeTruthy());
	}, 10_000);

	it('applies merge presets via pressed buttons and keeps advanced selects in sync', async () => {
		const { findByRole, getByRole, getByLabelText } = renderPage();
		await fireEvent.click(await findByRole('radio', { name: /Leave video files in place/ }));
		const aggressive = await findByRole('button', { name: /Aggressive/ });
		expect(aggressive.getAttribute('aria-pressed')).toBe('false');
		await fireEvent.click(aggressive);
		expect(aggressive.getAttribute('aria-pressed')).toBe('true');
		expect((getByLabelText(/Scalar Fields/) as HTMLSelectElement).value).toBe('prefer-scraper');
		expect((getByLabelText(/Array Fields/) as HTMLSelectElement).value).toBe('replace');
	});

it('opens Options as an anchored popover and returns focus on Escape', async () => {
		const { findByRole, getByRole } = renderPage();
		await findByRole('radio', { name: /Organize into another location/ });
		const trigger = getByRole('button', { name: /^Options$/ });
		expect(trigger.getAttribute('aria-expanded')).toBe('false');
		await fireEvent.click(trigger);
		expect(trigger.getAttribute('aria-expanded')).toBe('true');
		const panel = getByRole('region', { name: 'Options' });
		expect(panel.className).toContain('absolute');
		expect(panel.className).toContain('bottom-full');
		expect(getByRole('button', { name: 'Close' })).toBeTruthy();
		await fireEvent.keyDown(window, { key: 'Escape' });
		await waitFor(() => expect(trigger.getAttribute('aria-expanded')).toBe('false'));
		expect(document.activeElement).toBe(trigger);
	});
	it('collapses to a persisted manifest strip and expands again', async () => {
		const { container, findByRole, getByRole } = renderPage();
		await findByRole('radio', { name: /Organize into another location/ });
		const collapse = getByRole('button', { name: 'Collapse plan' });
		expect(collapse.getAttribute('aria-expanded')).toBe('true');
		expect(getByRole('region', { name: 'Operation plan' })).toBeTruthy();
		await fireEvent.click(collapse);
		const expand = getByRole('button', { name: 'Expand plan' });
		expect(expand.getAttribute('aria-expanded')).toBe('false');
		expect(container.querySelector('span[title*="Organize videos into another location"]')).toBeTruthy();
		await waitFor(() => {
			const stored = JSON.parse(sessionStorage.getItem('javinizer_browse_scrape_state') ?? '{}');
			expect(stored.planExpanded).toBe(false);
			const encoded = document.cookie.split('; ').find((value) => value.startsWith(`${BrowseBootstrapCookie}=`))?.split('=')[1];
			expect(encoded ? decodeBrowseBootstrap(encoded)?.planExpanded : undefined).toBe(false);
		});
		await fireEvent.click(expand);
		expect(getByRole('button', { name: 'Collapse plan' }).getAttribute('aria-expanded')).toBe('true');
	});

	it('renders a persisted collapsed plan before hydration', () => {
		const bootstrap: BrowseBootstrap = {
			version: 1,
			applyPlan: null,
			initialPath: '',
			destinationPath: '',
			forceRefresh: false,
			showScraperSelector: false,
			selectedScrapers: [],
			manualScrapeMode: false,
			planExpanded: false
		};
		const { getByRole, queryByRole } = renderPage({ data: { browseBootstrap: bootstrap } });
		expect(getByRole('button', { name: 'Expand plan' }).getAttribute('aria-expanded')).toBe('false');
		expect(queryByRole('region', { name: 'Operation plan' })).toBeNull();
	});

	it('keeps disclosure accessibility and motion contracts in source', () => {
		expect(pageSource).toContain('aria-controls="apply-plan-body"');
		expect(pageSource).toContain('role="region"');
		expect(pageSource).toContain('transition:slide|local');
		expect(pageSource).toContain('w-36 justify-between');
		expect(pageSource).toContain('border-transparent');
		expect(pageSource).toContain('in:fade|local');
		expect(appStyles).toContain('scrollbar-gutter: stable');
	});

	it('keeps responsive-critical classes for narrow and wide layouts', () => {
		expect(pageSource).toContain('flex-col gap-1 rounded-lg border border-border bg-muted-soft p-1 sm:flex-row');
		expect(pageSource).toContain('sm:grid-cols-[1.75rem_minmax(0,1fr)]');
		expect(pageSource).toContain('flex flex-col gap-2 sm:flex-row');
		expect(pageSource).toContain('grid gap-4 lg:grid-cols-2');
		expect(pageSource).toContain('@media (prefers-reduced-motion: reduce)');
	});
});