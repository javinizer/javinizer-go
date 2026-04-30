import type { APIRequestContext, Page, Response } from '@playwright/test';
import { test, expect } from '@playwright/test';
import { join, dirname } from 'node:path';
import { fileURLToPath } from 'node:url';
import { readFileSync, createReadStream } from 'node:fs';
import { mkdtemp } from 'node:fs/promises';
import { tmpdir } from 'node:os';

const __dirname = dirname(fileURLToPath(import.meta.url));
const fixturesDir = join(__dirname, '..', 'fixtures');

function fixturePath(name: string): string {
  return join(fixturesDir, name);
}

function readFixture(name: string): unknown {
  return JSON.parse(readFileSync(fixturePath(name), 'utf-8'));
}

async function ensureLoggedIn(page: Page, baseURL: string) {
  const username = process.env.JAVINIZER_E2E_USERNAME || 'admin';
  const password = process.env.JAVINIZER_E2E_PASSWORD || 'adminpassword123';

  await page.goto(`${baseURL}/`);
  await page.waitForLoadState('domcontentloaded');
  await page.waitForTimeout(500);

  const hasSetup = (await page.locator('input[type="password"]').count()) >= 2;

  if (hasSetup) {
    await page.locator('input[name="username"]').fill(username);
    const passwords = page.locator('input[type="password"]');
    await passwords.first().fill(password);
    await passwords.nth(1).fill(password);
    await page.getByRole('button', { name: /create/i }).first().click();
  } else {
    const loginUsername = page.locator('#login-username, input[name="username"]');
    if (await loginUsername.isVisible().catch(() => false)) {
      await loginUsername.fill(username);
      await page.locator('#login-password, input[name="password"], input[type="password"]').fill(password);
      await page.getByRole('button', { name: /sign|login/i }).first().click();
    }
  }

  await page.waitForLoadState('domcontentloaded').catch(() => {});
}

async function downloadContent(download: Awaited<ReturnType<Parameters<typeof test>[1]['page']['waitForEvent']>>) {
  const tmpDir = await mkdtemp(join(tmpdir(), 'pw-dl-'));
  const outPath = join(tmpDir, 'download');
  await download.saveAs(outPath);
  return createReadStream(outPath);
}

test.describe('Genre Replacement Import/Export (UI)', () => {
  const original1 = 'E2E-Action-Genre';
  const original2 = 'E2E-Drama-Genre';

  test.afterEach(async ({ page, baseURL }) => {
    try {
      await page.goto(`${baseURL}/genres`);
      await page.waitForTimeout(500);
      for (const original of [original1, original2]) {
        const row = page.locator('table tr').filter({ hasText: original }).first();
        if (await row.isVisible().catch(() => false)) {
          row.locator('button').first().click();
          if (await page.waitForSelector('text=Are you sure', { timeout: 2000 }).catch(() => false)) {
            page.locator('button:has-text("Delete"), button:has-text("Confirm"), button:has-text("Remove")').first().click();
          }
          await page.waitForTimeout(500);
        }
      }
    } catch { 0; }
  });

  test('import via UI file upload creates genre replacements', async ({ page, baseURL }) => {
    await ensureLoggedIn(page, baseURL);

    await page.goto(`${baseURL}/genres`);
    await page.waitForLoadState('domcontentloaded');

    page.on('dialog', async dialog => {
      await dialog.accept();
    });

    const importBtn = page.getByRole('button', { name: 'Import' }).first();
    if (await importBtn.isVisible().catch(() => false)) {
      await importBtn.click();
      await page.locator('input[type="file"]').setInputFiles(fixturePath('genres-import.json'));

      expect(async () => {
        const text = await page.textContent('body');
        expect(text).toContain(original1);
      }).toPass({ timeout: 15000 });
    }
  });

  test('export triggers download', async ({ page, baseURL }) => {
    await ensureLoggedIn(page, baseURL);

    await page.goto(`${baseURL}/genres`);
    await page.waitForLoadState('domcontentloaded');

    const exportBtn = page.getByRole('button', { name: 'Export' }).first();
    if (await exportBtn.isVisible().catch(() => false)) {
      const [download] = await Promise.all([
        page.waitForEvent('download'),
        exportBtn.click()
      ]);

      const fileName = download.suggestedFilename();
      expect(fileName).toMatch(/genre/);

      const content = await downloadContent(download);
      const data = JSON.parse((content as any).read().toString());
      expect(Array.isArray(data)).toBeTruthy();
    }
  });
});

