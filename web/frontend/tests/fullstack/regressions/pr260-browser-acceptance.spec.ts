import { test, expect, type APIRequestContext } from '@playwright/test';
import {
	BACKEND_BASE,
	DEFAULT_INPUT_DIR,
	loginAgainstRealBackend,
	navigateToReviewPage,
	seedInputFiles,
	submitScrape,
	waitForJobCompletion,
	soleResult,
} from '../helpers';

type ActressRestore = {
	id: number;
	payload: {
		dmm_id: number;
		first_name: string;
		last_name: string;
		japanese_name: string;
		thumb_url: string;
		aliases: string;
	};
};

let actressRestore: ActressRestore | undefined;
let createdActressID: number | undefined;

test.afterEach(async ({ request }: { request: APIRequestContext }) => {
	if (!actressRestore && !createdActressID) return;
	await loginAgainstRealBackend(request);
	if (createdActressID) {
		const createdID = createdActressID;
		createdActressID = undefined;
		const response = await request.delete(BACKEND_BASE + '/api/v1/actresses/' + createdID, {
			failOnStatusCode: false,
		});
		expect(response.status(), await response.text()).toBe(200);
	}
	if (actressRestore) {
		const restore = actressRestore;
		actressRestore = undefined;
		const response = await request.put(BACKEND_BASE + '/api/v1/actresses/' + restore.id, {
			data: restore.payload,
			failOnStatusCode: false,
		});
		expect(response.status(), await response.text()).toBe(200);
	}
});

