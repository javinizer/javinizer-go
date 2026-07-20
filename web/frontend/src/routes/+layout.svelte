<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { browser } from '$app/environment';
	import { QueryClientProvider } from '@tanstack/svelte-query';
	import Navigation from '$lib/components/Navigation.svelte';
	import ToastContainer from '$lib/components/ui/ToastContainer.svelte';
	import DialogContainer from '$lib/components/ui/DialogContainer.svelte';
	import BackgroundJobIndicator from '$lib/components/BackgroundJobIndicator.svelte';
	import ProgressModal from '$lib/components/ProgressModal.svelte';
	import { apiClient } from '$lib/api/client';
	import { BaseClient } from '$lib/api/clients/common';
	import { websocketStore } from '$lib/stores/websocket';
	import { getBackgroundJobState, reopenModal, dismiss, closeModal } from '$lib/stores/background-job.svelte';
	import { getQueryClient } from '$lib/query/client';
	import { getThemeStore } from '$lib/stores/theme.svelte';
	import SetupWizard from '$lib/components/setup/SetupWizard.svelte';
	import { clearClientStorage } from '$lib/utils/storage';
	import * as m from '$lib/paraglide/messages';
	import { bootstrapLocale } from '$lib/i18n/locale';
	import { translateErrorCode } from '$lib/i18n/api-messages';
	import { ApiError } from '$lib/api/clients/common';
	import LocaleReconciler from '$lib/components/LocaleReconciler.svelte';
	import LanguageSelector from '$lib/components/LanguageSelector.svelte';
	import '../app.css';

	let { children } = $props();

	let bgJobId = $derived(getBackgroundJobState().jobId);
	let bgShowModal = $derived(getBackgroundJobState().showModal);

	let authLoading = $state(true);
	let authSubmitting = $state(false);
	let authUnavailable = $state(false);
	let authInitialized = $state(false);
	let authAuthenticated = $state(false);
	let authUsername = $state('');
	let authError = $state<string | null>(null);
	let loginUsername = $state('');
	let loginPassword = $state('');
	let loginRememberMe = $state(true);
	let clientStorageCleared = false;

	function localizeApiError(error: unknown, fallback: string): string {
		if (error instanceof ApiError && error.code) {
			return translateErrorCode(error.code, error.params ?? null, error.message || fallback);
		}
		return error instanceof Error ? error.message : fallback;
	}

	function syncWebSocketAuthState() {
		if (authAuthenticated) {
			websocketStore.connect();
		} else {
			websocketStore.disconnect();
		}
	}

	async function refreshAuthStatus() {
		authError = null;

		try {
			const status = await apiClient.getAuthStatus();
			authUnavailable = false;
			authInitialized = status.initialized;
			if (!status.initialized && !clientStorageCleared) {
				clearClientStorage();
				BaseClient.setSessionID(null);
				clientStorageCleared = true;
			}
			authAuthenticated = status.authenticated;
			authUsername = status.username ?? '';
			if (!loginUsername && authUsername) {
				loginUsername = authUsername;
			}
		} catch (error) {
			authUnavailable = true;
			authAuthenticated = false;
			authUsername = '';
			authError = localizeApiError(error, m.auth_failed_status());
		} finally {
			authLoading = false;
			syncWebSocketAuthState();
		}
	}

	async function handleLoginSubmit(event: SubmitEvent) {
		event.preventDefault();
		authError = null;
		authSubmitting = true;
		try {
			const loginResult = await apiClient.loginAuth({
				username: loginUsername,
				password: loginPassword,
				remember_me: loginRememberMe
			});
			loginPassword = '';
			if (loginResult?.session_id) { BaseClient.setSessionID(loginResult.session_id); }
			await refreshAuthStatus();
		} catch (error) {
			authError = localizeApiError(error, m.auth_failed_login());
		} finally {
			authSubmitting = false;
		}
	}

	async function handleLogout() {
		authError = null;
		try {
			await apiClient.logoutAuth();
		} catch (error) {
			authError = localizeApiError(error, m.auth_failed_logout());
		} finally {
			BaseClient.setSessionID(null);
			authAuthenticated = false;
			authUsername = '';
			loginPassword = '';
			syncWebSocketAuthState();
			await refreshAuthStatus();
		}
	}

	onMount(() => {
		getThemeStore().initTheme();
		// Bootstrap the interface locale before rendering the auth UI so the
		// login/setup screens are localized. Reconciliation with the configured
		// ui.language happens after the config loads (see configQuery effect).
		void bootstrapLocale();
		refreshAuthStatus();
	});

	// Reconcile the interface locale with the configured ui.language once the
	// config is available (post-auth). The reconcile logic lives in
	// LocaleReconciler.svelte, rendered inside <QueryClientProvider> so the
	// createConfigQuery call has access to the QueryClient context.

	onDestroy(() => {
		getThemeStore().destroyTheme();
		websocketStore.disconnect();
	});
