import { test, expect, type FixtureActor, type SyncHarness } from './helpers';
import type { Page } from '@playwright/test';
const dialog = (p: Page) => p.getByRole('dialog', { name: 'One-shot Japanese-name sync' });
async function catalog(page: Page, sync: SyncHarness) {
	await sync.login(page);
	await page.goto('/actresses');
	await expect(page.getByRole('heading', { name: 'Actress Database', exact: true })).toBeVisible();
}
async function select(page: Page, actors: FixtureActor[]) {
	for (const a of actors) {
		const card = page
			.getByRole('heading', { name: a.name, exact: true })
			.locator('xpath=ancestor::div[.//input[@type="checkbox"]][1]');
		await card.getByRole('checkbox').check();
	}
	await page.getByRole('button', { name: 'Sync selected Japanese names', exact: true }).click();
	await expect(dialog(page)).toBeVisible();
}
async function choose(page: Page, a: FixtureActor, id = a.source_id) {
	const d = dialog(page);
	await d.getByRole('button', { name: `Choose source for ${a.name}`, exact: true }).click();
	await d.getByRole('searchbox', { name: 'Source name, alias, or ID' }).fill(id);
	await d.getByRole('button', { name: 'Search offline evidence', exact: true }).click();
	// has is resolved relative to each list item, so do not include the dialog ancestor.
	const result = d.getByRole('listitem').filter({ has: page.getByText(id, { exact: true }) });
	await expect(result).toHaveCount(1);
	await result.getByRole('button', { name: 'Choose this source identity', exact: true }).click();
}
async function review(page: Page) {
	await dialog(page).getByRole('button', { name: 'Review proposed changes', exact: true }).click();
	await expect(
		dialog(page).getByRole('heading', { name: 'Review exact changes', exact: true }),
	).toBeVisible();
}
async function confirm(page: Page) {
	await dialog(page)
		.getByRole('checkbox', { name: /I have checked/ })
		.check();
	await dialog(page).getByRole('button', { name: 'Confirm one-shot sync', exact: true }).click();
	await expect(
		dialog(page).getByRole('heading', { name: 'Per-actress results', exact: true }),
	).toBeVisible();
}
async function completed(page: Page) {
	await expect(dialog(page).getByRole('status').filter({ hasText: 'Completed' })).toBeVisible();
}
async function history(page: Page) {
	await page.getByRole('button', { name: 'Sync history', exact: true }).click();
	await expect(
		dialog(page).getByRole('heading', { name: 'Durable sync history', exact: true }),
	).toBeVisible();
}

test('explicit review applies once, preserves historical credits, and survives reload/restart', async ({
	page,
	sync,
}, testInfo) => {
	await catalog(page, sync);
	const before = await sync.inspect(),
		a = sync.fixture.actors.managed;
	await select(page, [a]);
	await choose(page, a);
	await review(page);
	await expect(dialog(page).getByText(a.before_name, { exact: true })).toBeVisible();
	await expect(dialog(page).getByText(a.after_name, { exact: true }).first()).toBeVisible();
	await expect(
		dialog(page).getByRole('button', { name: 'Confirm one-shot sync', exact: true }),
	).toBeDisabled();
	expect((await sync.inspect()).jobs).toHaveLength(0);
	await page.screenshot({ path: testInfo.outputPath('desktop-review.png'), fullPage: true });
	await confirm(page);
	await completed(page);
	let after = await sync.inspect();
	expect(after.jobs).toHaveLength(1);
	expect(after.tasks).toHaveLength(1);
	expect(after.tasks[0].status).toBe('applied');
	expect(after.actors.find((x) => x.id === a.id)?.japanese_name).toBe(a.after_name);
	expect(after.credits).toEqual(before.credits);
	// Canonical renames retain the former canonical name as a trusted alias.
	// This is not permission to import the source's untrusted alias metadata.
	const expectedAliases = [
		...before.aliases,
		{ alias_name: a.before_name, canonical_name: a.after_name },
	];
	expect(after.aliases).toEqual(expectedAliases);
	expect(after.actors.filter((actor) => actor.id !== a.id)).toEqual(
		before.actors.filter((actor) => actor.id !== a.id),
	);
	expect(after.actors.map(({ id, aliases }) => ({ id, aliases }))).toEqual(
		before.actors.map(({ id, aliases }) => ({ id, aliases })),
	);
	await page.reload();
	await history(page);
	await dialog(page).getByRole('button', { name: 'View results', exact: true }).click();
	await completed(page);
	await dialog(page).getByRole('button', { name: 'Close', exact: true }).click();
	await sync.restart();
	await page.reload();
	await history(page);
	await dialog(page).getByRole('button', { name: 'View results', exact: true }).click();
	await completed(page);
	after = await sync.inspect();
	expect(after.jobs).toHaveLength(1);
	expect(after.credits).toEqual(before.credits);
	expect(after.aliases).toEqual(expectedAliases);
	expect(after.actors.filter((actor) => actor.id !== a.id)).toEqual(
		before.actors.filter((actor) => actor.id !== a.id),
	);
});