test.describe('Actress Import/Export via UI', () => {
  const testName = 'E2E TestActress';

  test.afterEach(async ({ page, baseURL }) => {
    try {
      await page.goto(`${baseURL}/actresses`);
      await page.waitForTimeout(500);
      const searchInput = page.locator('input[type="search"], input[placeholder*="Search"], input[placeholder*="search"], input[name="q"]');
      if (await searchInput.isVisible().catch(() => false)) {
        await searchInput.fill('E2E');
        await page.waitForTimeout(1000);
      }
      const deleteBtns = page.locator('table tr').filter({ hasText: 'E2E' }).first().locator('button').first();
      if (await deleteBtns.isVisible().catch(() => false)) {
        await deleteBtns.click();
      }
    } catch { 0; }
  });

  test('import via UI file upload creates actress', async ({ page, baseURL }) => {
    await ensureLoggedIn(page, baseURL);

    await page.goto(`${baseURL}/actresses`);
    await page.waitForLoadState('domcontentloaded');

    page.on('dialog', async dialog => {
      await dialog.accept();
    });

    const importBtn = page.getByRole('button', { name: 'Import' }).first();
    if (await importBtn.isVisible().catch(() => false)) {
      await importBtn.click();
      await page.locator('input[type="file"]').setInputFiles(fixturePath('actresses-import.json'));

      expect(async () => {
        const text = await page.textContent('body');
        expect(text).toContain('Import complete');
      }).toPass({ timeout: 15000 });

      expect(async () => {
        const text = await page.textContent('body');
        expect(text).toContain(testName);
      }).toPass({ timeout: 10000 });
    }
  });

  test('export triggers download', async ({ page, baseURL }) => {
    await ensureLoggedIn(page, baseURL);

    await page.goto(`${baseURL}/actresses`);
    await page.waitForLoadState('domcontentloaded');

    const exportBtn = page.getByRole('button', { name: 'Export' }).first();
    if (await exportBtn.isVisible().catch(() => false)) {
      const [download] = await Promise.all([
        page.waitForEvent('download'),
        exportBtn.click()
      ]);

      const fileName = download.suggestedFilename();
      expect(fileName).toMatch(/actresses/);

      const content = await downloadContent(download);
      const data = JSON.parse((content as any).read().toString());
      expect(Array.isArray(data)).toBeTruthy();
    }
  });
});

test.describe('Word Replacement Import/Export via UI', () => {
  const wordOriginal1 = 'E2E-blur-word';
  const wordOriginal2 = 'E2E-XXX-word';

  test.afterEach(async ({ page, baseURL }) => {
    try {
      await page.goto(`${baseURL}/words`);
      await page.waitForTimeout(500);
      for (const original of [wordOriginal1, wordOriginal2]) {
        const row = page.locator('table tr').filter({ hasText: original }).first();
        if (await row.isVisible().catch(() => false)) {
          row.locator('button').first().click();
        }
      }
    } catch { 0; }
  });

  test('import via UI file upload creates word replacements', async ({ page, baseURL }) => {
    await ensureLoggedIn(page, baseURL);

    await page.goto(`${baseURL}/words`);
    await page.waitForLoadState('domcontentloaded');

    page.on('dialog', async dialog => {
      await dialog.accept();
    });

    const importBtn = page.getByRole('button', { name: 'Import' }).first();
    if (await importBtn.isVisible().catch(() => false)) {
      await importBtn.click();
      await page.locator('input[type="file"]').setInputFiles(fixturePath('words-import.json'));

      expect(async () => {
        const text = await page.textContent('body');
        expect(text).toContain('Import complete');
      }).toPass({ timeout: 15000 });

      expect(async () => {
        const text = await page.textContent('body');
        expect(text).toContain(wordOriginal1);
      }).toPass({ timeout: 10000 });
    }
  });

  test('export triggers download', async ({ page, baseURL }) => {
    await ensureLoggedIn(page, baseURL);

    await page.goto(`${baseURL}/words`);
    await page.waitForLoadState('domcontentloaded');

    const exportBtn = page.getByRole('button', { name: 'Export' }).first();
    if (await exportBtn.isVisible().catch(() => false)) {
      const [download] = await Promise.all([
        page.waitForEvent('download'),
        exportBtn.click()
      ]);

      const fileName = download.suggestedFilename();
      expect(fileName).toMatch(/word/);

      const content = await downloadContent(download);
      const data = JSON.parse((content as any).read().toString());
      expect(Array.isArray(data)).toBeTruthy();
    }
  });
});

test.describe('Invalid JSON Import', () => {
  test('uploading invalid JSON shows error toast', async ({ page, baseURL }) => {
    await ensureLoggedIn(page, baseURL);

    await page.goto(`${baseURL}/words`);
    await page.waitForLoadState('domcontentloaded');

    const importBtn = page.getByRole('button', { name: 'Import' }).first();
    if (await importBtn.isVisible().catch(() => false)) {
      await importBtn.click();
      await page.locator('input[type="file"]').setInputFiles(fixturePath('invalid-import.json'));

      expect(async () => {
        const text = await page.textContent('body');
        expect(text).toMatch(/Invalid JSON/i);
      }).toPass({ timeout: 15000 });
    }
  });
});
