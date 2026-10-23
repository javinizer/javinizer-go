import { describe, expect, it, vi } from 'vitest';
import type { Page, Route } from '@playwright/test';
import { JOB_ID, setupMockRoutes } from '../../../tests/frontend/review-helpers';

describe('review grid API fixtures', () => {
	it('fulfills collision review locally instead of falling through to an absent backend', async () => {
		const handlers = new Map<string, (route: Route) => Promise<void>>();
		const page = {
			route: vi.fn(async (pattern: string, handler: (route: Route) => Promise<void>) => {
				handlers.set(pattern, handler);
			}),
		} as unknown as Page;
		await setupMockRoutes(page);
		const fulfill = vi.fn();
		const passthrough = vi.fn();
		const route = {
			request: () => ({
				url: () => `http://localhost/api/v1/batch/${JOB_ID}/video-collisions`,
				method: () => 'POST',
			}),
			fulfill,
			continue: passthrough,
		} as unknown as Route;
		await handlers.get('**/api/v1/batch/**')!(route);
		expect(passthrough).not.toHaveBeenCalled();
		expect(fulfill).toHaveBeenCalledExactlyOnceWith({
			json: { items: [], new: 5, replaced: 0, skipped: 0, unresolved: 0, same_file: 0 },
		});
	});
});