test('Back Escape close and reopen discard unsubmitted permission', async ({ page, sync }) => {
	await catalog(page, sync);
	const before = await sync.inspect(),
		a = sync.fixture.actors.managed;
	await select(page, [a]);
	await choose(page, a);
	await review(page);
	await dialog(page).getByRole('button', { name: 'Back to source choices', exact: true }).click();
	await review(page);
	await page.keyboard.press('Escape');
	await expect(dialog(page)).not.toBeVisible();
	await expect(
		page.getByRole('button', { name: 'Sync selected Japanese names', exact: true }),
	).toBeFocused();
	await page.getByRole('button', { name: 'Sync selected Japanese names', exact: true }).click();
	await expect(
		dialog(page).getByRole('button', { name: 'Review proposed changes', exact: true }),
	).toBeDisabled();
	await dialog(page).getByRole('button', { name: 'Close', exact: true }).click();
	expect(await sync.inspect()).toEqual(before);
});

for (const key of ['manual', 'guarded'] as const)
	test(`protected ${key} actor rejects complete batch`, async ({ page, sync }) => {
		await catalog(page, sync);
		const before = await sync.inspect(),
			a = sync.fixture.actors[key];
		await select(page, [a]);
		await choose(page, a);
		await dialog(page)
			.getByRole('button', { name: 'Review proposed changes', exact: true })
			.click();
		await expect(dialog(page).getByRole('alert')).toContainText('protected');
		expect(await sync.inspect()).toEqual(before);
	});

for (const mode of ['disabled', 'missing', 'stale', 'future'] as const)
	test.describe(mode, () => {
		test.use({ syncOptions: { mode } });
		test('unavailable or stale evidence never authorizes a write', async ({ page, sync }) => {
			await catalog(page, sync);
			const before = await sync.inspect(),
				a = sync.fixture.actors.managed;
			await select(page, [a]);
			if (mode === 'disabled' || mode === 'missing') {
				await expect(
					dialog(page).getByRole('button', { name: 'Search offline evidence', exact: true }),
				).toBeDisabled();
				await expect(
					dialog(page).getByRole('link', { name: 'Read offline provisioning instructions' }),
				).toBeVisible();
			} else {
				await choose(page, a);
				await dialog(page)
					.getByRole('button', { name: 'Review proposed changes', exact: true })
					.click();
				await expect(dialog(page).getByRole('alert')).toContainText('stale');
			}
			await dialog(page).getByRole('button', { name: 'Sync history', exact: true }).click();
			await expect(
				dialog(page).getByRole('heading', { name: 'Durable sync history' }),
			).toBeVisible();
			expect(await sync.inspect()).toEqual(before);
		});
	});

