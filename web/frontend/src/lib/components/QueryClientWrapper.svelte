<script lang="ts">
	import { QueryClient, QueryClientProvider } from '@tanstack/svelte-query';
	let { client, children }: { client: QueryClient; children: import('svelte').Snippet } = $props();
</script>

<QueryClientProvider client={client}>
	{@render children()}
</QueryClientProvider>
