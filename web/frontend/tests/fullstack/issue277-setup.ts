import { test, expect } from '@playwright/test';
import { mkdirSync } from 'node:fs';
import { resolve } from 'node:path';

test('authenticate disposable e2e backend', async ({ request }) => {
	mkdirSync(resolve('test-results-issue277'), { recursive: true });
	const response = await request.post('/api/v1/auth/login', {
		data: { username: 'admin', password: 'adminpassword123' },
	});
	expect(response.ok(), await response.text()).toBeTruthy();
	expect((await response.json()).authenticated).toBeTruthy();
	await request.storageState({ path: resolve('test-results-issue277/auth-state.json') });
});