</script>

<svelte:head>
</svelte:head>

{#if !authAuthenticated}
	<div class="fixed top-4 right-4 z-50">
		<LanguageSelector />
	</div>
{/if}

{#if authLoading}
	<div class="min-h-screen bg-background flex items-center justify-center px-4">
		<div class="w-full max-w-md rounded-lg border bg-card p-6 shadow-sm text-center">
			<p class="text-lg font-semibold">{m.auth_checking()}</p>
		</div>
	</div>
{:else if authUnavailable}
	<div class="min-h-screen bg-background flex items-center justify-center px-4 py-10">
		<div class="w-full max-w-md rounded-lg border bg-card p-6 shadow-sm space-y-4">
			<div class="space-y-1">
				<h1 class="text-2xl font-bold">{m.auth_service_unavailable()}</h1>
				<p class="text-sm text-muted-foreground">
					{m.auth_service_unavailable_desc()}
				</p>
			</div>

			{#if authError}
				<div class="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
					{authError}
				</div>
			{/if}

			<button
				type="button"
				onclick={() => refreshAuthStatus()}
				class="w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground"
			>
				{m.auth_retry()}
			</button>
		</div>
	</div>
{:else if !authInitialized}
	<SetupWizard onComplete={() => { void refreshAuthStatus(); }} />
{:else if !authAuthenticated}
	<div class="min-h-screen bg-background flex items-center justify-center px-4 py-10">
		<div class="w-full max-w-md rounded-lg border bg-card p-6 shadow-sm space-y-4">
			<div class="space-y-1">
				<h1 class="text-2xl font-bold">{m.auth_login_required()}</h1>
				<p class="text-sm text-muted-foreground">
					{m.auth_login_required_desc()}
				</p>
			</div>

			{#if authError}
				<div class="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
					{authError}
				</div>
			{/if}

			<form class="space-y-3" onsubmit={handleLoginSubmit}>
				<div class="space-y-1">
					<label class="text-sm font-medium" for="login-username">{m.auth_username()}</label>
					<input
						id="login-username"
						class="w-full rounded-md border bg-background px-3 py-2 text-sm"
						type="text"
						required
						autocomplete="username"
						bind:value={loginUsername}
					/>
				</div>
				<div class="space-y-1">
					<label class="text-sm font-medium" for="login-password">{m.auth_password()}</label>
					<input
						id="login-password"
						class="w-full rounded-md border bg-background px-3 py-2 text-sm"
						type="password"
						required
						autocomplete="current-password"
						bind:value={loginPassword}
					/>
				</div>
				<label class="flex items-start gap-3 rounded-md border border-border/60 bg-muted/20 px-3 py-2 text-sm">
					<input
						type="checkbox"
						class="mt-0.5 rounded"
						bind:checked={loginRememberMe}
					/>
					<span class="space-y-0.5">
						<span class="block font-medium text-foreground">{m.auth_remember_me()}</span>
						<span class="block text-xs text-muted-foreground">
							{m.auth_remember_me_desc()}
						</span>
					</span>
				</label>
				<button
					type="submit"
					disabled={authSubmitting}
					class="w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60"
				>
					{authSubmitting ? m.auth_signing_in() : m.auth_sign_in()}
				</button>
			</form>
		</div>
	</div>
{:else}
	{#if browser}
		<QueryClientProvider client={getQueryClient()}>
			<LocaleReconciler getAuthenticated={() => authAuthenticated} />
			<div class="min-h-screen bg-background">
				<Navigation authenticated={authAuthenticated} username={authUsername} onLogout={handleLogout} />
				<main class="route-container">
					{#key page.url.pathname}
						<div class="route-content">
							{@render children?.()}
						</div>
					{/key}
				</main>
			<ToastContainer />
			<DialogContainer />
			{#if bgJobId && !bgShowModal}
				<BackgroundJobIndicator
					jobId={bgJobId}
					onReopen={reopenModal}
					onDismiss={dismiss}
				/>
			{/if}
			{#if bgShowModal && bgJobId}
				<ProgressModal
					jobId={bgJobId}
					onClose={closeModal}
				/>
			{/if}
		</div>
		</QueryClientProvider>
	{:else}
		<div class="min-h-screen bg-background">
			<Navigation authenticated={authAuthenticated} username={authUsername} onLogout={handleLogout} />
			<main class="route-container">
				{#key page.url.pathname}
					<div class="route-content">
						{@render children?.()}
					</div>
				{/key}
			</main>
			<ToastContainer />
			<DialogContainer />
		</div>
	{/if}
{/if}

<style>
	.route-container {
		position: relative;
	}

	.route-content {
		min-height: 0;
	}
</style>
