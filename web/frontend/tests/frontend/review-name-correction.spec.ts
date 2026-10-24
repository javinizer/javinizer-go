/**
 * Offline browser contract for future-only JAVLibrary name corrections.
 * Exercises the real review page and source viewer, not a component harness.
 * Backend persistence/CAS correctness is covered by the API tests separately.
 */
import { expect, test, type BrowserContext, type Locator, type Page } from '@playwright/test';
import type { BatchJobResponse, ScraperResult } from '../../src/lib/api/types';
import { mockConfig } from './review-helpers';

const JOB_ID = 'name-correction-browser-job';
const RESULT_ID = 'name-correction-result';
const MOVIE_ID = 'NAME-001';
const SOURCE = `/synthetic/${MOVIE_ID}.mp4`;
const RESULT_PATH = `/api/v1/batch/${JOB_ID}/results/${RESULT_ID}`;
const OVERRIDE_PATH = '/api/v1/scrapers/javlibrary/name-overrides/';
const ORIGINAL_LABEL = 'Lee Mary Jane';
const ENTITY_LABEL = 'O&#39;Neil &amp; Jane';
const SCOPE_LABEL = 'Use this correction for future scrapes only';
const TIMESTAMP = '2026-10-24T00:00:00Z';

interface NameRule {
	source_name: string;
	first_name: string;
	last_name: string;
}

interface PutCorrection extends NameRule {
	expected_rule: NameRule | null;
}

interface Provenance {
	source: string;
	source_id: string;
	display_name: string;
	state: 'unknown_order' | 'user_corrected' | 'native_unsplit' | 'single_name';
}

type SourceFixture = ScraperResult & { cast_name_provenance?: (Provenance | null)[] };

function provenance(
	source_id: string,
	display_name: string,
	state: Provenance['state'],
): Provenance {
	return { source: 'javlibrary', source_id, display_name, state };
}

function sourceResults(): SourceFixture[] {
	return [
		{
			source: 'javlibrary',
			id: MOVIE_ID,
			title: 'Source title, not the current movie title',
			actresses: [
				{ first_name: 'Lee', last_name: 'Mary Jane' }, // Legacy split is not verified order.
				{ japanese_name: '鈴木はるか' },
				{ first_name: 'Rin' },
				{ first_name: 'de', last_name: 'la Cruz Ana María' },
				{ first_name: 'O&#39;Neil', last_name: '&amp; Jane' },
				{ first_name: 'Haruka', last_name: 'Suzuki' },
				{ first_name: 'Legacy', last_name: 'Unknown' }, // No provenance entry.
			],
			cast_name_provenance: [
				provenance('a1', ORIGINAL_LABEL, 'unknown_order'),
				provenance('a2', '鈴木はるか', 'native_unsplit'),
				provenance('a3', 'Rin', 'single_name'),
				provenance('a4', 'de la Cruz Ana María', 'unknown_order'),
				provenance('a5', ENTITY_LABEL, 'unknown_order'),
				provenance('a6', 'Suzuki Haruka', 'user_corrected'),
				null,
			],
		},
	];
}

function reviewJob(): BatchJobResponse {
	return {
		id: JOB_ID,
		status: 'completed',
		total_files: 1,
		completed: 1,
		failed: 0,
		operation_count: 0,
		reverted_count: 0,
		noop_count: 0,
		excluded: {},
		progress: 100,
		destination: '/synthetic/output',
		started_at: TIMESTAMP,
		completed_at: TIMESTAMP,
		update: false,
		results: {
			[SOURCE]: {
				result_id: RESULT_ID,
				file_path: SOURCE,
				movie_id: MOVIE_ID,
				status: 'completed',
				revision: 1,
				movie: {
					id: MOVIE_ID,
					title: 'Unchanged current movie',
					actresses: [{ id: 1, first_name: 'Original', last_name: 'Credit' }],
					credits: [],
				},
				// The JAVLibrary radio is deliberately inactive. A bubbled click or
				// keydown on its correction button would otherwise invoke onApply.
				field_sources: { title: 'javbus', actresses: 'javbus' },
				started_at: TIMESTAMP,
				ended_at: TIMESTAMP,
				is_multi_part: false,
				part_number: 0,
				part_suffix: '',
			},
		},
	};
}

