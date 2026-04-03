<script lang="ts">
	import { RefreshCw } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormNumberInput from '$lib/components/settings/FormNumberInput.svelte';
	import FormPasswordInput from '$lib/components/settings/FormPasswordInput.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import { apiClient } from '$lib/api/client';
	import type { DeepLUsageResponse } from '$lib/api/types';

	interface Props {
		config: any;
		inputClass: string;
		fetchTranslationModels: () => Promise<void>;
		fetchingTranslationModels: boolean;
		translationModelOptions: string[];
	}

	let {
		config,
		inputClass,
		fetchTranslationModels,
		fetchingTranslationModels,
		translationModelOptions
	}: Props = $props();
	const translationEnabled = $derived(config?.metadata?.translation?.enabled ?? false);

	let deeplUsage: DeepLUsageResponse | null = $state<DeepLUsageResponse | null>(null);
	let fetchingDeepLUsage = $state(false);
	let deeplUsageError = $state<string | null>(null);

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
			deeplUsageError = 'API key is required';
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
		} catch (err: any) {
			deeplUsageError = err?.message || 'Failed to fetch usage data';
		} finally {
			fetchingDeepLUsage = false;
		}
	}
</script>

<SettingsSection title="Translation Settings" description="Translate aggregated metadata to a target language using configurable providers" defaultExpanded={false}>
	<SettingsSubsection title="General">
			<FormToggle
				label="Enable translation"
				description="Translate metadata after aggregation and before saving to database"
				checked={config.metadata.translation?.enabled ?? false}
			onchange={(val) => {
				if (!config.metadata.translation) config.metadata.translation = {};
					config.metadata.translation.enabled = val;
				}}
			/>

			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<div class="py-4 border-b border-border">
					<label class="block text-sm font-medium mb-2" for="translation-provider">Provider</label>
					<select id="translation-provider" bind:value={config.metadata.translation.provider} class={inputClass}>
						<option value="openai">OpenAI-Compatible (OpenAI/OpenRouter/etc.)</option>
						<option value="deepl">DeepL</option>
						<option value="google">Google Translate</option>
					</select>
				</div>

				<FormTextInput
					label="Source language"
					description="Source language code (use 'auto' when provider supports auto-detection)"
					value={config.metadata.translation?.source_language ?? 'en'}
					placeholder="en"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						config.metadata.translation.source_language = val.trim();
					}}
				/>

				<FormTextInput
					label="Target language"
					description="Target language code for translated metadata"
					value={config.metadata.translation?.target_language ?? 'ja'}
					placeholder="ja"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						config.metadata.translation.target_language = val.trim();
					}}
				/>

				<FormNumberInput
					label="Timeout"
					description="Maximum time to wait for translation API calls"
					value={config.metadata.translation?.timeout_seconds ?? 60}
					min={5}
					max={300}
					unit="seconds"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						config.metadata.translation.timeout_seconds = val;
					}}
				/>

				<FormToggle
					label="Apply to primary metadata"
					description="Replace primary movie fields with translated text"
					checked={config.metadata.translation?.apply_to_primary ?? true}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						config.metadata.translation.apply_to_primary = val;
					}}
				/>

				<FormToggle
					label="Overwrite existing target translation"
					description="Overwrite target-language translation entries already returned by scrapers"
					checked={config.metadata.translation?.overwrite_existing_target ?? true}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						config.metadata.translation.overwrite_existing_target = val;
					}}
				/>
			</fieldset>
		</SettingsSubsection>

	<SettingsSubsection title="Field Selection">
		<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
			<FormToggle
				label="Translate title"
				description="Translate the title field"
				checked={config.metadata.translation?.fields?.title ?? true}
				onchange={(val) => {
					if (!config.metadata.translation) config.metadata.translation = {};
					if (!config.metadata.translation.fields) config.metadata.translation.fields = {};
					config.metadata.translation.fields.title = val;
				}}
			/>
			<FormToggle
				label="Translate original title"
				description="Translate the original title field"
				checked={config.metadata.translation?.fields?.original_title ?? true}
				onchange={(val) => {
					if (!config.metadata.translation) config.metadata.translation = {};
					if (!config.metadata.translation.fields) config.metadata.translation.fields = {};
					config.metadata.translation.fields.original_title = val;
				}}
			/>
			<FormToggle
				label="Translate description"
				description="Translate the description field"
				checked={config.metadata.translation?.fields?.description ?? true}
				onchange={(val) => {
					if (!config.metadata.translation) config.metadata.translation = {};
					if (!config.metadata.translation.fields) config.metadata.translation.fields = {};
					config.metadata.translation.fields.description = val;
				}}
			/>
			<FormToggle
				label="Translate director"
				description="Translate the director field"
				checked={config.metadata.translation?.fields?.director ?? true}
				onchange={(val) => {
					if (!config.metadata.translation) config.metadata.translation = {};
					if (!config.metadata.translation.fields) config.metadata.translation.fields = {};
					config.metadata.translation.fields.director = val;
				}}
			/>
			<FormToggle
				label="Translate maker"
				description="Translate the maker/studio field"
				checked={config.metadata.translation?.fields?.maker ?? true}
				onchange={(val) => {
					if (!config.metadata.translation) config.metadata.translation = {};
					if (!config.metadata.translation.fields) config.metadata.translation.fields = {};
					config.metadata.translation.fields.maker = val;
				}}
			/>
			<FormToggle
				label="Translate label"
				description="Translate the label field"
				checked={config.metadata.translation?.fields?.label ?? true}
				onchange={(val) => {
					if (!config.metadata.translation) config.metadata.translation = {};
					if (!config.metadata.translation.fields) config.metadata.translation.fields = {};
					config.metadata.translation.fields.label = val;
				}}
			/>
			<FormToggle
				label="Translate series"
				description="Translate the series field"
				checked={config.metadata.translation?.fields?.series ?? true}
				onchange={(val) => {
					if (!config.metadata.translation) config.metadata.translation = {};
					if (!config.metadata.translation.fields) config.metadata.translation.fields = {};
					config.metadata.translation.fields.series = val;
				}}
			/>
			<FormToggle
				label="Translate genres"
				description="Translate genre names"
				checked={config.metadata.translation?.fields?.genres ?? true}
				onchange={(val) => {
					if (!config.metadata.translation) config.metadata.translation = {};
					if (!config.metadata.translation.fields) config.metadata.translation.fields = {};
					config.metadata.translation.fields.genres = val;
				}}
			/>
			<FormToggle
				label="Translate actresses"
				description="Translate actress names"
				checked={config.metadata.translation?.fields?.actresses ?? true}
				onchange={(val) => {
					if (!config.metadata.translation) config.metadata.translation = {};
					if (!config.metadata.translation.fields) config.metadata.translation.fields = {};
					config.metadata.translation.fields.actresses = val;
				}}
			/>
		</fieldset>
	</SettingsSubsection>

	{#if config.metadata.translation?.provider === 'openai'}
		<SettingsSubsection title="OpenAI-Compatible Provider">
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<FormTextInput
					label="Base URL"
					description="OpenAI-compatible API base URL (works with OpenAI, OpenRouter, and compatible services)"
					value={config.metadata.translation?.openai?.base_url ?? 'https://api.openai.com/v1'}
					placeholder="https://api.openai.com/v1"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						if (!config.metadata.translation.openai) config.metadata.translation.openai = {};
						config.metadata.translation.openai.base_url = val.trim();
					}}
				/>

				<div class="py-4 border-b border-border">
					<div class="flex items-center justify-between mb-2 gap-2">
						<label class="block text-sm font-medium" for="translation-openai-model-select">Model</label>
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
								{fetchingTranslationModels ? 'Fetching...' : 'Fetch Models'}
							{/snippet}
						</Button>
					</div>

					{#if translationModelOptions.length > 0}
						<select id="translation-openai-model-select" bind:value={config.metadata.translation.openai.model} class={inputClass}>
							{#each translationModelOptions as modelName}
								<option value={modelName}>{modelName}</option>
							{/each}
						</select>
						<p class="text-xs text-muted-foreground mt-1">
							Loaded from <code>{config.metadata.translation?.openai?.base_url}</code>. You can still edit manually below.
						</p>
					{/if}

					<input
						id="translation-openai-model-input"
						type="text"
						value={config.metadata.translation?.openai?.model ?? 'gpt-4o-mini'}
						oninput={(e) => {
							if (!config.metadata.translation) config.metadata.translation = {};
							if (!config.metadata.translation.openai) config.metadata.translation.openai = {};
							config.metadata.translation.openai.model = e.currentTarget.value.trim();
						}}
						class="{inputClass} mt-3"
						placeholder="gpt-4o-mini"
					/>
					<p class="text-xs text-muted-foreground mt-1">Manual model override.</p>
				</div>

				<FormPasswordInput
					label="API Key"
					description="API key for the configured OpenAI-compatible service"
					value={config.metadata.translation?.openai?.api_key ?? ''}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						if (!config.metadata.translation.openai) config.metadata.translation.openai = {};
						config.metadata.translation.openai.api_key = val;
					}}
				/>
			</fieldset>
		</SettingsSubsection>
	{:else if config.metadata.translation?.provider === 'deepl'}
		<SettingsSubsection title="DeepL Provider">
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<div class="py-4 border-b border-border">
					<label class="block text-sm font-medium mb-2" for="deepl-mode">Mode</label>
					<select id="deepl-mode" bind:value={config.metadata.translation.deepl.mode} class={inputClass}>
						<option value="free">Free API</option>
						<option value="pro">Pro API</option>
					</select>
					<p class="text-xs text-muted-foreground mt-1">
						Use <code>free</code> for DeepL API Free plan, or <code>pro</code> for paid DeepL API.
					</p>
				</div>

				<FormTextInput
					label="Base URL (optional)"
					description="Optional DeepL endpoint override (leave blank to use mode defaults)"
					value={config.metadata.translation?.deepl?.base_url ?? ''}
					placeholder="https://api-free.deepl.com"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						if (!config.metadata.translation.deepl) config.metadata.translation.deepl = {};
						config.metadata.translation.deepl.base_url = val.trim();
					}}
				/>

				<FormPasswordInput
					label="API Key"
					description="DeepL API key (required for both free and pro API modes)"
					value={config.metadata.translation?.deepl?.api_key ?? ''}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						if (!config.metadata.translation.deepl) config.metadata.translation.deepl = {};
						config.metadata.translation.deepl.api_key = val;
					}}
				/>

				<div class="py-4 border-b border-border">
					<div class="flex items-center justify-between mb-3">
						<div>
							<h4 class="text-sm font-medium">Usage</h4>
							<p class="text-xs text-muted-foreground">Current billing period character usage</p>
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
								{fetchingDeepLUsage ? 'Fetching...' : 'Refresh'}
							{/snippet}
						</Button>
					</div>

					{#if deeplUsageError}
						<p class="text-xs text-destructive mb-2">{deeplUsageError}</p>
					{/if}

					{#if deeplUsage}
						<div class="space-y-2">
							<div class="flex items-center justify-between text-sm">
								<span class="font-medium">Characters used</span>
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
								<span>{usagePercentage.toFixed(1)}% used</span>
								<span>{formatNumber(deeplUsage.character_limit - deeplUsage.character_count)} remaining</span>
							</div>
							{#if deeplUsage.start_time && deeplUsage.end_time}
								<p class="text-xs text-muted-foreground">
									Billing period: {new Date(deeplUsage.start_time).toLocaleDateString()} – {new Date(deeplUsage.end_time).toLocaleDateString()}
								</p>
							{/if}
						</div>
					{:else if !fetchingDeepLUsage && !deeplUsageError}
						<p class="text-xs text-muted-foreground">Click Refresh to load usage data</p>
					{/if}
				</div>
			</fieldset>
		</SettingsSubsection>
	{:else if config.metadata.translation?.provider === 'deepl'}
		<SettingsSubsection title="DeepL Provider">
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<div class="py-4 border-b border-border">
					<label class="block text-sm font-medium mb-2" for="deepl-mode">Mode</label>
					<select id="deepl-mode" bind:value={config.metadata.translation.deepl.mode} class={inputClass}>
						<option value="free">Free API</option>
						<option value="pro">Pro API</option>
					</select>
					<p class="text-xs text-muted-foreground mt-1">
						Use <code>free</code> for DeepL API Free plan, or <code>pro</code> for paid DeepL API.
					</p>
				</div>

				<FormTextInput
					label="Base URL (optional)"
					description="Optional DeepL endpoint override (leave blank to use mode defaults)"
					value={config.metadata.translation?.deepl?.base_url ?? ''}
					placeholder="https://api-free.deepl.com"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						if (!config.metadata.translation.deepl) config.metadata.translation.deepl = {};
						config.metadata.translation.deepl.base_url = val.trim();
					}}
				/>

				<FormPasswordInput
					label="API Key"
					description="DeepL API key (required for both free and pro API modes)"
					value={config.metadata.translation?.deepl?.api_key ?? ''}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						if (!config.metadata.translation.deepl) config.metadata.translation.deepl = {};
						config.metadata.translation.deepl.api_key = val;
					}}
				/>
			</fieldset>
		</SettingsSubsection>
	{:else if config.metadata.translation?.provider === 'google'}
		<SettingsSubsection title="Google Provider">
			<fieldset disabled={!translationEnabled} class={`space-y-0 ${!translationEnabled ? 'opacity-60' : ''}`}>
				<div class="py-4 border-b border-border">
					<label class="block text-sm font-medium mb-2" for="google-mode">Mode</label>
					<select id="google-mode" bind:value={config.metadata.translation.google.mode} class={inputClass}>
						<option value="free">Free (public endpoint)</option>
						<option value="paid">Paid (Cloud Translation API)</option>
					</select>
				</div>

				<FormTextInput
					label="Base URL (optional)"
					description="Optional Google Translate endpoint override"
					value={config.metadata.translation?.google?.base_url ?? ''}
					placeholder="https://translation.googleapis.com"
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						if (!config.metadata.translation.google) config.metadata.translation.google = {};
						config.metadata.translation.google.base_url = val.trim();
					}}
				/>

				<FormPasswordInput
					label="API Key"
					description="Required only for paid mode"
					value={config.metadata.translation?.google?.api_key ?? ''}
					disabled={config.metadata.translation?.google?.mode !== 'paid'}
					onchange={(val) => {
						if (!config.metadata.translation) config.metadata.translation = {};
						if (!config.metadata.translation.google) config.metadata.translation.google = {};
						config.metadata.translation.google.api_key = val;
					}}
				/>
			</fieldset>
		</SettingsSubsection>
	{/if}
</SettingsSection>
