import { describe, expect, it, vi } from 'vitest';
import { load } from './+page.server';

const listing = { current_path: '/videos', parent_path: '/', items: [{ name: 'IPX-123.mp4', path: '/videos/IPX-123.mp4', is_dir: false, size: 1, mod_time: '2026-01-01T00:00:00Z' }] };

describe('Browse page server data', () => {
	it('uses the persisted path and returns its initial listing', async () => {
		const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify(listing), { status: 200 }));
		const result = await load({
			fetch: fetchMock,
			parent: vi.fn().mockResolvedValue({ authStatus: { authenticated: true }, browseBootstrap: { initialPath: '/videos' } }),
			request: new Request('http://localhost:5174/browse', { headers: { cookie: 'javinizer_session=session-1' } })
		} as never);
		expect(fetchMock).toHaveBeenCalledWith('http://127.0.0.1:8765/api/v1/browse', expect.objectContaining({ method: 'POST', body: JSON.stringify({ path: '/videos', scope: 'operation' }) }));
		expect(result).toEqual({ initialPath: '/videos', initialBrowse: listing });
	});

	it('resolves cwd before browsing when no path was persisted', async () => {
		const fetchMock = vi.fn()
			.mockResolvedValueOnce(new Response(JSON.stringify({ path: '/library' }), { status: 200 }))
			.mockResolvedValueOnce(new Response(JSON.stringify({ ...listing, current_path: '/library' }), { status: 200 }));
		const result = await load({ fetch: fetchMock, parent: vi.fn().mockResolvedValue({ authStatus: { authenticated: true }, browseBootstrap: null }), request: new Request('http://localhost:5174/browse') } as never);
		expect(result).toMatchObject({ initialPath: '/library' });
		expect(fetchMock).toHaveBeenCalledTimes(2);
	});

	it('does not load directory data for anonymous requests', async () => {
		const fetchMock = vi.fn();
		const result = await load({ fetch: fetchMock, parent: vi.fn().mockResolvedValue({ authStatus: { authenticated: false } }), request: new Request('http://localhost:5174/browse') } as never);
		expect(result).toEqual({ initialPath: '', initialBrowse: null });
		expect(fetchMock).not.toHaveBeenCalled();
	});
});