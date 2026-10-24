<script lang="ts">
	import { onMount, tick } from 'svelte';
	import * as m from '$lib/paraglide/messages';
	import { apiClient } from '$lib/api/client';
	import { ApiError } from '$lib/api/clients/common';
	import { getQueryClient } from '$lib/query/client';
	import type { ActressNameOverride, CastNameProvenance } from '$lib/api/types';
	import { portalToBody } from '$lib/actions/portal';
	import Button from '$lib/components/ui/Button.svelte';
	import { normalizeNamePart, splitReconstructsSource } from '$lib/utils/source-cast';

	let { provenance, onClose }: { provenance: CastNameProvenance; onClose: () => void } = $props();
	let phase = $state<'loading' | 'editing' | 'saving' | 'saved' | 'blocked'>('loading');
	let firstName = $state('');
	let lastName = $state('');
	let confirmed = $state(false);
	let expectedRule = $state<ActressNameOverride | null>(null);
	let error = $state('');
	let recoveryRequired = $state(false);
	let dialog: HTMLDivElement;
	let alive = true;
	let readController: AbortController | undefined;
	const matchesSource = $derived(splitReconstructsSource(provenance.display_name, firstName, lastName));
	const differentSavedLabel = $derived(expectedRule !== null && normalizeNamePart(expectedRule.source_name) !== normalizeNamePart(provenance.display_name));

	function configurationRecoveryMessage(cause: unknown): string {
		if (!(cause instanceof ApiError)) return '';
		if (cause.code === 'NAME_OVERRIDE_CONFIG_UNAVAILABLE') return m.review_name_correction_config_unavailable();
		if (cause.code === 'NAME_OVERRIDE_ROLLBACK_FAILED') return m.review_name_correction_recovery_required();
		return '';
	}

	async function load() {
		if (phase === 'saving' || recoveryRequired) return;
		readController?.abort();
		const controller = new AbortController();
		readController = controller;
		phase = 'loading';
		confirmed = false;
		error = '';
		try {
			const { rule } = await apiClient.scrapers.getNameOverride(provenance.source_id, {
				signal: controller.signal,
				timeoutMs: 15000,
			});
			if (!alive || controller.signal.aborted) return;
			expectedRule = rule;
			firstName = rule && normalizeNamePart(rule.source_name) === normalizeNamePart(provenance.display_name) ? rule.first_name : '';
			lastName = rule && normalizeNamePart(rule.source_name) === normalizeNamePart(provenance.display_name) ? rule.last_name : '';
			phase = 'editing';
		} catch (cause) {
			if (!alive || controller.signal.aborted) return;
			const recoveryMessage = configurationRecoveryMessage(cause);
			recoveryRequired = !!recoveryMessage;
			error = recoveryMessage || m.review_name_correction_load_failed();
			phase = 'blocked';
		}
	}

	async function save(event: SubmitEvent) {
		event.preventDefault();
		if (phase !== 'editing' || !matchesSource || !confirmed) return;
		phase = 'saving';
		error = '';
		try {
			await apiClient.scrapers.saveNameOverride(provenance.source_id, {
				source_name: provenance.display_name,
				first_name: normalizeNamePart(firstName),
				last_name: normalizeNamePart(lastName),
				expected_rule: expectedRule,
			});
			// This is config-only. Do not invalidate or mutate review/movie data.
			void getQueryClient().invalidateQueries({ queryKey: ['config'] }).catch(() => undefined);
			if (alive) phase = 'saved';
		} catch (cause) {
			if (!alive) return;
			confirmed = false;
			phase = 'blocked';
			const recoveryMessage = configurationRecoveryMessage(cause);
			recoveryRequired = !!recoveryMessage;
			error = recoveryMessage ? recoveryMessage
				: cause instanceof ApiError && cause.status === 409
					? m.review_name_correction_conflict()
					: m.review_name_correction_uncertain();
		}
	}

	function close() {
		// A lost connection is not proof that a write failed. Keep the dialog open
		// while saving and require an authoritative read before any retry.
		if (phase !== 'saving') onClose();
	}

	function handleKey(event: KeyboardEvent) {
		if (event.key === 'Escape') {
			event.preventDefault();
			event.stopPropagation();
			close();
		}
		if (event.key !== 'Tab' || !dialog) return;
		const focusable = Array.from(dialog.querySelectorAll<HTMLElement>(
			'button:not(:disabled), input:not(:disabled), [tabindex="0"]',
		));
		const first = focusable[0];
		const last = focusable.at(-1);
		if (!first) { event.preventDefault(); dialog.focus(); return; }
		if (!dialog.contains(document.activeElement)) {
			event.preventDefault();
			(event.shiftKey ? last : first)?.focus();
			return;
		}
		if (event.shiftKey && (document.activeElement === first || document.activeElement === dialog)) {
			event.preventDefault(); last?.focus();
		} else if (!event.shiftKey && (document.activeElement === last || document.activeElement === dialog)) {
			event.preventDefault(); first.focus();
		}
	}

	onMount(() => {
		const previousFocus = document.activeElement;
		void tick().then(() => { if (alive) dialog.focus(); });
		void load();
		return () => {
			alive = false;
			readController?.abort();
			void tick().then(() => {
				if (previousFocus instanceof HTMLElement && previousFocus.isConnected) previousFocus.focus();
			});
		};
	});
