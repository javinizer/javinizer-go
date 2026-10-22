import { afterEach, expect, it, vi } from 'vitest';
import { ActressClient } from './actress';
import type { ActressFilter } from '../types';
afterEach(() => vi.unstubAllGlobals());
it.each<ActressFilter>([
	'missing_dmm',
	'has_dmm',
	'missing_thumbnail',
	'missing_japanese_name',
	'japanese_name_only',
	'missing_metadata',
])('serializes %s with encoded search, sort and pagination', async (filter) => {
	const fetch = vi
		.fn()
		.mockResolvedValue(new Response(JSON.stringify({ actresses: [], total: 0 })));
	vi.stubGlobal('fetch', fetch);
	await new ActressClient('http://api.test').listActresses({
		filter,
		q: '123 & name',
		sort_by: 'dmm_id',
		sort_order: 'desc',
		limit: 20,
		offset: 20,
	});
	const url = new URL(fetch.mock.calls[0][0]);
	expect(Object.fromEntries(url.searchParams)).toEqual({
		filter,
		q: '123 & name',
		sort_by: 'dmm_id',
		sort_order: 'desc',
		limit: '20',
		offset: '20',
	});
});
it('omits empty parameters for the unfiltered catalog', async () => {
	const fetch = vi
		.fn()
		.mockImplementation(async () => new Response(JSON.stringify({ actresses: [], total: 0 })));
	vi.stubGlobal('fetch', fetch);
	const client = new ActressClient('http://api.test');
	await client.listActresses();
	await client.listActresses({ q: '', offset: 0 });
	for (const [url] of fetch.mock.calls) expect(url).toBe('http://api.test/api/v1/actresses');
});
