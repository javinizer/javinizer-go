/**
 * HTTP API helpers for full-stack E2E specs. Every helper drives the REAL
 * /api/v1 HTTP transport (proxied through the Vite dev server to the real
 * cmd/javinizer-e2e Go binary) — no Playwright `page.route` mocks.
 *
 * Organization:
 * - All HTTP mutations (login, scrape, preview, organize) live here.
 * - Job polling + result-shape helpers live in ./jobs.ts (read-side helpers).
 * - Browser-navigation helpers live in ./navigation.ts.
 *
 * Helpers assume the APIRequestContext is already authenticated (via either
 * global-setup's storageState or an in-test {@link loginAgainstRealBackend}
 * call) before invoking write endpoints.
 */
import type { APIRequestContext } from '@playwright/test';
import { expect } from '@playwright/test';
import {
	type AuthLoginResponse,
	type OperationListResponse,
	type OrganizePreviewResponse,
	type SubmitScrapeOptions,
} from './types';

/**
 * Canonical backend base URL. Hardcoded to the port cmd/javinizer-e2e binds
 * by default (18080). Specs use a relative path under baseURL when driving
 * the browser; these helpers go directly to the backend because the
 * APIRequestContext fixture's baseURL is the Vite dev server (5175 by
 * default) and Vite's proxy adds latency for no value when we only need
 * JSON. The Vite proxy has identical auth semantics (it just forwards).
 */
export const BACKEND_BASE = process.env.E2E_BACKEND_BASE ?? 'http://127.0.0.1:18080';

/**
 * Log in against the REAL backend. The session cookie is persisted on the
 * supplied APIRequestContext's cookie jar, so all subsequent calls on the
 * same fixture carry the auth cookie.
 *
 * Most specs don't call this directly — global-setup runs login once +
 * saves storageState, then the chromium project's `use.storageState`
 * inherits the cookie. Call this directly only when you need a freshly
 * authenticated fixture (e.g. a spec asserting auth behavior).
 */
export async function loginAgainstRealBackend(api: APIRequestContext): Promise<AuthLoginResponse> {
	const resp = await api.post(`${BACKEND_BASE}/api/v1/auth/login`, {
		data: { username: 'admin', password: 'adminpassword123' },
	});
	expect(
		resp.ok(),
		`login against real backend failed: ${resp.status()} ${await resp.text()}`,
	).toBeTruthy();
	const body = (await resp.json()) as AuthLoginResponse;
	expect(body.authenticated, 'login response must confirm authenticated=true').toBeTruthy();
	return body;
}

/**
 * Submit `POST /api/v1/batch/scrape` against the running Go backend.
 * Returns the created job_id. Pair with `waitForJobCompletion`
 * (./jobs.ts) to retrieve the final results.
 */
export async function submitScrape(
	api: APIRequestContext,
	opts: SubmitScrapeOptions,
): Promise<string> {
	const resp = await api.post(`${BACKEND_BASE}/api/v1/batch/scrape`, {
		data: {
			files: opts.files,
			selected_scrapers: opts.selectedScrapers ?? ['e2emock'],
			destination: opts.destination ?? '/tmp/javinizer-e2e-output',
			operation_mode: opts.operationMode ?? 'organize',
			update: opts.update ?? false,
		},
	});
	expect(
		resp.ok(),
		`POST /batch/scrape failed: ${resp.status()} ${await resp.text()}`,
	).toBeTruthy();
	const body = await resp.json();
	expect(body.job_id, 'scrape response must include job_id').toBeTruthy();
	return body.job_id as string;
}

/**
 * Submit `POST /api/v1/batch/:jobId/results/:resultId/preview` — drives
 * the real preview handler + real workflow.Preview + real organizer
 * target-path resolution. Used by specs asserting organize-preview
 * file-name / extension / target-path behavior.
 */
export async function submitPreview(
	api: APIRequestContext,
	jobId: string,
	resultId: string,
	destination: string = '/tmp/javinizer-e2e-output',
): Promise<OrganizePreviewResponse> {
	const resp = await api.post(`${BACKEND_BASE}/api/v1/batch/${jobId}/results/${resultId}/preview`, {
		data: { destination, operation_mode: 'organize' },
	});
	expect(resp.ok(), `POST preview failed: ${resp.status()} ${await resp.text()}`).toBeTruthy();
	return (await resp.json()) as OrganizePreviewResponse;
}

/**
 * Submit `POST /api/v1/batch/:jobId/organize` — drives the real apply
 * phase + organize workflow. The endpoint's contract is 200 on both
 * success AND partial-per-file failure; the job's per-file statuses
 * reflect individual failures, not the HTTP status. Callers fetch the
 * post-apply job state via `waitForJobCompletion` (./jobs.ts).
 */
export async function submitOrganize(
	api: APIRequestContext,
	jobId: string,
	destination: string,
): Promise<void> {
	const resp = await api.post(`${BACKEND_BASE}/api/v1/batch/${jobId}/organize`, {
		data: { destination, operation_mode: 'organize', copy_only: false },
	});
	expect(resp.ok(), `POST organize failed: ${resp.status()} ${await resp.text()}`).toBeTruthy();
}

/**
 * Fetch `GET /api/v1/jobs/:jobId/operations` — the BatchFileOperation
 * records that back the job-detail operations list. Recording is
 * independent of the AllowRevert toggle: records are always written when
 * the apply phase runs, so this endpoint must return a non-empty list for
 * any job that reached the organize phase — regardless of whether revert
 * is opted in. Guards the regression where completed/organized jobs
 * rendered "No operations recorded for this job" because recording was
 * wrongly gated behind AllowRevert.
 */
export async function fetchJobOperations(
	api: APIRequestContext,
	jobId: string,
): Promise<OperationListResponse> {
	const resp = await api.get(`${BACKEND_BASE}/api/v1/jobs/${jobId}/operations`);
	expect(
		resp.ok(),
		`GET /jobs/${jobId}/operations failed: ${resp.status()} ${await resp.text()}`,
	).toBeTruthy();
	return (await resp.json()) as OperationListResponse;
}
