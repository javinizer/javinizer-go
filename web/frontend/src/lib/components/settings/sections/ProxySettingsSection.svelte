<script lang="ts">
	import { RefreshCw, X, Check, AlertTriangle } from 'lucide-svelte';
	import Button from '$lib/components/ui/Button.svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import FormNumberInput from '$lib/components/settings/FormNumberInput.svelte';
	import FormTextInput from '$lib/components/settings/FormTextInput.svelte';
	import FormToggle from '$lib/components/settings/FormToggle.svelte';
	import type { SettingsConfig, ProxyConfig as ProxyConfigType, FlareSolverrConfig as FlareSolverrConfigType } from '$lib/api/types';

	interface TestResult {
		success: boolean;
		timestamp: number;
		message?: string;
		configSnapshot?: string;
	}

	interface Props {
		config: SettingsConfig;
		inputClass: string;
		testingProxy: boolean;
		testingFlareSolverr: boolean;
		testingProfile: Record<string, boolean>;
		savingProfile: Record<string, boolean>;
		loading: boolean;
		saving: boolean;
		// Test state props
		profileTestResults: Record<string, TestResult>;
		globalProxyTestResult: TestResult | null;
		globalFlareSolverrTestResult: TestResult | null;
		canSaveProfile: (name: string) => boolean;
		isTestExpired: (result: TestResult | null | undefined) => boolean;
		invalidateGlobalProxyTest: () => void;
		invalidateGlobalFlareSolverrTest: () => void;
		// Existing props
		getProxyProfileNames: () => string[];
		addProxyProfile: () => void;
		renameProxyProfile: (oldName: string, rawNewName: string) => void;
		removeProxyProfile: (name: string) => void;
		setProxyProfileField: (name: string, field: 'url' | 'username' | 'password', value: string) => void;
		saveProxyProfile: (profileName: string) => Promise<void>;
		runNamedProxyProfileTest: (profileName: string) => Promise<void>;
		runProxyTest: (mode: 'direct' | 'flaresolverr') => Promise<void>;
	}

	let {
		config,
		inputClass,
		testingProxy,
		testingFlareSolverr,
		testingProfile,
		savingProfile,
		loading,
		saving,
		// Test state props
		profileTestResults,
		globalProxyTestResult,
		globalFlareSolverrTestResult,
		canSaveProfile,
		isTestExpired,
		invalidateGlobalProxyTest,
		invalidateGlobalFlareSolverrTest,
		// Existing props
		getProxyProfileNames,
		addProxyProfile,
		renameProxyProfile,
		removeProxyProfile,
		setProxyProfileField,
		saveProxyProfile,
		runNamedProxyProfileTest,
		runProxyTest
	}: Props = $props();
	const scraperProxyEnabled = $derived(config?.scrapers?.proxy?.enabled ?? false);
	const flaresolverrEnabled = $derived(config?.scrapers?.flaresolverr?.enabled ?? false);
</script>

