import { describe, expect, it } from 'vitest';
import { decodeBrowseBootstrap, encodeBrowseBootstrap, type BrowseBootstrap } from './browse-bootstrap';

const bootstrap: BrowseBootstrap = {
	version: 1,
	applyPlan: { version: 1, video_operation: 'leave-in-place', nfo_output: 'write', media_policy: 'missing', merge: { scalar_strategy: 'prefer-nfo', array_strategy: 'merge' } },
	initialPath: '/videos/日本語',
	destinationPath: '/output',
	forceRefresh: true,
	showScraperSelector: true,
	selectedScrapers: ['dmm', 'javdb'],
	manualScrapeMode: false,
	planExpanded: false
};

describe('Browse SSR bootstrap', () => {
	it('round-trips compact visual state through a cookie-safe encoding', () => {
		expect(decodeBrowseBootstrap(encodeBrowseBootstrap(bootstrap))).toEqual(bootstrap);
	});

	it('defaults pre-disclosure cookies to expanded', () => {
		const { planExpanded: _, ...legacy } = bootstrap;
		expect(decodeBrowseBootstrap(encodeURIComponent(JSON.stringify(legacy)))?.planExpanded).toBe(true);
	});

	it('rejects malformed and unsupported values', () => {
		expect(decodeBrowseBootstrap('%')).toBeNull();
		expect(decodeBrowseBootstrap(encodeURIComponent(JSON.stringify({ version: 2 })))).toBeNull();
	});
});