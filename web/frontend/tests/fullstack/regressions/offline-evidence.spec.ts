/** Genuine production dump adapter and artifact reader; no successful API response mocks. */
import { test, expect, type APIRequestContext } from '@playwright/test';
import type {
	Actress,
	BatchJobResponse,
	BatchRescrapeResponse,
	FileResult,
	Movie,
} from '../../../src/lib/api/types';
import { DEFAULT_INPUT_DIR, DEFAULT_OUTPUT_DIR, seedInputFiles } from '../helpers/fixtures';
import { installOfflineEvidenceBrowserGuard } from '../helpers/offline-evidence-browser.cjs';

function canonicalActor(actor: Actress) {
	return {
		id: actor.id,
		dmm_id: actor.dmm_id,
		first_name: actor.first_name,
		last_name: actor.last_name,
		japanese_name: actor.japanese_name,
		aliases: actor.aliases,
		verified: actor.verified,
		origin: actor.origin,
	};
}
function canonicalMovie(movie: Movie | undefined) {
	return {
		actresses: movie?.actresses?.map(canonicalActor),
		credits: movie?.credits?.map(({ created_at, updated_at, ...credit }) => credit),
	};
}
async function getResult(request: APIRequestContext, jobId: string): Promise<FileResult> {
	const response = await request.get(`/api/v1/batch/${jobId}?include_data=true`);
	expect(response.ok()).toBe(true);
	const job: BatchJobResponse = await response.json();
	expect(Object.keys(job.results)).toHaveLength(1);
	return Object.values(job.results)[0];
}
async function databaseActors(request: APIRequestContext) {
	const response = await request.get('/api/v1/actresses?limit=100');
	expect(response.ok()).toBe(true);
	const body = await response.json();
	return (body.actresses as Actress[])
		.map(canonicalActor)
		.sort((a, b) => (a.id ?? 0) - (b.id ?? 0));
}

