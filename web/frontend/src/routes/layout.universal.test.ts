import { describe, it, expect, vi, afterEach } from 'vitest';
import type { AuthStatusResponse } from '$lib/api/types';
import { BrowseBootstrapCookie, encodeBrowseBootstrap, type BrowseBootstrap } from '$lib/browse-bootstrap';

const AUTH_STATUS: AuthStatusResponse = { initialized: true, authenticated: true, username: 'admin' };

function bootstrap(overrides: Partial<BrowseBootstrap> = {}): BrowseBootstrap {
	return {
		version: 1,
		applyPlan: null,
		initialPath: '/videos',
		destinationPath: '',
		forceRefresh: false,
		showScraperSelector: false,
		selectedScrapers: [],
		manualScrapeMode: false,
		planExpanded: false,
		...overrides
	};
}

async function importLoad(browserValue: boolean) {
	vi.resetModules();
	vi.doMock('$app/environment', () => ({ browser: browserValue }));
	return import('./+layout');
}

function stubCookie(value: string) {
	vi.spyOn(document, 'cookie', 'get').mockReturnValue(value);
}

afterEach(() => {
	vi.restoreAllMocks();
	vi.unstubAllGlobals();
	vi.doUnmock('$app/environment');
	delete (window as unknown as { __JAVINIZER_SSR__?: unknown }).__JAVINIZER_SSR__;
});

describe('layout universal load — browser branch', () => {
	it('returns the injected auth status without a network request', async () => {
		const { load } = await importLoad(true);
		(window as unknown as { __JAVINIZER_SSR__?: unknown }).__JAVINIZER_SSR__ = { authStatus: AUTH_STATUS };
		const globalFetch = vi.fn();
		vi.stubGlobal('fetch', globalFetch);
		stubCookie('');
		const eventFetch = vi.fn();
		const result = await load({ fetch: eventFetch } as never);
		expect(result).toEqual({ authStatus: AUTH_STATUS, browseBootstrap: null });
		expect(globalFetch).not.toHaveBeenCalled();
		expect(eventFetch).not.toHaveBeenCalled();
	});

	it('falls back to a same-origin global fetch when no injection exists', async () => {
		const { load } = await importLoad(true);
		const globalFetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(AUTH_STATUS), { status: 200 }));
		vi.stubGlobal('fetch', globalFetch);
		stubCookie('');
		const eventFetch = vi.fn();
		const result = await load({ fetch: eventFetch } as never);
		expect(result).toEqual({ authStatus: AUTH_STATUS, browseBootstrap: null });
		expect(globalFetch).toHaveBeenCalledWith('/api/v1/auth/status', expect.objectContaining({ signal: expect.any(AbortSignal) }));
		expect(eventFetch).not.toHaveBeenCalled();
	});

	it('returns null auth status when the fetch responds 503', async () => {
		const { load } = await importLoad(true);
		vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'Auth not available' }), { status: 503 })));
		stubCookie('');
		const result = await load({ fetch: vi.fn() } as never);
		expect(result).toEqual({ authStatus: null, browseBootstrap: null });
	});

	it('returns null auth status when the fetch throws', async () => {
		const { load } = await importLoad(true);
		vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));
		stubCookie('');
		const result = await load({ fetch: vi.fn() } as never);
		expect(result).toEqual({ authStatus: null, browseBootstrap: null });
	});

	it('returns null auth status when a 200 response body is malformed JSON', async () => {
		const { load } = await importLoad(true);
		vi.stubGlobal('fetch', vi.fn().mockResolvedValue(new Response('not json', { status: 200 })));
		stubCookie('');
		const result = await load({ fetch: vi.fn() } as never);
		expect(result).toEqual({ authStatus: null, browseBootstrap: null });
	});

	it('fetches auth status when the injection lacks an authStatus key', async () => {
		const { load } = await importLoad(true);
		(window as unknown as { __JAVINIZER_SSR__?: unknown }).__JAVINIZER_SSR__ = { browseBootstrap: bootstrap({ initialPath: '/injected' }) };
		const globalFetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(AUTH_STATUS), { status: 200 }));
		vi.stubGlobal('fetch', globalFetch);
		stubCookie('');
		const result = await load({ fetch: vi.fn() } as never);
		expect(globalFetch).toHaveBeenCalledOnce();
		expect(result).toMatchObject({ authStatus: AUTH_STATUS, browseBootstrap: { initialPath: '/injected' } });
	});

	it('returns null auth status when the fetch is aborted by the 3s timeout', async () => {
		const { load } = await importLoad(true);
		vi.stubGlobal('fetch', vi.fn((_url: string, init?: RequestInit) => new Promise<Response>((_resolve, reject) => {
			init?.signal?.addEventListener('abort', () => reject(new DOMException('The operation timed out', 'TimeoutError')));
		})));
		stubCookie('');
		const result = await load({ fetch: vi.fn() } as never);
		expect(result).toEqual({ authStatus: null, browseBootstrap: null });
	}, 10000);

	it('revalidates with the stored session header when the injection is unauthenticated', async () => {
		const injected = { initialized: true, authenticated: false };
		const { load } = await importLoad(true);
		(window as unknown as { __JAVINIZER_SSR__?: unknown }).__JAVINIZER_SSR__ = { authStatus: injected };
		localStorage.setItem('javinizer_session', 'sess-desktop');
		const globalFetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(AUTH_STATUS), { status: 200 }));
		vi.stubGlobal('fetch', globalFetch);
		stubCookie('');
		const result = await load({ fetch: vi.fn() } as never);
		expect(globalFetch).toHaveBeenCalledWith(
			'/api/v1/auth/status',
			expect.objectContaining({ headers: { 'X-Session-ID': 'sess-desktop' } })
		);
		expect(result).toMatchObject({ authStatus: AUTH_STATUS });
	});

	it('keeps the injected unauthenticated status when revalidation fails without a stored session', async () => {
		const injected = { initialized: true, authenticated: false };
		const { load } = await importLoad(true);
		(window as unknown as { __JAVINIZER_SSR__?: unknown }).__JAVINIZER_SSR__ = { authStatus: injected };
		localStorage.removeItem('javinizer_session');
		vi.stubGlobal('fetch', vi.fn().mockRejectedValue(new Error('offline')));
		stubCookie('');
		const result = await load({ fetch: vi.fn() } as never);
		expect(result).toMatchObject({ authStatus: injected });
	});

	it('reads the browse bootstrap from document.cookie before the injected value', async () => {
		const { load } = await importLoad(true);
		(window as unknown as { __JAVINIZER_SSR__?: unknown }).__JAVINIZER_SSR__ = {
			authStatus: AUTH_STATUS,
			browseBootstrap: bootstrap({ planExpanded: true, initialPath: '/injected' })
		};
		vi.stubGlobal('fetch', vi.fn());
		stubCookie(`javinizer_session=session-1; ${BrowseBootstrapCookie}=${encodeBrowseBootstrap(bootstrap({ planExpanded: false, initialPath: '/cookie' }))}`);
		const result = await load({ fetch: vi.fn() } as never);
		expect(result).toMatchObject({ browseBootstrap: { planExpanded: false, initialPath: '/cookie' } });
	});

	it('falls back to the injected browse bootstrap when the cookie is absent', async () => {
		const { load } = await importLoad(true);
		(window as unknown as { __JAVINIZER_SSR__?: unknown }).__JAVINIZER_SSR__ = {
			authStatus: AUTH_STATUS,
			browseBootstrap: bootstrap({ initialPath: '/injected' })
		};
		vi.stubGlobal('fetch', vi.fn());
		stubCookie('javinizer_session=session-1');
		const result = await load({ fetch: vi.fn() } as never);
		expect(result).toMatchObject({ browseBootstrap: { initialPath: '/injected', planExpanded: false } });
	});

	it('treats a malformed bootstrap cookie as absent', async () => {
		const { load } = await importLoad(true);
		(window as unknown as { __JAVINIZER_SSR__?: unknown }).__JAVINIZER_SSR__ = { authStatus: AUTH_STATUS };
		vi.stubGlobal('fetch', vi.fn());
		stubCookie(`${BrowseBootstrapCookie}=%invalid%`);
		const result = await load({ fetch: vi.fn() } as never);
		expect(result).toEqual({ authStatus: AUTH_STATUS, browseBootstrap: null });
	});
});

