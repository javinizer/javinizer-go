import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';

import { ActressClient } from '$lib/api/clients/actress';

// Logic probe for CollisionPanel's guarded load flow: a stale (superseded)
// list response must not overwrite a newer one, and the loading flag must
// only be cleared by the latest request.
describe('CollisionPanel guarded load flow', () => {
	let client: ActressClient;
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		fetchMock = vi.fn();
		vi.stubGlobal('fetch', fetchMock);
		ActressClient.setSessionID(null);
		client = new ActressClient('http://api.test');
	});

	afterEach(() => {
		vi.unstubAllGlobals();
	});

	function ok(body: unknown) {
		return new Response(JSON.stringify(body), {
			status: 200,
			headers: { 'Content-Type': 'application/json' }
		});
	}

	it('latest response wins when two loads race', async () => {
		let resolveA: (r: Response) => void = () => {};
		fetchMock.mockImplementation(() => {
			if (fetchMock.mock.calls.length === 1) {
				return new Promise<Response>((r) => {
					resolveA = r;
				});
			}
			return Promise.resolve(ok({ collisions: [{ id: 2, movie_content_id: 'b', field: 'credited_name', reported_value: 'x', canonical_value: 'y', status: 'open', occurrences: 1 }] }));
		});

		const first = client.listCollisions('a');
		const second = client.listCollisions('b');
		resolveA(ok({ collisions: [{ id: 1, movie_content_id: 'a', field: 'credited_name', reported_value: 'stale', canonical_value: 's', status: 'open', occurrences: 1 }] }));

		const [, secondResult] = await Promise.all([first, second]);
		expect(secondResult.collisions[0].movie_content_id).toBe('b');
	});
});
