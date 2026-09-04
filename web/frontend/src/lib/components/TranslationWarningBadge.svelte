<script lang="ts">
	import { Languages } from 'lucide-svelte';
	import type { TranslationWarningCode } from '$lib/api/types';
	import { translationWarningMessage } from '$lib/utils/translation-warning';

	interface Props {
		/** Machine-readable warning code from the backend (present on Slim payloads too). */
		code?: TranslationWarningCode | string;
		/** Raw warning string (full results only); preferred for rate_limited, fallback for unknown/missing codes. */
		warning?: string;
		/**
		 * Compact renders an icon-only chip (grid cards / mid-run badges keyed off
		 * the code alone). The default block renders the full message inline
		 * (single-scrape/rescrape results and the review detail panel).
		 */
		compact?: boolean;
	}

	let { code, warning, compact = false }: Props = $props();

	const message = $derived(translationWarningMessage(code, warning));
</script>

{#if message}
	{#if compact}
		<span
			class="text-warning bg-warning/15 text-xs font-medium px-1.5 py-0.5 rounded-full inline-flex items-center gap-1"
			title={message}
			aria-label={message}
		>
			<Languages class="h-3 w-3" />
		</span>
	{:else}
		<div
			class="flex items-start gap-2 rounded-md border border-warning/50 bg-warning/10 px-3 py-2 text-sm"
			role="alert"
		>
			<Languages class="h-4 w-4 text-warning shrink-0 mt-0.5" />
			<p class="text-warning">{message}</p>
		</div>
	{/if}
{/if}
