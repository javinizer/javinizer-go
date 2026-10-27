<script lang="ts">
	import * as m from '$lib/paraglide/messages';

	let {
		checked = $bindable(false),
		disabled = false,
	}: { checked?: boolean; disabled?: boolean } = $props();
</script>

<label class="flex cursor-pointer items-start gap-3 rounded-md border border-border bg-background p-3">
	<input
		type="checkbox"
		bind:checked
		{disabled}
		class="mt-1 h-4 w-4 shrink-0 rounded border-input text-primary focus:ring-2 focus:ring-primary"
	/>
	<span>
		<span class="block text-sm font-medium">{m.offline_evidence_opt_in()}</span>
		<span class="mt-1 block text-xs text-muted-foreground">{m.offline_evidence_opt_in_hint()}</span>
	</span>
</label>