async function mockAPI(context: BrowserContext) {
	const state = {
		rules: new Map<string, NameRule>(),
		gets: [] as string[],
		completedGets: [] as string[],
		getGates: new Map<string, Promise<void>>(),
		puts: [] as { id: string; body: PutCorrection }[],
		requests: [] as string[],
		unexpected: [] as string[],
		forbiddenWrites: [] as string[],
		nextPut: 'success' as 'success' | 'conflict' | 'lost' | 'unavailable',
		concurrentRule: null as NameRule | null,
		putGate: null as Promise<void> | null,
	};
	await context.addInitScript(() => localStorage.setItem('javinizer-locale', 'en'));
	await context.routeWebSocket('**/ws/progress', () => {});
	await context.route('**/api/v1/**', async (route) => {
		const request = route.request();
		const path = new URL(request.url()).pathname;
		const method = request.method();
		state.requests.push(`${method} ${path}`);
		if (path.startsWith(OVERRIDE_PATH)) {
			const id = decodeURIComponent(path.slice(OVERRIDE_PATH.length));
			if (method === 'GET') {
				state.gets.push(id);
				const rule = state.rules.get(id) ?? null;
				const gate = state.getGates.get(id);
				if (gate) await gate;
				await route.fulfill({ json: { rule } });
				state.completedGets.push(id);
				return;
			}
			if (method === 'PUT') {
				const body = request.postDataJSON() as PutCorrection;
				state.puts.push({ id, body });
				if (state.putGate) await state.putGate;
				if (state.nextPut === 'conflict') {
					if (state.concurrentRule) state.rules.set(id, state.concurrentRule);
					return route.fulfill({
						status: 409,
						json: { error: 'The saved correction changed. Reload before saving.' },
					});
				}
				if (state.nextPut === 'unavailable') {
					return route.fulfill({ status: 503, json: { error: 'Synthetic save unavailable' } });
				}
				const rule = {
					source_name: body.source_name,
					first_name: body.first_name,
					last_name: body.last_name,
				};
				state.rules.set(id, rule);
				// A lost response is intentionally after the mock server committed.
				// Retrying this PUT blindly could overwrite another user's correction.
				if (state.nextPut === 'lost') return route.abort('failed');
				return route.fulfill({ json: { rule } });
			}
		}
		if (method === 'GET' && path === `/api/v1/batch/${JOB_ID}`)
			return route.fulfill({ json: reviewJob() });
		if (method === 'GET' && path === `${RESULT_PATH}/sources`)
			return route.fulfill({ json: { results: sourceResults() } });
		if (method === 'GET' && path === `${RESULT_PATH}/existing-nfo`)
			return route.fulfill({ json: { exists: false } });
		if (method === 'POST' && path === `/api/v1/batch/${JOB_ID}/video-collisions`)
			return route.fulfill({
				json: { items: [], new: 0, replaced: 0, skipped: 0, unresolved: 0, same_file: 0 },
			});
		if (method === 'POST' && path === `${RESULT_PATH}/preview`)
			return route.fulfill({
				json: {
					folder_name: MOVIE_ID,
					file_name: `${MOVIE_ID}.mp4`,
					full_path: `/synthetic/output/${MOVIE_ID}.mp4`,
				},
			});
		if (method === 'POST' && path === `${RESULT_PATH}/display-title-preview`)
			return route.fulfill({ json: { display_title: 'Unchanged current movie' } });
		const reads: Record<string, unknown> = {
			'/api/v1/auth/status': { initialized: true, authenticated: true, username: 'synthetic' },
			'/api/v1/config': {
				...mockConfig,
				output: { ...mockConfig.output, first_name_order: true },
			},
			'/api/v1/scrapers': {
				scrapers: [{ name: 'javlibrary', display_title: 'JAVLibrary', enabled: true }],
			},
			'/api/v1/version': { current: 'test', update_available: false, source: 'disabled' },
			'/api/v1/jobs': { jobs: [], total: 0 },
			'/api/v1/genres/favorites': { favorites: [] },
			'/api/v1/actresses/collisions': { collisions: [] },
		};
		if (method === 'GET' && Object.hasOwn(reads, path)) return route.fulfill({ json: reads[path] });
		if (method !== 'GET') state.forbiddenWrites.push(`${method} ${path}`);
		state.unexpected.push(`${method} ${path}`);
		return route.fulfill({ status: 501, json: { error: 'Unexpected mocked API request' } });
	});
	return state;
}

