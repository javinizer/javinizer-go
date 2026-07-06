<script lang="ts">
	import { onMount, onDestroy } from 'svelte';
	import { page } from '$app/state';
	import { browser } from '$app/environment';
	import { QueryClientProvider } from '@tanstack/svelte-query';
	import favicon from '$lib/assets/favicon.svg';
	import Navigation from '$lib/components/Navigation.svelte';
	import ToastContainer from '$lib/components/ui/ToastContainer.svelte';
	import DialogContainer from '$lib/components/ui/DialogContainer.svelte';
	import BackgroundJobIndicator from '$lib/components/BackgroundJobIndicator.svelte';
	import ProgressModal from '$lib/components/ProgressModal.svelte';
	import AllowedDirectoriesEditor from '$lib/components/AllowedDirectoriesEditor.svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import { apiClient } from '$lib/api/client';
	import { BaseClient } from '$lib/api/clients/common';
	import { websocketStore } from '$lib/stores/websocket';
	import { toastStore } from '$lib/stores/toast';
	import { getBackgroundJobState, reopenModal, dismiss, closeModal } from '$lib/stores/background-job.svelte';
	import { getQueryClient } from '$lib/query/client';
	import { getThemeStore } from '$lib/stores/theme.svelte';
	import { Save, FolderCheck, ArrowRight } from 'lucide-svelte';
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
	let setupUsername = $state('');
	let setupPassword = $state('');
	let setupPasswordConfirm = $state('');
	let loginUsername = $state('');
	let loginPassword = $state('');
	let loginRememberMe = $state(true);
	let setupNeedsDirs = $state(false);
	let setupDirs = $state<string[]>([]);
	let setupDirsSubmitting = $state(false);
	let setupSecurityDefaults = $state({
		denied_directories: [] as string[],
		allow_unc: false,
		allowed_unc_servers: [] as string[],
	});

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
			authAuthenticated = status.authenticated;
			authUsername = status.username ?? '';
			if (!loginUsername && authUsername) {
				loginUsername = authUsername;
			}
		} catch (error) {
			authUnavailable = true;
			authAuthenticated = false;
			authUsername = '';
			authError = error instanceof Error ? error.message : 'Failed to load authentication status';
		} finally {
			authLoading = false;
			syncWebSocketAuthState();
		}
	}

	async function handleSetupSubmit(event: SubmitEvent) {
		event.preventDefault();
		authError = null;

		if (setupPassword !== setupPasswordConfirm) {
			authError = 'Passwords do not match';
			return;
		}

		authSubmitting = true;
		try {
			const setupResult = await apiClient.setupAuth({
				username: setupUsername,
				password: setupPassword
			});
			setupPassword = '';
			setupPasswordConfirm = '';
			if (setupResult?.session_id) { BaseClient.setSessionID(setupResult.session_id); }
			loginPassword = '';
			await refreshAuthStatus();
			if (authAuthenticated) {
				setupNeedsDirs = true;
				try {
					const fresh = await apiClient.getConfig();
					const sec = fresh?.api?.security;
					setupSecurityDefaults = {
						denied_directories: [...(sec?.denied_directories ?? [])],
						allow_unc: sec?.allow_unc ?? false,
						allowed_unc_servers: [...(sec?.allowed_unc_servers ?? [])],
					};
					setupDirs = [...(sec?.allowed_directories ?? [])];
				} catch (error) {
					setupSecurityDefaults = {
						denied_directories: [],
						allow_unc: false,
						allowed_unc_servers: [],
					};
					setupDirs = [];
					authError = error instanceof Error
						? `Failed to load current security settings: ${error.message}. You can still save allowed directories below.`
						: 'Failed to load current security settings. You can still save allowed directories below.';
				}
			}
		} catch (error) {
			authError = error instanceof Error ? error.message : 'Failed to initialize authentication';
		} finally {
			authSubmitting = false;
		}
	}

	async function handleSetupDirsSubmit(event: SubmitEvent) {
		event.preventDefault();
		authError = null;
		setupDirsSubmitting = true;
		try {
			await apiClient.updateSecurityConfig({
				allowed_directories: setupDirs,
				denied_directories: setupSecurityDefaults.denied_directories,
				allow_unc: setupSecurityDefaults.allow_unc,
				allowed_unc_servers: setupSecurityDefaults.allowed_unc_servers,
			});
			setupNeedsDirs = false;
			if (setupDirs.length > 0) {
				toastStore.success('Allowed directories saved. Ready to scan.', 4000);
			} else {
				toastStore.info('You can add allowed directories later in Settings → Security.', 5000);
			}
		} catch (error) {
			authError = error instanceof Error ? error.message : 'Failed to save allowed directories';
		} finally {
			setupDirsSubmitting = false;
		}
	}

	function handleSetupDirsSkip() {
		setupNeedsDirs = false;
		setupDirs = [];
		toastStore.info('You can add allowed directories later in Settings → Security.', 5000);
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
			authError = error instanceof Error ? error.message : 'Failed to login';
		} finally {
			authSubmitting = false;
		}
	}

	async function handleLogout() {
		authError = null;
		try {
			await apiClient.logoutAuth();
		} catch (error) {
			authError = error instanceof Error ? error.message : 'Failed to logout';
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
		refreshAuthStatus();
	});

	onDestroy(() => {
		getThemeStore().destroyTheme();
		websocketStore.disconnect();
	});
</script>

