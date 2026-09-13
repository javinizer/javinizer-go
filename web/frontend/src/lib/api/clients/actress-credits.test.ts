import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { ActressClient } from './actress';

describe('ActressClient identity/credit endpoints', () => {
	const client = new ActressClient('http://api.test');
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		fetchMock = vi.fn();
		vi.stubGlobal('fetch', fetchMock);
		ActressClient.setSessionID(null);
	});

	afterEach(() => {
		vi.unstubAllGlobals();
		ActressClient.setSessionID(null);
	});

	function okResponse(body: unknown) {
		return new Response(JSON.stringify(body), {
			status: 200,
			headers: { 'Content-Type': 'application/json' }
		});
	}

	it('listCandidates builds the query and returns candidates', async () => {
		fetchMock.mockResolvedValue(okResponse({ candidates: [{ id: 7, verified: false }], total: 1 }));
		const res = await client.listCandidates(25, 5);
		expect(res.total).toBe(1);
		expect(res.candidates[0].verified).toBe(false);
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe('http://api.test/api/v1/actresses/candidates?limit=25&offset=5');
		expect(init.method).toBeUndefined();
	});

	it('promoteCandidate posts to the promote endpoint', async () => {
		fetchMock.mockResolvedValue(okResponse({ id: 7, verified: true, origin: 'user' }));
		const res = await client.promoteCandidate(7, { first_name: 'A', last_name: 'B' });
		expect(res.origin).toBe('user');
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe('http://api.test/api/v1/actresses/candidates/7/promote');
		expect(init.method).toBe('POST');
		expect(JSON.parse(init.body)).toEqual({ first_name: 'A', last_name: 'B' });
	});

	it('listCollisions requires movie_id and parses collisions', async () => {
		fetchMock.mockResolvedValue(
			okResponse({
				collisions: [
					{
						id: 3,
						credit_id: 9,
						movie_content_id: 'ipx700',
						field: 'credited_name',
						reported_value: 'Wrong',
						canonical_value: 'Right',
						status: 'open',
						occurrences: 2,
						sources_seen: 'dmm'
					}
				]
			})
		);
		const res = await client.listCollisions('ipx700');
		expect(res.collisions[0].field).toBe('credited_name');
		const [url] = fetchMock.mock.calls[0];
		expect(url).toBe('http://api.test/api/v1/actresses/collisions?movie_id=ipx700');
	});

	it('resolveCollision posts resolution and target', async () => {
		fetchMock.mockResolvedValue(okResponse({ resolved: true, remaining_open: 0 }));
		const res = await client.resolveCollision(3, {
			resolution: 'keep_identity'
		});
		expect(res.resolved).toBe(true);
		expect(res.remaining_open).toBe(0);
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe('http://api.test/api/v1/actresses/collisions/3/resolve');
		expect(init.method).toBe('POST');
		expect(JSON.parse(init.body)).toEqual({ resolution: 'keep_identity' });
	});

	it('updateCreditOverride and suppressCredit post payloads', async () => {
		fetchMock.mockImplementation(() => Promise.resolve(okResponse({ ok: true })));
		await client.updateCreditOverride(11, 'Stage Name', true);
		const [overrideURL, overrideInit] = fetchMock.mock.calls[0];
		expect(overrideURL).toBe('http://api.test/api/v1/actresses/credits/11/override');
		expect(overrideInit.method).toBe('POST');
		expect(JSON.parse(overrideInit.body)).toEqual({
			override_name: 'Stage Name',
			user_override: true
		});

		await client.suppressCredit(11, true);
		const [suppressURL, suppressInit] = fetchMock.mock.calls[1];
		expect(suppressURL).toBe('http://api.test/api/v1/actresses/credits/11/suppress');
		expect(JSON.parse(suppressInit.body)).toEqual({ suppressed: true });
	});
});
