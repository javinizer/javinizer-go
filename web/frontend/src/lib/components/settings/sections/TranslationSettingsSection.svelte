<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { formatDate } from '$lib/i18n/format';
	import { RefreshCw, ChevronDown, Check } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormNumberInput from '$lib/components/settings/FormNumberInput.svelte';
	import FormPasswordInput from '$lib/components/settings/FormPasswordInput.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import { apiClient } from '$lib/api/client';
	import type { DeepLUsageResponse, SettingsConfig, TranslationConfig as TranslationConfigType, OpenAICompatibleTranslationConfig as OpenAICompatibleTranslationConfigType, AnthropicTranslationConfig as AnthropicTranslationConfigType, DeepLTranslationConfig as DeepLTranslationConfigType, GoogleTranslationConfig as GoogleTranslationConfigType, TranslationFieldsConfig as TranslationFieldsConfigType } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	selectClass: string;
		fetchTranslationModels: () => Promise<void>;
		fetchingTranslationModels: boolean;
		translationModelOptions: string[];
	}

	let {
		config,
		inputClass, selectClass,
		fetchTranslationModels,
		fetchingTranslationModels,
		translationModelOptions
	}: Props = $props();
	const translationEnabled = $derived(config?.metadata?.translation?.enabled ?? false);

	let deeplUsage: DeepLUsageResponse | null = $state<DeepLUsageResponse | null>(null);
	let fetchingDeepLUsage = $state(false);
	let deeplUsageError = $state<string | null>(null);
	let advancedExpanded = $state(false);

	const usagePercentage = $derived(
		deeplUsage && deeplUsage.character_limit > 0
			? (deeplUsage.character_count / deeplUsage.character_limit) * 100
			: 0
	);

	function formatNumber(n: number): string {
		if (n >= 1_000_000_000) return (n / 1_000_000_000).toFixed(1) + 'B';
		if (n >= 1_000_000) return (n / 1_000_000).toFixed(1) + 'M';
		if (n >= 1_000) return (n / 1_000).toFixed(1) + 'K';
		return n.toString();
	}

	async function fetchDeepLUsage() {
		const apiKey = config.metadata.translation?.deepl?.api_key ?? '';
		if (!apiKey.trim()) {
			deeplUsageError = m.settings_translation_deepl_usage_error_required();
			return;
		}

		fetchingDeepLUsage = true;
		deeplUsageError = null;
		deeplUsage = null;

		try {
			const mode = config.metadata.translation?.deepl?.mode ?? 'free';
			const baseURL = config.metadata.translation?.deepl?.base_url ?? '';
			deeplUsage = await apiClient.getDeepLUsage({
				mode,
				base_url: baseURL,
				api_key: apiKey
			});
		} catch (err: unknown) {
			deeplUsageError = err instanceof Error ? err.message : m.settings_translation_deepl_fetch_failed();
		} finally {
			fetchingDeepLUsage = false;
		}
	}
</script>

