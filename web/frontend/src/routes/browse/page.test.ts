import { describe, it, expect, vi, beforeEach, afterEach, afterAll, beforeAll } from 'vitest';
import { cleanup, render, fireEvent, waitFor, within } from '@testing-library/svelte';
import { QueryClient } from '@tanstack/svelte-query';
import QueryClientWrapper from '$lib/components/QueryClientWrapper.svelte';
import BrowsePage from './+page.svelte';
import { goto as mockGoto } from '$app/navigation';
import { decodeBrowseBootstrap } from '$lib/browse-bootstrap';

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
// jsdom lacks matchMedia — Svelte's MediaQuery (used for prefers-reduced-motion) needs it.
if (!window.matchMedia) {
	window.matchMedia = (q: string) => ({ matches: false, media: q, onchange: null, addEventListener: () => {}, removeEventListener: () => {}, addListener: () => {}, removeListener: () => {}, dispatchEvent: () => false });
}
const savedAnimate = window.Element.prototype.animate;
const savedGetAnimations = window.Element.prototype.getAnimations;
window.Element.prototype.getAnimations = function () {
	return [];
};
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
	window.Element.prototype.getAnimations = savedGetAnimations;
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

function renderPage(props: Record<string, unknown> = {}) {
	return render(BrowsePage, props, {
		wrapper: QueryClientWrapper,
		wrapperProps: { client }
	});
}

