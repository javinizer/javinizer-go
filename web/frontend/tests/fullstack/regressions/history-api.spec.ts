/**
 * History API full-stack spec.
 *
 * Pins the history API contract end-to-end against the real e2emock
 * backend: GET /history (list + pagination + filters + empty-state),
 * GET /history/stats (empty-state shape), DELETE /history/:id (404 on
 * missing), DELETE /history (bulk — 400 on missing params, 0-deleted on
 * no match). Every action hits the real /api/v1/history endpoints
 * (proxied to cmd/javinizer-e2e) → real HistoryRepository → real
 * :memory: SQLite. No page.route mocking.
 *
 * Why this matters: the history API is an entire CRUD surface (list,
 * stats, single delete, bulk delete) with ZERO fullstack coverage. The
 * dashboard's "Total Operations" + "Success Rate (7d)" + "Failures (7d)"
 * metrics all depend on GET /history/stats; the "Recent Runs" list
 * depends on GET /history. A regression in the history handlers, the
 * pagination parser, the filter wiring, or the HistoryRepository query
 * methods is invisible without this spec.
 *
 * Known limitation — the empty-state contract: nothing in the codebase
 * calls HistoryRepo.Create, so the history table is always empty in the
 * e2e backend (scrape/organize do not record history). This spec
 * therefore pins the empty-state contract + the error paths (404 on
 * missing-id delete, 400 on missing-params bulk delete, filter
 * pagination behavior). The happy-path "list has records + delete
 * removes one" can't be exercised until the write side is wired — that's
 * a product gap, not a test gap, and is documented in the assertions
 * below (the empty-state is asserted as the current correct behavior).
 *
 * Dashboard dependency: the root / page's statsQuery + recentRunsQuery
 * hit these endpoints. If GET /history/stats 500s or returns the wrong
 * shape, the dashboard's metrics render '-' / '0' — this spec guards
 * that contract.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';
import { BACKEND_BASE, loginAgainstRealBackend } from '../helpers';

interface HistoryListResponse {
	records: unknown[];
	total: number;
	limit: number;
	offset: number;
}

interface HistoryStats {
	total: number;
	success: number;
	failed: number;
	reverted: number;
	by_operation: {
		scrape: number;
		organize: number;
		download: number;
		nfo: number;
	};
}

async function getHistory(
	api: APIRequestContext,
	params?: Record<string, string | number>,
): Promise<HistoryListResponse> {
	const query = params
		? `?${new URLSearchParams(
				Object.fromEntries(Object.entries(params).map(([k, v]) => [k, String(v)])),
			).toString()}`
		: '';
	const resp = await api.get(`${BACKEND_BASE}/api/v1/history${query}`);
	expect(resp.ok(), `GET /history failed: ${resp.status()}`).toBeTruthy();
	return (await resp.json()) as HistoryListResponse;
}

async function getHistoryStats(api: APIRequestContext): Promise<HistoryStats> {
	const resp = await api.get(`${BACKEND_BASE}/api/v1/history/stats`);
	expect(resp.ok(), `GET /history/stats failed: ${resp.status()}`).toBeTruthy();
	return (await resp.json()) as HistoryStats;
}

test.describe('History API: real contract against the e2emock backend', () => {
	test('GET /history returns the well-formed empty-state response', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// The history table is empty (nothing writes to it — see spec header).
		// The empty-state contract: records is an empty array (not null),
		// total is 0, + limit/offset echo the request. The dashboard's
		// "Recent Runs" list renders "No operations recorded yet." off this.
		const body = await getHistory(request, { limit: 10, offset: 0 });
		expect(body.records, 'records must be an array (not null)').toEqual([]);
		expect(body.total, 'total must be 0 in the empty state').toBe(0);
		expect(body.limit, 'limit must echo the request').toBe(10);
		expect(body.offset, 'offset must echo the request').toBe(0);
	});

	test('GET /history respects limit + offset pagination params', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Even in the empty state, the handler must parse + echo pagination
		// (core.ParsePagination). limit=5 + offset=3 → echoed in the response.
		// A regression in ParsePagination (e.g., ignoring params, or clamping
		// incorrectly) would surface here.
		const body = await getHistory(request, { limit: 5, offset: 3 });
		expect(body.limit, 'limit must echo 5').toBe(5);
		expect(body.offset, 'offset must echo 3').toBe(3);
		expect(body.records, 'records must still be an array').toEqual([]);
	});

	test('GET /history accepts operation + status + movie_id filters without 500', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// The filters route to CountByOperation/ListByOperation (operation),
		// CountByStatus/ListByStatus (status), + CountByMovieID/ListByMovieID
		// (movie_id). Each must resolve without 500 even when the filtered
		// set is empty. A regression in any filter branch (e.g., a nil repo
		// method, a bad query) would 500 here.
		const filterCases: Record<string, string>[] = [
			{ operation: 'scrape' },
			{ operation: 'organize' },
			{ status: 'success' },
			{ status: 'failed' },
			{ status: 'reverted' },
			{ movie_id: 'NONEXISTENT-999' },
		];
		for (const params of filterCases) {
			const body = await getHistory(request, params);
			expect(body.records, `${JSON.stringify(params)}: records must be an array`).toEqual([]);
			expect(body.total, `${JSON.stringify(params)}: total must be 0`).toBe(0);
		}
	});

	test('GET /history/stats returns the well-formed zero-stats shape', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// The dashboard's "Total Operations" / "Success Rate (7d)" /
		// "Failures (7d)" metrics read this endpoint. The shape must be
		// stable: total/success/failed/reverted as numbers + by_operation
		// with all four operation keys present (the dashboard iterates them).
		const stats = await getHistoryStats(request);
		expect(typeof stats.total, 'total must be a number').toBe('number');
		expect(typeof stats.success, 'success must be a number').toBe('number');
		expect(typeof stats.failed, 'failed must be a number').toBe('number');
		expect(typeof stats.reverted, 'reverted must be a number').toBe('number');
		expect(stats.by_operation, 'by_operation must be present').toBeTruthy();
		for (const op of ['scrape', 'organize', 'download', 'nfo']) {
			expect(
				typeof stats.by_operation[op as keyof HistoryStats['by_operation']],
				`by_operation.${op} must be a number`,
			).toBe('number');
		}
		// Empty state: all zero.
		expect(stats.total).toBe(0);
	});

	test('DELETE /history/:id with a non-existent id returns 404', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// The handler does FindByID first → 404 if missing. A valid uint that
		// doesn't exist exercises this path. (A non-numeric id would 400.)
		const resp = await api_deleteHistory(request, '999999');
		expect(resp.status(), 'deleting a non-existent history id must 404').toBe(404);
		const body = await resp.json();
		expect(body.error, 'the 404 must carry an error message').toBeTruthy();
	});

	test('DELETE /history/:id with a non-numeric id returns 400', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const resp = await api_deleteHistory(request, 'not-a-number');
		expect(resp.status(), 'a non-numeric id must 400 (not 404, not 500)').toBe(400);
	});

	test('DELETE /history (bulk) without params returns 400', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// The bulk delete requires either older_than_days or movie_id. With
		// neither, it must 400 (not silently delete nothing, not 500).
		const resp = await api_deleteHistoryBulk(request, {});
		expect(resp.status(), 'bulk delete without params must 400').toBe(400);
		const body = await resp.json();
		expect(body.error, 'the 400 must explain the required params').toContain(
			'older_than_days',
		);
	});

	test('DELETE /history (bulk) with movie_id returns 200 + deleted=0 for no match', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Bulk delete by a non-existent movie_id: FindByMovieID returns empty
		// → deleted=0 → 200. This pins the happy-path bulk contract (the
		// count-then-delete flow) without mutating real data.
		const resp = await api_deleteHistoryBulk(request, { movie_id: 'NONEXISTENT-999' });
		expect(resp.ok(), 'bulk delete by movie_id must succeed (200)').toBeTruthy();
		const body = await resp.json();
		expect(body.deleted, 'deleted must be 0 for a non-existent movie_id').toBe(0);
	});

	test('DELETE /history (bulk) with older_than_days returns 200 + deleted=0', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Bulk delete by older_than_days: count-before vs count-after. With an
		// empty table, both are 0 → deleted=0 → 200. Pins the count-diff flow.
		const resp = await api_deleteHistoryBulk(request, { older_than_days: 30 });
		expect(resp.ok(), 'bulk delete by older_than_days must succeed (200)').toBeTruthy();
		const body = await resp.json();
		expect(body.deleted, 'deleted must be 0 on an empty table').toBe(0);
	});

	test('DELETE /history (bulk) with older_than_days=0 returns 400', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// The handler rejects days < 1 (0 + negatives). Pins the input guard.
		const resp = await api_deleteHistoryBulk(request, { older_than_days: 0 });
		expect(resp.status(), 'older_than_days=0 must 400').toBe(400);
	});
});

/** Raw DELETE /history/:id — exposes the response for status assertions. */
async function api_deleteHistory(
	api: APIRequestContext,
	id: string,
): ReturnType<APIRequestContext['delete']> {
	return api.delete(`${BACKEND_BASE}/api/v1/history/${id}`);
}

/** Raw DELETE /history — exposes the response for status assertions. */
async function api_deleteHistoryBulk(
	api: APIRequestContext,
	params: Record<string, string | number>,
): ReturnType<APIRequestContext['delete']> {
	const query = Object.keys(params).length
		? `?${new URLSearchParams(
				Object.fromEntries(Object.entries(params).map(([k, v]) => [k, String(v)])),
			).toString()}`
		: '';
	return api.delete(`${BACKEND_BASE}/api/v1/history${query}`);
}
