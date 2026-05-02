import type { BatchJobResponse, FileResult, Movie, ScraperInfo } from '$lib/api/types';

const JOB_ID = 'mock-job-001';

function makeMovie(id: string, overrides: Partial<Movie> = {}): Movie {
	return {
		id,
		title: `${id} Title`,
		display_title: `${id} Title`,
		...overrides,
	};
}

const completeMovie = makeMovie('COMPLETE-1', {
	poster_url: 'https://example.com/poster.jpg',
	cover_url: 'https://example.com/cover.jpg',
	actresses: [{ id: 1, first_name: 'Yui', last_name: 'Hatano' }],
	genres: [{ name: 'Drama' }, { name: 'Romance' }],
	description: 'A complete movie with all metadata',
	maker: 'Test Maker',
	release_date: '2024-01-15',
	director: 'Test Director',
	runtime: 120,
	trailer_url: 'https://example.com/trailer.mp4',
	screenshot_urls: ['https://example.com/ss1.jpg', 'https://example.com/ss2.jpg', 'https://example.com/ss3.jpg'],
	label: 'Test Label',
	series: 'Test Series',
});

const partialMovie = makeMovie('PARTIAL-1', {
	poster_url: 'https://example.com/poster2.jpg',
	cover_url: 'https://example.com/cover2.jpg',
	actresses: [{ id: 2, first_name: 'Aki', last_name: 'Sora' }],
	genres: [{ name: 'Action' }],
});

const incompleteMovie = makeMovie('INCOMPLETE-1');

const incompleteMovie2 = makeMovie('INCOMPLETE-2', {
	maker: 'Some Maker',
});

const completeMovie2 = makeMovie('COMPLETE-2', {
	poster_url: 'https://example.com/poster3.jpg',
	cover_url: 'https://example.com/cover3.jpg',
	actresses: [{ id: 3, first_name: 'Miku', last_name: 'Abeno' }],
	genres: [{ name: 'Comedy' }, { name: 'Drama' }],
	description: 'Another complete movie',
	maker: 'Another Maker',
	release_date: '2024-03-20',
	runtime: 90,
	screenshot_urls: ['https://example.com/ss4.jpg', 'https://example.com/ss5.jpg', 'https://example.com/ss6.jpg'],
});

function makeFileResult(movie: Movie, filePath?: string): FileResult {
	return {
		file_path: filePath || `/videos/${movie.id}.mp4`,
		movie_id: movie.id,
		status: 'completed',
		started_at: '2024-01-15T10:00:00Z',
		ended_at: '2024-01-15T10:00:05Z',
		data: movie,
	};
}

function buildBatchJob(excluded: Record<string, boolean> = {}): BatchJobResponse {
	return {
		id: JOB_ID,
		status: 'completed',
		total_files: 5,
		completed: 5,
		failed: 0,
		operation_count: 0,
		reverted_count: 0,
		excluded,
		progress: 100,
		destination: '/output',
		results: {
			'/videos/COMPLETE-1.mp4': makeFileResult(completeMovie),
			'/videos/PARTIAL-1.mp4': makeFileResult(partialMovie),
			'/videos/INCOMPLETE-1.mp4': makeFileResult(incompleteMovie),
			'/videos/INCOMPLETE-2.mp4': makeFileResult(incompleteMovie2),
			'/videos/COMPLETE-2.mp4': makeFileResult(completeMovie2),
		},
		started_at: '2024-01-15T10:00:00Z',
		completed_at: '2024-01-15T10:00:05Z',
		update: false,
	};
}

export const mockConfig = {
	output: {
		download_cover: true,
		download_poster: true,
		download_trailer: true,
		download_extrafanart: true,
		operation_mode: 'organize',
	},
	metadata: {
		completeness: {
			enabled: true,
			tiers: {
				essential: { weight: 3, fields: ['title', 'poster_url', 'cover_url', 'actresses', 'genres'] },
				important: { weight: 2, fields: ['description', 'maker', 'release_date', 'director', 'runtime', 'trailer_url', 'screenshot_urls'] },
				nice_to_have: { weight: 1, fields: ['label', 'series', 'rating_score', 'original_title'] },
			},
		},
	},
	webui: {
		default_review_view: 'grid-poster',
	},
};

export const mockScraperInfos: ScraperInfo[] = [
	{ name: 'javbus', display_title: 'JavBus', enabled: true },
	{ name: 'javdb', display_title: 'JavDB', enabled: true },
	{ name: 'r18', display_title: 'R18', enabled: false },
];

export { JOB_ID };