test.describe('durable lifecycle', () => {
	test.use({ syncOptions: { pauseAfter: 1 } });
	test('partial cancellation preserves applied receipts and dismissal does not cancel', async ({
		page,
		sync,
	}) => {
		await catalog(page, sync);
		const before = await sync.inspect();
		const actors = [sync.fixture.actors.managed, sync.fixture.actors.managed2];
		await select(page, actors);
		for (const a of actors) await choose(page, a);
		await review(page);
		await confirm(page);
		await sync.waitForPause();
		await dialog(page).getByRole('button', { name: 'Cancel remaining work', exact: true }).click();
		await dialog(page).getByRole('button', { name: 'Keep job running', exact: true }).click();
		expect((await sync.inspect()).jobs[0].cancelled).toBe(false);
		await dialog(page).getByRole('button', { name: 'Cancel remaining work', exact: true }).click();
		const jobId = (await sync.inspect()).jobs[0].id;
		const cancelledResponse = page.waitForResponse(
			(response) =>
				response.request().method() === 'POST' &&
				new URL(response.url()).pathname === `/api/v1/actress-name-sync/jobs/${jobId}/cancel`,
		);
		await dialog(page).getByRole('button', { name: 'Confirm cancellation', exact: true }).click();
		expect((await cancelledResponse).status()).toBe(200);
		// A click does not await the async write. Keep the next claim gated until
		// the real cancellation is committed, rather than racing it with release.
		expect((await sync.inspect()).jobs[0].cancelled).toBe(true);
		await sync.releaseWorker();
		await expect(dialog(page).getByRole('status').filter({ hasText: 'Cancelled' })).toBeVisible();
		const after = await sync.inspect();
		expect(after.tasks.filter((t) => t.status === 'applied')).toHaveLength(1);
		expect(after.tasks.filter((t) => t.status === 'cancelled')).toHaveLength(1);
		expect(after.credits).toEqual(before.credits);
	});
	test('restart resumes remaining work without a second job', async ({ page, sync }) => {
		await catalog(page, sync);
		const actors = [sync.fixture.actors.managed, sync.fixture.actors.managed2];
		await select(page, actors);
		for (const a of actors) await choose(page, a);
		await review(page);
		await confirm(page);
		await sync.waitForPause();
		await sync.restart({ pauseAfter: -1 });
		await page.reload();
		await history(page);
		await dialog(page).getByRole('button', { name: 'View results', exact: true }).click();
		await completed(page);
		const after = await sync.inspect();
		expect(after.jobs).toHaveLength(1);
		expect(after.tasks.filter((t) => t.status === 'applied')).toHaveLength(2);
	});
});

test('lost confirmation response retries the same committed nonce after restart', async ({
	page,
	sync,
}) => {
	await catalog(page, sync);
	const a = sync.fixture.actors.managed;
	await select(page, [a]);
	await choose(page, a);
	await review(page);
	let original = '';
	await page.route(
		'**/api/v1/actress-name-sync/jobs',
		async (route) => {
			original = route.request().postDataJSON().review_id;
			const response = await route.fetch();
			expect(response.status()).toBe(202);
			await route.abort('failed');
		},
		{ times: 1 },
	);
	await dialog(page)
		.getByRole('checkbox', { name: /I have checked/ })
		.check();
	await dialog(page).getByRole('button', { name: 'Confirm one-shot sync', exact: true }).click();
	await expect(
		dialog(page).getByRole('button', { name: 'Retry same confirmation', exact: true }),
	).toBeVisible();
	expect((await sync.inspect()).jobs).toHaveLength(1);
	await sync.restart();
	await page.reload();
	await history(page);
	const request = page.waitForRequest(
		(r) => r.method() === 'POST' && r.url().endsWith('/actress-name-sync/jobs'),
	);
	await dialog(page).getByRole('button', { name: 'Retry same confirmation', exact: true }).click();
	expect((await request).postDataJSON()).toEqual({ review_id: original, confirmed: true });
	await completed(page);
	expect((await sync.inspect()).jobs).toHaveLength(1);
});

test('unconfirmed review cannot survive server restart as permission', async ({ page, sync }) => {
	await catalog(page, sync);
	const before = await sync.inspect(),
		a = sync.fixture.actors.managed;
	await select(page, [a]);
	await choose(page, a);
	await review(page);
	await sync.restart();
	await dialog(page)
		.getByRole('checkbox', { name: /I have checked/ })
		.check();
	await dialog(page).getByRole('button', { name: 'Confirm one-shot sync', exact: true }).click();
	await expect(dialog(page).getByRole('alert')).toContainText('expired');
	expect(await sync.inspect()).toEqual(before);
});

test('mobile keyboard dialog traps focus and ambiguous names never auto-select', async ({
	page,
	sync,
}, testInfo) => {
	await page.setViewportSize({ width: 390, height: 844 });
	await catalog(page, sync);
	const before = await sync.inspect(),
		a = sync.fixture.actors.unchanged;
	await select(page, [a]);
	const d = dialog(page);
	await d.getByRole('searchbox').fill('同じ名前');
	await d.getByRole('button', { name: 'Search offline evidence', exact: true }).click();
	await expect(
		d.getByRole('button', { name: 'Choose this source identity', exact: true }),
	).toHaveCount(2);
	await expect(
		d.getByRole('button', { name: 'Review proposed changes', exact: true }),
	).toBeDisabled();
	for (let i = 0; i < 18; i++) {
		await page.keyboard.press('Tab');
		expect(await d.evaluate((el) => el.contains(document.activeElement))).toBe(true);
	}
	await page.screenshot({ path: testInfo.outputPath('mobile-choices.png'), fullPage: true });
	const box = await d.boundingBox();
	expect(box!.x).toBeGreaterThanOrEqual(0);
	expect(box!.width).toBeLessThanOrEqual(390);
	expect(await d.evaluate((el) => el.scrollWidth <= el.clientWidth + 1)).toBe(true);
	await page.keyboard.press('Escape');
	expect(await sync.inspect()).toEqual(before);
});

