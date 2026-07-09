<script lang="ts">
	import { apiClient } from '$lib/api/client';
	import type { PathAutocompleteSuggestion } from '$lib/api/types';
	import { Folder, ChevronRight, LoaderCircle, CornerDownLeft, ArrowUp, ArrowDown } from 'lucide-svelte';
	import Button from './ui/Button.svelte';
	import { portalToBody } from '$lib/actions/portal';

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
		drillOnSelect?: boolean;
		scope?: 'operation' | 'configure';
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
		drillOnSelect = false,
		scope = 'operation',
		class: className = ''
	}: Props = $props();

	const pathAutocompleteLimit = 8;
	const SEP = '/';

	let focused = $state(false);
	let pathSuggestions = $state<PathAutocompleteSuggestion[]>([]);
	let activeIndex = $state(-1);
	let userNavigated = $state(false);
	let autocompleteLoading = $state(false);
	let autocompleteDebounceId: ReturnType<typeof setTimeout> | null = null;
	let autocompleteRequestToken = 0;
	let blurTimeoutId: ReturnType<typeof setTimeout> | null = null;
	let inputEl = $state<HTMLInputElement | null>(null);
	let autocompleteError = $state<string | null>(null);

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
		if (!focused) return [];
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

	let showSuggestions = $derived(focused && displayedSuggestions.length > 0);

	function currentFragment(): string {
		const trimmed = value.trim();
		const idx = Math.max(trimmed.lastIndexOf(SEP), trimmed.lastIndexOf('\\'));
		if (idx === -1) return trimmed;
		return trimmed.slice(idx + 1);
	}

	function clearSuggestions() {
		autocompleteRequestToken += 1;
		pathSuggestions = [];
		activeIndex = -1;
		userNavigated = false;
		autocompleteLoading = false;
		autocompleteError = null;
	}

	function clampActive() {
		const n = displayedSuggestions.length;
		if (n === 0) {
			activeIndex = -1;
			return;
		}
		if (activeIndex < 0) activeIndex = 0;
		if (activeIndex >= n) activeIndex = n - 1;
	}

	async function fetchSuggestions(inputPath: string) {
		const requestToken = ++autocompleteRequestToken;
		autocompleteLoading = true;
		try {
			const response = await apiClient.autocompletePath({
				path: inputPath,
				limit: pathAutocompleteLimit,
				scope
			});
			if (requestToken !== autocompleteRequestToken || !focused) return;
			pathSuggestions = response.suggestions;
			activeIndex = -1;
			autocompleteError = null;
		} catch (e) {
			if (requestToken !== autocompleteRequestToken) return;
			pathSuggestions = [];
			activeIndex = -1;
			const msg = e instanceof Error ? e.message : '';
			if (scope === 'operation' && (msg.includes('403') || msg.includes('allowed directories'))) {
				if (msg.includes('no allowed directories configured')) {
					autocompleteError = 'No allowed directories configured — add one in Settings → Security.';
				} else if (msg.includes('outside allowed directories') || msg.includes('path outside')) {
					autocompleteError = 'This path is outside your allowed directories. Add it in Settings → Security.';
				} else {
					autocompleteError = null;
				}
			} else {
				autocompleteError = null;
			}
		} finally {
			if (requestToken === autocompleteRequestToken) autocompleteLoading = false;
		}
	}

	function withTrailingSep(p: string): string {
		if (p === '' || p.endsWith(SEP) || p.endsWith('\\')) return p;
		const sep = p.includes('\\') && !p.includes(SEP) ? '\\' : SEP;
		return p + sep;
	}

	function selectSuggestion(suggestion: PathAutocompleteSuggestion) {
		const drill = drillOnSelect;
		const next = drill ? withTrailingSep(suggestion.path) : suggestion.path;
		value = next;
		onchange?.(next);
		onnavigate?.(next);
		userNavigated = false;
		pathSuggestions = [];
		activeIndex = -1;
		autocompleteRequestToken += 1;
		if (autocompleteDebounceId) {
			clearTimeout(autocompleteDebounceId);
			autocompleteDebounceId = null;
		}
		if (blurTimeoutId) {
			clearTimeout(blurTimeoutId);
			blurTimeoutId = null;
		}
		if (inputEl) {
			inputEl.focus();
			const end = value.length;
			requestAnimationFrame(() => {
				inputEl?.setSelectionRange(end, end);
			});
		}
		if (drill) {
			void fetchSuggestions(next);
		}
	}

	function handleKeydown(e: KeyboardEvent) {
		const n = displayedSuggestions.length;
		if (e.key === 'ArrowDown' && n > 0) {
			e.preventDefault();
			userNavigated = true;
			activeIndex = activeIndex >= n - 1 ? 0 : activeIndex + 1;
		} else if (e.key === 'ArrowUp' && n > 0) {
			e.preventDefault();
			userNavigated = true;
			activeIndex = activeIndex <= 0 ? n - 1 : activeIndex - 1;
		} else if (e.key === 'Tab' && n > 0 && userNavigated && activeIndex >= 0) {
			e.preventDefault();
			selectSuggestion(displayedSuggestions[activeIndex]);
		} else if (e.key === 'Enter') {
			if (userNavigated && showSuggestions && activeIndex >= 0 && displayedSuggestions[activeIndex]) {
				e.preventDefault();
				selectSuggestion(displayedSuggestions[activeIndex]);
				return;
			}
			onnavigate?.(value.trim());
		} else if (e.key === 'Escape') {
			if (escapeValue !== undefined) value = escapeValue;
			focused = false;
			clearSuggestions();
		}
	}

	function handleInput() {
		userNavigated = false;
		activeIndex = -1;
		onchange?.(value);
		const inputPath = value.trim();

		if (autocompleteDebounceId) {
			clearTimeout(autocompleteDebounceId);
			autocompleteDebounceId = null;
		}

		if (!focused || !inputPath) {
			clearSuggestions();
			return;
		}

		autocompleteDebounceId = setTimeout(() => {
			void fetchSuggestions(inputPath);
		}, 120);
	}

	function handleFocus() {
		focused = true;
		if (blurTimeoutId) {
			clearTimeout(blurTimeoutId);
			blurTimeoutId = null;
		}
	}

	function handleBlur() {
		blurTimeoutId = setTimeout(() => {
			focused = false;
			clearSuggestions();
			blurTimeoutId = null;
		}, 120);
	}

	$effect(() => {
		void displayedSuggestions;
		clampActive();
	});

	let popoverStyle = $state('');

	function repositionPopover() {
		if (!inputEl) return;
		const r = inputEl.getBoundingClientRect();
		popoverStyle = `position:fixed;top:${Math.round(r.bottom + 4)}px;left:${Math.round(r.left)}px;width:${Math.round(r.width)}px;z-index:50;`;
	}

	$effect(() => {
		if (!showSuggestions) return;
		repositionPopover();
		const onScrollResize = () => repositionPopover();
		window.addEventListener('scroll', onScrollResize, true);
		window.addEventListener('resize', onScrollResize);
		const id = requestAnimationFrame(repositionPopover);
		return () => {
			window.removeEventListener('scroll', onScrollResize, true);
			window.removeEventListener('resize', onScrollResize);
			cancelAnimationFrame(id);
		};
	});