export async function setupMockRoutes(page: import('@playwright/test').Page): Promise<void> {
	let excluded: Record<string, boolean> = {};

	await page.route('**/api/v1/batch/**', async (route) => {
		const url = route.request().url();
		const method = route.request().method();

		if (method === 'GET' && url.includes(`/batch/${JOB_ID}`)) {
			await route.fulfill({ json: buildBatchJob(excluded) });
		} else if (method === 'POST' && url.includes('/batch-exclude')) {
			const body = route.request().postDataJSON();
			const movieIds: string[] = body?.movie_ids ?? [];
			const currentJob = buildBatchJob(excluded);
			const filePathsToExclude: string[] = [];
			for (const [filePath, result] of Object.entries(currentJob.results)) {
				const r = result as FileResult;
				if (movieIds.includes(r.movie_id)) {
					filePathsToExclude.push(filePath);
				}
			}
			for (const fp of filePathsToExclude) {
				excluded[fp] = true;
			}
			await route.fulfill({
				json: {
					excluded: movieIds,
					failed: [],
					job: buildBatchJob(excluded),
				},
			});
		} else if (method === 'POST' && url.includes('/exclude')) {
			const urlParts = url.split('/');
			const moviesIdx = urlParts.indexOf('movies');
			const movieId = moviesIdx !== -1 ? urlParts[moviesIdx + 1] : '';
			const job = buildBatchJob(excluded);
			for (const [filePath, result] of Object.entries(job.results)) {
				const r = result as FileResult;
				if (r.movie_id === movieId) {
					excluded[filePath] = true;
				}
			}
			await route.fulfill({ json: { message: 'excluded' } });
		} else if (method === 'POST' && url.includes('/batch-rescrape')) {
			await route.fulfill({
				json: {
					results: [],
					succeeded: 0,
					failed: 0,
					job: buildBatchJob(excluded),
				},
			});
		} else if (method === 'POST' && url.includes('/organize')) {
			await route.fulfill({ json: { success: true } });
		} else if (method === 'PATCH' && url.includes('/movies/')) {
			await route.fulfill({ json: { success: true } });
		} else {
			await route.continue();
		}
	});

	await page.route('**/api/v1/config', async (route) => {
		await route.fulfill({ json: mockConfig });
	});

	await page.route('**/api/v1/scrapers', async (route) => {
		await route.fulfill({ json: { scrapers: mockScraperInfos } });
	});

	await page.route('**/api/v1/auth/status', async (route) => {
		await route.fulfill({ json: { initialized: true, authenticated: true, username: 'admin' } });
	});

	await page.route('**/api/v1/temp/image**', async (route) => {
		await route.fulfill({
			status: 200,
			contentType: 'image/svg+xml',
			body: '<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"/>',
		});
	});
}

export async function navigateToReviewPage(page: import('@playwright/test').Page): Promise<void> {
	await setupMockRoutes(page);
	await page.goto(`/review/${JOB_ID}`);
	await page.waitForLoadState('domcontentloaded');
}

export async function switchToGridView(page: import('@playwright/test').Page): Promise<void> {
	const posterButton = page.getByRole('button', { name: /^poster$/i });
	await posterButton.click();
	await page.waitForSelector('div[role="button"], div[role="checkbox"]', { timeout: 10_000 });
}

export function getGridCardSelector(): string {
	return 'div[role="button"], div[role="checkbox"]';
}

export async function getGridCards(page: import('@playwright/test').Page): Promise<import('@playwright/test').Locator[]> {
	const cards = page.locator(getGridCardSelector());
	const count = await cards.count();
	const result: import('@playwright/test').Locator[] = [];
	for (let i = 0; i < count; i++) {
		result.push(cards.nth(i));
	}
	return result;
}

export function getGridCard(page: import('@playwright/test').Page, index: number): import('@playwright/test').Locator {
	return page.locator(getGridCardSelector()).nth(index);
}

export async function enableSelectionMode(page: import('@playwright/test').Page): Promise<void> {
	const selectBtn = page.getByRole('button', { name: /^select$/i }).first();
	await selectBtn.waitFor({ state: 'visible', timeout: 10_000 });
	const isPressed = await selectBtn.getAttribute('aria-pressed');
	if (isPressed !== 'true') {
		await selectBtn.click();
	}
}

export function getSelectAllButton(page: import('@playwright/test').Page): import('@playwright/test').Locator {
	return page.getByRole('button', { name: /select all|deselect all/i });
}

export function getCompletenessDial(page: import('@playwright/test').Page, cardIndex: number): import('@playwright/test').Locator {
	const card = page.locator(getGridCardSelector()).nth(cardIndex);
	return card.locator('div[role="img"][aria-label*="complete"]');
}

export function getCompletenessFilterButton(page: import('@playwright/test').Page, tier: 'Incomplete' | 'Partial' | 'Complete'): import('@playwright/test').Locator {
	return page.getByRole('button', { name: new RegExp(`^${tier}\\s*\\(\\d+\\)`) }).first();
}

export function getBulkRemoveButton(page: import('@playwright/test').Page): import('@playwright/test').Locator {
	return page.getByRole('button', { name: /remove/i });
}

export function getBulkRescrapeButton(page: import('@playwright/test').Page): import('@playwright/test').Locator {
	return page.getByRole('button', { name: /rescrape/i });
}

export function getDetailViewButton(page: import('@playwright/test').Page): import('@playwright/test').Locator {
	return page.getByRole('button', { name: 'Detail', exact: true });
}

export function getGridViewButton(page: import('@playwright/test').Page): import('@playwright/test').Locator {
	return page.getByRole('button', { name: /^poster$/i });
}

export function getRescrapeModal(page: import('@playwright/test').Page): import('@playwright/test').Locator {
	return page.locator('.fixed.inset-0.bg-black\\/50').filter({ hasText: /rescrape/i }).first();
}