<svelte:head>
	<link rel="icon" href={favicon} />
</svelte:head>

{#if authLoading}
	<div class="min-h-screen bg-background flex items-center justify-center px-4">
		<div class="w-full max-w-md rounded-lg border bg-card p-6 shadow-sm text-center">
			<p class="text-lg font-semibold">Checking authentication...</p>
		</div>
	</div>
{:else if authUnavailable}
	<div class="min-h-screen bg-background flex items-center justify-center px-4 py-10">
		<div class="w-full max-w-md rounded-lg border bg-card p-6 shadow-sm space-y-4">
			<div class="space-y-1">
				<h1 class="text-2xl font-bold">Authentication Service Unavailable</h1>
				<p class="text-sm text-muted-foreground">
					The app could not reach the authentication endpoint. Check server status and retry.
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
				Retry
			</button>
		</div>
	</div>
{:else if !authAuthenticated}
	<div class="min-h-screen bg-background flex items-center justify-center px-4 py-10">
		<div class="w-full max-w-md rounded-lg border bg-card p-6 shadow-sm space-y-4">
			<div class="space-y-1">
				<h1 class="text-2xl font-bold">
					{#if authInitialized}
						Login Required
					{:else}
						First-Time Setup
					{/if}
				</h1>
				<p class="text-sm text-muted-foreground">
					{#if authInitialized}
						Sign in with your configured username and password.
					{:else}
						Create the default username and password for this server.
					{/if}
				</p>
			</div>

			{#if authError}
				<div class="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
					{authError}
				</div>
			{/if}

			{#if authInitialized}
				<form class="space-y-3" onsubmit={handleLoginSubmit}>
					<div class="space-y-1">
						<label class="text-sm font-medium" for="login-username">Username</label>
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
						<label class="text-sm font-medium" for="login-password">Password</label>
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
							<span class="block font-medium text-foreground">Remember me</span>
							<span class="block text-xs text-muted-foreground">
								Keep this browser signed in across browser and server restarts for the normal session lifetime.
							</span>
						</span>
					</label>
					<button
						type="submit"
						disabled={authSubmitting}
						class="w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60"
					>
						{authSubmitting ? 'Signing in...' : 'Sign In'}
					</button>
				</form>
			{:else}
				<form class="space-y-3" onsubmit={handleSetupSubmit}>
					<div class="space-y-1">
						<label class="text-sm font-medium" for="setup-username">Username</label>
						<input
							id="setup-username"
							class="w-full rounded-md border bg-background px-3 py-2 text-sm"
							type="text"
							required
							autocomplete="username"
							bind:value={setupUsername}
						/>
					</div>
					<div class="space-y-1">
						<label class="text-sm font-medium" for="setup-password">Password</label>
						<input
							id="setup-password"
							class="w-full rounded-md border bg-background px-3 py-2 text-sm"
							type="password"
							required
							minlength="8"
							autocomplete="new-password"
							bind:value={setupPassword}
						/>
					</div>
					<div class="space-y-1">
						<label class="text-sm font-medium" for="setup-password-confirm">Confirm Password</label>
						<input
							id="setup-password-confirm"
							class="w-full rounded-md border bg-background px-3 py-2 text-sm"
							type="password"
							required
							minlength="8"
							autocomplete="new-password"
							bind:value={setupPasswordConfirm}
						/>
					</div>
					<button
						type="submit"
						disabled={authSubmitting}
						class="w-full rounded-md bg-primary px-4 py-2 text-sm font-medium text-primary-foreground disabled:opacity-60"
					>
						{authSubmitting ? 'Saving...' : 'Create Credentials'}
					</button>
				</form>
			{/if}
		</div>
	</div>
{:else if setupNeedsDirs}
	<div class="min-h-screen bg-background flex items-center justify-center px-4 py-10">
		<div class="w-full max-w-md rounded-lg border bg-card p-6 shadow-sm space-y-4">
			<div class="space-y-1">
				<h1 class="text-2xl font-bold">Add Allowed Directories</h1>
				<p class="text-sm text-muted-foreground">
					Add the folders you want to scan for videos; you can change this later in Settings → Security.
					With no allowed directories configured, all file operations are blocked.
				</p>
			</div>

			{#if authError}
				<div class="rounded-md border border-destructive/40 bg-destructive/10 px-3 py-2 text-sm text-destructive">
					{authError}
				</div>
			{/if}

			<AllowedDirectoriesEditor
				bind:directories={setupDirs}
				whitelistPaths={setupDirs}
				emptyHint="No directories added yet. Add one below to enable scanning, or skip and add later."
			/>

			<form class="space-y-2" onsubmit={handleSetupDirsSubmit}>
				<Button type="submit" disabled={setupDirsSubmitting} class="w-full">
					{#snippet children()}
						<FolderCheck class="h-4 w-4 mr-2" />
						{setupDirsSubmitting ? 'Saving...' : 'Save & Continue'}
					{/snippet}
				</Button>
			</form>
			<button
				type="button"
				disabled={setupDirsSubmitting}
				onclick={handleSetupDirsSkip}
				class="w-full inline-flex items-center justify-center gap-1 rounded-md px-4 py-2 text-sm font-medium text-muted-foreground hover:text-foreground disabled:opacity-60"
			>
				Skip for now
				<ArrowRight class="h-3.5 w-3.5" />
			</button>
		</div>
	</div>
{:else}
	{#if browser}
		<QueryClientProvider client={getQueryClient()}>
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
