import { browser } from '$app/environment';
import type { LayoutLoad } from './$types';
import type { AuthStatusResponse } from '$lib/api/types';
import { BaseClient } from '$lib/api/clients/common';
import { BrowseBootstrapCookie, decodeBrowseBootstrap, type BrowseBootstrap } from '$lib/browse-bootstrap';

type InjectedSSRState = {
	authStatus?: AuthStatusResponse | null;
	browseBootstrap?: BrowseBootstrap | null;
};

function injectedSSRState(): InjectedSSRState | null {
	if (typeof window === 'undefined') return null;
	return (window as unknown as { __JAVINIZER_SSR__?: InjectedSSRState }).__JAVINIZER_SSR__ ?? null;
}

async function fetchAuthStatus(
	fetcher: typeof fetch,
	headers?: Record<string, string>
): Promise<AuthStatusResponse | null> {
	try {
		const response = await fetcher('/api/v1/auth/status', {
			signal: AbortSignal.timeout(3000),
			...(headers ? { headers } : {})
		});
		if (!response.ok) return null;
		return (await response.json()) as AuthStatusResponse;
	} catch {
		return null;
	}
}

function readBrowseBootstrapCookie(): BrowseBootstrap | null {
	const value = document.cookie
		.split(';')
		.map((entry) => entry.trim())
		.find((entry) => entry.startsWith(`${BrowseBootstrapCookie}=`))
		?.slice(BrowseBootstrapCookie.length + 1);
	return value ? decodeBrowseBootstrap(value) : null;
}

export const load: LayoutLoad = async ({ fetch }) => {
	if (browser) {
		const injected = injectedSSRState();
		let authStatus = injected?.authStatus ?? null;
		if (!authStatus?.authenticated) {
			const sessionID = BaseClient.getSessionID();
			const revalidated = await fetchAuthStatus(
				globalThis.fetch.bind(globalThis),
				sessionID ? { 'X-Session-ID': sessionID } : undefined
			);
			if (revalidated) authStatus = revalidated;
		}
		const browseBootstrap = readBrowseBootstrapCookie() ?? injected?.browseBootstrap ?? null;
		return { authStatus, browseBootstrap };
	}
	return { authStatus: await fetchAuthStatus(fetch), browseBootstrap: null };
};
