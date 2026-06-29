import { describe, it, expect, vi, beforeEach, afterAll, beforeAll } from 'vitest';
import { render, fireEvent, waitFor } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import BrowsePage from './+page.svelte';
import { goto as mockGoto } from '$app/navigation';

// STORAGE_KEY must mirror +page.svelte exactly. A Back round-trip from /manual
// preserves /browse selection because this snapshot is re-read on mount.
const STORAGE_KEY_SCRAPE_STATE = 'javinizer_browse_scrape_state';

// This project's jsdom env does not expose localStorage/sessionStorage on the
// global scope, but +page.svelte reads bare `localStorage`/`sessionStorage`
// without a typeof guard. Install a minimal Map-backed Storage shim so the
// component's $effects and the test's seed/read both work.
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

// jsdom lacks the Web Animations API; +page.svelte uses animate:flip and
// transition:fade/slide on mount-rendered blocks, so polyfill animate().
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

// Spies are created inside the factory (hoisting-safe) and captured below via
// dynamic import, matching the manual/page.test.ts pattern.
vi.mock('$lib/stores/pending-scrape.svelte', () => ({
	setPendingScrape: vi.fn(),
	clearPendingScrape: vi.fn(),
	buildPendingScrapeSnapshot: vi.fn((input: Record<string, unknown>) => ({ ...input, update: false }))
}));

const client = new QueryClient({ defaultOptions: { queries: { retry: false } } });
const mod = await import('$lib/api/client');
const apiClient = vi.mocked(mod.apiClient);
const pending = await import('$lib/stores/pending-scrape.svelte');
const pendingSet = vi.mocked(pending.setPendingScrape);

function mockScraper(name: string, enabled = true) {
	return {
		name,
		display_title: name,
		enabled
	};
}

function mockConfig() {
	return {
		output: {
			folder_format: '',
			file_format: '',
			subfolder_format: [],
			operation_mode: 'organize'
		},
		api: { security: { allowed_directories: [] } }
	};
}

function mockBrowseResponse(path: string) {
	return {
		current_path: path,
		parent_path: '',
		items: [
			{ name: 'a.mp4', path: `${path}/a.mp4`, is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' },
			{ name: 'b.mp4', path: `${path}/b.mp4`, is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' }
		]
	};
}

function renderPage() {
	return render(BrowsePage, {}, {
		wrapper: QueryClientWrapper,
		wrapperProps: { client }
	});
}

beforeEach(() => {
	vi.clearAllMocks();
	apiClient.getConfig.mockResolvedValue(mockConfig() as never);
	apiClient.getScrapers.mockResolvedValue([
		mockScraper('javbus', true),
		mockScraper('javdb', false)
	] as never);
	apiClient.getCurrentWorkingDirectory.mockResolvedValue({ path: '/library' } as never);
	apiClient.browse.mockResolvedValue(mockBrowseResponse('/library') as never);
	apiClient.batchScrape.mockResolvedValue({ job_id: 'job-1' } as never);
	sessionStorage.clear();
	localStorage.clear();
});

describe('/browse D4 — sessionStorage hydrate + Manual Scrape checkbox', () => {
	it('4.5 (P0-5): hydrates selectedFiles + globals from sessionStorage on mount (Back round-trip preserves selection)', async () => {
		const seed = {
			selectedFiles: ['/library/kept-a.mp4', '/library/kept-b.mp4'],
			operationMode: 'scrape',
			operationModeOverride: 'organize',
			operationModeOverrideTouched: false,
			forceRefresh: false,
			showScraperSelector: true,
			selectedScrapers: ['javbus'],
			selectedPreset: undefined,
			scalarStrategy: 'prefer-nfo',
			arrayStrategy: 'merge',
			manualScrapeMode: false
		};
		sessionStorage.setItem(STORAGE_KEY_SCRAPE_STATE, JSON.stringify(seed));

		const { findByText, getByText } = renderPage();

		// The hydrate $effect restores selectedFiles, so the "Selected Files"
		// card renders the pre-seeded paths — a Back round-trip preserves them.
		await findByText('2 Files Selected for Scraping');
		expect(getByText('kept-a.mp4')).toBeTruthy();
		expect(getByText('kept-b.mp4')).toBeTruthy();

		// manualScrapeMode:false ⇒ primary action stays the Scrape path.
		expect(getByText(/Scrape 2 Files/)).toBeTruthy();
		expect(pendingSet).not.toHaveBeenCalled();
	});

	it('4.6 (P1-26): toggling "Manual Scrape" flips the primary action, snapshots manualScrapeMode=true, and goto(\'/manual\')', async () => {
		// Pre-seed a single selection so the action button is enabled.
		sessionStorage.setItem(
			STORAGE_KEY_SCRAPE_STATE,
			JSON.stringify({
				selectedFiles: ['/library/pick.mp4'],
				operationMode: 'scrape',
				operationModeOverride: 'organize',
				operationModeOverrideTouched: false,
				forceRefresh: false,
				showScraperSelector: false,
				selectedScrapers: [],
				selectedPreset: undefined,
				scalarStrategy: 'prefer-nfo',
				arrayStrategy: 'merge',
				manualScrapeMode: false
			})
		);

		const { findByText, getByLabelText, getByText, getByRole } = renderPage();
		await findByText('pick.mp4');

		// Primary action is the Scrape path before toggling.
		expect(getByText(/Scrape 1 File/)).toBeTruthy();

		// Open the Options panel so the Manual Scrape checkbox is rendered.
		const optionsBtn = getByRole('button', { name: /Options/ });
		await fireEvent.click(optionsBtn);

		// Toggle the Manual Scrape checkbox on. Two option checkboxes share the
		// "Manual Scr..." prefix, so disambiguate by the unique description in the
		// checkbox's accessible name.
		const manualCheckbox = getByLabelText(/Review & override IDs/) as HTMLInputElement;
		expect(manualCheckbox.checked).toBe(false);
		await fireEvent.click(manualCheckbox);
		expect(manualCheckbox.checked).toBe(true);

		// (a) Primary action flips to the manual path.
		expect(getByText('Continue to manual review')).toBeTruthy();
		// A "Manual" active-option indicator badge appears.
		expect(getByText('Manual')).toBeTruthy();

		// (b) The persist $event wrote a snapshot with manualScrapeMode=true.
		const raw = sessionStorage.getItem(STORAGE_KEY_SCRAPE_STATE);
		expect(raw).not.toBeNull();
		const snapshot = JSON.parse(raw as string);
		expect(snapshot.manualScrapeMode).toBe(true);
		expect(snapshot.selectedFiles).toEqual(['/library/pick.mp4']);

		// (c) Clicking the primary action calls goto('/manual') with a snapshot.
		const actionBtn = getByText('Continue to manual review');
		await fireEvent.click(actionBtn);
		await waitFor(() => expect(pendingSet).toHaveBeenCalledTimes(1));
		expect(mockGoto).toHaveBeenCalledWith('/manual');
	});
});
