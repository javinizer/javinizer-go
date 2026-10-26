import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { ActressNameSyncClient } from './actress-name-sync';
import { ApiError } from './common';
const client = new ActressNameSyncClient('http://api.test');
let fetchMock: ReturnType<typeof vi.fn>;
beforeEach(() => {
	fetchMock = vi
		.fn()
		.mockResolvedValue({ ok: true, status: 200, text: async () => '{"items":[]}' });
	vi.stubGlobal('fetch', fetchMock);
});
afterEach(() => vi.unstubAllGlobals());
function last() {
	return fetchMock.mock.lastCall as [string, RequestInit];
}
describe('ActressNameSyncClient', () => {
	it('keeps reads in the offline namespace and forwards abort signals', async () => {
		const signal = new AbortController().signal;
		await client.status(signal);
		expect(last()[0]).toBe('http://api.test/api/v1/actress-name-sync/status');
		expect(last()[1].signal).toBe(signal);
		await client.sources({ q: '名前 & id', offset: 20, limit: 20 }, signal);
		const url = new URL(last()[0]);
		expect(url.searchParams.get('q')).toBe('名前 & id');
		expect(url.searchParams.get('offset')).toBe('20');
		expect(url.searchParams.get('limit')).toBe('20');
		expect(last()[1].signal).toBe(signal);
	});
	it('serializes only explicit identity selectors, never richer client authority', async () => {
		await client.review([
			{ actress_id: 17, source_key: 'v1:source', after_name: 'evil', confirmed: true } as never,
		]);
		expect(JSON.parse(last()[1].body as string)).toEqual({
			choices: [{ actress_id: 17, source_key: 'v1:source' }],
		});
		expect(last()[0]).toMatch(/\/reviews$/);
		expect(last()[1].method).toBe('POST');
		expect(new Headers(last()[1].headers).get('X-Requested-With')).toBe(
			'javinizer-offline-name-sync',
		);
	});
	it('confirmation submits only the server nonce and literal confirmation', async () => {
		await client.confirm('a'.repeat(64));
		expect(JSON.parse(last()[1].body as string)).toEqual({
			review_id: 'a'.repeat(64),
			confirmed: true,
		});
		expect(new Headers(last()[1].headers).get('Content-Type')).toBe('application/json');
		expect(new Headers(last()[1].headers).get('X-Requested-With')).toBe(
			'javinizer-offline-name-sync',
		);
	});
	it('cancellation is explicit and URL-encodes the job path', async () => {
		await client.cancel('x/y?z');
		expect(last()[0]).toMatch(/\/jobs\/x%2Fy%3Fz\/cancel$/);
		expect(JSON.parse(last()[1].body as string)).toEqual({ confirmed: true });
		expect(last()[1].method).toBe('POST');
	});
	it('encodes job and receipt path segments and opaque history cursors', async () => {
		await client.job('x/y');
		expect(last()[0]).toMatch(/\/jobs\/x%2Fy$/);
		await client.tasks('x/y', { offset: 50, limit: 50 });
		expect(last()[0]).toMatch(/\/jobs\/x%2Fy\/tasks\?offset=50&limit=50$/);
		await client.jobs({ cursor: 'opaque/+=', limit: 32 });
		expect(new URL(last()[0]).searchParams.get('cursor')).toBe('opaque/+=');
	});
	it.each([-1, 0, 101, 0.5, NaN, Infinity])(
		'rejects invalid source/receipt limit %s without fetching',
		async (limit) => {
			await expect(client.sources({ limit })).rejects.toBeInstanceOf(RangeError);
			await expect(client.tasks('job', { limit })).rejects.toBeInstanceOf(RangeError);
			expect(fetchMock).not.toHaveBeenCalled();
		},
	);
	it.each([-1, 0, 33, 0.5, NaN, Infinity])(
		'rejects invalid history limit %s without fetching',
		async (limit) => {
			await expect(client.jobs({ limit })).rejects.toBeInstanceOf(RangeError);
			expect(fetchMock).not.toHaveBeenCalled();
		},
	);
	it.each([-1, 0.5, NaN, Infinity, Number.MAX_SAFE_INTEGER + 1])(
		'rejects invalid offset %s without fetching',
		async (offset) => {
			await expect(client.sources({ offset })).rejects.toBeInstanceOf(RangeError);
			await expect(client.tasks('job', { offset })).rejects.toBeInstanceOf(RangeError);
			expect(fetchMock).not.toHaveBeenCalled();
		},
	);
	it.each([
		'AUTH_UNAUTHORIZED',
		'SYNC_REVIEW_PRINCIPAL',
		'SYNC_PROTECTED',
		'SYNC_STALE',
		'SYNC_REVIEW_EXPIRED',
		'SYNC_CAPACITY',
		'SYNC_DISABLED',
		'SYNC_UNAVAILABLE',
		'SYNC_STALE_GENERATION',
		'SYNC_RELOAD_BUSY',
		'SYNC_RELOAD_FAILED',
		'SYNC_STOPPING',
	])('preserves structured %s errors without replaying', async (code) => {
		fetchMock.mockResolvedValue({
			ok: false,
			status: 409,
			json: async () => ({ error: 'private detail', code }),
		});
		await expect(client.confirm('nonce')).rejects.toMatchObject({ code, status: 409 });
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});
	it.each(['review', 'confirm', 'cancel'] as const)(
		'never retries a %s network failure',
		async (method) => {
			fetchMock.mockRejectedValue(new TypeError('offline'));
			const call =
				method === 'review'
					? client.review([{ actress_id: 1, source_key: 'key' }])
					: client[method]('id');
			await expect(call).rejects.toBeInstanceOf(TypeError);
			expect(fetchMock).toHaveBeenCalledTimes(1);
		},
	);
	it('keeps non-JSON error fallback without leaking into retry behavior', async () => {
		fetchMock.mockResolvedValue({ ok: false, status: 503, text: async () => '<html>down</html>' });
		await expect(client.status()).rejects.toBeInstanceOf(ApiError);
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});
	it.each([
		'status',
		'reload',
		'sources',
		'review',
		'confirm',
		'jobs',
		'job',
		'tasks',
		'cancel',
	] as const)('passes cancellation for %s without replay', async (method) => {
		const signal = new AbortController().signal;
		fetchMock.mockRejectedValue(new DOMException('aborted', 'AbortError'));
		const calls = {
			status: () => client.status(signal),
			reload: () => client.reload(signal),
			sources: () => client.sources({}, signal),
			review: () => client.review([], signal),
			confirm: () => client.confirm('id', signal),
			jobs: () => client.jobs({}, signal),
			job: () => client.job('id', signal),
			tasks: () => client.tasks('id', {}, signal),
			cancel: () => client.cancel('id', signal),
		};
		await expect(calls[method]()).rejects.toMatchObject({ name: 'AbortError' });
		expect(last()[1].signal).toBe(signal);
		expect(fetchMock).toHaveBeenCalledTimes(1);
	});
});

it('reload posts an empty authenticated-intent request without client artifact authority or retry', async () => {
	const signal = new AbortController().signal;
	await client.reload(signal);
	expect(last()[0]).toMatch(/\/sources\/reload$/);
	expect(last()[1].method).toBe('POST');
	expect(last()[1].body).toBe('{}');
	expect(last()[1].signal).toBe(signal);
	expect(new Headers(last()[1].headers).get('X-Requested-With')).toBe(
		'javinizer-offline-name-sync',
	);
	fetchMock.mockRejectedValue(new TypeError('lost response'));
	await expect(client.reload()).rejects.toBeInstanceOf(TypeError);
	expect(fetchMock).toHaveBeenCalledTimes(2);
});
it('sends a displayed digest only as a discovery and review precondition', async () => {
	const digest = 'a'.repeat(64);
	await client.sources({ artifact_digest: digest });
	expect(new URL(last()[0]).searchParams.get('artifact_digest')).toBe(digest);
	await client.review([{ actress_id: 1, source_key: 'key' }], undefined, digest);
	expect(JSON.parse(last()[1].body as string)).toEqual({
		choices: [{ actress_id: 1, source_key: 'key' }],
		artifact_digest: digest,
	});
});