type MockState = Awaited<ReturnType<typeof mockAPI>>;
const sourceViewer = (page: Page) =>
	page.getByRole('dialog', { name: 'Source Merge Resolver', exact: true });
const correctionDialog = (page: Page) =>
	page.getByRole('dialog', { name: 'Correct source name', exact: true });
const saveButton = (dialog: Locator) =>
	dialog.getByRole('button', { name: 'Save for future scrapes', exact: true });
const scopeCheckbox = (dialog: Locator) =>
	dialog.getByRole('checkbox', { name: SCOPE_LABEL, exact: true });

async function openSources(page: Page) {
	await page.goto(`/review/${JOB_ID}?view=detail`);
	await expect(page.getByText('Original Credit', { exact: true })).toBeVisible();
	await page.getByRole('button', { name: 'Sources', exact: true }).click();
	const viewer = sourceViewer(page);
	await expect(viewer).toBeVisible();
	await viewer.getByRole('button', { name: /^Actresses(?:\s|$)/ }).click();
	await expect(
		viewer.getByRole('button', { name: `Correct name for ${ORIGINAL_LABEL}`, exact: true }),
	).toBeVisible();
	return viewer;
}

async function openCorrection(page: Page, label = ORIGINAL_LABEL) {
	await sourceViewer(page)
		.getByRole('button', { name: `Correct name for ${label}`, exact: true })
		.click();
	const dialog = correctionDialog(page);
	await expect(dialog).toBeVisible();
	await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toBeEnabled();
	return dialog;
}

async function fillCorrection(dialog: Locator, first = 'Mary Jane', last = 'Lee') {
	await dialog.getByRole('textbox', { name: 'Given name', exact: true }).fill(first);
	await dialog.getByRole('textbox', { name: 'Family name', exact: true }).fill(last);
}

async function expectCurrentMovieUnchanged(page: Page, state: MockState) {
	await sourceViewer(page).getByRole('button', { name: 'Close', exact: true }).last().click();
	await expect(sourceViewer(page)).not.toBeVisible();
	await expect(page.getByText('Original Credit', { exact: true })).toBeVisible();
	await expect(page.getByRole('heading', { name: 'Actresses (1)', exact: true })).toBeVisible();
	expect(
		state.forbiddenWrites,
		'A correction must not apply a source or edit any movie/catalog credit',
	).toEqual([]);
	expect(state.unexpected).toEqual([]);
}

test('saving a reviewed split is explicitly future-only and leaves the current movie unchanged', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	await openSources(page);
	const dialog = await openCorrection(page);
	await expect(dialog.getByText(ORIGINAL_LABEL, { exact: true })).toBeVisible();
	await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue('');
	await expect(dialog.getByRole('textbox', { name: 'Family name', exact: true })).toHaveValue('');
	await expect(scopeCheckbox(dialog)).not.toBeChecked();
	await expect(saveButton(dialog)).toBeDisabled();

	await fillCorrection(dialog, 'Lee', 'Mary Jane');
	await dialog.getByRole('button', { name: 'Swap names', exact: true }).click();
	await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue(
		'Mary Jane',
	);
	await expect(dialog.getByRole('textbox', { name: 'Family name', exact: true })).toHaveValue(
		'Lee',
	);
	await expect(saveButton(dialog)).toBeDisabled();
	await scopeCheckbox(dialog).check();
	await page.screenshot({
		path: test.info().outputPath('name-correction-review.png'),
		fullPage: true,
	});
	await saveButton(dialog).click();
	await expect(dialog.getByText('Saved for future scrapes.', { exact: true })).toBeVisible();
	await expect(dialog).toContainText(/current movie/i);
	await expect(dialog).toContainText(/historical/i);
	expect(state.gets).toEqual(['a1']);
	expect(state.puts).toEqual([
		{
			id: 'a1',
			body: {
				source_name: ORIGINAL_LABEL,
				first_name: 'Mary Jane',
				last_name: 'Lee',
				expected_rule: null,
			},
		},
	]);
	await dialog.getByRole('button', { name: 'Done', exact: true }).click();
	await expect(dialog).not.toBeVisible();
	await expect(sourceViewer(page).getByText(ORIGINAL_LABEL, { exact: true }).first()).toBeVisible();
	await expectCurrentMovieUnchanged(page, state);
});

