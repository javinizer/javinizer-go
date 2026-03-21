<script lang="ts">
	import { onMount } from 'svelte';
	import { cubicOut } from 'svelte/easing';
	import { fly } from 'svelte/transition';
	import { CircleCheckBig, X, CircleAlert, Info, CircleX } from 'lucide-svelte';

	interface Props {
		id: string;
		type?: 'success' | 'error' | 'info' | 'warning';
		message: string;
		duration?: number;
		onDismiss: (id: string) => void;
	}

	let { id, type = 'info', message, duration = 5000, onDismiss }: Props = $props();

	let progress = $state(100);
	let interval: ReturnType<typeof setInterval> | null = null;

	const icons = {
		success: CircleCheckBig,
		error: CircleX,
		info: Info,
		warning: CircleAlert
	};

	const styles = {
		success: 'bg-green-50 border-green-200 text-green-800',
		error: 'bg-red-50 border-red-200 text-red-800',
		info: 'bg-blue-50 border-blue-200 text-blue-800',
		warning: 'bg-yellow-50 border-yellow-200 text-yellow-800'
	};

	const iconStyles = {
		success: 'text-green-500',
		error: 'text-red-500',
		info: 'text-blue-500',
		warning: 'text-yellow-500'
	};

	const Icon = $derived(icons[type]);

	onMount(() => {
		if (duration > 0) {
			const step = 100 / (duration / 50);
			interval = setInterval(() => {
				progress -= step;
				if (progress <= 0) {
					if (interval) clearInterval(interval);
					onDismiss(id);
				}
			}, 50);
		}

		return () => {
			if (interval) clearInterval(interval);
		};
	});

	function handleDismiss() {
		if (interval) clearInterval(interval);
		onDismiss(id);
	}
</script>

<div
	in:fly|local={{ x: 32, duration: 260, easing: cubicOut }}
	out:fly|local={{ x: 32, duration: 180, easing: cubicOut }}
	class="flex items-start gap-3 p-4 rounded-lg border shadow-lg min-w-[300px] max-w-md {styles[
		type
	]}"
	role="alert"
>
	<Icon class="h-5 w-5 shrink-0 mt-0.5 {iconStyles[type]}" />
	<div class="flex-1 min-w-0">
		<p class="text-sm font-medium">{message}</p>
		{#if duration > 0}
			<div class="mt-2 h-1 bg-white/30 rounded-full overflow-hidden">
				<div
					class="h-full bg-current transition-all duration-50 ease-linear"
					style="width: {progress}%"
				></div>
			</div>
		{/if}
	</div>
	<button
		onclick={handleDismiss}
		class="shrink-0 p-1 hover:bg-white/20 rounded transition-colors"
		aria-label="Dismiss"
	>
		<X class="h-4 w-4" />
	</button>
</div>