test('PR260 browser resolve then save and reload preserves canonical cast', async ({
	page,
	request,
}: {
	page: import('@playwright/test').Page;
	request: APIRequestContext;
}) => {
	test.setTimeout(120_000);
	await loginAgainstRealBackend(request);

	const runTag = String(Date.now() % 100000).padStart(5, '0');
	const targetID = `GOOD-${runTag}`;
	const targetFile = `${targetID}.mp4`;
	const editedTitle = `PR260 browser title ${runTag}`;
	await seedInputFiles([targetFile]);

	const existingIdentitiesResponse = await request.get(
		`${BACKEND_BASE}/api/v1/actresses?limit=100`,
	);
	expect(existingIdentitiesResponse.ok(), await existingIdentitiesResponse.text()).toBe(true);
	const existingIdentities = await existingIdentitiesResponse.json();
	const existingTarget = existingIdentities.actresses.find(
		(actress: { dmm_id: number }) => actress.dmm_id === 1,
	) as
		| {
				id: number;
				dmm_id: number;
				first_name?: string;
				last_name?: string;
				japanese_name?: string;
				thumb_url?: string;
				aliases?: string;
		  }
		| undefined;
	if (existingTarget) {
		actressRestore = {
			id: existingTarget.id,
			payload: {
				dmm_id: existingTarget.dmm_id,
				first_name: existingTarget.first_name ?? '',
				last_name: existingTarget.last_name ?? '',
				japanese_name: existingTarget.japanese_name ?? '',
				thumb_url: existingTarget.thumb_url ?? '',
				aliases: existingTarget.aliases ?? '',
			},
		};
	}
	const identityPayload = {
		dmm_id: 1,
		first_name: `PR260${runTag}`,
		last_name: 'Canonical',
		japanese_name: `PR260${runTag} Canonical`,
	};
	if (existingTarget) {
		const releaseResponse = await request.put(
			`${BACKEND_BASE}/api/v1/actresses/${existingTarget.id}`,
			{
				data: { ...actressRestore!.payload, dmm_id: 800000 + Number(runTag) },
				failOnStatusCode: false,
			},
		);
		expect(releaseResponse.status(), await releaseResponse.text()).toBe(200);
	}
	const canonicalResponse = await request.post(`${BACKEND_BASE}/api/v1/actresses`, {
		data: identityPayload,
		failOnStatusCode: false,
	});
	expect(canonicalResponse.status(), await canonicalResponse.text()).toBe(201);
	const target = await canonicalResponse.json();
	createdActressID = target.id;
	expect(target.verified, 'fixture target must be verified').toBe(true);
	expect(target.id, 'fixture target must have an id').toBeGreaterThan(0);

	const relinkIdentityResponse = await request.post(`${BACKEND_BASE}/api/v1/actresses`, {
		data: {
			dmm_id: 900000 + Number(runTag),
			first_name: `PR260${runTag}`,
			last_name: 'Relinked',
			japanese_name: `PR260${runTag} Relinked`,
		},
		failOnStatusCode: false,
	});
	expect(relinkIdentityResponse.status(), await relinkIdentityResponse.text()).toBe(201);
	const relinkTarget = await relinkIdentityResponse.json();
	expect(relinkTarget.verified, 'fixture relink target must be verified').toBe(true);
	expect(relinkTarget.id, 'fixture relink target must have an id').toBeGreaterThan(0);

	const collisionJob = await submitScrape(request, {
		files: [`${DEFAULT_INPUT_DIR}/${targetFile}`],
	});
	const collisionJobBody = await waitForJobCompletion(request, collisionJob, {
		timeoutMs: 120_000,
	});
	const initialRevision = (soleResult(collisionJobBody).result as { revision?: number }).revision;
	const collisionResult = soleResult(collisionJobBody).result;
	expect(collisionResult.movie_id).toBe(targetID);

	let collisionList: { collisions: Array<Record<string, unknown>> } = { collisions: [] };
	await expect
		.poll(
			async () => {
				const collisionListResponse = await request.get(
					`${BACKEND_BASE}/api/v1/actresses/collisions?movie_id=${encodeURIComponent(collisionResult.movie_id)}`,
				);
				if (!collisionListResponse.ok()) return -1;
				collisionList = await collisionListResponse.json();
				return collisionList.collisions.length;
			},
			{
				timeout: 30_000,
				intervals: [100, 250, 500],
				message: 'real apply must persist one open collision',
			},
		)
		.toBe(1);
	expect(collisionList.collisions[0]).toMatchObject({
		movie_content_id: targetID,
		field: 'credited_name',
		status: 'open',
	});
	const browserBatchPromise = page.waitForResponse(
		(response) =>
			response.url().includes(`/api/v1/batch/${collisionJob}`) &&
			response.url().includes('include_data=true'),
	);
	await navigateToReviewPage(page, `${collisionJob}?view=detail`);
	const browserBatch = await browserBatchPromise;
	const browserBody = (await browserBatch.json()) as {
		results: Record<string, { movie?: { code?: string } }>;
	};
	const browserMovie = Object.values(browserBody.results)[0]?.movie;
	expect(browserMovie?.code, JSON.stringify(browserMovie)).toBe(targetID);
	await expect(page.getByText('Collisions — organize is blocked until resolved')).toBeVisible();

	await page.getByRole('button', { name: 'Relink', exact: true }).click();
	const chooser = page.getByRole('dialog', { name: 'Choose verified actress to relink' });
	await expect(chooser).toBeVisible();
	const searchInput = chooser.getByRole('textbox', { name: 'Search verified actresses' });
	await searchInput.fill(`PR260${runTag}`);
	await chooser.getByRole('button', { name: 'Search', exact: true }).click();
	const targetOption = chooser.getByRole('button', {
		name: new RegExp(`#${relinkTarget.id}$`),
	});
	await expect(targetOption).toContainText(`Relinked PR260${runTag}`);
	await targetOption.click();
	const resolveResponsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'POST' &&
			response.url().includes('/actresses/collisions/') &&
			response.url().endsWith('/resolve'),
	);
	await chooser.getByRole('button', { name: 'Confirm relink' }).click();
	const resolveResponse = await resolveResponsePromise;
	expect(resolveResponse.ok(), await resolveResponse.text()).toBe(true);
	await expect(page.getByRole('button', { name: 'Relink', exact: true })).toHaveCount(0, {
		timeout: 30_000,
	});

	const titleLabel = page
		.locator('label')
		.filter({ hasText: /^Title/ })
		.first();
	const titleInput = titleLabel.locator('..').locator('input').first();
	await expect(titleInput).toHaveValue(new RegExp(`E2E Movie ${targetID}`));
	await titleInput.fill(editedTitle);
	await titleInput.press('Tab');
	const saveButton = page.getByRole('button', { name: /Save changes/ });
	await expect(saveButton).toBeVisible();
	const saveRequestPromise = page.waitForRequest(
		(requestEvent) =>
			requestEvent.method() === 'PATCH' &&
			requestEvent.url().includes(`/api/v1/batch/${collisionJob}/results/`),
	);
	const saveResponsePromise = page.waitForResponse(
		(response) =>
			response.request().method() === 'PATCH' &&
			response.url().includes(`/api/v1/batch/${collisionJob}/results/`),
	);
	await saveButton.click();
	const [saveRequest, saveResponse] = await Promise.all([saveRequestPromise, saveResponsePromise]);
	expect(saveResponse.ok(), await saveResponse.text()).toBe(true);
	const savePayload = saveRequest.postDataJSON() as {
		movie?: unknown;
		expected_result_revision?: unknown;
		expected_result_revisions?: unknown;
	};
	expect(savePayload.movie).toBeTruthy();
	expect(
		typeof savePayload.expected_result_revision === 'number' ||
			typeof savePayload.expected_result_revisions === 'object',
		'save must carry the result revision protocol',
	).toBe(true);

	const afterSaveBatchResponse = await request.get(
		`${BACKEND_BASE}/api/v1/batch/${collisionJob}?include_data=true`,
	);
	expect(afterSaveBatchResponse.ok(), await afterSaveBatchResponse.text()).toBe(true);
	const afterSaveBatch = (await afterSaveBatchResponse.json()) as {
		results: Record<string, { result_id: string; movie_id: string; revision: number }>;
	};
	const afterSaveResult = Object.values(afterSaveBatch.results).find(
		(result) => result.result_id === collisionResult.result_id,
	);
	expect(afterSaveResult).toBeTruthy();
	expect(afterSaveResult?.movie_id).toBe(targetID);
	expect(afterSaveResult?.revision).toBeGreaterThan(initialRevision ?? 0);

	const movieResponse = await request.get(
		`${BACKEND_BASE}/api/v1/movies/${encodeURIComponent(targetID)}`,
	);
	expect(movieResponse.ok(), await movieResponse.text()).toBe(true);
	const persistedMovie = (await movieResponse.json()).movie;
	expect(persistedMovie.id).toBe(targetID);
	expect(persistedMovie.title).toBe(editedTitle);
	expect(persistedMovie.actresses).toHaveLength(1);
	expect(persistedMovie.actresses[0]).toMatchObject({
		id: relinkTarget.id,
		first_name: `PR260${runTag}`,
		last_name: 'Relinked',
		verified: true,
	});
	expect(persistedMovie.actresses[0].id).not.toBe(target.id);

	const resolvedCollisionResponse = await request.get(
		`${BACKEND_BASE}/api/v1/actresses/collisions?movie_id=${encodeURIComponent(targetID)}`,
	);
	expect(resolvedCollisionResponse.ok(), await resolvedCollisionResponse.text()).toBe(true);
	expect((await resolvedCollisionResponse.json()).collisions).toHaveLength(0);

	await page.reload();
	await page.waitForLoadState('domcontentloaded');
	await page.waitForLoadState('networkidle');
	await expect(titleInput).toHaveValue(editedTitle);
	await expect(page.locator(`p[title="Relinked PR260${runTag}"]`)).toBeVisible();
});
