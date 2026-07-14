<script lang="ts">
	import { generateUUID } from '$lib/utils/uuid';
	import * as m from '$lib/paraglide/messages';

	interface Props {
		label: string;
		description?: string;
		value: string;
		placeholder?: string;
		onchange: (value: string) => void;
		disabled?: boolean;
		showTagList?: boolean;
		id?: string;
	}

	let {
		label,
		description,
		value = $bindable(''),
		placeholder,
		onchange,
		disabled = false,
		showTagList = false,
		id = `template-${generateUUID()}`
	}: Props = $props();

	const TEMPLATE_TAGS = [
		'<ID>',
		'<TITLE>',
		'<STUDIO>',
		'<YEAR>',
		'<ACTORS>',
		'<DIRECTOR>',
		'<MAKER>',
		'<LABEL>',
		'<SET>',
		'<RELEASEDATE>',
		'<RUNTIME>',
		'<RESOLUTION>'
	];

	let showTags = $state(false);

	function handleInput(event: Event) {
		const target = event.target as HTMLInputElement;
		onchange(target.value);
	}

	function toggleTags() {
		showTags = !showTags;
	}
</script>

<div class="form-row py-4 border-b border-border last:border-0">
	<div class="form-label flex-1">
		<label for={id} class="text-sm font-medium text-foreground">
			{label}
		</label>
		{#if description}
			<p class="text-sm text-muted-foreground mt-1" id="{id}-desc">{description}</p>
		{/if}
		{#if showTagList}
			<button
				type="button"
				onclick={toggleTags}
				class="text-xs text-primary hover:underline mt-2"
			>
				{showTags ? m.form_hide_available_tags() : m.form_show_available_tags()}
			</button>
			{#if showTags}
				<div class="mt-2 p-3 bg-accent/50 rounded-md">
					<p class="text-xs font-medium text-foreground mb-2">{m.form_available_template_tags()}</p>
					<div class="flex flex-wrap gap-2">
						{#each TEMPLATE_TAGS as tag}
							<code class="text-xs bg-background px-2 py-1 rounded border border-border">{tag}</code>
						{/each}
					</div>
				</div>
			{/if}
		{/if}
	</div>
	<div class="form-control flex-1">
		<input
			type="text"
			{id}
			bind:value
			oninput={handleInput}
			{placeholder}
			{disabled}
			aria-describedby={description ? `${id}-desc` : undefined}
			class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm font-mono disabled:opacity-50"
		/>
	</div>
</div>

<style>
	.form-row {
		display: flex;
		align-items: start;
		gap: 1rem;
	}

	@media (max-width: 768px) {
		.form-row {
			flex-direction: column;
		}
	}
</style>
