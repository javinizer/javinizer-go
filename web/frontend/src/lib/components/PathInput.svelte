<script lang="ts">
	import { apiClient } from '$lib/api/client';
	import type { PathAutocompleteSuggestion } from '$lib/api/types';
	import { Folder, ArrowRight, LoaderCircle } from 'lucide-svelte';
	import Button from './ui/Button.svelte';

	interface Props {
		value?: string;
		onchange?: (value: string) => void;
		placeholder?: string;
		whitelistPaths?: string[];
		showNavigateButton?: boolean;
		onnavigate?: (path: string) => void;
		navigateDisabled?: boolean;
		loading?: boolean;
		escapeValue?: string;
		class?: string;
	}

	let {
		value = $bindable(''),
		onchange,
		placeholder = 'Enter path (e.g., /path/to/videos)',
		whitelistPaths = [],
		showNavigateButton = false,
		onnavigate,
		navigateDisabled = false,
		loading = false,
		escapeValue,
		class: className = ''
	}: Props = $props();

	let isEditing = $state(false);
	let pathSuggestions = $state<PathAutocompleteSuggestion[]>([]);
	let activeSuggestionIndex = $state(-1);
	let userNavigatedSuggestions = $state(false);
	let autocompleteLoading = $state(false);
	let autocompleteDebounceId: ReturnType<typeof setTimeout> | null = null;
	let autocompleteRequestToken = 0;

	const pathAutocompleteLimit = 8;

	let whitelistSuggestions = $derived.by(() => {
		if (whitelistPaths.length === 0) return [];
		return whitelistPaths.map((p) => ({
			name: p.split(/[\\/]/).pop() || p,
			path: p,
			is_dir: true
		} as PathAutocompleteSuggestion));
	});

	let displayedSuggestions = $derived.by(() => {
		if (pathSuggestions.length > 0) return pathSuggestions;
		if (!isEditing) return [];
		const input = value.trim().toLowerCase();
		if (input === '' && whitelistSuggestions.length > 0) return whitelistSuggestions;
		if (input !== '' && whitelistSuggestions.length > 0) {
			const filtered = whitelistSuggestions.filter((s) =>
				s.path.toLowerCase().includes(input) || s.name.toLowerCase().includes(input)
			);
			if (filtered.length > 0) return filtered;
		}
		return [];
	});

	let showSuggestions = $derived(displayedSuggestions.length > 0 && isEditing);

	function clearSuggestions() {
		autocompleteRequestToken += 1;
		pathSuggestions = [];
		activeSuggestionIndex = -1;
		userNavigatedSuggestions = false;
		autocompleteLoading = false;
	}

	function selectSuggestion(suggestion: PathAutocompleteSuggestion) {
		value = suggestion.path;
		isEditing = false;
		clearSuggestions();
		onchange?.(suggestion.path);
		onnavigate?.(suggestion.path);
	}

	async function fetchSuggestions(inputPath: string) {
		const requestToken = ++autocompleteRequestToken;
		autocompleteLoading = true;

		try {
			const response = await apiClient.autocompletePath({
				path: inputPath,
				limit: pathAutocompleteLimit
			});

			if (requestToken !== autocompleteRequestToken || !isEditing) return;

			pathSuggestions = response.suggestions;
			activeSuggestionIndex = -1;
		} catch {
			if (requestToken !== autocompleteRequestToken) return;
			pathSuggestions = [];
			activeSuggestionIndex = -1;
		} finally {
			if (requestToken === autocompleteRequestToken) {
				autocompleteLoading = false;
			}
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		if (e.key === 'ArrowDown' && displayedSuggestions.length > 0) {
			e.preventDefault();
			userNavigatedSuggestions = true;
			activeSuggestionIndex =
				activeSuggestionIndex >= displayedSuggestions.length - 1 ? 0 : activeSuggestionIndex + 1;
		} else if (e.key === 'ArrowUp' && displayedSuggestions.length > 0) {
			e.preventDefault();
			userNavigatedSuggestions = true;
			activeSuggestionIndex =
				activeSuggestionIndex <= 0 ? displayedSuggestions.length - 1 : activeSuggestionIndex - 1;
		} else if (e.key === 'Enter') {
			if (
				userNavigatedSuggestions &&
				showSuggestions &&
				activeSuggestionIndex >= 0 &&
				displayedSuggestions[activeSuggestionIndex]
			) {
				e.preventDefault();
				selectSuggestion(displayedSuggestions[activeSuggestionIndex]);
				return;
			}
			onnavigate?.(value.trim());
		} else if (e.key === 'Escape') {
			if (escapeValue !== undefined) {
				value = escapeValue;
			}
			isEditing = false;
			clearSuggestions();
		}
	}

	function handleInput() {
		userNavigatedSuggestions = false;
		activeSuggestionIndex = -1;
		onchange?.(value);
		const inputPath = value.trim();

		if (autocompleteDebounceId) {
			clearTimeout(autocompleteDebounceId);
			autocompleteDebounceId = null;
		}

		if (!isEditing || !inputPath) {
			clearSuggestions();
			return;
		}

		autocompleteDebounceId = setTimeout(() => {
			void fetchSuggestions(inputPath);
		}, 160);
	}

	function handleFocus() {
		isEditing = true;
	}
</script>

<div class="relative flex-1">
	<input
		type="text"
		bind:value
		onkeydown={handleKeydown}
		oninput={handleInput}
		onfocus={handleFocus}
		onblur={() => {
			setTimeout(() => {
				isEditing = false;
			}, 120);
		}}
		{placeholder}
		class="w-full px-3 py-1.5 pr-9 border rounded-md bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm {className}"
	/>
	{#if autocompleteLoading}
		<div class="absolute inset-y-0 right-3 flex items-center text-muted-foreground">
			<LoaderCircle class="h-3.5 w-3.5 animate-spin" />
		</div>
	{/if}

	{#if showSuggestions}
		<div class="absolute z-20 mt-2 w-full rounded-lg border bg-background shadow-lg overflow-hidden">
			<div class="px-3 py-1.5 text-xs text-muted-foreground border-b border-border/60 bg-muted/30">
				Type a path, or use ↑↓ to pick a suggestion
			</div>
			<div class="max-h-64 overflow-y-auto py-1">
				{#each displayedSuggestions as suggestion, index (suggestion.path)}
					<button
						type="button"
						onmousedown={(event) => {
							event.preventDefault();
							selectSuggestion(suggestion);
						}}
						class="w-full flex items-center gap-2 px-3 py-2 text-left text-sm transition-colors
							{index === activeSuggestionIndex ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/60'}"
					>
						<Folder class="h-4 w-4 text-blue-500 shrink-0" />
						<div class="min-w-0 flex-1">
							<div class="truncate font-medium">{suggestion.name}</div>
							<div class="truncate text-xs text-muted-foreground font-mono">{suggestion.path}</div>
						</div>
					</button>
				{/each}
			</div>
		</div>
	{/if}
</div>

{#if showNavigateButton}
	<Button variant="outline" size="sm" onclick={() => onnavigate?.(value.trim())} disabled={!value.trim() || navigateDisabled || loading} title="Navigate to path">
		{#snippet children()}
			<ArrowRight class="h-4 w-4" />
		{/snippet}
	</Button>
{/if}
