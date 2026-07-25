export type ReviewTabId = 'movies' | 'failed';

export type ReviewViewMode = 'detail' | 'grid-poster' | 'grid-cover';

export const VIEW_URL_PARAM = 'view';

export function viewModeToUrlParam(mode: ReviewViewMode): string {
	return mode === 'grid-poster' ? 'poster' : mode === 'grid-cover' ? 'cover' : 'detail';
}

export function shouldSyncTab(currentParam: string | null, activeTab: ReviewTabId): boolean {
	const expectedParam = activeTab === 'movies' ? null : activeTab;
	return currentParam !== expectedParam;
}

export function buildTabUrl(baseUrl: URL, activeTab: ReviewTabId): URL {
	const url = new URL(baseUrl);
	if (activeTab === 'movies') {
		url.searchParams.delete('tab');
	} else {
		url.searchParams.set('tab', activeTab);
	}
	return url;
}

export function buildReviewUrl(
	baseUrl: URL,
	activeTab: ReviewTabId,
	viewMode: ReviewViewMode,
): URL {
	const url = buildTabUrl(baseUrl, activeTab);
	url.searchParams.set(VIEW_URL_PARAM, viewModeToUrlParam(viewMode));
	return url;
}