test('Cancel and Escape discard the draft without saving or closing the source viewer', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	await openSources(page);
	for (const dismissal of ['Cancel', 'Escape']) {
		const dialog = await openCorrection(page);
		await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue('');
		await expect(scopeCheckbox(dialog)).not.toBeChecked();
		await fillCorrection(dialog);
		await scopeCheckbox(dialog).check();
		if (dismissal === 'Cancel')
			await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
		else await dialog.getByRole('textbox', { name: 'Given name', exact: true }).press('Escape');
		await expect(dialog).not.toBeVisible();
		await expect(sourceViewer(page)).toBeVisible();
		expect(state.puts).toEqual([]);
	}
	await expectCurrentMovieUnchanged(page, state);
});

test('keyboard activation of a correction never bubbles into applying the source', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	const viewer = await openSources(page);
	for (const key of ['Enter', 'Space']) {
		const correction = viewer.getByRole('button', {
			name: `Correct name for ${ORIGINAL_LABEL}`,
			exact: true,
		});
		await correction.focus();
		await correction.press(key);
		const dialog = correctionDialog(page);
		await expect(dialog).toBeVisible();
		// Use actual key events: fill() alone would not exercise j/k shortcuts.
		await dialog
			.getByRole('textbox', { name: 'Given name', exact: true })
			.pressSequentially('Jack');
		await dialog
			.getByRole('textbox', { name: 'Family name', exact: true })
			.pressSequentially('King');
		await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue(
			'Jack',
		);
		await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
		await expect(viewer.getByRole('heading', { name: 'Actresses', exact: true })).toBeVisible();
	}
	expect(state.puts).toEqual([]);
	await expectCurrentMovieUnchanged(page, state);
});

test('rapid repeated submission sends exactly one PUT while the first save is pending', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	let releasePut!: () => void;
	state.putGate = new Promise<void>((resolve) => {
		releasePut = resolve;
	});
	await openSources(page);
	const dialog = await openCorrection(page);
	await fillCorrection(dialog);
	await scopeCheckbox(dialog).check();
	try {
		await saveButton(dialog).evaluate((element) => {
			(element as HTMLButtonElement).click();
			(element as HTMLButtonElement).click();
		});
		await expect.poll(() => state.puts.length).toBe(1);
		await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toBeDisabled();
		expect(state.puts).toHaveLength(1);
	} finally {
		releasePut();
	}
	await expect(dialog.getByText('Saved for future scrapes.', { exact: true })).toBeVisible();
	expect(state.puts).toHaveLength(1);
	await dialog.getByRole('button', { name: 'Done', exact: true }).click();
	await expectCurrentMovieUnchanged(page, state);
});

