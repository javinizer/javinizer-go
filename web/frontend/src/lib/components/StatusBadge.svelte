<script lang="ts">
	import { CheckCircle2, CircleX, Undo2, LoaderCircle, AlertTriangle } from 'lucide-svelte';

	interface Props {
		status: 'success' | 'failed' | 'reverted' | 'running' | 'organized' | 'cancelled' | 'partially-reverted';
		size?: 'sm' | 'default';
	}

	let { status, size = 'default' }: Props = $props();

	const sizeClass = $derived(size === 'sm' ? 'text-xs' : 'text-xs');

	const config = $derived.by(() => {
		switch (status) {
			case 'success':
				return {
					icon: CheckCircle2,
					bgClass: 'bg-green-500/10 dark:bg-green-500/10',
					textClass: 'text-green-500 dark:text-green-400',
					label: 'Success'
				};
			case 'organized':
				return {
					icon: CheckCircle2,
					bgClass: 'bg-purple-500/10 dark:bg-purple-500/10',
					textClass: 'text-purple-500 dark:text-purple-400',
					label: 'Organized'
				};
			case 'failed':
				return {
					icon: CircleX,
					bgClass: 'bg-red-500/10 dark:bg-red-500/10',
					textClass: 'text-red-500 dark:text-red-400',
					label: 'Failed'
				};
			case 'reverted':
				return {
					icon: Undo2,
					bgClass: 'bg-yellow-500/10 dark:bg-yellow-500/10',
					textClass: 'text-yellow-500 dark:text-yellow-400',
					label: 'Reverted'
				};
			case 'running':
				return {
					icon: LoaderCircle,
					bgClass: 'bg-blue-500/10 dark:bg-blue-500/10',
					textClass: 'text-blue-500 dark:text-blue-400',
					label: 'Running'
				};
			case 'cancelled':
				return {
					icon: CircleX,
					bgClass: 'bg-gray-500/10 dark:bg-gray-500/10',
					textClass: 'text-gray-400 dark:text-gray-400',
					label: 'Cancelled'
				};
			case 'partially-reverted':
				return {
					icon: AlertTriangle,
					bgClass: 'bg-orange-500/10 dark:bg-orange-500/10',
					textClass: 'text-orange-500 dark:text-orange-400',
					label: 'Partial'
				};
			default:
				return {
					icon: CircleX,
					bgClass: 'bg-gray-500/10 dark:bg-gray-500/10',
					textClass: 'text-gray-400 dark:text-gray-400',
					label: status
				};
		}
	});
</script>

<span
	class="inline-flex items-center gap-1 px-1.5 py-0.5 rounded {sizeClass} font-medium {config.bgClass} {config.textClass}"
	aria-label="{config.label}"
>
	{#if status === 'running'}
		<config.icon class="h-3 w-3 animate-spin" />
	{:else}
		<config.icon class="h-3 w-3" />
	{/if}
	{config.label}
</span>
