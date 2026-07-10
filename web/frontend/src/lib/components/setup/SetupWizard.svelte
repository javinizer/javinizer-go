<script lang="ts">
	import { onMount } from 'svelte';
	import { cubicOut, quintOut } from 'svelte/easing';
	import { scale } from 'svelte/transition';
	import { apiClient } from '$lib/api/client';
	import { BaseClient } from '$lib/api/clients/common';
	import { toastStore } from '$lib/stores/toast';
	import { getThemeStore } from '$lib/stores/theme.svelte';
	import { websocketStore } from '$lib/stores/websocket';
	import Button from '$lib/components/ui/Button.svelte';
	import StepCredentials from './StepCredentials.svelte';
	import StepCredentialsSuccess from './StepCredentialsSuccess.svelte';
	import StepDirectories from './StepDirectories.svelte';
	import StepScrapers from './StepScrapers.svelte';
	import type { Scraper } from '$lib/api/types';
	import {
		ArrowLeft,
		ArrowRight,
		Check,
		Loader2,
	} from 'lucide-svelte';

	interface Props {
		onComplete: () => void;
	}

	let { onComplete }: Props = $props();

	type StepId = 'credentials' | 'directories' | 'scrapers';

	const steps: { id: StepId; label: string; hint: string }[] = [
		{ id: 'credentials', label: 'Admin Account', hint: 'Secure your server' },
		{ id: 'directories', label: 'Library Folders', hint: 'Where your media lives' },
		{ id: 'scrapers', label: 'Scrapers', hint: 'Metadata sources' },
	];

	let stepIndex = $state(0);
	let submitting = $state(false);
	let error = $state<string | null>(null);

	let credentials = $state({ username: '', password: '', confirm: '' });

	// Credentials confirmation interstitial. After the admin account is created
	// we pause on an explicit acknowledgement screen instead of auto-advancing,
	// so the user is clearly notified their credentials were registered.
	let credentialsRegistered = $state(false);
	let registeredUsername = $state('');
	let registeredAt = $state(new Date());
	let sessionActive = $state(false);

	let inCredentialsSuccess = $derived(stepIndex === 0 && credentialsRegistered);

	let dirs = $state<string[]>([]);

	let availableScrapers = $state<Scraper[]>([]);
	let selectedScrapers = $state<string[]>([]);
	let scrapersLoading = $state(false);
	let scrapersFetched = $state(false);

	let stageEl = $state<HTMLElement | null>(null);
	let stageHeight = $state<number | null>(null);
	let heightReady = $state(false);

	const TRANSITION_MS = 360;

	$effect(() => {
		const el = stageEl;
		if (!el) return;
		const measure = () => {
			stageHeight = el.offsetHeight;
			heightReady = true;
		};
		void measure();
		if (typeof ResizeObserver === 'undefined') {
			return;
		}
		const ro = new ResizeObserver(measure);
		ro.observe(el);
		return () => ro.disconnect();
	});

	let isLastStep = $derived(stepIndex === steps.length - 1);

	function gotoStep(index: number) {
		if (index < 0 || index >= steps.length) return;
		stepIndex = index;
		error = null;
	}

	function syncWebSocketAuth() {
		websocketStore.connect();
	}

	async function fetchScrapers() {
		if (scrapersFetched || scrapersLoading) return;
		scrapersLoading = true;
		try {
			availableScrapers = await apiClient.getScrapers();
			selectedScrapers = availableScrapers.filter((s) => s.enabled).map((s) => s.name);
		} catch {
			error = 'Could not load available scrapers. You can configure them later in Settings → Scrapers.';
			toastStore.error('Could not load available scrapers', 4000);
		} finally {
			scrapersLoading = false;
			scrapersFetched = true;
		}
	}

	// Pre-fill the directories list with a sensible absolute default path so
	// the user lands on the library step with something to work from instead
	// of an empty list. /api/v1/cwd returns the first allowed directory if any
	// are configured, otherwise the process working directory — or, when that
	// is a root path (desktop app launched from Finder/Explorer, where CWD is
	// "/" or the bundle dir), an empty string. Only pre-fill once.
	let dirsPrefilled = $state(false);
	async function prefillDefaultDir() {
		if (dirsPrefilled || dirs.length > 0) return;
		dirsPrefilled = true;
		try {
			const { path } = await apiClient.getCurrentWorkingDirectory();
			if (path && dirs.length === 0 && stepIndex <= 1) dirs = [path];
		} catch {
			// Non-fatal: the user can still type/browse a directory manually.
		}
	}

	// Step 1: create the admin account (required to obtain a session for the
	// protected scraper/config endpoints). This is the only commit before the
	// final Finish — credentials are irreversible, so Back is hidden afterwards.
	// Instead of auto-advancing, we reveal a confirmation interstitial so the
	// user is explicitly notified their credentials were registered; they then
	// press Continue to proceed to the library folders step.
	async function handleCredentialsNext() {
		error = null;
		if (credentials.password !== credentials.confirm) {
			error = 'Passwords do not match';
			return;
		}
		submitting = true;
		try {
			const result = await apiClient.setupAuth({
				username: credentials.username,
				password: credentials.password,
			});
			registeredUsername = credentials.username;
			registeredAt = new Date();
			sessionActive = Boolean(result?.session_id);
			credentials.password = '';
			credentials.confirm = '';
			if (result?.session_id) BaseClient.setSessionID(result.session_id);
			syncWebSocketAuth();
			credentialsRegistered = true;
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to create admin account';
		} finally {
			submitting = false;
		}
	}

	// Advance from the credentials confirmation interstitial to the directories
	// step. Only reachable after the account has been successfully registered.
	function handleCredentialsContinue() {
		gotoStep(1);
		toastStore.success('Admin account created', 3000);
	}

	// Step 2: directories are staged locally; nothing is committed until Finish
	// so the transition to the scrapers step is instant.
	function handleDirectoriesNext() {
		error = null;
		gotoStep(2);
	}

	function handleDirectoriesSkip() {
		error = null;
		dirs = [];
		gotoStep(2);
	}

	// Final step: commit the staged directories and scraper selection together.
	async function handleScrapersFinish() {
		error = null;
		submitting = true;
		try {
			const fresh = await apiClient.getConfig();
			const sec = fresh?.api?.security;
			await apiClient.updateSecurityConfig({
				allowed_directories: dirs,
				denied_directories: [...(sec?.denied_directories ?? [])],
				allow_unc: sec?.allow_unc ?? false,
				allowed_unc_servers: [...(sec?.allowed_unc_servers ?? [])],
			});

			if (fresh.api?.security) {
				fresh.api.security.allowed_directories = dirs;
			}
			const sc = (fresh.scrapers ?? {}) as Record<string, unknown>;
			if (availableScrapers.length > 0) {
				sc.priority = [...selectedScrapers];
				for (const scraper of availableScrapers) {
					if (!sc[scraper.name]) sc[scraper.name] = {};
					(sc[scraper.name] as Record<string, unknown>).enabled = selectedScrapers.includes(scraper.name);
				}
				fresh.scrapers = sc as typeof fresh.scrapers;
				await apiClient.request('/api/v1/config', {
					method: 'PUT',
					body: JSON.stringify(fresh),
				});
			}
			toastStore.success('Setup complete. Welcome to Javinizer.', 4000);
			onComplete();
		} catch (e) {
			error = e instanceof Error ? e.message : 'Failed to save setup configuration';
		} finally {
			submitting = false;
		}
	}

	function handleBack() {
		if (stepIndex === 2) gotoStep(1);
	}

	function handleNext() {
		if (inCredentialsSuccess) void handleCredentialsContinue();
		else if (stepIndex === 0) void handleCredentialsNext();
		else if (stepIndex === 1) void handleDirectoriesNext();
		else void handleScrapersFinish();
	}

	let canProceed = $derived.by(() => {
		if (inCredentialsSuccess) return true;
		if (submitting) return false;
		if (stepIndex === 0) {
			return (
				credentials.username.trim().length > 0 &&
				credentials.password.length >= 8 &&
				credentials.confirm.length >= 8
			);
		}
		if (isLastStep && scrapersLoading) return false;
		return true;
	});

	let nextLabel = $derived(
		inCredentialsSuccess
			? 'Continue to library setup'
			: submitting
			? stepIndex === 0
				? 'Creating…'
				: isLastStep
					? 'Finishing…'
					: 'Saving…'
			: isLastStep
				? 'Finish Setup'
				: stepIndex === 0
					? 'Create admin account'
					: 'Continue',
	);

	onMount(() => {
		getThemeStore().initTheme();
	});

	// Lazily fetch the scraper list when the user reaches the scrapers step.
	// Requires an authenticated session (created in step 1), so it can only
	// run once the credentials step is complete.
	$effect(() => {
		if (stepIndex === 2) void fetchScrapers();
	});

	// Pre-fill the directories step with a sensible default absolute path as
	// soon as an authenticated session exists (created in step 1).
	$effect(() => {
		if (credentialsRegistered) void prefillDefaultDir();
	});