test('a 409 preserves the draft and requires explicit reload and re-review before replacing a newer rule', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	const originalRule: NameRule = {
		source_name: ORIGINAL_LABEL,
		first_name: 'Lee',
		last_name: 'Mary Jane',
	};
	const newerRule: NameRule = {
		source_name: ORIGINAL_LABEL,
		first_name: 'Lee Mary',
		last_name: 'Jane',
	};
	state.rules.set('a1', originalRule);
	state.concurrentRule = newerRule;
	state.nextPut = 'conflict';
	await openSources(page);
	const dialog = await openCorrection(page);
	await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue('Lee');
	await fillCorrection(dialog);
	await scopeCheckbox(dialog).check();
	await saveButton(dialog).click();
	const reload = dialog.getByRole('button', { name: 'Reload saved correction', exact: true });
	await expect(reload).toBeVisible();
	await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue(
		'Mary Jane',
	);
	await expect(saveButton(dialog)).toBeDisabled();
	expect(state.gets).toEqual(['a1']);
	expect(state.puts).toHaveLength(1);
	expect(state.puts[0].body.expected_rule).toEqual(originalRule);

	state.nextPut = 'success';
	await reload.click();
	await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue(
		'Lee Mary',
	);
	await expect(scopeCheckbox(dialog)).not.toBeChecked();
	await expect(saveButton(dialog)).toBeDisabled();
	expect(state.gets).toEqual(['a1', 'a1']);
	expect(state.puts).toHaveLength(1);
	await fillCorrection(dialog);
	await scopeCheckbox(dialog).check();
	await saveButton(dialog).click();
	await expect(dialog.getByText('Saved for future scrapes.', { exact: true })).toBeVisible();
	expect(state.puts).toHaveLength(2);
	expect(state.puts[1].body).toEqual({
		source_name: ORIGINAL_LABEL,
		first_name: 'Mary Jane',
		last_name: 'Lee',
		expected_rule: newerRule,
	});
	await dialog.getByRole('button', { name: 'Done', exact: true }).click();
	await expectCurrentMovieUnchanged(page, state);
});

for (const failure of ['lost', 'unavailable'] as const) {
	test(`${failure === 'lost' ? 'lost response after commit' : 'server failure'} keeps the draft and never retries the save automatically`, async ({
		page,
		context,
	}) => {
		const state = await mockAPI(context);
		state.nextPut = failure;
		await openSources(page);
		const dialog = await openCorrection(page);
		await fillCorrection(dialog);
		await scopeCheckbox(dialog).check();
		await saveButton(dialog).click();
		const reload = dialog.getByRole('button', { name: 'Reload saved correction', exact: true });
		await expect(reload).toBeVisible();
		await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue(
			'Mary Jane',
		);
		await expect(dialog.getByRole('textbox', { name: 'Family name', exact: true })).toHaveValue(
			'Lee',
		);
		await expect(dialog.getByText('Saved for future scrapes.', { exact: true })).not.toBeVisible();
		await expect(saveButton(dialog)).toBeDisabled();
		expect(state.gets).toEqual(['a1']);
		expect(state.puts).toHaveLength(1);

		// Reload is a GET even if the failed response concealed a committed PUT.
		state.nextPut = 'success';
		await reload.click();
		await expect.poll(() => state.gets.length).toBe(2);
		await expect(scopeCheckbox(dialog)).not.toBeChecked();
		await expect(saveButton(dialog)).toBeDisabled();
		if (failure === 'lost') {
			await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue(
				'Mary Jane',
			);
			await expect(dialog.getByRole('textbox', { name: 'Family name', exact: true })).toHaveValue(
				'Lee',
			);
		}
		expect(state.puts).toHaveLength(1);
		await dialog.getByRole('button', { name: 'Cancel', exact: true }).click();
		await expectCurrentMovieUnchanged(page, state);
	});
}