test('real R18 dump evidence is explicitly requested, read-only, preserved on failure and cleared by opt-out', async ({
	page,
	context,
	request,
}, testInfo) => {
	await installOfflineEvidenceBrowserGuard(context);
	await context.addInitScript(() => localStorage.setItem('javinizer-locale', 'en'));
	await seedInputFiles(['EVIDENCE-001.mp4']);
	const initialRequest = {
		files: [`${DEFAULT_INPUT_DIR}/EVIDENCE-001.mp4`],
		selected_scrapers: ['r18dev'],
		destination: DEFAULT_OUTPUT_DIR,
		operation_mode: 'organize',
		update: false,
	};
	expect(initialRequest).not.toHaveProperty('show_offline_evidence');
	const start = await request.post('/api/v1/batch/scrape', { data: initialRequest });
	expect(start.ok(), await start.text()).toBe(true);
	const { job_id: jobId } = await start.json();
	await expect
		.poll(async () => {
			const response = await request.get(`/api/v1/batch/${jobId}?include_data=true`);
			expect(response.ok()).toBe(true);
			const job: BatchJobResponse = await response.json();
			// Admission can precede the first result. A missing row is pending, not an assertion error.
			return Object.values(job.results ?? {})[0]?.status ?? 'pending';
		})
		.toBe('completed');
	const initial = await getResult(request, jobId);
	expect(initial.offline_evidence).toBeUndefined();
	expect(initial.movie?.id).toBe('EVIDENCE-001');
	const canonical = canonicalMovie(initial.movie),
		actors = await databaseActors(request);
	expect(JSON.stringify(canonical)).not.toContain('Supplemental');
	expect(initial.movie?.actresses?.length).toBeGreaterThan(0);
	const endpoint = `/api/v1/batch/${jobId}/results/${initial.result_id}/rescrape`;
	const requests: Record<string, unknown>[] = [];
	page.on('request', (req) => {
		if (req.method() === 'POST' && new URL(req.url()).pathname === endpoint)
			requests.push(req.postDataJSON());
	});
	await page.goto(`/review/${jobId}?view=detail`);
	const panel = page.getByRole('region', { name: 'R18 source evidence', exact: true });
	await expect(page.getByText('Original dump actor', { exact: true }).first()).toBeVisible();
	await expect(panel).toHaveCount(0);
	const modal = page
		.locator('.fixed.inset-0.bg-black\\/50')
		.filter({ has: page.getByRole('checkbox', { name: /Show offline R18/ }) });
	const option = modal.getByRole('checkbox', { name: /Show offline R18/ });
	await page.getByRole('button', { name: 'Rescrape', exact: true }).click();
	await expect(option).not.toBeChecked();
	await option.check();
	const optedResponse = page.waitForResponse(
		(res) => res.request().method() === 'POST' && new URL(res.url()).pathname === endpoint,
	);
	await modal.getByRole('button', { name: 'Rescrape', exact: true }).click();
	const response = await optedResponse;
	expect(response.ok(), await response.text()).toBe(true);
	const withEvidence: BatchRescrapeResponse = await response.json();
	await expect(modal).not.toBeVisible();
	expect(requests).toHaveLength(1);
	expect(requests[0].show_offline_evidence).toBe(true);
	expect(withEvidence.offline_evidence?.outcome).toBe('evidence_available');
	expect(withEvidence.offline_evidence?.snapshot_digest).toMatch(/^[a-f0-9]{64}$/);
	expect(withEvidence.offline_evidence?.observations?.map((item) => item.source_id).sort()).toEqual(
		['007', '7'],
	);
	expect(
		withEvidence.offline_evidence?.observations?.every(
			(item) => item.source === 'r18dev' && item.outcome === 'evidence_available',
		),
	).toBe(true);
	expect(canonicalMovie(withEvidence.movie)).toEqual(canonical);
	expect(await databaseActors(request)).toEqual(actors);
	await expect(panel).toBeVisible();
	for (const text of [
		'r18dev:7',
		'r18dev:007',
		'Supplemental evidence name',
		'Supplemental leading-zero name',
		'Supplemental whole alias',
		'Supplemental leading-zero alias',
	])
		await expect(panel.getByText(text, { exact: true })).toBeVisible();
	await expect(panel.locator('input,button,img,a')).toHaveCount(0);
	await panel.getByText('Stored snapshot details', { exact: true }).click();
	await expect(
		panel.getByText(withEvidence.offline_evidence!.snapshot_digest!, { exact: true }),
	).toBeVisible();
	await testInfo.attach('read-only-evidence.png', {
		body: await panel.screenshot(),
		contentType: 'image/png',
	});
	await page.setViewportSize({ width: 390, height: 844 });
	await expect(panel.getByText('r18dev:007', { exact: true })).toBeVisible();
	await expect(panel.getByText('r18dev:7', { exact: true })).toBeVisible();
	await expect(page.getByText('Original dump actor', { exact: true }).first()).toBeVisible();
	await expect(panel.locator('input,button,img,a')).toHaveCount(0);
	const bounds = await panel.boundingBox();
	expect(bounds).not.toBeNull();
	expect(bounds!.x).toBeGreaterThanOrEqual(0);
	expect(bounds!.x + bounds!.width).toBeLessThanOrEqual(391);
	expect(await page.evaluate(() => document.documentElement.scrollWidth <= window.innerWidth)).toBe(
		true,
	);
	await testInfo.attach('mobile-evidence.png', {
		body: await panel.screenshot(),
		contentType: 'image/png',
	});
	await page.setViewportSize({ width: 1280, height: 720 });
	await page.reload();
	await expect(panel.getByText('Supplemental evidence name', { exact: true })).toBeVisible();
	expect((await getResult(request, jobId)).offline_evidence).toEqual(withEvidence.offline_evidence);

	// A real unsupported fixture query must fail, retaining the committed result/report.
	await page.getByRole('button', { name: 'Rescrape', exact: true }).click();
	await expect(option).not.toBeChecked();
	await option.check();
	await modal.getByRole('button', { name: 'Manual Search', exact: true }).click();
	await modal.locator('#manual-search-input').fill('UNKNOWN-999');
	const failedResponse = page.waitForResponse(
		(res) => res.request().method() === 'POST' && new URL(res.url()).pathname === endpoint,
	);
	await modal.getByRole('button', { name: 'Search', exact: true }).click();
	expect((await failedResponse).ok()).toBe(false);
	expect(requests).toHaveLength(2);
	expect(requests[1].show_offline_evidence).toBe(true);
	const retained = await getResult(request, jobId);
	expect(retained.offline_evidence).toEqual(withEvidence.offline_evidence);
	expect(canonicalMovie(retained.movie)).toEqual(canonical);
	expect(await databaseActors(request)).toEqual(actors);
	await modal.getByRole('button', { name: 'Cancel', exact: true }).click();
	await expect(panel.getByText('Supplemental evidence name', { exact: true })).toBeVisible();

	// Successful fresh opt-out must omit the flag and clear the whole sidecar.
	await page.getByRole('button', { name: 'Rescrape', exact: true }).click();
	await expect(option).not.toBeChecked();
	await modal.getByRole('button', { name: 'Rescrape from File', exact: true }).click();
	const optedOutResponse = page.waitForResponse(
		(res) => res.request().method() === 'POST' && new URL(res.url()).pathname === endpoint,
	);
	await modal.getByRole('button', { name: 'Rescrape', exact: true }).click();
	const optedOut = await optedOutResponse;
	expect(optedOut.ok(), await optedOut.text()).toBe(true);
	expect((await optedOut.json()).offline_evidence).toBeUndefined();
	expect(requests).toHaveLength(3);
	expect(requests[2]).not.toHaveProperty('show_offline_evidence');
	await expect(modal).not.toBeVisible();
	await expect(panel).toHaveCount(0);
	await page.reload();
	await expect(page.getByText('Original dump actor', { exact: true }).first()).toBeVisible();
	await expect(panel).toHaveCount(0);
	const final = await getResult(request, jobId);
	expect(final.offline_evidence).toBeUndefined();
	expect(canonicalMovie(final.movie)).toEqual(canonical);
	expect(await databaseActors(request)).toEqual(actors);
	await testInfo.attach('real-api-evidence-contract.json', {
		body: JSON.stringify({ requests, report: withEvidence.offline_evidence }),
		contentType: 'application/json',
	});
});