test('new source query supersedes a held real older response', async ({ page, sync }) => {
	await catalog(page, sync);
	const a = sync.fixture.actors.unicode;
	await select(page, [a]);
	let release!: () => void, captured!: () => void, settled!: () => void;
	const routeSettled = new Promise<void>((resolve) => {
		settled = resolve;
	});
	const hold = new Promise<void>((r) => {
			release = r;
		}),
		seen = new Promise<void>((r) => {
			captured = r;
		});
	await page.route('**/api/v1/actress-name-sync/sources?**', async (route) => {
		if (new URL(route.request().url()).searchParams.get('q') !== '同じ名前') {
			await route.continue();
			return;
		}
		const response = await route.fetch();
		captured();
		await hold;
		try {
			await route.fulfill({ response }).catch(() => {});
		} finally {
			settled();
		}
	});
	const d = dialog(page);
	try {
		await d.getByRole('searchbox').fill('同じ名前');
		await d.getByRole('button', { name: 'Search offline evidence', exact: true }).click();
		await seen;
		await d.getByRole('searchbox').fill(a.source_id);
		await d.getByRole('button', { name: 'Search offline evidence', exact: true }).click();
		await expect(d.getByText(a.source_id, { exact: true })).toBeVisible();
		release();
		await routeSettled;
		await expect(
			d.getByRole('button', { name: 'Choose this source identity', exact: true }),
		).toHaveCount(1);
		await expect(d.getByText('r18-same-b', { exact: true })).toHaveCount(0);
		await expect(d.getByText('r18-unchanged', { exact: true })).toHaveCount(0);
		await expect(
			d.getByRole('button', { name: 'Review proposed changes', exact: true }),
		).toBeDisabled();
		expect((await sync.inspect()).jobs).toHaveLength(0);
	} finally {
		release();
	}
});

test.describe('manual mutation while queued', () => {
	test.use({ syncOptions: { pauseAfter: 0 } });
	test('explicit clear fences confirmed work and preserves credit overrides', async ({
		page,
		sync,
	}) => {
		await catalog(page, sync);
		const before = await sync.inspect(),
			a = sync.fixture.actors.managed;
		await select(page, [a]);
		await choose(page, a);
		await review(page);
		await confirm(page);
		await sync.waitForPause();
		const response = await page.request.put(`/api/v1/actresses/${a.id}`, {
			data: {
				dmm_id: a.dmm_id,
				first_name: a.name,
				last_name: '',
				japanese_name: '',
				thumb_url: '',
				aliases: 'Keep local alias metadata',
			},
		});
		expect(response.ok(), await response.text()).toBe(true);
		await sync.releaseWorker();
		await completed(page);
		const after = await sync.inspect();
		expect(after.actors.find((x) => x.id === a.id)?.japanese_name).toBe('');
		expect(after.tasks[0].status).toBe('invalidated');
		expect(after.credits).toEqual(before.credits);
	});
});

test('repeated confirmation clicks create only one durable batch', async ({ page, sync }) => {
	await catalog(page, sync);
	const a = sync.fixture.actors.managed;
	await select(page, [a]);
	await choose(page, a);
	await review(page);
	let posts = 0;
	page.on('request', (r) => {
		if (r.method() === 'POST' && r.url().endsWith('/actress-name-sync/jobs')) posts++;
	});
	await dialog(page)
		.getByRole('checkbox', { name: /I have checked/ })
		.check();
	await dialog(page).getByRole('button', { name: 'Confirm one-shot sync', exact: true }).dblclick();
	await completed(page);
	expect(posts).toBe(1);
	expect((await sync.inspect()).jobs).toHaveLength(1);
});

test('a selected actor deleted before review cannot create a stale identity update', async ({
	page,
	sync,
}) => {
	await catalog(page, sync);
	const a = sync.fixture.actors.managed2;
	await select(page, [a]);
	await choose(page, a);
	const response = await page.request.delete(`/api/v1/actresses/${a.id}`);
	expect(response.ok()).toBe(true);
	await dialog(page).getByRole('button', { name: 'Review proposed changes', exact: true }).click();
	await expect(dialog(page).getByRole('alert')).toContainText('not found');
	expect((await sync.inspect()).jobs).toHaveLength(0);
});
test('missing Japanese source names never clear a local name', async ({ page, sync }) => {
	await catalog(page, sync);
	const before = await sync.inspect(),
		a = sync.fixture.actors.managed;
	await select(page, [a]);
	await choose(page, a, 'r18-missing-name');
	await dialog(page).getByRole('button', { name: 'Review proposed changes', exact: true }).click();
	await expect(dialog(page).getByRole('alert')).toContainText('stale');
	expect(await sync.inspect()).toEqual(before);
});

