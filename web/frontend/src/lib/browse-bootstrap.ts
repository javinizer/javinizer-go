import type { BatchApplyPlan } from '$lib/api/types';

export const BrowseBootstrapCookie = 'javinizer_browse_bootstrap';

export interface BrowseBootstrap {
	version: 1;
	applyPlan: BatchApplyPlan | null;
	planMigrationWarning?: string;
	initialPath: string;
	destinationPath: string;
	forceRefresh: boolean;
	showScraperSelector: boolean;
	selectedScrapers: string[];
	manualScrapeMode: boolean;
	planExpanded: boolean;
}

export function encodeBrowseBootstrap(value: BrowseBootstrap): string {
	return encodeURIComponent(JSON.stringify(value));
}

export function decodeBrowseBootstrap(value: string): BrowseBootstrap | null {
	try {
		const parsed = JSON.parse(decodeURIComponent(value)) as Partial<BrowseBootstrap>;
		if (parsed.version !== 1) return null;
		if (!Array.isArray(parsed.selectedScrapers) || !parsed.selectedScrapers.every((item) => typeof item === 'string')) return null;
		return {
			version: 1,
			applyPlan: parsed.applyPlan ?? null,
			planMigrationWarning: typeof parsed.planMigrationWarning === 'string' ? parsed.planMigrationWarning : undefined,
			initialPath: typeof parsed.initialPath === 'string' ? parsed.initialPath : '',
			destinationPath: typeof parsed.destinationPath === 'string' ? parsed.destinationPath : '',
			forceRefresh: parsed.forceRefresh === true,
			showScraperSelector: parsed.showScraperSelector === true,
			selectedScrapers: parsed.selectedScrapers,
			manualScrapeMode: parsed.manualScrapeMode === true,
			planExpanded: typeof parsed.planExpanded === 'boolean' ? parsed.planExpanded : true
		};
	} catch {
		return null;
	}
}