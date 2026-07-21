<script lang="ts">
	import { browser } from '$app/environment';
	import { onMount } from 'svelte';
	import { fly } from 'svelte/transition';
	import { cubicOut } from 'svelte/easing';
	import { Languages, ChevronDown } from 'lucide-svelte';
	import { LOCALE_STORAGE_KEY, SUPPORTED_LOCALES, selectLocale } from '$lib/i18n/locale';
	import * as m from '$lib/paraglide/messages';

	// Pre-auth there is no config access, so mirror the cached explicit pick
	// and fall back to 'auto' (browser preference). Post-auth the configured
	// ui.language wins again via reconcileWithConfig.
	function currentSelection(): string {
		if (!browser) return 'auto';
		const cached = localStorage.getItem(LOCALE_STORAGE_KEY);
		if (cached && SUPPORTED_LOCALES.some((l) => l.tag === cached)) return cached;
		return 'auto';
	}

	// SSR/hydration renders 'auto' everywhere; the cached pick is applied
	// post-mount to avoid a hydration value mismatch on the <select>.
	let selected = $state('auto');
	onMount(() => {
		selected = currentSelection();
	});

	async function handleChange(event: Event) {
		const value = (event.target as HTMLSelectElement).value;
		selected = value;
		await selectLocale(value);
	}
</script>

<div
	class="group relative inline-flex items-center"
	in:fly={{ y: -12, duration: 450, delay: 250, easing: cubicOut }}
>
	<Languages
		class="pointer-events-none absolute left-3 h-4 w-4 text-muted-foreground transition-colors group-hover:text-foreground"
		strokeWidth={1.8}
		aria-hidden="true"
	/>
	<select
		class="cursor-pointer appearance-none rounded-full border border-border bg-card/95 py-2 pl-9 pr-9 text-sm font-medium text-foreground shadow-sm backdrop-blur transition-all hover:border-foreground/25 hover:shadow-md focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-ring"
		aria-label={m.settings_language()}
		value={selected}
		onchange={(e) => void handleChange(e)}
	>
		{#each SUPPORTED_LOCALES as locale (locale.tag)}
			<option value={locale.tag}>{locale.selfName}</option>
		{/each}
	</select>
	<ChevronDown
		class="pointer-events-none absolute right-3 h-4 w-4 text-muted-foreground transition-transform duration-200 group-focus-within:rotate-180"
		strokeWidth={2}
		aria-hidden="true"
	/>
</div>