<SettingsSection title="Proxy and Flaresolverr Settings" description="Configure global proxy fallback, reusable proxy profiles, and FlareSolverr for Cloudflare bypass" defaultExpanded={false}>
	<SettingsSubsection title="Scraper Proxy">
		<FormToggle
			label="Enable scraper proxy"
			description="Enable global fallback proxy. Scrapers set to 'Inherit Global Proxy' will use this."
			checked={config.scrapers.proxy?.enabled ?? false}
			onchange={(val) => {
				if (!config.scrapers.proxy) config.scrapers.proxy = {} as ProxyConfigType;
				config.scrapers.proxy.enabled = val;
				invalidateGlobalProxyTest();
			}}
		/>

		<fieldset disabled={!scraperProxyEnabled} class={`space-y-0 ${!scraperProxyEnabled ? 'opacity-60' : ''}`}>
			<div class="py-4 border-b border-border">
				<label class="block text-sm font-medium mb-2" for="default-proxy-profile">Default proxy profile</label>
				<select
					id="default-proxy-profile"
					class={inputClass}
					value={config.scrapers.proxy?.default_profile ?? ''}
					onchange={(e) => {
						if (!config.scrapers.proxy) config.scrapers.proxy = {} as ProxyConfigType;
						config.scrapers.proxy.default_profile = e.currentTarget.value;
						invalidateGlobalProxyTest();
					}}
				>
					{#each getProxyProfileNames() as profileName}
						<option value={profileName}>{profileName}</option>
					{/each}
				</select>
				<p class="text-xs text-muted-foreground mt-1">
					Default global fallback profile. Scrapers in 'Inherit Global Proxy' mode use this profile.
				</p>
			</div>

			<div class="py-4 border-b border-border">
				<div class="flex items-center justify-between mb-3">
					<div>
						<p class="block text-sm font-medium">Proxy profiles</p>
						<p class="text-xs text-muted-foreground mt-1">
							Reusable proxy definitions that scrapers can reference by profile name.
						</p>
					</div>
					<Button variant="outline" size="sm" onclick={addProxyProfile}>
						{#snippet children()}Add Profile{/snippet}
					</Button>
				</div>

				<div class="space-y-3">
					{#each getProxyProfileNames() as profileName}
						{@const profile = config.scrapers.proxy?.profiles?.[profileName]}
						{@const testResult = profileTestResults[profileName]}
						{@const saveEnabled = canSaveProfile(profileName)}
						{@const hasUrl = (profile?.url ?? '').trim() !== ''}
						<div class="rounded-md border p-3 space-y-2">
							<div class="flex items-center gap-2">
								<input
									type="text"
									value={profileName}
									onchange={(e) => renameProxyProfile(profileName, e.currentTarget.value)}
									class="flex-1 px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
								/>
								<Button
									variant="ghost"
									size="icon"
									disabled={getProxyProfileNames().length <= 1}
									onclick={() => removeProxyProfile(profileName)}
									class="h-8 w-8"
								>
									{#snippet children()}
										<X class="h-4 w-4" />
									{/snippet}
								</Button>
							</div>
							<input
								type="text"
								value={profile?.url ?? ''}
								placeholder="http://proxy.example.com:8080"
								oninput={(e) => setProxyProfileField(profileName, 'url', e.currentTarget.value)}
								class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
							/>
							<div class="grid grid-cols-2 gap-2">
								<input
									type="text"
									value={profile?.username ?? ''}
									placeholder="Username (optional)"
									oninput={(e) => setProxyProfileField(profileName, 'username', e.currentTarget.value)}
									class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
								/>
								<input
									type="password"
									value={profile?.password ?? ''}
									placeholder="Password (optional)"
									oninput={(e) => setProxyProfileField(profileName, 'password', e.currentTarget.value)}
									class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
								/>
							</div>
							<div class="flex items-center gap-2 pt-1">
								<Button
									variant="outline"
									size="sm"
									onclick={() => saveProxyProfile(profileName)}
									disabled={!saveEnabled || savingProfile[profileName] || loading || saving}
									title={!testResult
										? 'Test the profile before saving'
										: !testResult.success
											? 'Fix proxy settings and test again'
											: isTestExpired(testResult)
												? 'Test has expired, run test again'
												: 'Save verified proxy profile'}
								>
									{#snippet children()}
										{#if saveEnabled}
											<Check class="h-4 w-4 mr-2 text-green-500" />
										{/if}
										{savingProfile[profileName] ? 'Saving...' : 'Save Profile'}
									{/snippet}
								</Button>

								<Button
									variant="outline"
									size="sm"
									onclick={() => runNamedProxyProfileTest(profileName)}
									disabled={testingProfile[profileName] || savingProfile[profileName] || loading || saving || !hasUrl}
								>
									{#snippet children()}
										<RefreshCw class={`h-4 w-4 mr-2 ${testingProfile[profileName] ? 'animate-spin' : ''}`} />
										{testingProfile[profileName] ? 'Testing...' : 'Test Profile'}
									{/snippet}
								</Button>

								{#if testResult}
									<span class="text-xs {testResult.success ? 'text-green-600' : 'text-red-600'}">
										{#if testResult.success}
											✓ Verified
										{:else}
											✗ Failed
										{/if}
									</span>
								{:else}
									<span class="text-xs text-muted-foreground">Test required before save</span>
								{/if}
							</div>
						</div>
					{/each}
				</div>
			</div>

			<div class="pt-2">
				<p class="text-xs text-muted-foreground">
					Global proxy validation uses the default profile test. Run <span class="font-medium">Test Profile</span> on the current default profile.
				</p>
				{#if globalProxyTestResult}
					<p class="text-xs mt-1 {globalProxyTestResult.success ? 'text-green-600' : 'text-red-600'}">
						{#if globalProxyTestResult.success}
							✓ Global proxy verified
						{:else}
							✗ Global proxy test failed
						{/if}
					</p>
				{/if}
			</div>

			{#if globalProxyTestResult && !globalProxyTestResult.success}
				<p class="text-xs text-red-600 mt-2">
					Fix proxy settings and test again before saving configuration.
				</p>
			{/if}
		</fieldset>
	</SettingsSubsection>

	<SettingsSubsection title="FlareSolverr">
		<FormToggle
			label="Enable FlareSolverr"
			description="Use FlareSolverr to bypass Cloudflare protection (required for JavLibrary). Run FlareSolverr via Docker: docker run -p 8191:8191 ghcr.io/flaresolverr/flaresolverr:latest"
			checked={config.scrapers?.flaresolverr?.enabled ?? false}
			onchange={(val) => {
				if (!config.scrapers.flaresolverr) config.scrapers.flaresolverr = {} as FlareSolverrConfigType;
				config.scrapers.flaresolverr.enabled = val;
				invalidateGlobalFlareSolverrTest();
			}}
		/>

		<fieldset disabled={!flaresolverrEnabled} class={`space-y-0 ${!flaresolverrEnabled ? 'opacity-60' : ''}`}>
			<FormTextInput
				label="FlareSolverr URL"
				description="FlareSolverr API endpoint"
				value={config.scrapers?.flaresolverr?.url ?? 'http://localhost:8191/v1'}
				placeholder="http://localhost:8191/v1"
				onchange={(val) => {
					if (!config.scrapers.flaresolverr) config.scrapers.flaresolverr = {} as FlareSolverrConfigType;
					config.scrapers.flaresolverr.url = val;
					invalidateGlobalFlareSolverrTest();
				}}
			/>

			<FormNumberInput
				label="Timeout"
				description="Maximum time to wait for FlareSolverr to solve challenges"
				value={config.scrapers?.flaresolverr?.timeout ?? 30}
				min={5}
				max={300}
				unit="seconds"
				onchange={(val) => {
					if (!config.scrapers.flaresolverr) config.scrapers.flaresolverr = {} as FlareSolverrConfigType;
					config.scrapers.flaresolverr.timeout = val;
					invalidateGlobalFlareSolverrTest();
				}}
			/>

			<FormNumberInput
				label="Max retries"
				description="Number of retry attempts for failed FlareSolverr requests"
				value={config.scrapers?.flaresolverr?.max_retries ?? 3}
				min={0}
				max={10}
				onchange={(val) => {
					if (!config.scrapers.flaresolverr) config.scrapers.flaresolverr = {} as FlareSolverrConfigType;
					config.scrapers.flaresolverr.max_retries = val;
					invalidateGlobalFlareSolverrTest();
				}}
			/>

			<FormNumberInput
				label="Session TTL"
				description="How long to keep FlareSolverr browser sessions alive"
				value={config.scrapers?.flaresolverr?.session_ttl ?? 300}
				min={60}
				max={3600}
				unit="seconds"
				onchange={(val) => {
					if (!config.scrapers.flaresolverr) config.scrapers.flaresolverr = {} as FlareSolverrConfigType;
					config.scrapers.flaresolverr.session_ttl = val;
					invalidateGlobalFlareSolverrTest();
				}}
			/>

			<div class="pt-2 flex items-center gap-2">
				<Button
					variant="outline"
					size="sm"
					onclick={() => runProxyTest('flaresolverr')}
					disabled={testingFlareSolverr || loading || saving}
				>
					{#snippet children()}
						<RefreshCw class={`h-4 w-4 mr-2 ${testingFlareSolverr ? 'animate-spin' : ''}`} />
						{testingFlareSolverr ? 'Testing FlareSolverr...' : 'Test FlareSolverr'}
					{/snippet}
				</Button>

				{#if globalFlareSolverrTestResult}
					<span class="text-xs {globalFlareSolverrTestResult.success ? 'text-green-600' : 'text-red-600'}">
						{#if globalFlareSolverrTestResult.success}
							✓ FlareSolverr working
						{:else}
							✗ Test failed
						{/if}
					</span>
				{/if}
			</div>

			{#if globalFlareSolverrTestResult && !globalFlareSolverrTestResult.success}
				<p class="text-xs text-red-600 mt-2">
					Fix FlareSolverr settings and test again before saving configuration.
				</p>
			{/if}
		</fieldset>
	</SettingsSubsection>
</SettingsSection>