</script>

<svelte:window onkeydown={handleKey} />

<div class="fixed inset-0 z-[60] flex items-center justify-center bg-black/60 p-4" use:portalToBody>
	<div bind:this={dialog} role="dialog" aria-modal="true" aria-labelledby="name-correction-title"
		aria-describedby="name-correction-scope" tabindex="-1"
		class="w-full max-w-lg max-h-[90vh] overflow-y-auto rounded-lg border bg-background p-6 shadow-xl outline-none">
		<h2 id="name-correction-title" class="text-lg font-semibold">{m.review_name_correction_title()}</h2>
		<p id="name-correction-scope" class="mt-2 text-sm text-muted-foreground">
			{m.review_name_correction_scope()}
		</p>
		<div class="my-4 rounded-md border bg-muted/40 p-3">
			<p class="text-xs text-muted-foreground">{m.review_name_correction_original({ source_id: provenance.source_id })}</p>
			<p class="mt-1 whitespace-pre-wrap break-words font-medium">{provenance.display_name}</p>
		</div>
		{#if phase === 'saved'}
			<div role="status" class="space-y-2 text-sm">
				<p class="font-medium text-green-700 dark:text-green-400">{m.review_name_correction_saved()}</p>
				<p>{m.review_name_correction_rescrape()}</p>
				<p>{m.review_name_correction_guard()}</p>
			</div>
			<div class="mt-6 flex justify-end"><Button onclick={close}>{m.review_name_correction_done()}</Button></div>
		{:else}
			{#if phase === 'loading'}<p role="status" class="text-sm">{m.review_name_correction_loading()}</p>{/if}
			{#if differentSavedLabel}
				<p class="mb-4 text-sm text-amber-700 dark:text-amber-400">{m.review_name_correction_different_label({ source_name: expectedRule?.source_name ?? '' })}</p>
			{/if}
			<form onsubmit={save} class="space-y-4">
				<div class="grid gap-4 sm:grid-cols-2">
					<label class="text-sm font-medium">{m.review_name_correction_given()}
						<input class="mt-1 w-full rounded-md border bg-background px-3 py-2" bind:value={firstName} oninput={() => { confirmed = false; }}
							disabled={phase !== 'editing'} autocomplete="off" aria-describedby="name-split-help" />
					</label>
					<label class="text-sm font-medium">{m.review_name_correction_family()}
						<input class="mt-1 w-full rounded-md border bg-background px-3 py-2" bind:value={lastName} oninput={() => { confirmed = false; }}
							disabled={phase !== 'editing'} autocomplete="off" aria-describedby="name-split-help" />
					</label>
				</div>
				<Button variant="outline" size="sm" disabled={phase !== 'editing'} onclick={() => {
					[firstName, lastName] = [lastName, firstName]; confirmed = false;
				}}>{m.review_name_correction_swap()}</Button>
				<p id="name-split-help" class="text-xs text-muted-foreground">
					{m.review_name_correction_help()}
				</p>
				{#if phase === 'editing' && (firstName || lastName) && !matchesSource}
					<p role="status" class="text-sm text-amber-700 dark:text-amber-400">{m.review_name_correction_invalid()}</p>
				{/if}
				<label class="flex items-start gap-2 text-sm">
					<input type="checkbox" class="mt-1" bind:checked={confirmed} disabled={phase !== 'editing'} />
					<span>{m.review_name_correction_confirm()}</span>
				</label>
				{#if error}
					<p role="alert" class="text-sm text-destructive">{error}</p>
					{#if !recoveryRequired}
						<Button variant="outline" onclick={load}>{m.review_name_correction_reload()}</Button>
					{/if}
				{/if}
				{#if phase === 'saving'}<p role="status" class="text-sm">{m.review_name_correction_saving_help()}</p>{/if}
				<div class="flex flex-wrap justify-end gap-2 pt-2">
					<Button variant="outline" onclick={close} disabled={phase === 'saving'}>{m.common_cancel()}</Button>
					<Button type="submit" disabled={phase !== 'editing' || !matchesSource || !confirmed}>
						{phase === 'saving' ? m.review_name_correction_saving() : m.review_name_correction_save()}
					</Button>
				</div>
			</form>
		{/if}
	</div>
</div>
