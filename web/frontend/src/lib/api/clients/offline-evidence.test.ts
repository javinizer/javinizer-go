import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { JobClient, ScraperClient } from './jobs';
const scraper = new ScraperClient('http://api.test');
const jobs = new JobClient('http://api.test');
const fetchMock = vi.fn();
const report = { outcome: 'evidence_available', admitted_at: 'stored', snapshot_digest: 'A' };
beforeEach(() => {
	fetchMock.mockReset();
	fetchMock.mockResolvedValue(
		new Response(
			JSON.stringify({ movie: { id: 'ABC-001', title: 'Original' }, offline_evidence: report }),
			{ status: 200 },
		),
	);
	vi.stubGlobal('fetch', fetchMock);
	JobClient.setSessionID(null);
});
afterEach(() => {
	vi.unstubAllGlobals();
	JobClient.setSessionID(null);
});
it.each([undefined, false, true])(
	'direct scrape serializes only explicit true (%s) and keeps evidence outside Movie',
	async (enabled) => {
		const result = await scraper.scrapeMovie('ABC-001', { show_offline_evidence: enabled });
		const body = JSON.parse(fetchMock.mock.calls[0][1].body);
		if (enabled) expect(body.show_offline_evidence).toBe(true);
		else expect(body).not.toHaveProperty('show_offline_evidence');
		expect(result.offline_evidence).toEqual(report);
		expect(result.movie).not.toHaveProperty('offline_evidence');
		expect(body).not.toHaveProperty('artifact_directory');
	},
);
it('direct rescrape preserves the sidecar in its wrapper, not the movie', async () => {
	const result = await scraper.rescrapeMovie('ABC-001', {
		selected_scrapers: ['r18dev'],
		show_offline_evidence: true,
	});
	expect(JSON.parse(fetchMock.mock.calls[0][1].body).show_offline_evidence).toBe(true);
	expect(result.offline_evidence).toEqual(report);
	expect(result.movie).not.toHaveProperty('offline_evidence');
});
it.each([false, true])('batch API preserves request flag absence/true (%s)', async (enabled) => {
	await jobs.batchScrape({
		files: ['/movie'],
		strict: false,
		force: false,
		...(enabled ? { show_offline_evidence: true } : {}),
	});
	const body = JSON.parse(fetchMock.mock.calls[0][1].body);
	if (enabled) expect(body.show_offline_evidence).toBe(true);
	else expect(body).not.toHaveProperty('show_offline_evidence');
});