test('provenance labels remain exact and only uncertain or user-corrected entries offer a split correction', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	const viewer = await openSources(page);
	for (const label of [
		ORIGINAL_LABEL,
		'鈴木はるか',
		'Rin',
		'de la Cruz Ana María',
		ENTITY_LABEL,
		'Suzuki Haruka',
		'Legacy Unknown',
	]) {
		await expect(viewer.getByText(label, { exact: true }).first()).toBeVisible();
	}
	for (const label of [ORIGINAL_LABEL, 'de la Cruz Ana María', ENTITY_LABEL, 'Suzuki Haruka']) {
		await expect(
			viewer.getByRole('button', { name: `Correct name for ${label}`, exact: true }),
		).toBeVisible();
	}
	for (const label of ['鈴木はるか', 'Rin', 'Legacy Unknown']) {
		await expect(
			viewer.getByRole('button', { name: `Correct name for ${label}`, exact: true }),
		).toHaveCount(0);
	}
	await expect(viewer.getByText("O'Neil & Jane", { exact: true })).toHaveCount(0);
	await expect(viewer.getByText(/\bverified\b/i)).toHaveCount(0);
	await expect(
		viewer.getByText('Name order unverified · original source label', { exact: true }),
	).toHaveCount(3);
	await expect(viewer.getByText('Native name · kept unsplit', { exact: true })).toBeVisible();
	await expect(viewer.getByText('Single name · kept unsplit', { exact: true })).toBeVisible();
	await expect(viewer.getByText(/^Name-order provenance unavailable\./)).toBeVisible();
	const dialog = await openCorrection(page, 'de la Cruz Ana María');
	await expect(dialog.getByText('de la Cruz Ana María', { exact: true })).toBeVisible();
	await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue('');
	await expect(dialog.getByRole('textbox', { name: 'Family name', exact: true })).toHaveValue('');
	await fillCorrection(dialog, 'Ana María', 'de la Cruz');
	await scopeCheckbox(dialog).check();
	await saveButton(dialog).click();
	await expect(dialog.getByText('Saved for future scrapes.', { exact: true })).toBeVisible();
	expect(state.puts[0]).toEqual({
		id: 'a4',
		body: {
			source_name: 'de la Cruz Ana María',
			first_name: 'Ana María',
			last_name: 'de la Cruz',
			expected_rule: null,
		},
	});
	await dialog.getByRole('button', { name: 'Done', exact: true }).click();
	await expectCurrentMovieUnchanged(page, state);
});

test('HTML entity text is neither re-decoded nor interpreted as markup in the label or saved rule', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	await openSources(page);
	const dialog = await openCorrection(page, ENTITY_LABEL);
	await expect(dialog.getByText(ENTITY_LABEL, { exact: true })).toBeVisible();
	await expect(dialog.getByText("O'Neil & Jane", { exact: true })).toHaveCount(0);
	await fillCorrection(dialog, '&amp; Jane', 'O&#39;Neil');
	await scopeCheckbox(dialog).check();
	await saveButton(dialog).click();
	await expect(dialog.getByText('Saved for future scrapes.', { exact: true })).toBeVisible();
	expect(state.puts).toEqual([
		{
			id: 'a5',
			body: {
				source_name: ENTITY_LABEL,
				first_name: '&amp; Jane',
				last_name: 'O&#39;Neil',
				expected_rule: null,
			},
		},
	]);
	await dialog.getByRole('button', { name: 'Done', exact: true }).click();
	await expectCurrentMovieUnchanged(page, state);
});

test('an existing user correction loads its saved split without rewriting the historical source label', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	const saved: NameRule = {
		source_name: 'Suzuki Haruka',
		first_name: 'Haruka',
		last_name: 'Suzuki',
	};
	state.rules.set('a6', saved);
	await openSources(page);
	const dialog = await openCorrection(page, saved.source_name);
	await expect(dialog.getByText(saved.source_name, { exact: true })).toBeVisible();
	await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue(
		'Haruka',
	);
	await expect(dialog.getByRole('textbox', { name: 'Family name', exact: true })).toHaveValue(
		'Suzuki',
	);
	await expect(scopeCheckbox(dialog)).not.toBeChecked();
	await fillCorrection(dialog, 'Suzuki', 'Haruka');
	await scopeCheckbox(dialog).check();
	await saveButton(dialog).click();
	await expect(dialog.getByText('Saved for future scrapes.', { exact: true })).toBeVisible();
	expect(state.puts[0]).toEqual({
		id: 'a6',
		body: {
			source_name: saved.source_name,
			first_name: 'Suzuki',
			last_name: 'Haruka',
			expected_rule: saved,
		},
	});
	await dialog.getByRole('button', { name: 'Done', exact: true }).click();
	await expect(
		sourceViewer(page).getByText(saved.source_name, { exact: true }).first(),
	).toBeVisible();
	await expectCurrentMovieUnchanged(page, state);
});

