<script lang="ts">
	import { onMount } from 'svelte';
	import { createConfigQuery } from '$lib/query/queries';
	import { reconcileWithConfig } from '$lib/i18n/locale';
	import type { UIConfig } from '$lib/api/types';

	interface Props {
		getAuthenticated: () => boolean;
	}

	let { getAuthenticated }: Props = $props();

	const configQuery = createConfigQuery(getAuthenticated);

	$effect(() => {
		const ui: UIConfig | null | undefined = configQuery.data?.ui ?? null;
		if (ui) {
			void reconcileWithConfig(ui);
		}
	});
</script>