</script>



<div class="wizard-shell">
	<!-- Brand / stepper rail -->
	<aside class="brand-rail">
		<div class="brand-glow brand-glow-a"></div>
		<div class="brand-glow brand-glow-b"></div>
		<div class="brand-grid"></div>

		<div class="brand-inner">
			<div class="brand-mark" in:scale|local={{ start: 0.8, duration: 420, easing: quintOut }}>
				<svg viewBox="0 0 512 512" width="34" height="34" aria-hidden="true">
					<rect width="512" height="512" rx="112" fill="currentColor" />
					<path d="M330 150v198c0 54-37 88-92 88-44 0-74-19-92-58l52-26c9 19 23 29 41 29 22 0 36-16 36-42V150h55z" fill="#ffffff" />
				</svg>
			</div>
			<div class="brand-word">Javinizer</div>

			<nav class="stepper" aria-label="Setup progress">
				<div class="stepper-track" aria-hidden="true">
					<div
						class="stepper-track-fill"
						style="height: {Math.max(0, (stepIndex / (steps.length - 1)) * 100)}%"
					></div>
				</div>
				{#each steps as step, i (step.id)}
					<div class="stepper-row" class:active={stepIndex === i} class:done={stepIndex > i} aria-current={stepIndex === i ? 'step' : undefined}>
						<div class="stepper-node">
							{#if stepIndex > i}
								<Check class="stepper-check" />
							{:else}
								<span class="stepper-num">{i + 1}</span>
							{/if}
						</div>
						<div class="stepper-text">
							<div class="stepper-label">{step.label}</div>
							<div class="stepper-hint">{step.hint}</div>
						</div>
					</div>
				{/each}
			</nav>

			<div class="brand-foot">
				<div class="brand-foot-row"><span class="brand-dot"></span> Step {stepIndex + 1} of {steps.length}</div>
			</div>
		</div>
	</aside>

	<!-- Content -->
	<main class="content-pane">
		<div class="content-frame">
			<div class="content-stage"
				style="height: {stageHeight !== null ? `${stageHeight}px` : 'auto'}; transition: {heightReady ? `height ${TRANSITION_MS}ms cubic-bezier(0.33, 1, 0.68, 1)` : 'none'};">
				{#key `${stepIndex}-${credentialsRegistered}`}
					<div
						class="content-card stage-card"
						bind:this={stageEl}
						in:scale|local={{ start: 1.03, duration: TRANSITION_MS, easing: cubicOut }}
						out:scale|local={{ start: 0.985, duration: TRANSITION_MS, easing: cubicOut }}
					>
						{#if stepIndex === 0 && credentialsRegistered}
							<StepCredentialsSuccess
								username={registeredUsername}
								{sessionActive}
								registeredAt={registeredAt}
							/>
						{:else if stepIndex === 0}
							<StepCredentials bind:credentials {error} {submitting} onSubmit={handleNext} />
						{:else if stepIndex === 1}
							<StepDirectories bind:dirs {error} {submitting} />
						{:else}
							<StepScrapers
								bind:selected={selectedScrapers}
								scrapers={availableScrapers}
								loading={scrapersLoading}
								{error}
								{submitting}
							/>
						{/if}
					</div>
				{/key}
			</div>

			<div class="content-nav">
				{#if stepIndex === 2}
					<Button variant="ghost" onclick={handleBack} disabled={submitting}>
						{#snippet children()}
							<ArrowLeft class="h-4 w-4" />
							Back
						{/snippet}
					</Button>
				{:else}
					<div></div>
				{/if}

				<div class="nav-right">
					{#if stepIndex === 1}
						<Button variant="ghost" onclick={handleDirectoriesSkip} disabled={submitting}>
							{#snippet children()}Skip for now{/snippet}
						</Button>
					{/if}
					<Button onclick={handleNext} disabled={!canProceed}>
						{#snippet children()}
							{#if submitting}
								<Loader2 class="h-4 w-4 animate-spin" />
							{:else if inCredentialsSuccess}
								<ArrowRight class="h-4 w-4" />
							{:else if isLastStep}
								<Check class="h-4 w-4" />
							{:else}
								<ArrowRight class="h-4 w-4" />
							{/if}
							{nextLabel}
						{/snippet}
					</Button>
				</div>
			</div>
		</div>
	</main>
</div>

<style>
	.wizard-shell {
		display: grid;
		grid-template-columns: minmax(280px, 1fr) minmax(0, 2fr);
		min-height: 100vh;
		width: 100%;
	}

	/* ---- Brand rail ---- */
	.brand-rail {
		position: relative;
		overflow: hidden;
		background: radial-gradient(120% 100% at 0% 0%, hsl(224 76% 12%) 0%, hsl(222 84% 6%) 55%, hsl(222 84% 4%) 100%);
		color: hsl(210 40% 98%);
		padding: 2.75rem 2.25rem;
		display: flex;
		flex-direction: column;
		isolation: isolate;
	}

	.brand-glow {
		position: absolute;
		border-radius: 9999px;
		filter: blur(72px);
		opacity: 0.55;
		pointer-events: none;
		z-index: 0;
	}

	.brand-glow-a {
		width: 320px;
		height: 320px;
		background: hsl(217 91% 60% / 0.5);
		top: -80px;
		right: -90px;
	}

	.brand-glow-b {
		width: 260px;
		height: 260px;
		background: hsl(265 85% 62% / 0.32);
		bottom: -70px;
		left: -60px;
	}

	.brand-grid {
		position: absolute;
		inset: 0;
		background-image: radial-gradient(hsl(210 40% 98% / 0.07) 1px, transparent 1px);
		background-size: 22px 22px;
		mask-image: radial-gradient(120% 90% at 30% 20%, black 0%, transparent 78%);
		pointer-events: none;
		z-index: 0;
	}

	.brand-inner {
		position: relative;
		z-index: 1;
		display: flex;
		flex-direction: column;
		height: 100%;
	}

	.brand-mark {
		color: hsl(217 91% 60%);
		display: inline-flex;
		filter: drop-shadow(0 6px 18px hsl(217 91% 60% / 0.45));
	}

	.brand-word {
		font-family: ui-sans-serif, system-ui, -apple-system, 'Segoe UI', sans-serif;
		font-weight: 700;
		font-size: 1.85rem;
		letter-spacing: -0.02em;
		margin-top: 1rem;
		background: linear-gradient(180deg, hsl(210 40% 98%), hsl(210 40% 82%));
		-webkit-background-clip: text;
		background-clip: text;
		color: transparent;
	}

	.stepper {
		position: relative;
		margin-top: 3rem;
		display: flex;
		flex-direction: column;
		gap: 1.4rem;
	}

	.stepper-track {
		position: absolute;
		left: 17px;
		top: 8px;
		bottom: 8px;
		width: 2px;
		background: hsl(210 40% 98% / 0.12);
		border-radius: 9999px;
		overflow: hidden;
	}

	.stepper-track-fill {
		width: 100%;
		background: linear-gradient(180deg, hsl(217 91% 65%), hsl(265 85% 62%));
		border-radius: 9999px;
		transition: height 360ms cubic-bezier(0.33, 1, 0.68, 1);
	}

	.stepper-row {
		position: relative;
		display: flex;
		align-items: flex-start;
		gap: 0.85rem;
		opacity: 0.5;
		transition: opacity 220ms cubic-bezier(0.33, 1, 0.68, 1);
	}

	.stepper-row.active {
		opacity: 1;
	}

	.stepper-row.done {
		opacity: 0.78;
	}

	.stepper-node {
		flex-shrink: 0;
		width: 36px;
		height: 36px;
		border-radius: 9999px;
		display: flex;
		align-items: center;
		justify-content: center;
		background: hsl(217 84% 10%);
		border: 1.5px solid hsl(210 40% 98% / 0.18);
		font-weight: 600;
		font-size: 0.85rem;
		transition: all 240ms cubic-bezier(0.33, 1, 0.68, 1);
		z-index: 1;
	}

	.stepper-row.active .stepper-node {
		background: linear-gradient(140deg, hsl(217 91% 62%), hsl(265 85% 60%));
		border-color: transparent;
		box-shadow: 0 0 0 4px hsl(217 91% 60% / 0.18), 0 8px 20px hsl(217 91% 55% / 0.4);
		transform: scale(1.06);
	}

	.stepper-row.done .stepper-node {
		background: hsl(217 84% 18%);
		border-color: hsl(217 91% 60% / 0.5);
		color: hsl(217 91% 75%);
	}

	:global(.stepper-check) {
		width: 16px;
		height: 16px;
	}

	.stepper-num {
		color: hsl(210 40% 88%);
	}

	.stepper-row.active .stepper-num {
		color: hsl(210 40% 98%);
	}

	.stepper-text {
		padding-top: 0.35rem;
	}

	.stepper-label {
		font-size: 0.95rem;
		font-weight: 600;
		letter-spacing: -0.01em;
	}

	.stepper-hint {
		font-size: 0.78rem;
		color: hsl(210 40% 72%);
		margin-top: 0.1rem;
	}

	.brand-foot {
		margin-top: auto;
		padding-top: 2rem;
	}

	.brand-foot-row {
		display: inline-flex;
		align-items: center;
		gap: 0.5rem;
		font-size: 0.72rem;
		text-transform: uppercase;
		letter-spacing: 0.12em;
		color: hsl(210 40% 70%);
	}

	.brand-dot {
		width: 6px;
		height: 6px;
		border-radius: 9999px;
		background: hsl(217 91% 65%);
		box-shadow: 0 0 12px hsl(217 91% 65%);
	}

	/* ---- Content pane ---- */
	.content-pane {
		display: flex;
		align-items: stretch;
		justify-content: center;
		padding: 2rem 1.5rem;
		background: hsl(var(--background));
	}

	.content-frame {
		width: 100%;
		max-width: 640px;
		display: flex;
		flex-direction: column;
		justify-content: center;
		gap: 1.5rem;
		padding: 1.5rem 0;
	}

	.content-stage {
		position: relative;
		width: 100%;
	}

	.content-card {
		background: hsl(var(--card));
		border: 1px solid hsl(var(--border));
		border-radius: calc(var(--radius) + 6px);
		padding: 2.25rem;
		box-shadow:
			0 1px 2px hsl(222 84% 4% / 0.04),
			0 24px 60px -28px hsl(222 84% 4% / 0.25);
	}

	.stage-card {
		position: absolute;
		top: 0;
		left: 0;
		right: 0;
		transform-origin: center top;
		will-change: transform, opacity;
	}

	.content-nav {
		display: flex;
		align-items: center;
		justify-content: space-between;
		gap: 0.75rem;
		position: relative;
		z-index: 2;
	}

	.nav-right {
		display: flex;
		align-items: center;
		gap: 0.5rem;
		margin-left: auto;
	}

	@media (max-width: 900px) {
		.wizard-shell {
			grid-template-columns: 1fr;
		}

		.brand-rail {
			padding: 2rem 1.5rem 1.5rem;
		}

		.brand-glow-a {
			width: 220px;
			height: 220px;
		}

		.stepper {
			margin-top: 1.75rem;
			gap: 1rem;
		}

		.content-pane {
			padding: 1.25rem 1rem;
		}

		.content-card {
			padding: 1.5rem;
		}
	}
</style>
