/**
 * Cache-hit path spec.
 *
 * Real stack: browser → Vite proxy → real POST /api/v1/batch/scrape →
 * real worker scrape phase → real `tryCache` lookup against the real
 * MovieRepository in :memory: SQLite → real e2emock scraper (cache miss
 * only). On the second scrape of the same MovieID, `tryCache` returns
 * the cached Movie from the DB instead of re-invoking the e2emock
 * scraper.
 *
 * Pins:
 * - Scraping the SAME MovieID twice returns CONSISTENT metadata across
 *   both jobs (Title, Maker, Cover URL etc. all match). A regression in
 *   the cache-hit path that drops fields (the field-drop bug class —
 *   the same pattern that broke the failure / fallback / map-miss
 *   paths) shows up as a field mismatch between the cache-miss and
 *   cache-hit responses.
 * - Both jobs complete without error — the cache-hit path must not
 *   panic / nil-deref / drop the Movie struct on the result tracker.
 *   Commit 91400891 [INFO] fixed this for cache-retranslation; this
 *   spec exercises the plain cache-hit branch.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	loginAgainstRealBackend,
	submitScrape,
	waitForJobCompletion,
	waitForFileResultStatus,
	DEFAULT_INPUT_DIR,
	type FileResult,
} from '../helpers';

test.describe('Cache hit: second scrape of the same MovieID returns consistent metadata', () => {
	test('scrape-same-movie-twice produces identical Movie payloads on both job results', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// First scrape — cache MISS, e2emock is invoked, the Movie is
		// written to the DB by the real MovieRepository.
		const job1_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job1 = await waitForJobCompletion(request, job1_id);
		// Poll per-file status separately — there's a known race where the
		// job-level status flips terminal a beat before the per-file tracker
		// row is updated; asserting the FileResult immediately could observe
		// a stale "running" / "failed" per-file row.
		const result1 = await waitForFileResultStatus(request, job1_id, 'completed');

		// Second scrape — cache HIT, the scrape phase's tryCache lookup
		// returns the previously-stored Movie without invoking e2emock.
		const job2_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job2 = await waitForJobCompletion(request, job2_id);
		const result2 = await waitForFileResultStatus(request, job2_id, 'completed');

		// Both jobs must reach completed — the cache-hit path must not
		// crash the scrape phase (regression class: cache-hit nil deref
		// or field-drop on the tracker).
		expect(job1.status, 'cache-miss job must complete').toBe('completed');
		expect(job2.status, 'cache-hit job must complete').toBe('completed');
		expect(result1.status, 'cache-miss file status must be completed').toBe('completed');
		expect(result2.status, 'cache-hit file status must be completed').toBe('completed');

		// Core assertion: the Movie payload returned by the cache-hit path
		// matches the cache-miss payload on stable fields. We DON'T compare
		// the entire struct with toEqual because the cache-hit path touches
		// the DB (writes a translation record updated_at) — so timestamps
		// legitimately differ between miss and hit. Comparing the
		// user-visible fields catches the actual field-drop regression class
		// (a regression that drops the Movie pointer entirely, dropping
		// id / poster_url / field_sources).
		expect(result2.movie, 'cache-hit Movie must be populated (not nil)').toBeTruthy();
		expect(result1.movie, 'cache-miss Movie must be populated (not nil)').toBeTruthy();
		expect(result2.movie!.id, 'cache-hit Movie.id must match cache-miss').toBe(result1.movie!.id);
		expect(result2.movie!.title, 'cache-hit Movie.title must match cache-miss').toBe(
			result1.movie!.title,
		);
		expect(result2.movie!.poster_url, 'cache-hit Movie.poster_url must match cache-miss').toBe(
			result1.movie!.poster_url,
		);
		expect(result2.movie!.cover_url, 'cache-hit Movie.cover_url must match cache-miss').toBe(
			result1.movie!.cover_url,
		);

		assertFieldSourcesConsistent(result1, result2);
	});

	test('scrape-same-movie-twice does not leave stale error state on the cache-hit row', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// First scrape establishes the cache entry for GOOD-001.
		const job1_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job1_id);

		// Second scrape reads from the cache. The result MUST NOT carry
		// an error field from a prior failed state — regression class:
		// the cache-hit path reuses a result tracker slot whose `error`
		// field wasn't cleared, surfacing a stale failure message on a
		// successful cached scrape.
		const job2_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		const job2 = await waitForJobCompletion(request, job2_id);
		const result = await waitForFileResultStatus(request, job2_id, 'completed');

		expect(result.status, 'cache-hit row status must be completed').toBe('completed');
		expect(
			result.error ?? '',
			'cache-hit row must NOT carry a stale error from a prior state',
		).toBe('');
		expect(result.started_at, 'cache-hit row must populate started_at').toBeTruthy();
		expect(result.ended_at, 'cache-hit row must populate ended_at').toBeTruthy();
	});
});

/**
 * Assert that both the cache-miss and cache-hit FileResult report the
 * same per-field source attribution. The aggregator attaches a
 * field_sources map to every result; under cache-hit the entries should
 * still reference the original scraper name ("e2emock"), proving the
 * cache preserved provenance + the field_sources map wasn't dropped.
 */
function assertFieldSourcesConsistent(miss: FileResult, hit: FileResult): void {
	expect(miss.field_sources, 'cache-miss field_sources must be populated').toBeTruthy();
	expect(hit.field_sources, 'cache-hit field_sources must be populated (not dropped)').toBeTruthy();
	expect(hit.field_sources, 'cache-hit field_sources must match cache-miss').toEqual(
		miss.field_sources,
	);
	// Every field whose value came from the scraper should attribute to "e2emock".
	for (const [field, source] of Object.entries(hit.field_sources ?? {})) {
		expect(source, `field_sources.${field} must attribute to e2emock, got "${source}"`).toBe(
			'e2emock',
		);
	}
}
