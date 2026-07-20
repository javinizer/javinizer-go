<script lang="ts">
	import { browser } from '$app/environment';
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

	let selected = $state(currentSelection());

	async function handleChange(event: Event) {
		const value = (event.target as HTMLSelectElement).value;
		selected = value;
		await selectLocale(value);
	}
</script>

<select
	class="rounded-md border bg-background px-3 py-2 text-sm text-foreground shadow-sm"
	aria-label={m.settings_language()}
	value={selected}
	onchange={(e) => void handleChange(e)}
>
	{#each SUPPORTED_LOCALES as locale (locale.tag)}
		<option value={locale.tag}>{locale.selfName}</option>
	{/each}
</select>