<SettingsSection title={m.settings_translation_title()} description={m.settings_translation_desc()} defaultExpanded={false}>
	<SettingsSubsection title={m.settings_translation_general_subsection()}>
		<FormToggle
			label={m.settings_translation_enable_label()}
			description={m.settings_translation_enable_desc()}
			checked={config.metadata.translation?.enabled ?? false}
			onchange={(val) => {
				if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
				config.metadata.translation!.enabled = val;
			}}
		/>

		<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
			<div class="py-4 border-b border-border">
				<label class="block text-sm font-medium mb-2" for="translation-provider">{m.settings_translation_provider_label()}</label>
				<select id="translation-provider" bind:value={config.metadata.translation!.provider} class={selectClass}>
					<option value="openai">{m.settings_translation_provider_openai()}</option>
					<option value="openai-compatible">{m.settings_translation_provider_openai_compatible()}</option>
					<option value="anthropic">{m.settings_translation_provider_anthropic()}</option>
					<option value="deepl">{m.settings_translation_provider_deepl()}</option>
					<option value="google">{m.settings_translation_provider_google()}</option>
				</select>
			</div>
		</fieldset>
	</SettingsSubsection>

	{#if config.metadata.translation?.provider === 'openai'}
		<SettingsSubsection title={m.settings_translation_openai_subsection()}>
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<FormTextInput
					label={m.settings_translation_base_url_label()}
					description={m.settings_translation_openai_base_url_desc()}
					value={config.metadata.translation?.openai?.base_url ?? 'https://api.openai.com/v1'}
					placeholder="https://api.openai.com/v1"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!.openai) config.metadata.translation!.openai = {};
						config.metadata.translation!.openai.base_url = val.trim();
					}}
				/>

				<div class="py-4 border-b border-border">
					<div class="flex items-center justify-between mb-2 gap-2">
						<label class="block text-sm font-medium" for="translation-openai-model-select">{m.settings_translation_model_label()}</label>
						<Button
							variant="outline"
							size="sm"
							onclick={fetchTranslationModels}
							disabled={
								fetchingTranslationModels ||
								!(config.metadata.translation?.openai?.base_url ?? '').trim() ||
								!(config.metadata.translation?.openai?.api_key ?? '').trim()
							}
						>
							{#snippet children()}
								<RefreshCw class={`h-4 w-4 mr-2 ${fetchingTranslationModels ? 'animate-spin' : ''}`} />
								{fetchingTranslationModels ? m.settings_translation_fetching() : m.settings_translation_fetch_models()}
							{/snippet}
						</Button>
					</div>

					{#if translationModelOptions.length > 0}
						<select id="translation-openai-model-select" bind:value={config.metadata.translation!.openai!.model} class={selectClass}>
							{#each translationModelOptions as modelName}
								<option value={modelName}>{modelName}</option>
							{/each}
						</select>
						<p class="text-xs text-muted-foreground mt-1">
							{m.settings_translation_models_loaded({ url: config.metadata.translation?.openai?.base_url ?? '' })}
						</p>
					{/if}

					<input
						id="translation-openai-model-input"
						type="text"
						value={config.metadata.translation?.openai?.model ?? 'gpt-4o-mini'}
						oninput={(e) => {
							if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
							if (!config.metadata.translation!.openai) config.metadata.translation!.openai = {};
							config.metadata.translation!.openai.model = e.currentTarget.value.trim();
						}}
						class="{inputClass} mt-3"
						placeholder="gpt-4o-mini"
					/>
					<p class="text-xs text-muted-foreground mt-1">{m.settings_translation_manual_override()}</p>
				</div>

				<FormPasswordInput
					label={m.settings_translation_api_key_label()}
					description="OpenAI API key"
					value={config.metadata.translation?.openai?.api_key ?? ''}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!.openai) config.metadata.translation!.openai = {};
						config.metadata.translation!.openai.api_key = val;
					}}
				/>
			</fieldset>
		</SettingsSubsection>
	{:else if config.metadata.translation?.provider === 'openai-compatible'}
		<SettingsSubsection title={m.settings_translation_openai_compatible_subsection()}>
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<FormTextInput
					label={m.settings_translation_base_url_label()}
					description={m.settings_translation_openai_compatible_base_url_desc()}
					value={config.metadata.translation?.['openai_compatible']?.base_url ?? 'http://localhost:11434/v1'}
					placeholder="http://localhost:11434/v1"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!['openai_compatible']) config.metadata.translation!['openai_compatible'] = {} as OpenAICompatibleTranslationConfigType;
						config.metadata.translation!['openai_compatible'].base_url = val.trim();
					}}
				/>

				<div class="py-4 border-b border-border">
					<div class="flex items-center justify-between mb-2 gap-2">
						<label class="block text-sm font-medium" for="translation-openai_compatible-model-select">{m.settings_translation_model_label()}</label>
						<Button
							variant="outline"
							size="sm"
							onclick={fetchTranslationModels}
							disabled={
								fetchingTranslationModels ||
								!(config.metadata.translation?.['openai_compatible']?.base_url ?? '').trim()
							}
						>
							{#snippet children()}
								<RefreshCw class={`h-4 w-4 mr-2 ${fetchingTranslationModels ? 'animate-spin' : ''}`} />
								{fetchingTranslationModels ? m.settings_translation_fetching() : m.settings_translation_fetch_models()}
							{/snippet}
						</Button>
					</div>

					{#if translationModelOptions.length > 0}
						<select id="translation-openai_compatible-model-select" bind:value={config.metadata.translation!['openai_compatible']!.model} class={selectClass}>
							{#each translationModelOptions as modelName}
								<option value={modelName}>{modelName}</option>
							{/each}
						</select>
						<p class="text-xs text-muted-foreground mt-1">
							{m.settings_translation_models_loaded({ url: config.metadata.translation?.['openai_compatible']?.base_url ?? '' })}
						</p>
					{/if}

					<input
						id="translation-openai_compatible-model-input"
						type="text"
						value={config.metadata.translation?.['openai_compatible']?.model ?? ''}
						oninput={(e) => {
							if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
							if (!config.metadata.translation!['openai_compatible']) config.metadata.translation!['openai_compatible'] = {} as OpenAICompatibleTranslationConfigType;
							config.metadata.translation!['openai_compatible'].model = e.currentTarget.value.trim();
						}}
						class="{inputClass} mt-3"
						placeholder="llama3"
					/>
					<p class="text-xs text-muted-foreground mt-1">{m.settings_translation_manual_override()}</p>
				</div>

				<FormPasswordInput
					label={m.settings_translation_openai_compatible_api_key_label()}
					description={m.settings_translation_openai_compatible_api_key_desc()}
					value={config.metadata.translation?.['openai_compatible']?.api_key ?? ''}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!['openai_compatible']) config.metadata.translation!['openai_compatible'] = {} as OpenAICompatibleTranslationConfigType;
						config.metadata.translation!['openai_compatible'].api_key = val;
					}}
				/>

				<FormToggle
					label={m.settings_translation_enable_thinking_label()}
					description={m.settings_translation_enable_thinking_desc()}
					checked={config.metadata.translation?.['openai_compatible']?.enable_thinking ?? false}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!['openai_compatible']) config.metadata.translation!['openai_compatible'] = {} as OpenAICompatibleTranslationConfigType;
						config.metadata.translation!['openai_compatible'].enable_thinking = val;
					}}
				/>
				
			</fieldset>
		</SettingsSubsection>
	{:else if config.metadata.translation?.provider === 'anthropic'}
		<SettingsSubsection title={m.settings_translation_anthropic_subsection()}>
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<FormTextInput
					label={m.settings_translation_base_url_label()}
					description={m.settings_translation_anthropic_base_url_desc()}
					value={config.metadata.translation?.anthropic?.base_url ?? 'https://api.anthropic.com'}
					placeholder="https://api.anthropic.com"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!.anthropic) config.metadata.translation!.anthropic = {} as AnthropicTranslationConfigType;
						config.metadata.translation!.anthropic.base_url = val.trim();
					}}
				/>

				<div class="py-4 border-b border-border">
					<div class="flex items-center justify-between mb-2 gap-2">
						<label class="block text-sm font-medium" for="translation-anthropic-model-select">{m.settings_translation_model_label()}</label>
						<Button
							variant="outline"
							size="sm"
							onclick={fetchTranslationModels}
							disabled={
								fetchingTranslationModels ||
								!(config.metadata.translation?.anthropic?.base_url ?? '').trim() ||
								!(config.metadata.translation?.anthropic?.api_key ?? '').trim()
							}
						>
							{#snippet children()}
								<RefreshCw class={`h-4 w-4 mr-2 ${fetchingTranslationModels ? 'animate-spin' : ''}`} />
								{fetchingTranslationModels ? m.settings_translation_fetching() : m.settings_translation_fetch_models()}
							{/snippet}
						</Button>
					</div>

					{#if translationModelOptions.length > 0}
						<select id="translation-anthropic-model-select" bind:value={config.metadata.translation!.anthropic!.model} class={selectClass}>
							{#each translationModelOptions as modelName}
								<option value={modelName}>{modelName}</option>
							{/each}
						</select>
						<p class="text-xs text-muted-foreground mt-1">
							{m.settings_translation_models_loaded({ url: config.metadata.translation?.anthropic?.base_url ?? '' })}
						</p>
					{/if}

					<input
						id="translation-anthropic-model-input"
						type="text"
						value={config.metadata.translation?.anthropic?.model ?? ''}
						oninput={(e) => {
							if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
							if (!config.metadata.translation!.anthropic) config.metadata.translation!.anthropic = {} as AnthropicTranslationConfigType;
							config.metadata.translation!.anthropic.model = e.currentTarget.value.trim();
						}}
						class="{inputClass} mt-3"
						placeholder="claude-sonnet-4-20250514"
					/>
					<p class="text-xs text-muted-foreground mt-1">{m.settings_translation_manual_override()}</p>
				</div>

				<FormPasswordInput
					label={m.settings_translation_api_key_label()}
					description={m.settings_translation_anthropic_api_key_desc()}
					value={config.metadata.translation?.anthropic?.api_key ?? ''}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!.anthropic) config.metadata.translation!.anthropic = {} as AnthropicTranslationConfigType;
						config.metadata.translation!.anthropic.api_key = val;
					}}
				/>
			</fieldset>
		</SettingsSubsection>
	{:else if config.metadata.translation?.provider === 'deepl'}
		<SettingsSubsection title={m.settings_translation_deepl_subsection()}>
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<div class="py-4 border-b border-border">
					<label class="block text-sm font-medium mb-2" for="deepl-mode">{m.settings_translation_deepl_mode_label()}</label>
					<select id="deepl-mode" bind:value={config.metadata.translation!.deepl!.mode} class={selectClass}>
						<option value="free">{m.settings_translation_deepl_mode_free()}</option>
						<option value="pro">{m.settings_translation_deepl_mode_pro()}</option>
					</select>
					<p class="text-xs text-muted-foreground mt-1">
						{m.settings_translation_deepl_mode_desc()}
					</p>
				</div>

				<FormTextInput
					label={m.settings_translation_deepl_base_url_label()}
					description={m.settings_translation_deepl_base_url_desc()}
					value={config.metadata.translation?.deepl?.base_url ?? ''}
					placeholder="https://api-free.deepl.com"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!.deepl) config.metadata.translation!.deepl = {} as DeepLTranslationConfigType;
						config.metadata.translation!.deepl.base_url = val.trim();
					}}
				/>

				<FormPasswordInput
					label={m.settings_translation_api_key_label()}
					description={m.settings_translation_deepl_api_key_desc()}
					value={config.metadata.translation?.deepl?.api_key ?? ''}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!.deepl) config.metadata.translation!.deepl = {} as DeepLTranslationConfigType;
						config.metadata.translation!.deepl.api_key = val;
					}}
				/>

				<div class="py-4 border-b border-border">
					<div class="flex items-center justify-between mb-3">
						<div>
							<h4 class="text-sm font-medium">{m.settings_translation_deepl_usage_heading()}</h4>
							<p class="text-xs text-muted-foreground">{m.settings_translation_deepl_usage_desc()}</p>
						</div>
						<Button
							variant="outline"
							size="sm"
							onclick={fetchDeepLUsage}
							disabled={
								fetchingDeepLUsage ||
								!(config.metadata.translation?.deepl?.api_key ?? '').trim()
							}
						>
							{#snippet children()}
								<RefreshCw class={`h-4 w-4 mr-2 ${fetchingDeepLUsage ? 'animate-spin' : ''}`} />
								{fetchingDeepLUsage ? m.settings_translation_fetching() : m.common_refresh()}
							{/snippet}
						</Button>
					</div>

					{#if deeplUsageError}
						<p class="text-xs text-destructive mb-2">{deeplUsageError}</p>
					{/if}

					{#if deeplUsage}
						<div class="space-y-2">
							<div class="flex items-center justify-between text-sm">
								<span class="font-medium">{m.settings_translation_deepl_characters_used()}</span>
								<span class="text-muted-foreground">
									{formatNumber(deeplUsage.character_count)} / {formatNumber(deeplUsage.character_limit)}
								</span>
							</div>
							<div class="h-3 bg-secondary rounded-full overflow-hidden">
								<div
									class="h-full rounded-full transition-all duration-300 {usagePercentage > 90 ? 'bg-destructive' : usagePercentage > 70 ? 'bg-yellow-500' : 'bg-primary'}"
									style="width: {Math.min(100, usagePercentage)}%"
								></div>
							</div>
							<div class="flex items-center justify-between text-xs text-muted-foreground">
								<span>{m.settings_translation_deepl_percent_used({ percent: usagePercentage.toFixed(1) })}</span>
								<span>{m.settings_translation_deepl_remaining({ count: formatNumber(deeplUsage.character_limit - deeplUsage.character_count) })}</span>
							</div>
							{#if deeplUsage.start_time && deeplUsage.end_time}
								<p class="text-xs text-muted-foreground">
									{m.settings_translation_deepl_billing_period({ start: formatDate(deeplUsage.start_time), end: formatDate(deeplUsage.end_time) })}
								</p>
							{/if}
						</div>
					{:else if !fetchingDeepLUsage && !deeplUsageError}
						<p class="text-xs text-muted-foreground">{m.settings_translation_deepl_click_refresh()}</p>
					{/if}
				</div>
			</fieldset>
		</SettingsSubsection>
	{:else if config.metadata.translation?.provider === 'google'}
		<SettingsSubsection title={m.settings_translation_google_subsection()}>
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<div class="py-4 border-b border-border">
					<label class="block text-sm font-medium mb-2" for="google-mode">{m.settings_translation_google_mode_label()}</label>
					<select id="google-mode" bind:value={config.metadata.translation!.google!.mode} class={selectClass}>
						<option value="free">{m.settings_translation_google_mode_free()}</option>
						<option value="paid">{m.settings_translation_google_mode_paid()}</option>
					</select>
				</div>

				<FormTextInput
					label={m.settings_translation_deepl_base_url_label()}
					description={m.settings_translation_google_base_url_desc()}
					value={config.metadata.translation?.google?.base_url ?? ''}
					placeholder="https://translation.googleapis.com"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!.google) config.metadata.translation!.google = {} as GoogleTranslationConfigType;
						config.metadata.translation!.google.base_url = val.trim();
					}}
				/>

				<FormPasswordInput
					label={m.settings_translation_api_key_label()}
					description={m.settings_translation_google_api_key_desc()}
					value={config.metadata.translation?.google?.api_key ?? ''}
					disabled={config.metadata.translation?.google?.mode !== 'paid'}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						if (!config.metadata.translation!.google) config.metadata.translation!.google = {} as GoogleTranslationConfigType;
						config.metadata.translation!.google.api_key = val;
					}}
				/>
			</fieldset>
		</SettingsSubsection>
	{/if}

	<SettingsSubsection title={m.settings_translation_options_subsection()} isCollapsible={true} isExpanded={advancedExpanded} onToggle={() => advancedExpanded = !advancedExpanded}>
		{#if advancedExpanded}
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<FormTextInput
					label={m.settings_translation_source_language_label()}
					description={m.settings_translation_source_language_desc()}
					value={config.metadata.translation?.source_language ?? 'en'}
					placeholder="en"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						config.metadata.translation!.source_language = val.trim();
					}}
				/>

				<FormTextInput
					label={m.settings_translation_target_language_label()}
					description={m.settings_translation_target_language_desc()}
					value={config.metadata.translation?.target_language ?? 'ja'}
					placeholder="ja"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						config.metadata.translation!.target_language = val.trim();
					}}
				/>

				<FormNumberInput
					label={m.settings_translation_timeout_label()}
					description={m.settings_translation_timeout_desc()}
					value={config.metadata.translation?.timeout_seconds ?? 60}
					min={5}
					max={300}
					unit={m.common_unit_seconds()}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						config.metadata.translation!.timeout_seconds = val;
					}}
				/>

				<FormToggle
					label={m.settings_translation_apply_primary_label()}
					description={m.settings_translation_apply_primary_desc()}
					checked={config.metadata.translation?.apply_to_primary ?? true}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						config.metadata.translation!.apply_to_primary = val;
					}}
				/>

				<FormToggle
					label={m.settings_translation_overwrite_target_label()}
					description={m.settings_translation_overwrite_target_desc()}
					checked={config.metadata.translation?.overwrite_existing_target ?? true}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
						config.metadata.translation!.overwrite_existing_target = val;
					}}
				/>

				<div class="py-4 border-t border-border">
					<p class="text-sm font-medium mb-3">{m.settings_translation_fields_heading()}</p>
					<div class="grid grid-cols-2 gap-x-6 gap-y-1">
						{#each [
							{ key: 'title', label: m.field_title() },
							{ key: 'original_title', label: m.field_original_title_translation() },
							{ key: 'description', label: m.field_description() },
							{ key: 'director', label: m.field_director() },
							{ key: 'maker', label: m.field_maker() },
							{ key: 'label', label: m.field_label() },
							{ key: 'series', label: m.field_series() },
							{ key: 'genres', label: m.field_genres() },
							{ key: 'actresses', label: m.field_actresses() },
						] as field}
							<label class="flex items-center gap-2 py-1.5 cursor-pointer">
								<div class="relative">
									<input
										type="checkbox"
										checked={config.metadata.translation?.fields?.[field.key] !== false}
										onchange={(e) => {
											if (!config.metadata.translation) config.metadata.translation = {} as TranslationConfigType;
											if (!config.metadata.translation!.fields) config.metadata.translation!.fields = {};
											config.metadata.translation!.fields[field.key] = e.currentTarget.checked;
										}}
										class="peer h-4 w-4 rounded border-gray-300 text-primary focus:ring-2 focus:ring-primary disabled:opacity-50 cursor-pointer"
									/>
									<Check class="pointer-events-none absolute inset-0 h-4 w-4 text-primary opacity-0 peer-checked:opacity-100" />
								</div>
								<span class="text-sm">{field.label}</span>
							</label>
						{/each}
					</div>
				</div>
			</fieldset>
		{/if}
	</SettingsSubsection>
</SettingsSection>
