/**
 * /events endpoint spec.
 *
 * Real stack: browser → Vite proxy → real GET /api/v1/events → real
 * EventRepository.List against :memory: SQLite (real GORM paginated read).
 *
 * The e2emock backend emits a real "server initialized" system event on
 * BootstrapAPI (see internal/api/core/bootstrap.go's
 * eventEmitter.EmitSystemEvent call). Every real scrape + organize also
 * emits real events via the Emitter. So the events list grows over the
 * course of a test run as the other specs exercise scrape + organize
 * flows — this spec asserts those emissions surface through /events.
 *
 * Pins:
 * - Happy path: GET /events returns an array containing at least the
 *   server-init event (always present after BootstrapAPI).
 * - After a real scrape, GET /events returns entries referencing the
 *   just-scraped operation (organize events include the MovieID).
 * - Pagination via limit + offset.
 */
import { test, expect, type APIRequestContext } from '@playwright/test';

import {
	BACKEND_BASE,
	loginAgainstRealBackend,
	submitScrape,
	submitOrganize,
	waitForJobCompletion,
	DEFAULT_INPUT_DIR,
	DEFAULT_OUTPUT_DIR,
} from '../helpers';

test.describe('/events: real EventRepository.List surfaces emitted system + scrape events', () => {
	test('GET /events returns at least the server-initialization system event', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		const resp = await request.get(`${BACKEND_BASE}/api/v1/events?limit=50`);
		expect(resp.ok()).toBeTruthy();
		const body = await resp.json();
		// The events array may be nested under `events` or be the top-level
		// array depending on the contract — accept either shape.
		const events = Array.isArray(body) ? body : (body.events ?? []);
		expect(
			Array.isArray(events),
			'/events response must be / contain an events array',
		).toBeTruthy();
		expect(events.length, '/events must contain at least the server-init event').toBeGreaterThan(0);

		// The server-init event is emitted by BootstrapAPI in
		// internal/api/core/bootstrap.go with source="server". It must
		// be present in every fresh backend's event log — pinning here
		// so a regression that dropped the EmitSystemEvent call (or
		// mis-persisted it) surfaces immediately.
		const serverInit = events.find(
			(e: { source?: string; source_type?: string; message?: string }) =>
				(e.source === 'server' || e.source_type === 'server') &&
				(e.message ?? '').toLowerCase().includes('initialized'),
		);
		expect(serverInit, '/events must include the server-initialization system event').toBeTruthy();
	});

	test('after a real scrape + organize, /events contains entries referencing the operation', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		await loginAgainstRealBackend(request);

		// Seed — exercise both scrape + organize so the Emitter has
		// something to emit for this test's window.
		const job_id = await submitScrape(request, { files: [`${DEFAULT_INPUT_DIR}/GOOD-001.mp4`] });
		await waitForJobCompletion(request, job_id);
		await submitOrganize(request, job_id, `${DEFAULT_OUTPUT_DIR}/events-${Date.now()}`);
		await waitForJobCompletion(request, job_id, { timeoutMs: 30_000 });

		const resp = await request.get(`${BACKEND_BASE}/api/v1/events?limit=100`);
		expect(resp.ok()).toBeTruthy();
		const body = await resp.json();
		const events = Array.isArray(body) ? body : (body.events ?? []);

		// The organize phase emits at least one event tagged with the
		// MovieID — either success or failure, depending on the file's
		// real move outcome (the e2emock's 1-byte fixture may fail to
		// move, producing an "organize failed" event). Either way, an
		// event referencing GOOD-001 must be present.
		const matchingEvents = events.filter((e: { message?: string }) =>
			(e.message ?? '').includes('GOOD-001'),
		);
		expect(
			matchingEvents.length,
			'/events must contain at least one entry referencing the scraped MovieID',
		).toBeGreaterThan(0);
	});

	test('GET /events?limit=1 returns at most 1 entry + respects offset', async ({
		request,
	}: {
		request: APIRequestContext;
	}) => {
		// Pagination contract: limit + offset must be honored so the
		// frontend's events page can page through long histories without
		// loading everything in one request.
		await loginAgainstRealBackend(request);

		const page1 = await request.get(`${BACKEND_BASE}/api/v1/events?limit=1&offset=0`);
		expect(page1.ok()).toBeTruthy();
		const page1Body = await page1.json();
		const page1Events = Array.isArray(page1Body) ? page1Body : (page1Body.events ?? []);
		expect(page1Events, 'limit=1 must return at most 1 event').toHaveLength(1);

		// Page 2 with offset=1 must return a DIFFERENT event (assuming
		// there are at least 2 events in the log, which the server-init
		// + other specs' scrape emissions guarantee).
		const page2 = await request.get(`${BACKEND_BASE}/api/v1/events?limit=1&offset=1`);
		expect(page2.ok()).toBeTruthy();
		const page2Body = await page2.json();
		const page2Events = Array.isArray(page2Body) ? page2Body : (page2Body.events ?? []);
		expect(page2Events, 'offset=1 must return the next event').toHaveLength(1);

		const e1Id = page1Events[0]?.id ?? page1Events[0]?.event_id;
		const e2Id = page2Events[0]?.id ?? page2Events[0]?.event_id;
		if (e1Id && e2Id) {
			expect(e2Id, 'page 2 must return a different event than page 1').not.toBe(e1Id);
		}
	});
});
