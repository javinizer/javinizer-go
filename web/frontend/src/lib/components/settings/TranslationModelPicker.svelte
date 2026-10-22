<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import { RefreshCw } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';

	interface Props {
		selectId: string;
		inputId: string;
		inputClass: string;
		selectClass: string;
		model: string | undefined;
		defaultModel: string;
		placeholder: string;
		baseUrl: string;
		models: string[];
		fetching: boolean;
		fetchDisabled: boolean;
		onfetch: () => Promise<void>;
		onselect: (value: string) => void;
		oninput: (value: string) => void;
	}

	let {
		selectId, inputId, inputClass, selectClass, model, defaultModel, placeholder,
		baseUrl, models, fetching, fetchDisabled, onfetch, onselect, oninput
	}: Props = $props();
</script>

<div class="py-4 border-b border-border">
	<div class="flex items-center justify-between mb-2 gap-2">
		<label class="block text-sm font-medium" for={selectId}>{m.settings_translation_model_label()}</label>
		<Button variant="outline" size="sm" onclick={onfetch} disabled={fetchDisabled}>
			{#snippet children()}
				<RefreshCw class={`h-4 w-4 mr-2 ${fetching ? 'animate-spin' : ''}`} />
				{fetching ? m.settings_translation_fetching() : m.settings_translation_fetch_models()}
			{/snippet}
		</Button>
	</div>

	{#if models.length > 0}
		<!-- An unset model must stay undefined at runtime so native binding selects the first option. -->
		<select id={selectId} bind:value={() => model as string, onselect} class={selectClass}>
			{#each models as modelName}
				<option value={modelName}>{modelName}</option>
			{/each}
		</select>
		<p class="text-xs text-muted-foreground mt-1">
			{m.settings_translation_models_loaded({ url: baseUrl })}
		</p>
	{/if}

	<input
		id={inputId}
		type="text"
		value={model ?? defaultModel}
		oninput={(event) => oninput(event.currentTarget.value)}
		class="{inputClass} mt-3"
		{placeholder}
	/>
	<p class="text-xs text-muted-foreground mt-1">{m.settings_translation_manual_override()}</p>
</div>