</script>

<div class="relative flex-1">
	<input
		bind:this={inputEl}
		type="text"
		bind:value
		onkeydown={handleKeydown}
		oninput={handleInput}
		onfocus={handleFocus}
		onblur={handleBlur}
		{placeholder}
		autocomplete="off"
		spellcheck="false"
		class="ac-input w-full px-3 py-1.5 pr-9 border rounded-md bg-background focus:outline-none focus:ring-2 focus:ring-primary focus:border-primary transition-all font-mono text-sm {className}"
	/>

	{#if autocompleteLoading}
		<div class="ac-spinner absolute inset-y-0 right-3 flex items-center text-muted-foreground">
			<LoaderCircle class="h-3.5 w-3.5 animate-spin" />
		</div>
	{/if}

	{#if showSuggestions}
		<div use:portalToBody style={popoverStyle} class="ac-popover rounded-lg border border-border bg-popover text-popover-foreground shadow-xl shadow-black/10 overflow-hidden">
			<div class="ac-head flex items-center justify-between px-3 py-1.5 border-b border-border/60 bg-muted/40">
				<span class="text-[0.65rem] font-semibold uppercase tracking-wider text-muted-foreground">Folders</span>
				<span class="text-[0.65rem] tabular-nums text-muted-foreground/70">{displayedSuggestions.length}</span>
			</div>
			<div class="max-h-72 overflow-y-auto py-0.5">
				{#each displayedSuggestions as suggestion, index (suggestion.path)}
					{@const frag = currentFragment().toLowerCase()}
					{@const matched = frag !== '' && suggestion.name.toLowerCase().startsWith(frag)}
					{@const rest = matched ? suggestion.name.slice(frag.length) : ''}
					<button
						type="button"
						onmousedown={(event) => {
							event.preventDefault();
							selectSuggestion(suggestion);
						}}
						class="ac-row group w-full flex items-center gap-2.5 px-2.5 py-1.5 text-left transition-colors
							{index === activeIndex ? 'bg-accent text-accent-foreground' : 'hover:bg-accent/50'}"
					>
						<Folder class="h-4 w-4 shrink-0 {index === activeIndex ? 'text-primary' : 'text-blue-500/80'}" />
						<div class="min-w-0 flex-1 flex items-baseline gap-1">
							{#if matched}
								<span class="ac-match font-semibold truncate">{suggestion.name.slice(0, frag.length)}</span><span class="truncate font-medium opacity-90">{rest}</span>
							{:else}
								<span class="truncate font-medium">{suggestion.name}</span>
							{/if}
						</div>
						<span class="ac-path hidden sm:block truncate text-[0.7rem] text-muted-foreground font-mono max-w-[40%]">{suggestion.path}</span>
						<ChevronRight class="h-3.5 w-3.5 shrink-0 opacity-0 group-hover:opacity-100 transition-opacity text-muted-foreground" />
					</button>
				{/each}
			</div>
			<div class="ac-foot hidden sm:flex items-center gap-3 px-3 py-1.5 border-t border-border/60 bg-muted/30 text-[0.65rem] text-muted-foreground/80">
				<span class="flex items-center gap-1"><ArrowUp class="h-3 w-3" /><ArrowDown class="h-3 w-3" /> navigate</span>
				<span class="flex items-center gap-1"><CornerDownLeft class="h-3 w-3" /> select</span>
				<span class="ml-auto">esc to dismiss</span>
			</div>
		</div>
	{/if}
	{#if autocompleteError}
		<p class="mt-1 text-xs text-muted-foreground">{autocompleteError}</p>
	{/if}
</div>

{#if showNavigateButton}
	<Button variant="outline" size="sm" onclick={() => onnavigate?.(value.trim())} disabled={!value.trim() || navigateDisabled || loading} title="Navigate to path">
		{#snippet children()}
			<CornerDownLeft class="h-4 w-4" />
		{/snippet}
	</Button>
{/if}

<style>
	.ac-popover {
		animation: ac-pop 120ms cubic-bezier(0.16, 1, 0.3, 1) both;
		transform-origin: top center;
	}
	@keyframes ac-pop {
		from { transform: translateY(-4px) scaleY(0.98); }
		to { transform: translateY(0) scaleY(1); }
	}
	.ac-row {
		animation: ac-row-in 100ms cubic-bezier(0.16, 1, 0.3, 1) both;
	}
	@keyframes ac-row-in {
		from { opacity: 0; transform: translateX(-2px); }
		to { opacity: 1; transform: translateX(0); }
	}
</style>
