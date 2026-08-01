import type {
	ArrayMergeStrategy,
	BatchApplyPlan,
	BatchScrapeRequest,
	MediaPolicy,
	MergePreset,
	OperationMode,
	ScalarMergeStrategy,
	VideoOperation
} from '$lib/api/types';

const presetStrategies: Record<MergePreset, [ScalarMergeStrategy, ArrayMergeStrategy]> = {
	conservative: ['preserve-existing', 'merge'],
	'gap-fill': ['fill-missing-only', 'merge'],
	aggressive: ['prefer-scraper', 'replace']
};

export function operationFromMode(mode?: OperationMode): VideoOperation | null {
	switch (mode) {
		case 'organize': return 'organize';
		case 'in-place': return 'rename-in-place';
		case 'in-place-norenamefolder': return 'rename-file';
		default: return null;
	}
}

export function defaultApplyPlan(operation: VideoOperation, destination = ''): BatchApplyPlan {
	const plan: BatchApplyPlan = {
		version: 1,
		video_operation: operation,
		nfo_output: 'write',
		media_policy: 'missing'
	};
	if (operation === 'organize') plan.destination = destination.trim();
	if (operation === 'leave-in-place') {
		plan.merge = { scalar_strategy: 'prefer-nfo', array_strategy: 'merge' };
	}
	return plan;
}

export function initialApplyPlan(mode?: OperationMode, destination = ''): BatchApplyPlan | null {
	const operation = operationFromMode(mode);
	return operation ? defaultApplyPlan(operation, destination) : null;
}

export function normalizeApplyPlan(input: BatchApplyPlan): BatchApplyPlan {
	const enumErrors = validateApplyPlanEnums(input);
	if (enumErrors.length > 0) throw new Error(enumErrors.join(' '));
	const plan: BatchApplyPlan = { ...input, merge: input.merge ? { ...input.merge } : undefined };
	plan.destination = plan.video_operation === 'organize' ? (plan.destination ?? '').trim() : undefined;
	if (plan.video_operation === 'leave-in-place') {
		plan.merge ??= { scalar_strategy: 'prefer-nfo', array_strategy: 'merge' };
		if (plan.merge.source_preset) {
			const [scalar, array] = presetStrategies[plan.merge.source_preset];
			if (scalar !== plan.merge.scalar_strategy || array !== plan.merge.array_strategy) {
				throw new Error('Merge preset contradicts the selected strategies.');
			}
		}
	} else {
		delete plan.merge;
	}
	return plan;
}

export function normalizePersistedApplyPlan(plan: BatchApplyPlan): BatchApplyPlan {
	const normalized = normalizeApplyPlan(plan);
	const errors = validateApplyPlan(normalized);
	if (errors.length > 0) throw new Error(errors.join(' '));
	return normalized;
}

export function applyPreset(plan: BatchApplyPlan, preset: MergePreset): BatchApplyPlan {
	const next = normalizeApplyPlan(plan);
	if (next.video_operation !== 'leave-in-place' || !next.merge) return next;
	const [scalar, array] = presetStrategies[preset];
	next.merge = { scalar_strategy: scalar, array_strategy: array, source_preset: preset };
	return next;
}

export function setMergeStrategies(plan: BatchApplyPlan, scalar: ScalarMergeStrategy, array: ArrayMergeStrategy): BatchApplyPlan {
	const next = normalizeApplyPlan(plan);
	if (next.video_operation === 'leave-in-place') {
		next.merge = { scalar_strategy: scalar, array_strategy: array };
	}
	return next;
}

function validateApplyPlanEnums(plan: BatchApplyPlan): string[] {
	const errors: string[] = [];
	if (plan.version !== 1) errors.push('Unsupported apply plan version.');
	if (!['organize', 'rename-in-place', 'rename-file', 'leave-in-place'].includes(plan.video_operation)) errors.push('Unsupported video operation.');
	if (!['write', 'skip'].includes(plan.nfo_output)) errors.push('Unsupported NFO output policy.');
	if (!['missing', 'replace', 'skip'].includes(plan.media_policy)) errors.push('Unsupported media policy.');
	if (plan.merge) {
		if (!['prefer-nfo', 'prefer-scraper', 'preserve-existing', 'fill-missing-only'].includes(plan.merge.scalar_strategy)) errors.push('Unsupported scalar merge strategy.');
		if (!['merge', 'replace'].includes(plan.merge.array_strategy)) errors.push('Unsupported array merge strategy.');
		if (plan.merge.source_preset && !['conservative', 'gap-fill', 'aggressive'].includes(plan.merge.source_preset)) errors.push('Unsupported merge preset.');
	}
	return errors;
}

