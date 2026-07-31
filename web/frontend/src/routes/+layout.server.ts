import type { LayoutServerLoad } from './$types';
import type { AuthStatusResponse } from '$lib/api/types';
import { BrowseBootstrapCookie, decodeBrowseBootstrap } from '$lib/browse-bootstrap';

export const load: LayoutServerLoad = async ({ fetch, request }) => {
	const cookieHeader = request.headers.get('cookie') ?? '';
	const bootstrapValue = cookieHeader.split(';').map((value) => value.trim()).find((value) => value.startsWith(`${BrowseBootstrapCookie}=`))?.slice(BrowseBootstrapCookie.length + 1);
	const browseBootstrap = bootstrapValue ? decodeBrowseBootstrap(bootstrapValue) : null;
	const baseURL = process.env.JAVINIZER_SSR_API_URL || 'http://127.0.0.1:8765';
	const cookie = request.headers.get('cookie');
	try {
		const response = await fetch(`${baseURL}/api/v1/auth/status`, {
			headers: cookie ? { cookie } : undefined,
			signal: AbortSignal.timeout(3000)
		});
		if (!response.ok) return { authStatus: null, browseBootstrap };
		return { authStatus: await response.json() as AuthStatusResponse, browseBootstrap };
	} catch {
		return { authStatus: null, browseBootstrap };
	}
};