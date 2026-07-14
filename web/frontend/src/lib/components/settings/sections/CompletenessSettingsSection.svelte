<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import FormNumberInput from '$lib/components/settings/FormNumberInput.svelte';
	import type { SettingsConfig, CompletenessConfig, CompletenessTierDefinition } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	selectClass: string;
	}

	let { config, inputClass, selectClass }: Props = $props();

	const DEFAULT_COMPLETENESS_CONFIG: CompletenessConfig = {
		enabled: false,
		tiers: {
			essential: { weight: 50, fields: ['title', 'poster_url', 'cover_url', 'actresses', 'genres'] },
			important: { weight: 35, fields: ['description', 'maker', 'release_date', 'director', 'runtime', 'trailer_url', 'screenshot_urls'] },
			nice_to_have: { weight: 15, fields: ['label', 'series', 'rating_score', 'original_title', 'translations'] },
		},
	};

	const ALL_FIELDS: { key: string; label: string }[] = [
		{ key: 'title', label: m.field_title() },
		{ key: 'poster_url', label: m.field_poster() },
		{ key: 'cover_url', label: m.field_cover() },
		{ key: 'actresses', label: m.field_actresses() },
		{ key: 'genres', label: m.field_genres() },
		{ key: 'description', label: m.field_description() },
		{ key: 'maker', label: m.field_maker() },
		{ key: 'release_date', label: m.field_release_date() },
		{ key: 'director', label: m.field_director() },
		{ key: 'runtime', label: m.field_runtime() },
		{ key: 'trailer_url', label: m.field_trailer() },
		{ key: 'screenshot_urls', label: m.field_screenshots() },
		{ key: 'label', label: m.field_label() },
		{ key: 'series', label: m.field_series() },
		{ key: 'rating_score', label: m.field_rating() },
		{ key: 'original_title', label: m.field_original_title() },
		{ key: 'translations', label: m.field_translations() },
	];

	function ensureCompletenessConfig() {
		if (!config.metadata.completeness) {
			config.metadata.completeness = structuredClone(DEFAULT_COMPLETENESS_CONFIG);
		}
	}

	function handleEnableToggle(val: boolean) {
		ensureCompletenessConfig();
		if (val && !config.metadata.completeness!.tiers.essential.fields.length) {
			config.metadata.completeness = structuredClone(DEFAULT_COMPLETENESS_CONFIG);
		}
		config.metadata.completeness!.enabled = val;
	}

	function handleWeightChange(tierKey: 'essential' | 'important' | 'nice_to_have', value: number) {
		ensureCompletenessConfig();
		config.metadata.completeness!.tiers[tierKey].weight = value;
	}

	function handleFieldToggle(tierKey: 'essential' | 'important' | 'nice_to_have', fieldKey: string, checked: boolean) {
		ensureCompletenessConfig();
		const tierKeys = ['essential', 'important', 'nice_to_have'] as const;
		if (checked) {
			for (const tk of tierKeys) {
				if (tk !== tierKey) {
					config.metadata.completeness!.tiers[tk].fields =
						config.metadata.completeness!.tiers[tk].fields.filter(f => f !== fieldKey);
				}
			}
			if (!config.metadata.completeness!.tiers[tierKey].fields.includes(fieldKey)) {
				config.metadata.completeness!.tiers[tierKey].fields = [...config.metadata.completeness!.tiers[tierKey].fields, fieldKey];
			}
		} else {
			config.metadata.completeness!.tiers[tierKey].fields =
				config.metadata.completeness!.tiers[tierKey].fields.filter(f => f !== fieldKey);
		}
	}

	function resetToDefaults() {
		config.metadata.completeness = structuredClone(DEFAULT_COMPLETENESS_CONFIG);
		config.metadata.completeness.enabled = true;
	}

	let weightSum = $derived(
		(config.metadata.completeness?.tiers.essential.weight ?? 0) +
		(config.metadata.completeness?.tiers.important.weight ?? 0) +
		(config.metadata.completeness?.tiers.nice_to_have.weight ?? 0)
	);

	function isFieldInTier(tierKey: 'essential' | 'important' | 'nice_to_have', fieldKey: string): boolean {
		return config.metadata.completeness?.tiers[tierKey]?.fields?.includes(fieldKey) ?? false;
	}

	function getFieldCount(tierKey: 'essential' | 'important' | 'nice_to_have'): number {
		return config.metadata.completeness?.tiers[tierKey]?.fields?.length ?? 0;
	}

	const TIER_CONFIG: { key: 'essential' | 'important' | 'nice_to_have'; title: string }[] = [
		{ key: 'essential', title: m.settings_completeness_tier_essential() },
		{ key: 'important', title: m.settings_completeness_tier_important() },
		{ key: 'nice_to_have', title: m.settings_completeness_tier_nice_to_have() },
	];
</script>

<SettingsSection title={m.settings_completeness_title()} description={m.settings_completeness_desc()} defaultExpanded={false}>
	<div class="space-y-4">
		<FormToggle
			label={m.settings_completeness_enable_label()}
			description={m.settings_completeness_enable_desc()}
			checked={config.metadata.completeness?.enabled ?? false}
			onchange={handleEnableToggle}
		/>

		{#if config.metadata.completeness?.enabled}
			{#each TIER_CONFIG as tierConfig}
				<SettingsSubsection title={tierConfig.title}>
					<FormNumberInput
						label={m.settings_completeness_weight_label()}
						description={m.settings_completeness_weight_desc()}
						value={config.metadata.completeness?.tiers[tierConfig.key]?.weight ?? 0}
						min={1}
						max={100}
						unit="%"
						onchange={(val) => handleWeightChange(tierConfig.key, val)}
					/>

					<div class="py-2">
						<p class="text-sm font-medium mb-2">{m.settings_completeness_fields()}</p>
						<div class="grid grid-cols-1 md:grid-cols-2 gap-2">
							{#each ALL_FIELDS as field}
								<label class="flex items-center gap-2 text-sm">
									<input
										type="checkbox"
										checked={isFieldInTier(tierConfig.key, field.key)}
										onchange={(e) => handleFieldToggle(tierConfig.key, field.key, (e.target as HTMLInputElement).checked)}
										class="rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary cursor-pointer"
									/>
									<span>{field.label}</span>
								</label>
							{/each}
						</div>
						<p class="text-xs text-muted-foreground mt-2">{m.settings_completeness_fields_assigned({ count: getFieldCount(tierConfig.key) })}</p>
					</div>
				</SettingsSubsection>
			{/each}

			<div class="pt-4 border-t mt-4">
				{#if weightSum === 100}
					<p class="text-sm text-green-600 dark:text-green-400 font-medium">{m.settings_completeness_weights_ok()}</p>
				{:else}
					<p class="text-sm text-yellow-600 dark:text-yellow-400 font-medium">{m.settings_completeness_weights_warn({ weight: weightSum })}</p>
				{/if}
			</div>

			<div class="pt-2">
				<button
					type="button"
					class="text-sm text-primary hover:underline cursor-pointer"
					onclick={resetToDefaults}
				>
					{m.settings_completeness_reset()}
				</button>
			</div>
		{/if}
	</div>
</SettingsSection>