export function validateApplyPlan(plan: BatchApplyPlan | null): string[] {
	if (!plan) return ['Select a video file operation.'];
	const errors = validateApplyPlanEnums(plan);
	if (plan.video_operation === 'organize' && !(plan.destination ?? '').trim()) errors.push('Choose an organization destination.');
	if (plan.video_operation !== 'leave-in-place' && plan.media_policy === 'replace') errors.push('Existing media can only be replaced when video files stay in place.');
	if (plan.video_operation === 'leave-in-place' && plan.nfo_output === 'skip' && plan.media_policy === 'skip') errors.push('Choose at least one metadata or media output.');
	if (plan.video_operation === 'leave-in-place' && !plan.merge) errors.push('Choose an existing metadata merge policy.');
	return errors;
}

export function projectLegacyPlan(plan: BatchApplyPlan): Pick<BatchScrapeRequest, 'destination' | 'update' | 'operation_mode' | 'preset' | 'scalar_strategy' | 'array_strategy'> {
	const normalized = normalizeApplyPlan(plan);
	const modes: Record<VideoOperation, OperationMode> = {
		organize: 'organize',
		'rename-in-place': 'in-place',
		'rename-file': 'in-place-norenamefolder',
		'leave-in-place': 'metadata-artwork'
	};
	return {
		destination: normalized.destination,
		update: normalized.video_operation === 'leave-in-place',
		operation_mode: modes[normalized.video_operation],
		preset: normalized.merge?.source_preset,
		scalar_strategy: normalized.merge?.scalar_strategy,
		array_strategy: normalized.merge?.array_strategy
	};
}

const operationText: Record<VideoOperation, string> = {
	organize: 'Organize videos into another location',
	'rename-in-place': 'Rename videos and eligible dedicated folders in place',
	'rename-file': 'Rename video files without renaming their folders',
	'leave-in-place': 'Leave video files in place'
};

export function applyPlanSummary(plan: BatchApplyPlan): string[] {
	const normalized = normalizeApplyPlan(plan);
	const lines = [operationText[normalized.video_operation]];
	if (normalized.video_operation === 'organize' && normalized.destination) lines.push(`Destination: ${normalized.destination}`);
	lines.push(normalized.nfo_output === 'write' ? 'Write NFO metadata' : 'Skip NFO output');
	const media: Record<MediaPolicy, string> = {
		missing: 'Download missing enabled media',
		replace: 'Replace existing enabled media (destructive)',
		skip: 'Skip media downloads'
	};
	lines.push(media[normalized.media_policy]);
	if (normalized.merge) lines.push(`Existing metadata: ${normalized.merge.scalar_strategy}, arrays ${normalized.merge.array_strategy}`);
	return lines;
}

export function compactApplyPlanSummary(plan: BatchApplyPlan): string {
	return applyPlanSummary(plan).join(' · ');
}

export interface LegacyPlanState {
	browseMode?: 'scrape' | 'update';
	update?: boolean;
	effectiveOperationMode?: OperationMode;
	destination?: string;
	scalarStrategy?: ScalarMergeStrategy;
	arrayStrategy?: ArrayMergeStrategy;
}

export function migrateLegacyPlan(state: LegacyPlanState): { plan: BatchApplyPlan | null; warning?: string } {
	if (state.browseMode && state.update !== undefined && (state.browseMode === 'update') !== state.update) {
		return { plan: null, warning: 'Apply settings changed; select an operation again.' };
	}
	const update = state.update ?? state.browseMode === 'update';
	if (update) {
		const plan = defaultApplyPlan('leave-in-place');
		if (plan.merge) {
			plan.merge.scalar_strategy = state.scalarStrategy ?? 'prefer-nfo';
			plan.merge.array_strategy = state.arrayStrategy ?? 'merge';
		}
		return { plan };
	}
	const operation = state.effectiveOperationMode ? operationFromMode(state.effectiveOperationMode) : null;
	if (state.effectiveOperationMode && !operation) {
		return { plan: null, warning: 'Legacy apply settings require a new operation selection.' };
	}
	if (!operation) return { plan: null, warning: 'Legacy apply settings require a new operation selection.' };
	return { plan: defaultApplyPlan(operation, state.destination) };
}