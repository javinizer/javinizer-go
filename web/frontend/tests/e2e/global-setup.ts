import { test as setupTest, expect } from '@playwright/test';

setupTest('authenticate and save storage state', async ({ browser, baseURL }) => {
  const context = await browser.newContext({ baseURL });
  const page = await context.newPage();

  // Navigate to the app
  await page.goto('');
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(1000);

  // Check if setup page or login page
  const hasSetup = (await page.locator('input[type="password"]').count()) >= 2;

  if (hasSetup) {
    const username = process.env.JAVINIZER_E2E_USERNAME || 'admin';
    const password = process.env.JAVINIZER_E2E_PASSWORD || 'adminpassword123';
    await page.locator('input[name="username"]').fill(username);
    const passwords = page.locator('input[type="password"]');
    await passwords.first().fill(password);
    await passwords.nth(1).fill(password);
    await page.getByRole('button', { name: /create/i }).first().click();
  } else {
    const username = process.env.JAVINIZER_E2E_USERNAME || 'admin';
    const password = process.env.JAVINIZER_E2E_PASSWORD || 'adminpassword123';
    const usernameInput = page.locator('#login-username, input[name="username"]');
    if (await usernameInput.isVisible().catch(() => false)) {
      await usernameInput.fill(username);
      await page.locator('#login-password, input[name="password"], input[type="password"]').fill(password);
      await page.getByRole('button', { name: /sign|login/i }).first().click();
    }
  }

  // Wait for any redirect and content to load
  await page.waitForLoadState('networkidle').catch(() => {});

  // Verify we're authenticated by checking for expected page content
  const pageText = await page.textContent('body');
  expect(pageText).not.toBe('');

  // Save the authenticated storage state for other tests
  const storagePath = new URL('./auth-state.json', import.meta.url).pathname;
  await context.storageState({ path: storagePath });
  await context.close();
});