test('a saved rule for an older source label requires a fresh split and preserves the exact older CAS record', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	const olderRule: NameRule = { source_name: 'Lee Jane', first_name: 'Jane', last_name: 'Lee' };
	state.rules.set('a1', olderRule);
	await openSources(page);
	const dialog = await openCorrection(page);
	await expect(dialog.getByText(ORIGINAL_LABEL, { exact: true })).toBeVisible();
	const notice = dialog.getByText(/The saved correction is for a different source label:/);
	await expect(notice).toBeVisible();
	await expect(notice).toContainText(olderRule.source_name);
	await expect(dialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue('');
	await expect(dialog.getByRole('textbox', { name: 'Family name', exact: true })).toHaveValue('');
	await expect(scopeCheckbox(dialog)).not.toBeChecked();
	await expect(saveButton(dialog)).toBeDisabled();
	await fillCorrection(dialog);
	await expect(saveButton(dialog)).toBeDisabled();
	await scopeCheckbox(dialog).check();
	await saveButton(dialog).click();
	await expect(dialog.getByText('Saved for future scrapes.', { exact: true })).toBeVisible();
	expect(state.puts).toEqual([
		{
			id: 'a1',
			body: {
				source_name: ORIGINAL_LABEL,
				first_name: 'Mary Jane',
				last_name: 'Lee',
				expected_rule: olderRule,
			},
		},
	]);
	await dialog.getByRole('button', { name: 'Done', exact: true }).click();
	await expect(sourceViewer(page).getByText(ORIGINAL_LABEL, { exact: true }).first()).toBeVisible();
	await expectCurrentMovieUnchanged(page, state);
});

test('canceling an in-flight load cannot replace a different performer’s newer correction dialog', async ({
	page,
	context,
}) => {
	const state = await mockAPI(context);
	const newerLabel = 'de la Cruz Ana María';
	state.rules.set('a1', {
		source_name: ORIGINAL_LABEL,
		first_name: 'Mary Jane',
		last_name: 'Lee',
	});
	state.rules.set('a4', {
		source_name: newerLabel,
		first_name: 'Ana María',
		last_name: 'de la Cruz',
	});
	let releaseOldRead!: () => void;
	state.getGates.set(
		'a1',
		new Promise<void>((resolve) => {
			releaseOldRead = resolve;
		}),
	);
	await openSources(page);
	try {
		// Deliberately do not use openCorrection: the first form must stay loading.
		await sourceViewer(page)
			.getByRole('button', { name: `Correct name for ${ORIGINAL_LABEL}`, exact: true })
			.click();
		const firstDialog = correctionDialog(page);
		await expect(firstDialog).toBeVisible();
		await expect.poll(() => state.gets).toEqual(['a1']);
		await expect(
			firstDialog.getByRole('textbox', { name: 'Given name', exact: true }),
		).toBeDisabled();
		await firstDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
		await expect(firstDialog).not.toBeVisible();
		await expect(sourceViewer(page)).toBeVisible();

		const newerDialog = await openCorrection(page, newerLabel);
		await expect(newerDialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue(
			'Ana María',
		);
		await expect(
			newerDialog.getByRole('textbox', { name: 'Family name', exact: true }),
		).toHaveValue('de la Cruz');
		// Preserve a newly edited, explicitly reviewed draft as the old read resolves.
		await fillCorrection(newerDialog, 'de la Cruz', 'Ana María');
		await scopeCheckbox(newerDialog).check();
		releaseOldRead();
		await expect.poll(() => state.completedGets).toEqual(['a4', 'a1']);
		await expect(correctionDialog(page)).toHaveCount(1);
		await expect(newerDialog.getByText(newerLabel, { exact: true })).toBeVisible();
		await expect(newerDialog.getByText(ORIGINAL_LABEL, { exact: true })).toHaveCount(0);
		await expect(newerDialog.getByRole('textbox', { name: 'Given name', exact: true })).toHaveValue(
			'de la Cruz',
		);
		await expect(
			newerDialog.getByRole('textbox', { name: 'Family name', exact: true }),
		).toHaveValue('Ana María');
		await expect(scopeCheckbox(newerDialog)).toBeChecked();
		await expect(saveButton(newerDialog)).toBeEnabled();
		expect(state.gets).toEqual(['a1', 'a4']);
		expect(state.puts).toEqual([]);
		await newerDialog.getByRole('button', { name: 'Cancel', exact: true }).click();
		await expectCurrentMovieUnchanged(page, state);
	} finally {
		releaseOldRead();
	}
});
