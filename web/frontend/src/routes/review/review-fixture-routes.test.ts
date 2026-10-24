import { describe, expect, it, vi } from 'vitest';
import type { Page, Route } from '@playwright/test';
import type { VideoCollisionReport } from '$lib/api/types';
import { JOB_ID, setupMockRoutes } from '../../../tests/frontend/review-helpers';

async function fixture() {
	const handlers = new Map<string, (route: Route) => Promise<void>>();
	await setupMockRoutes({
		route: vi.fn(async (pattern: string, handler: (route: Route) => Promise<void>) => {
			handlers.set(pattern, handler);
		}),
	} as unknown as Page);
	return async (path: string, body: unknown = { destination: '/output' }) => {
		const fulfill = vi.fn();
		const passthrough = vi.fn();
		await handlers.get('**/api/v1/batch/**')!({
			request: () => ({
				url: () => `http://localhost/api/v1/batch/${JOB_ID}/${path}`,
				method: () => 'POST',
				postDataJSON: () => body,
			}),
			fulfill,
			continue: passthrough,
		} as unknown as Route);
		expect(passthrough).not.toHaveBeenCalled();
		expect(fulfill).toHaveBeenCalledOnce();
		return fulfill.mock.calls[0][0].json;
	};
}

describe('review grid API fixtures', () => {
	it('fulfills collision review with one new item per active movie, without backend passthrough', async () => {
		const post = await fixture();
		const report: VideoCollisionReport = await post('video-collisions');
		expect(report.items).toHaveLength(5);
		expect(report.new).toBe(report.items.length);
		expect(new Set(report.items.map(item => item.source)).size).toBe(5);
		for (const item of report.items) {
			expect(item).toEqual({
				source: expect.stringMatching(/^\/videos\/.*\.mp4$/),
				destination: item.source.replace('/videos/', '/output/'),
				source_size: 1024, destination_size: 0, kind: 'new',
			});
		}
		expect(report).toMatchObject({ replaced: 0, skipped: 0, unresolved: 0, same_file: 0 });
	});

	it('reconciles collision items and counts after single and bulk exclusions', async () => {
		const post = await fixture();
		await post('results/COMPLETE-1-result/exclude', {});
		let report: VideoCollisionReport = await post('video-collisions');
		expect(report.new).toBe(4);
		expect(report.items).toHaveLength(4);
		expect(report.items.some(item => item.source === '/videos/COMPLETE-1.mp4')).toBe(false);
		await post('batch-exclude', { result_ids: ['PARTIAL-1-result', 'COMPLETE-2-result'] });
		report = await post('video-collisions');
		expect(report.new).toBe(2);
		expect(report.items.map(item => item.source).sort()).toEqual(['/videos/INCOMPLETE-1.mp4', '/videos/INCOMPLETE-2.mp4']);
	});

	it('uses the requested destination and limits retry review to requested active sources', async () => {
		const post = await fixture();
		await post('results/PARTIAL-1-result/exclude', {});
		const report: VideoCollisionReport = await post('video-collisions', {
			destination: '/other', retry_file_paths: ['/videos/COMPLETE-1.mp4', '/videos/PARTIAL-1.mp4'],
		});
		expect(report.new).toBe(1);
		expect(report.items).toHaveLength(1);
		expect(report.items[0].destination).toBe('/other/COMPLETE-1.mp4');
	});
});
