import { afterEach, describe, expect, it, vi } from 'vitest';
import { ScraperClient } from './jobs';
import { ApiError } from './common';

const rule = { source_name: 'Lee Mary Jane', first_name: 'Mary Jane', last_name: 'Lee' };
const client = new ScraperClient('http://api.test');
afterEach(() => vi.restoreAllMocks());

describe('source name override API', () => {
	it('reads and saves only the source rule, carrying explicit absent-rule CAS', async () => {
		const request = vi.spyOn(client, 'request').mockResolvedValue({ rule: null });
		await expect(client.getNameOverride('ab123')).resolves.toEqual({ rule: null });
		expect(request).toHaveBeenCalledWith(
			'/api/v1/scrapers/javlibrary/name-overrides/ab123',
			undefined,
		);
		await client.saveNameOverride('ab123', { ...rule, expected_rule: null });
		expect(request).toHaveBeenLastCalledWith('/api/v1/scrapers/javlibrary/name-overrides/ab123', {
			method: 'PUT',
			body: JSON.stringify({ ...rule, expected_rule: null }),
			timeoutMs: 15000,
		});
	});

	it('retains a prior rule verbatim and propagates conflicts without retry', async () => {
		const request = vi
			.spyOn(client, 'request')
			.mockRejectedValue(new ApiError('changed', undefined, undefined, 409));
		await expect(
			client.saveNameOverride('ab123', { ...rule, expected_rule: rule }),
		).rejects.toMatchObject({ status: 409 });
		expect(request).toHaveBeenCalledOnce();
		expect(JSON.parse(request.mock.calls[0][1]?.body as string).expected_rule).toEqual(rule);
	});

	it.each(['', '../name', 'a/b', 'a'.repeat(65)])(
		'rejects invalid source ID %s before transport',
		async (id) => {
			const request = vi.spyOn(client, 'request');
			await expect(client.getNameOverride(id)).rejects.toThrow('Invalid JAVLibrary source ID');
			await expect(client.saveNameOverride(id, { ...rule, expected_rule: null })).rejects.toThrow(
				'Invalid JAVLibrary source ID',
			);
			expect(request).not.toHaveBeenCalled();
		},
	);
});
