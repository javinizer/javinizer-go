import { test, expect } from '@playwright/test';
import { JOB_ID, setupMockRoutes } from './review-helpers';

test('initial review load failure renders an accessible retry state', async ({ page }) => {
	await setupMockRoutes(page);
	let firstRequest = true;
	const pattern = `**/api/v1/batch/${JOB_ID}?include_data=true`;
	await page.route(pattern, async (route) => {
		if (firstRequest) {
			firstRequest = false;
			await route.fulfill({
				status: 500,
				contentType: 'application/json',
				body: JSON.stringify({ error: 'simulated review failure' }),
			});
			return;
		}
		await route.fallback();
	});

	try {
		await page.goto(`/review/${JOB_ID}`);
		const loadAlert = page.locator('[role="alert"][aria-labelledby="review-load-error-title"]');
		await expect(loadAlert).toBeVisible();
		await expect(loadAlert).toHaveAttribute('aria-labelledby', 'review-load-error-title');
		await expect(page.getByRole('button', { name: 'Retry load' })).toBeVisible();
		await expect(page.getByText('Loading batch job...')).not.toBeVisible();

		await page.getByRole('button', { name: 'Retry load' }).click();
		await expect(page.getByText('COMPLETE-1').first()).toBeVisible({ timeout: 10_000 });
	} finally {
		await page.unroute(pattern);
	}
});