describe('layout universal load — dev SSR branch', () => {
	it('fetches auth status via the load event fetch and returns null bootstrap', async () => {
		const { load } = await importLoad(false);
		const eventFetch = vi.fn().mockResolvedValue(new Response(JSON.stringify(AUTH_STATUS), { status: 200 }));
		const globalFetch = vi.fn();
		vi.stubGlobal('fetch', globalFetch);
		const result = await load({ fetch: eventFetch } as never);
		expect(result).toEqual({ authStatus: AUTH_STATUS, browseBootstrap: null });
		expect(eventFetch).toHaveBeenCalledWith('/api/v1/auth/status', expect.objectContaining({ signal: expect.any(AbortSignal) }));
		expect(globalFetch).not.toHaveBeenCalled();
	});

	it('returns null auth status when the response is not OK', async () => {
		const { load } = await importLoad(false);
		const eventFetch = vi.fn().mockResolvedValue(new Response(JSON.stringify({ error: 'Auth not available' }), { status: 503 }));
		const result = await load({ fetch: eventFetch } as never);
		expect(result).toEqual({ authStatus: null, browseBootstrap: null });
	});

	it('returns null auth status when the load event fetch throws', async () => {
		const { load } = await importLoad(false);
		const eventFetch = vi.fn().mockRejectedValue(new Error('backend down'));
		const result = await load({ fetch: eventFetch } as never);
		expect(result).toEqual({ authStatus: null, browseBootstrap: null });
	});
});
