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
		layout?: 'row' | 'stacked';
		tags?: string[];
		clickableTags?: boolean;
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
		layout = 'row',
		tags,
		clickableTags = true,
		id = `template-${generateUUID()}`
	}: Props = $props();

	const DEFAULT_TEMPLATE_TAGS = [
		'<ID>',
		'<TITLE>',
		'<ORIGINALTITLE>',
		'<STUDIO>',
		'<MAKER>',
		'<LABEL>',
		'<SERIES>',
		'<SET>',
		'<DIRECTOR>',
		'<YEAR>',
		'<RELEASEDATE>',
		'<RUNTIME>',
		'<RATING>',
		'<ACTORS>',
		'<ACTRESS>',
		'<GENRES>',
		'<RESOLUTION>'
	];

	const effectiveTags = $derived(tags ?? DEFAULT_TEMPLATE_TAGS);

	let showTags = $state(false);
	let inputEl: HTMLInputElement | undefined = $state();
	let hasSelection = $state(false);

	function handleInput(event: Event) {
		const target = event.target as HTMLInputElement;
		onchange(target.value);
	}

	function toggleTags() {
		showTags = !showTags;
	}

	function insertTag(tag: string) {
		const input = inputEl;
		if (!input) {
			const fallback = value + tag;
			value = fallback;
			onchange(fallback);
			return;
		}
		const pos = hasSelection ? (input.selectionStart ?? value.length) : value.length;
		const end = hasSelection ? (input.selectionEnd ?? pos) : pos;
		const next = value.slice(0, pos) + tag + value.slice(end);
		value = next;
		onchange(next);
		const caret = pos + tag.length;
		hasSelection = true;
		queueMicrotask(() => {
			input.focus();
			input.setSelectionRange(caret, caret);
		});
	}
</script>

{#snippet tagPicker()}
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
					{#each effectiveTags as tag (tag)}
						{#if clickableTags}
							<button
								type="button"
								onclick={() => insertTag(tag)}
								class="text-xs bg-background px-2 py-1 rounded border border-border hover:border-primary hover:text-primary transition-colors cursor-pointer font-mono"
							>{tag}</button>
						{:else}
							<code class="text-xs bg-background px-2 py-1 rounded border border-border font-mono">{tag}</code>
						{/if}
					{/each}
				</div>
			</div>
		{/if}
	{/if}
{/snippet}

{#if layout === 'stacked'}
	<div>
		<label for={id} class="block text-sm font-medium text-foreground mb-2">
			{label}
		</label>
		<input
			bind:this={inputEl}
			type="text"
			{id}
			bind:value
			oninput={handleInput}
			onfocus={() => (hasSelection = true)}
			{placeholder}
			{disabled}
			aria-describedby={description ? `${id}-desc` : undefined}
			class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm font-mono disabled:opacity-50"
		/>
		{#if description}
			<p class="text-xs text-muted-foreground mt-1" id="{id}-desc">{description}</p>
		{/if}
		{@render tagPicker()}
	</div>
{:else}
	<div class="form-row py-4 border-b border-border last:border-0">
		<div class="form-label flex-1">
			<label for={id} class="text-sm font-medium text-foreground">
				{label}
			</label>
			{#if description}
				<p class="text-sm text-muted-foreground mt-1" id="{id}-desc">{description}</p>
			{/if}
			{@render tagPicker()}
		</div>
		<div class="form-control flex-1">
			<input
				bind:this={inputEl}
				type="text"
				{id}
				bind:value
				oninput={handleInput}
				onfocus={() => (hasSelection = true)}
				{placeholder}
				{disabled}
				aria-describedby={description ? `${id}-desc` : undefined}
				class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm font-mono disabled:opacity-50"
			/>
		</div>
	</div>
{/if}

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