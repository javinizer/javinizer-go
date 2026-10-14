import { test, expect, type Page } from '@playwright/test';

const jobId = 'undo-result-job';
const movieId = 'TEST-001';
const originalPath = '/synthetic/incoming/TEST-001.mp4';
const newPath = '/synthetic/library/TEST-001.mp4';
const failureText = 'failed to revert move: invalid cross-device link';

async function setup(page: Page, outcomes: Array<'failure' | 'success' | 'skipped' | 'noop'>) {
	let status = 'applied';
	let calls = 0;
	const unhandled: string[] = [];
	await page.addInitScript(() => localStorage.setItem('javinizer-locale', 'en'));
	// Everything in this fixture is synthetic: no backend, media files, remote
	// service, or WebSocket server is reached, including unexpected requests.
	await page.routeWebSocket('**/ws/**', (ws) => ws.close());
	await page.route('**/*', async (route) => {
		const request = route.request();
		const url = new URL(request.url());
		if (
			url.origin !== new URL(page.url() === 'about:blank' ? request.url() : page.url()).origin &&
			!url.hostname.match(/^(localhost|127\.0\.0\.1)$/)
		) {
			unhandled.push(request.url());
			await route.abort();
			return;
		}
		if (!url.pathname.startsWith('/api/')) {
			await route.continue();
			return;
		}
		let json: unknown;
		if (url.pathname === '/api/v1/auth/status')
			json = { initialized: true, authenticated: true, username: 'fixture' };
		else if (url.pathname === '/api/v1/config') json = { output: { allow_revert: true } };
		else if (url.pathname === '/api/v1/version')
			json = { current: 'fixture', latest: 'fixture', update_available: false, source: 'disabled' };
		else if (url.pathname === '/api/v1/batch' && request.method() === 'GET') json = { jobs: [] };
		else if (url.pathname === '/api/v1/jobs' && request.method() === 'GET') json = { jobs: [] };
		else if (url.pathname === `/api/v1/jobs/${jobId}`)
			json = {
				id: jobId,
				status: status === 'reverted' || status === 'noop' ? 'reverted' : 'organized',
				started_at: '2026-01-01T00:00:00Z',
			};
		else if (url.pathname === `/api/v1/jobs/${jobId}/operations`)
			json = {
				job_id: jobId,
				job_status: status === 'reverted' || status === 'noop' ? 'reverted' : 'organized',
				total: 1,
				operations: [
					{
						id: 1,
						movie_id: movieId,
						original_path: originalPath,
						new_path: newPath,
						operation_type: 'move',
						revert_status: status,
						in_place_renamed: false,
						created_at: '2026-01-01T00:00:00Z',
					},
				],
			};
		else if (
			url.pathname === `/api/v1/jobs/${jobId}/operations/${movieId}/revert` &&
			request.method() === 'POST'
		) {
			const outcome = outcomes[calls++];
			if (!outcome) {
				unhandled.push('unexpected repeat Undo');
				await route.abort();
				return;
			}
			status =
				outcome === 'success'
					? 'reverted'
					: outcome === 'failure'
						? 'failed'
						: outcome === 'noop'
							? 'noop'
							: 'applied';
			json = {
				job_id: jobId,
				status: status === 'reverted' || status === 'noop' ? 'reverted' : 'organized',
				total: 1,
				succeeded: outcome === 'success' ? 1 : 0,
				failed: outcome === 'failure' ? 1 : 0,
				skipped: outcome === 'skipped' || outcome === 'noop' ? 1 : 0,
				errors:
					outcome === 'success'
						? []
						: [
								{
									operation_id: 1,
									movie_id: movieId,
									original_path: originalPath,
									new_path: newPath,
									outcome: outcome === 'failure' ? 'failed' : 'skipped',
									reason: outcome === 'failure' ? 'unexpected_path_state' : 'anchor_missing',
									error: outcome === 'failure' ? failureText : '',
								},
							],
			};
		} else {
			unhandled.push(`${request.method()} ${url.pathname}`);
			await route.abort();
			return;
		}
		await route.fulfill({ status: 200, json });
	});
	return { calls: () => calls, unhandled };
}

