<script lang="ts">
	import { cn } from '$lib/utils';

	interface Props {
		class?: string;
		children?: import('svelte').Snippet;
		[key: string]: unknown;
	}

	let { class: className, children, ...rest }: Props = $props();
</script>

<div class={cn('rounded-lg border bg-card text-card-foreground shadow-sm', className)} {...rest}>
	{@render children?.()}
</div>