afterEach(() => cleanup());

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
	it('migrates a legacy update snapshot without treating its hidden organize value as contradictory', async () => {
		sessionStorage.setItem(STORAGE_KEY_SCRAPE_STATE, JSON.stringify({ operationMode: 'update', operationModeOverride: 'organize', operationModeOverrideTouched: false, selectedFiles: [], selectedScrapers: [], scalarStrategy: 'prefer-nfo', arrayStrategy: 'merge' }));
		const { findByText } = renderPage();
		expect(await findByText('Leave video files in place', { exact: true })).toBeTruthy();
	});

	it('uses configured effective mode for an untouched legacy scrape snapshot', async () => {
		apiClient.getConfig.mockResolvedValue({ ...mockConfig(), output: { ...mockConfig().output, operation_mode: 'in-place' } } as never);
		localStorage.setItem('javinizer_input_path', '/library');
		localStorage.setItem('javinizer_output_path', '/other');
		sessionStorage.setItem(STORAGE_KEY_SCRAPE_STATE, JSON.stringify({ operationMode: 'scrape', operationModeOverride: 'organize', operationModeOverrideTouched: false, selectedFiles: [], selectedScrapers: [] }));
		const { findByText } = renderPage();
		expect(await findByText('Rename in place', { exact: true })).toBeTruthy();
	});

	it('restores the legacy implied rename-file mode for same-source output', async () => {
		localStorage.setItem('javinizer_input_path', '/library');
		localStorage.setItem('javinizer_output_path', '/library');
		sessionStorage.setItem(STORAGE_KEY_SCRAPE_STATE, JSON.stringify({ operationMode: 'scrape', operationModeOverride: 'organize', operationModeOverrideTouched: false, selectedFiles: [], selectedScrapers: [] }));
		const { findByText } = renderPage();
		expect(await findByText('Rename video file only', { exact: true })).toBeTruthy();
	});
	it('hydrates the canonical organize destination instead of stale local storage', async () => {
		localStorage.setItem('javinizer_output_path', '/stale');
		sessionStorage.setItem(STORAGE_KEY_SCRAPE_STATE, JSON.stringify({
			version: 2,
			applyPlan: { version: 1, video_operation: 'organize', destination: '/persisted', nfo_output: 'write', media_policy: 'missing' },
			selectedFiles: [], forceRefresh: false, showScraperSelector: false, selectedScrapers: [], manualScrapeMode: false
		}));
		const { findByText } = renderPage();
		expect(await findByText('Destination: /persisted')).toBeTruthy();
	});

	it('preserves a version-2 null plan and warning across refresh', async () => {
		sessionStorage.setItem(STORAGE_KEY_SCRAPE_STATE, JSON.stringify({
			version: 2, applyPlan: null, planMigrationWarning: 'Select an operation again.',
			selectedFiles: ['/library/a.mp4'], forceRefresh: false, showScraperSelector: false,
			selectedScrapers: [], manualScrapeMode: false
		}));
		const { getByText, getAllByRole } = renderPage();
		await waitFor(() => expect(getByText('Select an operation again.')).toBeTruthy());
		expect((getAllByRole('radio') as HTMLInputElement[]).every((radio) => !radio.checked)).toBe(true);
	});

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

		// The seeded selections genuinely exist in /library (a real Back
		// round-trip would list them), so the browse mock reflects that —
		// otherwise phantom-selection pruning would (correctly) drop them.
		apiClient.browse.mockResolvedValue({
			current_path: '/library',
			parent_path: '',
			items: [
				{ name: 'kept-a.mp4', path: '/library/kept-a.mp4', is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' },
				{ name: 'kept-b.mp4', path: '/library/kept-b.mp4', is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' }
			]
		} as never);

		const { findByText, getByText, getAllByText } = renderPage();

		// The hydrate $effect restores selectedFiles, so the "Selected Files"
		// card renders the pre-seeded paths — a Back round-trip preserves them.
		await findByText('2 Files Selected for Scraping');
		expect(getAllByText('kept-a.mp4').length).toBeGreaterThan(0);
		expect(getAllByText('kept-b.mp4').length).toBeGreaterThan(0);

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

		// The seeded selection genuinely exists in /library, so the browse mock
		// reflects that (otherwise phantom-selection pruning would drop it).
		apiClient.browse.mockResolvedValue({
			current_path: '/library',
			parent_path: '',
			items: [
				{ name: 'pick.mp4', path: '/library/pick.mp4', is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' }
			]
		} as never);

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

describe('/browse — phantom selection pruning on refresh', () => {
	it('drops a selected file that has been moved out of the listed directory after Refresh', async () => {
		// Initial listing: a.mp4 + b.mp4 both present.
		apiClient.browse.mockResolvedValueOnce({
			current_path: '/library',
			parent_path: '',
			items: [
				{ name: 'a.mp4', path: '/library/a.mp4', is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' },
				{ name: 'b.mp4', path: '/library/b.mp4', is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' }
			]
		} as never);
		// After Refresh: a.mp4 was organized + moved out; only b.mp4 remains.
		apiClient.browse.mockResolvedValueOnce({
			current_path: '/library',
			parent_path: '',
			items: [
				{ name: 'b.mp4', path: '/library/b.mp4', is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' }
			]
		} as never);

		const { findByText, getByText, getByTitle, getByRole } = renderPage();
		await findByText('a.mp4');

		// Select a.mp4 (only the file-list row renders it pre-selection).
		await fireEvent.click(getByText('a.mp4'));
		await findByText('1 File Selected for Scraping');

		// Refresh the directory — a.mp4 is gone from disk.
		await fireEvent.click(getByTitle('Refresh'));

		await waitFor(() => {
			const action = getByRole('button', { name: /^(Scrape|Update) 0 Files$/ });
			expect((action as HTMLButtonElement).disabled).toBe(true);
		});
	});

	it('preserves selections from other directories when refreshing an unrelated folder', async () => {
		// /library contains only a.mp4; a selection from /elsewhere is unrelated.
		apiClient.browse.mockResolvedValue({
			current_path: '/library',
			parent_path: '',
			items: [
				{ name: 'a.mp4', path: '/library/a.mp4', is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' }
			]
		} as never);

		sessionStorage.setItem(
			STORAGE_KEY_SCRAPE_STATE,
			JSON.stringify({
				selectedFiles: ['/elsewhere/kept.mp4'],
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

		const { findByText, getByTitle, getByText, getByRole } = renderPage();
		await findByText('a.mp4');

		// The unrelated selection is restored on hydrate and survives a refresh
		// of /library (it never lived there). Wait for the refresh browse call
		// to actually complete before asserting, so the test is not satisfied
		// trivially by the pre-refresh hydrated DOM.
		expect(getByText('kept.mp4')).toBeTruthy();
		await fireEvent.click(getByTitle('Refresh'));
		await waitFor(() => expect(apiClient.browse).toHaveBeenCalledTimes(2));
		expect(getByText('kept.mp4')).toBeTruthy();
		expect((getByRole('button', { name: /1 File/ }) as HTMLButtonElement).disabled).toBe(false);
	});

	it('persists the live-browsed directory in the SSR cookie, not the stale seed', async () => {
		apiClient.browse.mockResolvedValue({ current_path: '/library', parent_path: '', items: [] } as never);
		apiClient.getCurrentWorkingDirectory.mockResolvedValue({ path: '/cwd' } as never);
		const { findByRole } = renderPage({ data: { browseBootstrap: { version: 1, applyPlan: null, initialPath: '/seed', destinationPath: '', forceRefresh: false, showScraperSelector: false, selectedScrapers: [], manualScrapeMode: false, planExpanded: true } } });
		await findByRole('radio', { name: /Organize into another location/ });
		await waitFor(() => {
			const encoded = document.cookie.split('; ').find((v) => v.startsWith('javinizer_browse_bootstrap='))?.split('=')[1];
			expect(encoded ? decodeBrowseBootstrap(encoded)?.initialPath : undefined).toBe('/library');
		});
	});

	it('does not prune subfolder selections when refreshing the parent directory', async () => {
		// /library contains only a.mp4; a selection from a SUBFOLDER
		// (/library/sub/deep.mp4) is NOT a direct child of /library — its
		// parent is /library/sub — so refreshing /library must preserve it.
		// This guards against a future naive startsWith(currentDir) rewrite
		// that would over-prune nested selections from recursive scans.
		apiClient.browse.mockResolvedValue({
			current_path: '/library',
			parent_path: '',
			items: [
				{ name: 'a.mp4', path: '/library/a.mp4', is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' }
			]
		} as never);

		sessionStorage.setItem(
			STORAGE_KEY_SCRAPE_STATE,
			JSON.stringify({
				selectedFiles: ['/library/sub/deep.mp4'],
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

		const { findByText, getByTitle, getByText, getByRole } = renderPage();
		await findByText('a.mp4');

		// The subfolder selection is restored on hydrate and survives a refresh
		// of its parent /library (it is not a direct child, so pruning does
		// not touch it). Wait for the refresh browse call to complete first.
		expect(getByText('deep.mp4')).toBeTruthy();
		await fireEvent.click(getByTitle('Refresh'));
		await waitFor(() => expect(apiClient.browse).toHaveBeenCalledTimes(2));
		expect(getByText('deep.mp4')).toBeTruthy();
		expect((getByRole('button', { name: /1 File/ }) as HTMLButtonElement).disabled).toBe(false);
	});

	it('prunes stale SSR selections after session state hydration', async () => {
		sessionStorage.setItem(
			STORAGE_KEY_SCRAPE_STATE,
			JSON.stringify({
				selectedFiles: ['/library/stale.mp4', '/library/kept.mp4'],
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

		const { findAllByText, queryByText, getByText } = renderPage({
			data: {
				initialPath: '/library',
				initialBrowse: {
					current_path: '/library',
					parent_path: '',
					items: [
						{ name: 'kept.mp4', path: '/library/kept.mp4', is_dir: false, size: 0, mod_time: '2024-01-01T00:00:00Z' }
					]
				}
			}
		});

		await findAllByText('kept.mp4');
		await waitFor(() => {
			expect(queryByText('stale.mp4')).toBeNull();
			expect(getByText('1 File Selected for Scraping')).toBeTruthy();
		});
		expect(apiClient.browse).not.toHaveBeenCalled();
	});
});