async function undo(page: Page) {
	await page.getByRole('button', { name: 'Revert File', exact: true }).click();
	await page.getByRole('dialog').getByRole('button', { name: 'Revert file', exact: true }).click();
	await expect(page.getByRole('dialog')).toHaveCount(0);
}

test('HTTP 200 failed Undo shows details, remains retryable after reload, and clears after success', async ({
	page,
}) => {
	const fixture = await setup(page, ['failure', 'success']);
	await page.goto(`/jobs/${jobId}`);
	await undo(page);
	const details = page.getByRole('alert').filter({ hasText: failureText });
	await expect(details).toContainText('Some operations still need attention.');
	await expect(
		page.getByRole('alert').filter({ hasText: 'Reverted 0 of 1. 1 failed, 0 skipped.' }),
	).toBeVisible();
	await expect(details).toContainText(originalPath);
	await expect(details).toContainText(newPath);
	await expect(page.getByText(`Reverted ${movieId}`, { exact: true })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Revert File', exact: true })).toBeEnabled();
	await page.screenshot({ path: test.info().outputPath('failed-undo.png'), fullPage: true });
	await page.reload();
	await expect(page.getByRole('button', { name: 'Revert File', exact: true })).toBeEnabled();
	await undo(page);
	await expect(page.getByRole('alert').filter({ hasText: `Reverted ${movieId}` })).toBeVisible();
	await expect(page.getByRole('alert').filter({ hasText: failureText })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Reverted ✓', exact: true })).toBeDisabled();
	await page.screenshot({ path: test.info().outputPath('successful-retry.png'), fullPage: true });
	expect(fixture.calls()).toBe(2);
	expect(fixture.unhandled).toEqual([]);
});

test('HTTP 200 skipped Undo warns instead of claiming success', async ({ page }) => {
	const fixture = await setup(page, ['skipped']);
	await page.goto(`/jobs/${jobId}`);
	await undo(page);
	await expect(page.getByRole('alert').filter({ hasText: 'anchor_missing' })).toContainText(
		'Some operations still need attention.',
	);
	await expect(page.getByRole('alert').filter({ hasText: '0 failed, 1 skipped.' })).toBeVisible();
	await expect(page.getByText(`Reverted ${movieId}`, { exact: true })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Revert File', exact: true })).toBeEnabled();
	expect(fixture.calls()).toBe(1);
	expect(fixture.unhandled).toEqual([]);
});

test('recovered pending move reports skipped but authoritative noop clears attention and disables retry', async ({
	page,
}) => {
	const fixture = await setup(page, ['noop']);
	await page.goto(`/jobs/${jobId}`);
	await undo(page);
	await expect(page.getByRole('alert').filter({ hasText: '0 failed, 1 skipped.' })).toBeVisible();
	await expect(page.getByRole('button', { name: 'Nothing to revert', exact: true })).toBeDisabled();
	await expect(
		page.getByRole('alert').filter({ hasText: 'Some operations still need attention.' }),
	).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Revert File', exact: true })).toHaveCount(0);
	await page.reload();
	await expect(page.getByRole('button', { name: 'Nothing to revert', exact: true })).toBeDisabled();
	await expect(
		page.getByRole('alert').filter({ hasText: 'Some operations still need attention.' }),
	).toHaveCount(0);
	expect(fixture.calls()).toBe(1);
	expect(fixture.unhandled).toEqual([]);
});

test('batch errors survive another movie’s failed and successful retries; reload retains backend retry state', async ({
	page,
}) => {
	const fixture = await setup(page, []);
	const rows = [1, 2, 3].map((id) => ({
		id,
		movie_id: `TEST-00${id}`,
		original_path: `/synthetic/incoming/TEST-00${id}.mp4`,
		new_path: `/synthetic/library/TEST-00${id}.mp4`,
		operation_type: 'move',
		revert_status: 'applied',
		in_place_renamed: false,
		created_at: '2026-01-01T00:00:00Z',
	}));
	const calls: string[] = [];
	const counts = new Map<string, number>();
	const errorFor = (id: number, error: string, outcome = 'failed') => ({
		operation_id: id,
		movie_id: rows[id - 1].movie_id,
		original_path: rows[id - 1].original_path,
		new_path: rows[id - 1].new_path,
		error,
		outcome,
		reason: outcome === 'skipped' ? 'anchor_missing' : 'unexpected_path_state',
	});
	await page.route(`**/api/v1/jobs/${jobId}**`, async (route) => {
		const path = new URL(route.request().url()).pathname;
		let json: unknown;
		if (path === `/api/v1/jobs/${jobId}/operations`) {
			json = { job_id: jobId, job_status: 'organized', total: rows.length, operations: rows };
		} else if (path === `/api/v1/jobs/${jobId}/revert` && route.request().method() === 'POST') {
			calls.push('batch');
			rows[0].revert_status = 'failed';
			rows[2].revert_status = 'reverted';
			json = {
				job_id: jobId,
				status: 'organized',
				total: 3,
				succeeded: 1,
				failed: 1,
				skipped: 1,
				errors: [errorFor(1, failureText), errorFor(2, '', 'skipped')],
			};
		} else if (path.endsWith('/revert') && route.request().method() === 'POST') {
			const id = path.includes('/TEST-001/') ? 1 : path.includes('/TEST-002/') ? 2 : 0;
			if (!id) {
				fixture.unhandled.push(path);
				await route.abort();
				return;
			}
			const key = rows[id - 1].movie_id;
			const count = (counts.get(key) ?? 0) + 1;
			counts.set(key, count);
			calls.push(key);
			const failed = count === 1;
			rows[id - 1].revert_status = failed ? 'failed' : 'reverted';
			json = {
				job_id: jobId,
				status: 'organized',
				total: 1,
				succeeded: failed ? 0 : 1,
				failed: failed ? 1 : 0,
				skipped: 0,
				errors: failed
					? [errorFor(id, id === 1 ? 'retry destination conflict' : 'reloaded retry still blocked')]
					: [],
			};
		} else {
			await route.fallback();
			return;
		}
		await route.fulfill({ status: 200, json });
	});
	const row = (id: string) =>
		page.locator('div.bg-card').filter({ has: page.getByText(id, { exact: true }) });
	const retry = async (id: string) => {
		await row(id).getByRole('button', { name: 'Revert File', exact: true }).click();
		await page
			.getByRole('dialog')
			.getByRole('button', { name: 'Revert file', exact: true })
			.click();
		await expect(page.getByRole('dialog')).toHaveCount(0);
	};
	const unresolved = page
		.getByRole('alert')
		.filter({ hasText: 'Some operations still need attention.' });
	await page.goto(`/jobs/${jobId}`);
	await page.getByRole('button', { name: 'Revert Batch (3 files)', exact: true }).click();
	await page
		.getByRole('dialog')
		.getByRole('button', { name: 'Revert 3 files', exact: true })
		.click();
	await expect(unresolved.getByRole('listitem')).toHaveCount(2);
	await retry('TEST-001');
	await expect(unresolved).toContainText('retry destination conflict');
	await expect(unresolved).toContainText('anchor_missing');
	await expect(unresolved).toContainText(rows[1].original_path);
	await expect(unresolved).not.toContainText(failureText);
	await retry('TEST-001');
	await expect(unresolved.getByRole('listitem')).toHaveCount(1);
	await expect(unresolved).toContainText('anchor_missing');
	await expect(unresolved).toContainText(rows[1].new_path);
	await expect(unresolved).not.toContainText('retry destination conflict');
	await expect(
		row('TEST-001').getByRole('button', { name: 'Reverted ✓', exact: true }),
	).toBeDisabled();
	await page.reload();
	await expect(unresolved).toHaveCount(0);
	await expect(
		row('TEST-001').getByRole('button', { name: 'Reverted ✓', exact: true }),
	).toBeDisabled();
	await expect(
		row('TEST-002').getByRole('button', { name: 'Revert File', exact: true }),
	).toBeEnabled();
	await retry('TEST-002');
	await expect(unresolved).toContainText('reloaded retry still blocked');
	await retry('TEST-002');
	await expect(unresolved).toHaveCount(0);
	expect(calls).toEqual(['batch', 'TEST-001', 'TEST-001', 'TEST-002', 'TEST-002']);
	expect(fixture.unhandled).toEqual([]);
});