for (const pending of ['sources', 'reviews', 'jobs'] as const)
	test(`navigation while ${pending} response is pending cannot authorize another batch`, async ({
		page,
		sync,
	}) => {
		await catalog(page, sync);
		const before = await sync.inspect(),
			a = sync.fixture.actors.managed;
		await select(page, [a]);
		if (pending !== 'sources') await choose(page, a);
		if (pending === 'jobs') await review(page);
		let release!: () => void, captured!: () => void, settled!: () => void;
		const hold = new Promise<void>((r) => {
				release = r;
			}),
			received = new Promise<void>((r) => {
				captured = r;
			}),
			routeSettled = new Promise<void>((r) => {
				settled = r;
			});
		await page.route(
			`**/api/v1/actress-name-sync/${pending}*`,
			async (route) => {
				const response = await route.fetch();
				captured();
				await hold;
				try {
					await route.fulfill({ response }).catch(() => {});
				} finally {
					settled();
				}
			},
			{ times: 1 },
		);
		try {
			if (pending === 'sources') {
				await dialog(page).getByRole('searchbox').fill(a.source_id);
				await dialog(page)
					.getByRole('button', { name: 'Search offline evidence', exact: true })
					.click();
			} else if (pending === 'reviews')
				await dialog(page)
					.getByRole('button', { name: 'Review proposed changes', exact: true })
					.click();
			else {
				await dialog(page)
					.getByRole('checkbox', { name: /I have checked/ })
					.check();
				await dialog(page)
					.getByRole('button', { name: 'Confirm one-shot sync', exact: true })
					.click();
			}
			await received;
			// Deliberate full-document navigation while delivery is held, not after closing.
			await page.goto('/');
			release();
			await routeSettled;
			await page.goBack();
			await expect(
				page.getByRole('heading', { name: 'Actress Database', exact: true }),
			).toBeVisible();
			await expect(dialog(page)).not.toBeVisible();
			let after = await sync.inspect();
			expect(after.jobs).toHaveLength(pending === 'jobs' ? 1 : 0);
			expect(after.credits).toEqual(before.credits);
			await history(page);
			if (pending === 'jobs') {
				await dialog(page)
					.getByRole('button', { name: 'Retry same confirmation', exact: true })
					.click();
				await completed(page);
				after = await sync.inspect();
				expect(after.jobs).toHaveLength(1);
			} else {
				await expect(
					dialog(page).getByRole('button', { name: 'Retry same confirmation', exact: true }),
				).toHaveCount(0);
				expect(await sync.inspect()).toEqual(before);
			}
		} finally {
			release();
		}
	});

test('revoked session rejects confirmation and login never automatically replays it', async ({
	page,
	sync,
}) => {
	await catalog(page, sync);
	const before = await sync.inspect(),
		a = sync.fixture.actors.managed;
	await select(page, [a]);
	await choose(page, a);
	await review(page);
	const logout = await page.request.post('/api/v1/auth/logout');
	expect(logout.ok()).toBe(true);
	await dialog(page)
		.getByRole('checkbox', { name: /I have checked/ })
		.check();
	const rejected = page.waitForResponse(
		(r) => r.request().method() === 'POST' && r.url().endsWith('/actress-name-sync/jobs'),
	);
	await dialog(page).getByRole('button', { name: 'Confirm one-shot sync', exact: true }).click();
	expect((await rejected).status()).toBe(401);
	expect(await sync.inspect()).toEqual(before);
	await page.reload();
	await page.getByLabel('Username', { exact: true }).fill('admin');
	await page.getByLabel('Password', { exact: true }).fill('adminpassword123');
	await page.getByRole('button', { name: 'Sign In', exact: true }).click();
	await expect(page.getByRole('heading', { name: 'Actress Database', exact: true })).toBeVisible();
	await expect(dialog(page)).not.toBeVisible();
	expect(await sync.inspect()).toEqual(before);
	await history(page);
	expect(await sync.inspect()).toEqual(before);
});
