import { test, expect } from '@playwright/test';

test('authenticated SSR returns the application shell without an auth placeholder', async ({ request }) => {
	const response = await request.get('/browse');
	expect(response.ok()).toBe(true);
	const html = await response.text();
	expect(html).toContain('Browse &amp; Scrape');
	expect(html).not.toContain('Checking authentication');
	expect(html).not.toContain('Login Required');
});

test('Browse reflects the persisted plan state after hydration', async ({ page }) => {
	await page.goto('/browse');
	await expect(page.getByRole('radio', { name: /Organize into another location/ })).toBeChecked();
	await expect.poll(async () => (await page.context().cookies()).some((cookie) => cookie.name === 'javinizer_browse_bootstrap')).toBe(true);
	await page.getByRole('button', { name: 'Collapse plan' }).click();
	await expect(page.getByRole('button', { name: 'Expand plan' })).toHaveAttribute('aria-expanded', 'false');
	await expect.poll(async () => {
		const cookie = (await page.context().cookies()).find((item) => item.name === 'javinizer_browse_bootstrap');
		return cookie ? JSON.parse(decodeURIComponent(cookie.value)).planExpanded : undefined;
	}).toBe(false);
	const response = await page.reload();
	expect(response).not.toBeNull();
	const expandButton = page.getByRole('button', { name: 'Expand plan' });
	await expect(expandButton).toBeVisible();
	await expect(expandButton).toHaveAttribute('aria-expanded', 'false');
	await expect(page.getByRole('button', { name: 'Collapse plan' })).toHaveCount(0);
});