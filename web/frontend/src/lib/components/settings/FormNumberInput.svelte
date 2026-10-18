<script lang="ts">
	import { generateUUID } from '$lib/utils/uuid';

	interface Props {
		label: string;
		description?: string;
		value: number;
		min?: number;
		max?: number;
		step?: number;
		unit?: string;
		onchange: (value: number) => void;
		disabled?: boolean;
		id?: string;
	}

	let {
		label,
		description,
		value = $bindable(0),
		min,
		max,
		step = 1,
		unit,
		onchange,
		disabled = false,
		id = `number-${generateUUID()}`
	}: Props = $props();

	function handleInput(event: Event) {
		const target = event.target as HTMLInputElement;
		// Expand exponent notation before truncating to the existing integer callback contract.
		// Keep non-exponent parsing unchanged, including leading-dot decimal handling.
		const numValue = /e/i.test(target.value)
			? Math.trunc(target.valueAsNumber)
			: parseInt(target.value, 10);
		if (!isNaN(numValue)) {
			onchange(numValue);
		}
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
	</div>
	<div class="form-control flex items-center gap-2">
		<input
			type="number"
			{id}
			bind:value
			oninput={handleInput}
			{min}
			{max}
			{step}
			{disabled}
			aria-describedby={description ? `${id}-desc` : undefined}
			class="w-32 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm disabled:opacity-50"
		/>
		{#if unit}
			<span class="text-sm text-muted-foreground">{unit}</span>
		{/if}
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
