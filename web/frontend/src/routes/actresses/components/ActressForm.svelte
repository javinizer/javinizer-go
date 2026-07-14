<script lang="ts">
	import { fade } from 'svelte/transition';
	import { Plus, Pencil, Save } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import * as m from '$lib/paraglide/messages';

	type ActressForm = {
		dmm_id: string;
		first_name: string;
		last_name: string;
		japanese_name: string;
		thumb_url: string;
		aliases: string;
	};

	let {
		editingId,
		form = $bindable(),
		formError,
		isPending,
		onSave,
		onReset
	}: {
		editingId: number | null;
		form: ActressForm;
		formError: string | null;
		isPending: boolean;
		onSave: () => void;
		onReset: () => void;
	} = $props();
</script>

<div in:fade|local={{ duration: 220 }}>
	<Card
		class={`p-5 space-y-4 transition-colors ${
			editingId
				? 'border-primary/40 bg-primary/5'
				: 'border-dashed border-input/70 bg-card'
		}`}
	>
		<div class="flex items-center justify-between gap-2">
			<h2 class="text-lg font-semibold flex items-center gap-2">
				{#if editingId}
					<Pencil class="h-5 w-5 text-primary" />
					{m.actresses_edit_actress()}
				{:else}
					<Plus class="h-5 w-5 text-muted-foreground" />
					{m.actresses_create_actress()}
				{/if}
			</h2>
			<span
				class={`text-xs font-medium rounded-full px-2.5 py-1 ${
					editingId ? 'bg-primary/15 text-primary' : 'bg-muted text-muted-foreground'
				}`}
			>
				{editingId ? m.actresses_edit_mode() : m.actresses_create_mode()}
			</span>
		</div>
		<p class={`text-sm ${editingId ? 'text-primary/90' : 'text-muted-foreground'}`}>
			{editingId
				? m.actresses_editing_existing()
				: m.actresses_fill_new()}
		</p>

		{#if formError}
			<div class="rounded-md border border-destructive bg-destructive/10 p-3 text-sm text-destructive">
				{formError}
			</div>
		{/if}

		<div class="space-y-3">
			<div>
				<label class="text-sm font-medium" for="dmm-id">{m.actresses_dmm_id()}</label>
				<input
					id="dmm-id"
					type="number"
					min="0"
					bind:value={form.dmm_id}
					placeholder={m.actresses_dmm_id_placeholder()}
					class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
				/>
				<p class="mt-1 text-xs text-muted-foreground">{m.actresses_dmm_id_hint()}</p>
			</div>

			<div class="grid grid-cols-2 gap-3">
				<div>
					<label class="text-sm font-medium" for="first-name">{m.actresses_first_name()}</label>
					<input
						id="first-name"
						type="text"
						bind:value={form.first_name}
						placeholder={m.actresses_first_name_placeholder()}
						class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
					/>
				</div>
				<div>
					<label class="text-sm font-medium" for="last-name">{m.actresses_last_name()}</label>
					<input
						id="last-name"
						type="text"
						bind:value={form.last_name}
						placeholder={m.actresses_last_name_placeholder()}
						class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
					/>
				</div>
			</div>

			<div>
				<label class="text-sm font-medium" for="ja-name">{m.actresses_japanese_name()}</label>
				<input
					id="ja-name"
					type="text"
					bind:value={form.japanese_name}
					placeholder={m.actresses_japanese_name_placeholder()}
					class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
				/>
			</div>

			<div>
				<label class="text-sm font-medium" for="thumb-url">{m.actresses_thumb_url()}</label>
				<input
					id="thumb-url"
					type="url"
					bind:value={form.thumb_url}
					placeholder={m.actresses_thumb_url_placeholder()}
					class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
				/>
			</div>

			<div>
				<label class="text-sm font-medium" for="aliases">{m.actresses_aliases_label()}</label>
				<input
					id="aliases"
					type="text"
					bind:value={form.aliases}
					placeholder={m.actresses_aliases_placeholder()}
					class="mt-1 w-full rounded-md border border-input bg-background px-3 py-2 text-sm"
				/>
			</div>
		</div>

		<div class="flex items-center gap-2 pt-2">
			<Button onclick={onSave} disabled={isPending}>
				<Save class="h-4 w-4" />
				{isPending ? m.common_saving() : (editingId ? m.actresses_update() : m.actresses_create())}
			</Button>
			<Button variant="outline" onclick={onReset} disabled={isPending}>{m.common_clear()}</Button>
		</div>
	</Card>
</div>
