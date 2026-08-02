import type { BatchApplyPlan, MergePreset, OperationMode, ScalarMergeStrategy, ArrayMergeStrategy } from '$lib/api/types';
import { migrateLegacyPlan, normalizePersistedApplyPlan } from '$lib/apply-plan';
import * as m from '$lib/paraglide/messages';

export interface PendingScrape {
	version?: 2;
	files: string[];
	applyPlan?: BatchApplyPlan | null;
	browseMode?: 'scrape' | 'update';
	update?: boolean;
	effectiveOperationMode?: OperationMode;
	isInPlaceImplied?: boolean;
	destination?: string;
	preset?: MergePreset;
	scalarStrategy?: ScalarMergeStrategy;
	arrayStrategy?: ArrayMergeStrategy;
	migrationWarning?: string;
	showScraperSelector: boolean;
	selectedScrapers: string[];
	force: boolean;
}

interface LegacyPendingScrape {
	files: string[];
	browseMode?: 'scrape' | 'update';
	update?: boolean;
	effectiveOperationMode?: OperationMode;
	isInPlaceImplied?: boolean;
	destination?: string;
	showScraperSelector?: boolean;
	selectedScrapers?: string[];
	force?: boolean;
	preset?: MergePreset;
	scalarStrategy?: ScalarMergeStrategy;
	arrayStrategy?: ArrayMergeStrategy;
}

const STORAGE_KEY = 'javinizer_pending_scrape';
let state: PendingScrape | null = $state(null);

function migrate(value: PendingScrape | LegacyPendingScrape): PendingScrape {
	if ('version' in value && value.version === 2) {
		try {
			return { ...value, applyPlan: value.applyPlan ? normalizePersistedApplyPlan(value.applyPlan) : null };
		} catch {
			return { ...value, applyPlan: null, migrationWarning: m.apply_plan_warn_saved_unsupported() };
		}
	}
	const legacy = value as LegacyPendingScrape;
	const migrated = migrateLegacyPlan(legacy);
	return {
		version: 2,
		files: Array.isArray(legacy.files) ? legacy.files : [],
		applyPlan: migrated.plan,
		migrationWarning: migrated.warning,
		showScraperSelector: legacy.showScraperSelector ?? false,
		selectedScrapers: legacy.selectedScrapers ?? [],
		force: legacy.force ?? false
	};
}

function hydrate(): PendingScrape | null {
	if (typeof sessionStorage === 'undefined') return null;
	const raw = sessionStorage.getItem(STORAGE_KEY);
	if (!raw) return null;
	try {
		const snapshot = migrate(JSON.parse(raw) as PendingScrape | LegacyPendingScrape);
		sessionStorage.setItem(STORAGE_KEY, JSON.stringify(snapshot));
		return snapshot;
	} catch {
		sessionStorage.removeItem(STORAGE_KEY);
		return null;
	}
}

export function buildPendingScrapeSnapshot(input: Omit<PendingScrape, 'version'>): PendingScrape {
	if (input.applyPlan) return { ...input, version: 2, applyPlan: normalizePersistedApplyPlan(input.applyPlan) };
	const migrated = migrateLegacyPlan(input);
	return { ...input, version: 2, applyPlan: migrated.plan, migrationWarning: migrated.warning, update: input.browseMode === 'update' };
}

export function getPendingScrape(): PendingScrape | null {
	if (state === null) state = hydrate();
	return state;
}

export function setPendingScrape(snapshot: PendingScrape): void {
	state = migrate(snapshot);
	if (typeof sessionStorage !== 'undefined') sessionStorage.setItem(STORAGE_KEY, JSON.stringify(state));
}

export function clearPendingScrape(): void {
	state = null;
	if (typeof sessionStorage !== 'undefined') sessionStorage.removeItem(STORAGE_KEY);
}