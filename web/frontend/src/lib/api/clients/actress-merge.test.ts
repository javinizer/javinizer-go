import { afterEach, expect, it, vi } from 'vitest';
import { ActressClient } from './actress';
afterEach(() => vi.unstubAllGlobals());
it.each(['previewActressMerge', 'mergeActresses'] as const)(
	'serializes policy and explicit decisions for %s',
	async (method) => {
		const fetch = vi.fn().mockResolvedValue(new Response('{}'));
		vi.stubGlobal('fetch', fetch);
		const request = {
			target_id: 1,
			source_id: 2,
			policy: 'preserve_target' as const,
			resolutions: { last_name: 'target' as const, dmm_id: 'source' as const },
		};
		await new ActressClient('http://api.test')[method](request);
		expect(JSON.parse(fetch.mock.calls[0][1].body)).toEqual(request);
		expect(fetch.mock.calls[0][0]).toBe(
			`http://api.test/api/v1/actresses/merge${method === 'previewActressMerge' ? '/preview' : ''}`,
		);
	},
);
it('retains legacy callers that omit policy and resolutions', async () => {
	const fetch = vi.fn().mockImplementation(async () => new Response('{}'));
	vi.stubGlobal('fetch', fetch);
	const client = new ActressClient('http://api.test');
	await client.previewActressMerge({ target_id: 1, source_id: 2 });
	await client.mergeActresses({ target_id: 1, source_id: 2 });
	for (const [, init] of fetch.mock.calls)
		expect(JSON.parse(init.body)).toEqual({ target_id: 1, source_id: 2 });
});
