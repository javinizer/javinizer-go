import { afterEach, beforeEach, expect, it, vi } from 'vitest';
import { JobClient } from './jobs';

const client = new JobClient('http://api.test');
const fetchMock = vi.fn();
beforeEach(() => {
	fetchMock.mockReset();
	vi.stubGlobal('fetch', fetchMock);
	JobClient.setSessionID(null);
});
afterEach(() => {
	vi.unstubAllGlobals();
	JobClient.setSessionID(null);
});

it.each(['organize', 'update'] as const)(
	'serializes the owned generation and returns the %s admission receipt',
	async (mode) => {
		fetchMock.mockResolvedValue(
			new Response(JSON.stringify({ message: 'Started', apply_generation: 8 }), {
				status: 200,
				headers: { 'Content-Type': 'application/json' },
			}),
		);
		const common = { expected_apply_generation: 7, retry_file_paths: ['/synthetic/movie.mp4'] };
		const result =
			mode === 'organize'
				? await client.organizeBatchJob('job', {
						...common,
						destination: '/synthetic/out',
						copy_only: true,
						link_mode: 'hard',
					})
				: await client.updateBatchJob('job', {
						...common,
						force_overwrite: false,
						preserve_nfo: true,
					});
		expect(result.apply_generation).toBe(8);
		const [url, init] = fetchMock.mock.calls[0];
		expect(url).toBe(`http://api.test/api/v1/batch/job/${mode}`);
		expect(init.method).toBe('POST');
		expect(JSON.parse(init.body)).toMatchObject(common);
		expect(JSON.parse(init.body)).toMatchObject(
			mode === 'organize'
				? { copy_only: true, link_mode: 'hard' }
				: { force_overwrite: false, preserve_nfo: true },
		);
	},
);

it.each(['organize', 'update'] as const)(
	'keeps ordinary %s requests compatible without a generation precondition',
	async (mode) => {
		fetchMock.mockResolvedValue(
			new Response(JSON.stringify({ message: 'Legacy response' }), { status: 200 }),
		);
		if (mode === 'organize')
			await client.organizeBatchJob('job', { destination: '/synthetic/out' });
		else await client.updateBatchJob('job', {});
		expect(JSON.parse(fetchMock.mock.calls[0][1].body)).not.toHaveProperty(
			'expected_apply_generation',
		);
	},
);
