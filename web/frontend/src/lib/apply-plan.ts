import * as m from '$lib/paraglide/messages';
import type {
	ArrayMergeStrategy,
	BatchApplyPlan,
	BatchScrapeRequest,
	MediaPolicy,
	MergePreset,
	OperationMode,
	ScalarMergeStrategy,
	VideoOperation,
} from '$lib/api/types';

const presetStrategies: Record<MergePreset, [ScalarMergeStrategy, ArrayMergeStrategy]> = {
	conservative: ['preserve-existing', 'merge'],
	'gap-fill': ['fill-missing-only', 'merge'],
	aggressive: ['prefer-scraper', 'replace'],
};

export function operationFromMode(mode?: OperationMode): VideoOperation | null {
	switch (mode) {
		case 'organize':
			return 'organize';
		case 'in-place':
			return 'rename-in-place';
		case 'in-place-norenamefolder':
			return 'rename-file';
		case 'metadata-artwork':
			return 'metadata-artwork';
		default:
			return null;
	}
}

export function defaultApplyPlan(operation: VideoOperation, destination = ''): BatchApplyPlan {
	const plan: BatchApplyPlan = {
		version: 1,
		video_operation: operation,
		nfo_output: 'write',
		media_policy: 'missing',
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
	plan.destination =
		plan.video_operation === 'organize' ? (plan.destination ?? '').trim() : undefined;
	if (plan.video_operation === 'leave-in-place') {
		plan.merge ??= { scalar_strategy: 'prefer-nfo', array_strategy: 'merge' };
		if (plan.merge.source_preset) {
			const [scalar, array] = presetStrategies[plan.merge.source_preset];
			if (scalar !== plan.merge.scalar_strategy || array !== plan.merge.array_strategy) {
				throw new Error(m.apply_plan_err_preset_conflict());
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

export function setMergeStrategies(
	plan: BatchApplyPlan,
	scalar: ScalarMergeStrategy,
	array: ArrayMergeStrategy,
): BatchApplyPlan {
	const next = normalizeApplyPlan(plan);
	if (next.video_operation === 'leave-in-place') {
		next.merge = { scalar_strategy: scalar, array_strategy: array };
	}
	return next;
}

function validateApplyPlanEnums(plan: BatchApplyPlan): string[] {
	const errors: string[] = [];
	if (plan.version !== 1) errors.push(m.apply_plan_err_version());
	if (
		!['organize', 'rename-in-place', 'rename-file', 'leave-in-place', 'metadata-artwork'].includes(
			plan.video_operation,
		)
	)
		errors.push(m.apply_plan_err_operation());
	if (!['write', 'skip'].includes(plan.nfo_output)) errors.push(m.apply_plan_err_nfo_output());
	if (!['missing', 'replace', 'skip'].includes(plan.media_policy))
		errors.push(m.apply_plan_err_media_policy());
	if (plan.merge) {
		if (
			!['prefer-nfo', 'prefer-scraper', 'preserve-existing', 'fill-missing-only'].includes(
				plan.merge.scalar_strategy,
			)
		)
			errors.push(m.apply_plan_err_scalar_strategy());
		if (!['merge', 'replace'].includes(plan.merge.array_strategy))
			errors.push(m.apply_plan_err_array_strategy());
		if (
			plan.merge.source_preset &&
			!['conservative', 'gap-fill', 'aggressive'].includes(plan.merge.source_preset)
		)
			errors.push(m.apply_plan_err_preset());
	}
	return errors;
}

export function validateApplyPlan(plan: BatchApplyPlan | null): string[] {
	if (!plan) return [m.apply_plan_err_select_operation()];
	const errors = validateApplyPlanEnums(plan);
	if (plan.video_operation === 'organize' && !(plan.destination ?? '').trim())
		errors.push(m.apply_plan_err_destination());
	if (plan.video_operation !== 'leave-in-place' && plan.media_policy === 'replace')
		errors.push(m.apply_plan_err_replace_requires_in_place());
	if (
		(plan.video_operation === 'leave-in-place' || plan.video_operation === 'metadata-artwork') &&
		plan.nfo_output === 'skip' &&
		plan.media_policy === 'skip'
	)
		errors.push(m.apply_plan_err_choose_output());
	if (plan.video_operation === 'leave-in-place' && !plan.merge)
		errors.push(m.apply_plan_err_merge_policy());
	return errors;
}

export function projectLegacyPlan(
	plan: BatchApplyPlan,
): Pick<
	BatchScrapeRequest,
	'destination' | 'update' | 'operation_mode' | 'preset' | 'scalar_strategy' | 'array_strategy'
> {
	const normalized = normalizeApplyPlan(plan);
	const modes: Record<VideoOperation, OperationMode> = {
		organize: 'organize',
		'rename-in-place': 'in-place',
		'rename-file': 'in-place-norenamefolder',
		'leave-in-place': 'metadata-artwork',
		'metadata-artwork': 'metadata-artwork',
	};
	return {
		destination: normalized.destination,
		update: normalized.video_operation === 'leave-in-place',
		operation_mode: modes[normalized.video_operation],
		preset: normalized.merge?.source_preset,
		scalar_strategy: normalized.merge?.scalar_strategy,
		array_strategy: normalized.merge?.array_strategy,
	};
}

// Localized labels for merge strategy enums — the plan summary and the
// Browse preset cards must never show raw identifiers ("prefer-nfo"…) to
// non-English users. Unknown future values render as-is rather than hiding.
export function scalarStrategyLabel(strategy: ScalarMergeStrategy): string {
	switch (strategy) {
		case 'prefer-nfo':
			return m.browse_prefer_nfo();
		case 'prefer-scraper':
			return m.browse_prefer_scraped();
		case 'preserve-existing':
			return m.browse_preserve_existing();
		case 'fill-missing-only':
			return m.browse_fill_missing_only();
		default:
			return strategy;
	}
}

export function arrayStrategyLabel(strategy: ArrayMergeStrategy): string {
	switch (strategy) {
		case 'replace':
			return m.browse_replace();
		default:
			return m.browse_merge();
	}
}

// Resolved lazily at call time: paraglide message functions read the
// CURRENT locale, so a module-level Record would freeze the language of
// whatever locale was active when the bundle was first imported.
function operationText(op: VideoOperation): string {
	switch (op) {
		case 'organize':
			return m.apply_plan_op_organize();
		case 'rename-in-place':
			return m.apply_plan_op_rename_in_place();
		case 'rename-file':
			return m.apply_plan_op_rename_file();
		case 'leave-in-place':
			return m.apply_plan_op_leave_in_place();
		default:
			return m.apply_plan_op_metadata_artwork();
	}
}

export function applyPlanSummary(plan: BatchApplyPlan): string[] {
	const normalized = normalizeApplyPlan(plan);
	const lines = [operationText(normalized.video_operation)];
	if (normalized.video_operation === 'organize' && normalized.destination)
		lines.push(m.apply_plan_destination({ destination: normalized.destination }));
	lines.push(
		normalized.nfo_output === 'write' ? m.apply_plan_nfo_write() : m.apply_plan_nfo_skip(),
	);
	const media: Record<MediaPolicy, string> = {
		missing: m.apply_plan_media_missing(),
		replace: m.apply_plan_media_replace(),
		skip: m.apply_plan_media_skip(),
	};
	lines.push(media[normalized.media_policy]);
	if (normalized.merge)
		lines.push(
			m.apply_plan_merge_line({
				scalar: scalarStrategyLabel(normalized.merge.scalar_strategy),
				array: arrayStrategyLabel(normalized.merge.array_strategy),
			}),
		);
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

export function migrateLegacyPlan(state: LegacyPlanState): {
	plan: BatchApplyPlan | null;
	warning?: string;
} {
	if (
		state.browseMode &&
		state.update !== undefined &&
		(state.browseMode === 'update') !== state.update
	) {
		return { plan: null, warning: m.apply_plan_warn_settings_changed() };
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
	const operation = state.effectiveOperationMode
		? operationFromMode(state.effectiveOperationMode)
		: null;
	if (state.effectiveOperationMode && !operation) {
		return { plan: null, warning: m.apply_plan_warn_legacy_selection() };
	}
	if (!operation) return { plan: null, warning: m.apply_plan_warn_legacy_selection() };
	return { plan: defaultApplyPlan(operation, state.destination) };
}
