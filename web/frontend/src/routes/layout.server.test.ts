import { describe, expect, it, vi } from 'vitest';
import { load } from './+layout.server';

describe('layout server authentication', () => {
	it('decodes a valid browse bootstrap cookie from the request', async () => {
		const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ initialized: true, authenticated: true }), { status: 200 }));
		const bootstrap = encodeURIComponent(JSON.stringify({ version: 1, applyPlan: null, initialPath: '/videos', destinationPath: '', forceRefresh: false, showScraperSelector: false, selectedScrapers: [], manualScrapeMode: false, planExpanded: false }));
		const result = await load({
			fetch: fetchMock,
			request: new Request('http://localhost:5174/browse', { headers: { cookie: `javinizer_session=session-1; javinizer_browse_bootstrap=${bootstrap}` } })
		} as never);
		expect(result).toMatchObject({ browseBootstrap: { planExpanded: false, initialPath: '/videos' } });
	});

	it('returns null bootstrap for a malformed cookie value', async () => {
		const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ initialized: true, authenticated: true }), { status: 200 }));
		const result = await load({
			fetch: fetchMock,
			request: new Request('http://localhost:5174/browse', { headers: { cookie: 'javinizer_browse_bootstrap=%invalid%' } })
		} as never);
		expect(result).toMatchObject({ browseBootstrap: null });
	});

	it('forwards the incoming session cookie and returns authenticated state', async () => {
		const fetchMock = vi.fn().mockResolvedValue(new Response(JSON.stringify({ initialized: true, authenticated: true, username: 'admin' }), { status: 200 }));
		const result = await load({
			fetch: fetchMock,
			request: new Request('http://localhost:5174/browse', { headers: { cookie: 'javinizer_session=session-1' } })
		} as never);
		expect(fetchMock).toHaveBeenCalledWith('http://127.0.0.1:8765/api/v1/auth/status', expect.objectContaining({ headers: { cookie: 'javinizer_session=session-1' } }));
		expect(result).toEqual({ authStatus: { initialized: true, authenticated: true, username: 'admin' }, browseBootstrap: null });
	});

	it('falls back to unresolved client authentication when the backend is unavailable', async () => {
		const result = await load({
			fetch: vi.fn().mockRejectedValue(new Error('offline')),
			request: new Request('http://localhost:5174/browse')
		} as never);
		expect(result).toEqual({ authStatus: null, browseBootstrap: null });
	});
});