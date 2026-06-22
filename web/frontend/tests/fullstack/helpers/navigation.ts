/**
 * Browser-navigation helpers for full-stack E2E specs. Each helper drives a
 * `page` fixture against the real SvelteKit frontend and waits for the
 * user-visible DOM the spec will then assert on.
 *
 * Helpers DO NOT call `page.route(...)`. Every API request the frontend
 * issues flows through Vite's proxy to the real Go backend.
 */
import type { Page } from '@playwright/test';

/**
 * Drive the real browser to `/review/[jobId]` (the review page component,
 * SvelteKit SSR + hydrated by the real Vite dev server) and wait for the
 * page to settle before returning control to the spec.
 *
 * The review page renders one of two top-level states: the movie grid
 * (scrape-success path) or the "Unidentified Files" card (scrape-failure
 * path). Both render content via the shared Card component, so the spec
 * follows up with `getByText(...)` queries that have their own timeouts.
 */
export async function navigateToReviewPage(page: Page, jobId: string): Promise<void> {
	await page.goto(`/review/${jobId}`);
	await page.waitForLoadState('domcontentloaded');
	// networkidle gates ad-hoc-mount DOM queries: the review page does
	// additional fetches (movies, jobs, scrapers list) after hydration,
	// and assertions like `getByText('FAIL-003.mp4')` race if they run
	// before the post-mount fetch resolves.
	await page.waitForLoadState('networkidle');
}

/**
 * Drive the real browser to `/jobs` (the jobs list page). The jobs list
 * renders the per-job card with the (possibly inlined) movie card /
 * thumbnail preview. Spec follows with getByText queries as needed.
 */
export async function navigateToJobsPage(page: Page): Promise<void> {
	await page.goto('/jobs');
	await page.waitForLoadState('domcontentloaded');
	await page.waitForLoadState('networkidle');
}
