import type { PageServerLoad } from './$types';
import type { BrowseResponse } from '$lib/api/types';

export const load: PageServerLoad = async ({ fetch, parent, request }) => {
	const layout = await parent();
	if (!layout.authStatus?.authenticated) return { initialPath: '', initialBrowse: null };
	const baseURL = process.env.JAVINIZER_SSR_API_URL || 'http://127.0.0.1:8765';
	const cookie = request.headers.get('cookie');
	const headers = { ...(cookie ? { cookie } : {}), 'content-type': 'application/json' };
	let initialPath = layout.browseBootstrap?.initialPath ?? '';
	try {
		if (!initialPath) {
			const cwdResponse = await fetch(`${baseURL}/api/v1/cwd`, { headers, signal: AbortSignal.timeout(3000) });
			if (cwdResponse.ok) initialPath = ((await cwdResponse.json()) as { path?: string }).path ?? '';
		}
		if (!initialPath) return { initialPath: '', initialBrowse: null };
		const browseResponse = await fetch(`${baseURL}/api/v1/browse`, {
			signal: AbortSignal.timeout(3000),
			method: 'POST',
			headers,
			body: JSON.stringify({ path: initialPath, scope: 'operation' })
		});
		if (!browseResponse.ok) return { initialPath, initialBrowse: null };
		return { initialPath, initialBrowse: await browseResponse.json() as BrowseResponse };
	} catch {
		return { initialPath, initialBrowse: null };
	}
};