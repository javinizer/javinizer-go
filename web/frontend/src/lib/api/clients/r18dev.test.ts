import { describe, it, expect, vi, beforeEach } from 'vitest';

vi.mock('$app/environment', () => ({ browser: true }));

import { R18DevClient } from './r18dev';
import { BaseClient } from './common';

describe('R18DevClient.uploadDump', () => {
	const client = new R18DevClient('http://api.test');
	let fetchMock: ReturnType<typeof vi.fn>;

	beforeEach(() => {
		fetchMock = vi.fn().mockResolvedValue({
			ok: true,
			status: 202,
			text: vi.fn().mockResolvedValue('{"message":"upload staged"}'),
		} as unknown as Response);
		vi.stubGlobal('fetch', fetchMock);
	});

	it('posts the file as multipart FormData without an explicit Content-Type', async () => {
		const file = new File(['bytes'], 'r18dotdev_dump_2026-09-20.sql.gz');
		const res = await client.uploadDump(file);
		expect(res.message).toBe('upload staged');

		const [url, init] = fetchMock.mock.calls[0] as [string, RequestInit];
		expect(url).toBe('http://api.test/api/v1/r18dev/dump/upload');
		expect(init.method).toBe('POST');
		expect(init.body).toBeInstanceOf(FormData);
		const form = init.body as FormData;
		const part = form.get('file');
		expect(part).toBeInstanceOf(File);
		expect((part as File).name).toBe('r18dotdev_dump_2026-09-20.sql.gz');
		expect((init.headers as Record<string, string>)['Content-Type']).toBeUndefined();
	});
});
