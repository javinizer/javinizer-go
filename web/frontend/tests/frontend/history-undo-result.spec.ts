import { test, expect, type Page } from '@playwright/test';

const jobId = 'undo-result-job';
const movieId = 'TEST-001';
const originalPath = '/synthetic/incoming/TEST-001.mp4';
const newPath = '/synthetic/library/TEST-001.mp4';
const failureText = 'failed to revert move: invalid cross-device link';

async function setup(page: Page, outcomes: Array<'failure' | 'success' | 'skipped'>) {
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
		else if (url.pathname === '/api/v1/batch') json = { jobs: [] };
		else if (url.pathname === `/api/v1/jobs/${jobId}`)
			json = {
				id: jobId,
				status: status === 'reverted' ? 'reverted' : 'organized',
				started_at: '2026-01-01T00:00:00Z',
			};
		else if (url.pathname === `/api/v1/jobs/${jobId}/operations`)
			json = {
				job_id: jobId,
				job_status: status === 'reverted' ? 'reverted' : 'organized',
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
			status = outcome === 'success' ? 'reverted' : outcome === 'failure' ? 'failed' : 'applied';
			json = {
				job_id: jobId,
				status: status === 'reverted' ? 'reverted' : 'organized',
				total: 1,
				succeeded: outcome === 'success' ? 1 : 0,
				failed: outcome === 'failure' ? 1 : 0,
				skipped: outcome === 'skipped' ? 1 : 0,
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
	await expect(details).toContainText('Reverted 0 of 1. 1 failed, 0 skipped.');
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
		'0 failed, 1 skipped.',
	);
	await expect(page.getByText(`Reverted ${movieId}`, { exact: true })).toHaveCount(0);
	await expect(page.getByRole('button', { name: 'Revert File', exact: true })).toBeEnabled();
	expect(fixture.calls()).toBe(1);
	expect(fixture.unhandled).toEqual([]);
});
