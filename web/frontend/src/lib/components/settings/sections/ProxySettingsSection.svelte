<script lang="ts">
	import * as m from '$lib/paraglide/messages';
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
	selectClass: string;
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
		inputClass, selectClass,
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

<SettingsSection title={m.settings_proxy_title()} description={m.settings_proxy_desc()} defaultExpanded={false}>
	<SettingsSubsection title={m.settings_proxy_scraper_subsection()}>
		<FormToggle
			label={m.settings_proxy_enable_label()}
			description={m.settings_proxy_enable_desc()}
			checked={config.scrapers.proxy?.enabled ?? false}
			onchange={(val) => {
				if (!config.scrapers.proxy) config.scrapers.proxy = {} as ProxyConfigType;
				config.scrapers.proxy.enabled = val;
				invalidateGlobalProxyTest();
			}}
		/>

		<fieldset disabled={!scraperProxyEnabled} class={`space-y-0 ${!scraperProxyEnabled ? 'opacity-60' : ''}`}>
			<div class="py-4 border-b border-border">
				<label class="block text-sm font-medium mb-2" for="default-proxy-profile">{m.settings_proxy_default_profile_label()}</label>
				<select
					id="default-proxy-profile"
					class={selectClass}
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
					{m.settings_proxy_default_profile_desc()}
				</p>
			</div>

			<div class="py-4 border-b border-border">
				<div class="flex items-center justify-between mb-3">
					<div>
						<p class="block text-sm font-medium">{m.settings_proxy_profiles_label()}</p>
						<p class="text-xs text-muted-foreground mt-1">
							{m.settings_proxy_profiles_desc()}
						</p>
					</div>
					<Button variant="outline" size="sm" onclick={addProxyProfile}>
						{#snippet children()}{m.settings_proxy_add_profile()}{/snippet}
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
								placeholder={m.settings_proxy_url_placeholder()}
								oninput={(e) => setProxyProfileField(profileName, 'url', e.currentTarget.value)}
								class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
							/>
							<div class="grid grid-cols-2 gap-2">
								<input
									type="text"
									value={profile?.username ?? ''}
									placeholder={m.settings_proxy_username_placeholder()}
									oninput={(e) => setProxyProfileField(profileName, 'username', e.currentTarget.value)}
									class="w-full px-3 py-2 border rounded-md focus:ring-2 focus:ring-primary focus:border-primary transition-all bg-background text-sm"
								/>
								<input
									type="password"
									value={profile?.password ?? ''}
									placeholder={m.settings_proxy_password_placeholder()}
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
										? m.settings_proxy_save_tooltip_test()
										: !testResult.success
											? m.settings_proxy_save_tooltip_fix()
											: isTestExpired(testResult)
												? m.settings_proxy_save_tooltip_expired()
												: m.settings_proxy_save_tooltip_save()}
								>
									{#snippet children()}
										{#if saveEnabled}
											<Check class="h-4 w-4 mr-2 text-green-500" />
										{/if}
										{savingProfile[profileName] ? m.common_saving() : m.settings_proxy_save_profile()}
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
										{testingProfile[profileName] ? m.common_testing() : m.settings_proxy_test_profile()}
									{/snippet}
								</Button>

								{#if testResult}
									<span class="text-xs {testResult.success ? 'text-green-600' : 'text-red-600'}">
										{#if testResult.success}
											{m.settings_proxy_verified()}
										{:else}
											{m.settings_proxy_failed()}
										{/if}
									</span>
								{:else}
									<span class="text-xs text-muted-foreground">{m.settings_proxy_test_required()}</span>
								{/if}
							</div>
						</div>
					{/each}
				</div>
			</div>

			<div class="pt-2">
				<p class="text-xs text-muted-foreground">
					{m.settings_proxy_global_note()}
				</p>
				{#if globalProxyTestResult}
					<p class="text-xs mt-1 {globalProxyTestResult.success ? 'text-green-600' : 'text-red-600'}">
						{#if globalProxyTestResult.success}
							{m.settings_proxy_global_verified()}
						{:else}
							{m.settings_proxy_global_failed()}
						{/if}
					</p>
				{/if}
			</div>

			{#if globalProxyTestResult && !globalProxyTestResult.success}
				<p class="text-xs text-red-600 mt-2">
					{m.settings_proxy_fix_before_save()}
				</p>
			{/if}
		</fieldset>
	</SettingsSubsection>

	<SettingsSubsection title={m.settings_proxy_flaresolverr_subsection()}>
		<FormToggle
			label={m.settings_proxy_flaresolverr_enable_label()}
			description={m.settings_proxy_flaresolverr_enable_desc()}
			checked={config.scrapers?.flaresolverr?.enabled ?? false}
			onchange={(val) => {
				if (!config.scrapers.flaresolverr) config.scrapers.flaresolverr = {} as FlareSolverrConfigType;
				config.scrapers.flaresolverr.enabled = val;
				invalidateGlobalFlareSolverrTest();
			}}
		/>

		<fieldset disabled={!flaresolverrEnabled} class={`space-y-0 ${!flaresolverrEnabled ? 'opacity-60' : ''}`}>
			<FormTextInput
				label={m.settings_proxy_flaresolverr_url_label()}
				description={m.settings_proxy_flaresolverr_url_desc()}
				value={config.scrapers?.flaresolverr?.url ?? 'http://localhost:8191/v1'}
				placeholder="http://localhost:8191/v1"
				onchange={(val) => {
					if (!config.scrapers.flaresolverr) config.scrapers.flaresolverr = {} as FlareSolverrConfigType;
					config.scrapers.flaresolverr.url = val;
					invalidateGlobalFlareSolverrTest();
				}}
			/>

			<FormNumberInput
				label={m.settings_proxy_flaresolverr_timeout_label()}
				description={m.settings_proxy_flaresolverr_timeout_desc()}
				value={config.scrapers?.flaresolverr?.timeout ?? 30}
				min={5}
				max={300}
				unit={m.common_unit_seconds()}
				onchange={(val) => {
					if (!config.scrapers.flaresolverr) config.scrapers.flaresolverr = {} as FlareSolverrConfigType;
					config.scrapers.flaresolverr.timeout = val;
					invalidateGlobalFlareSolverrTest();
				}}
			/>

			<FormNumberInput
				label={m.settings_proxy_flaresolverr_max_retries_label()}
				description={m.settings_proxy_flaresolverr_max_retries_desc()}
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
				label={m.settings_proxy_flaresolverr_session_ttl_label()}
				description={m.settings_proxy_flaresolverr_session_ttl_desc()}
				value={config.scrapers?.flaresolverr?.session_ttl ?? 300}
				min={60}
				max={3600}
				unit={m.common_unit_seconds()}
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
						{testingFlareSolverr ? m.settings_proxy_testing_flaresolverr() : m.settings_proxy_test_flaresolverr()}
					{/snippet}
				</Button>

				{#if globalFlareSolverrTestResult}
					<span class="text-xs {globalFlareSolverrTestResult.success ? 'text-green-600' : 'text-red-600'}">
						{#if globalFlareSolverrTestResult.success}
							{m.settings_proxy_flaresolverr_working()}
						{:else}
							{m.settings_proxy_flaresolverr_test_failed()}
						{/if}
					</span>
				{/if}
			</div>

			{#if globalFlareSolverrTestResult && !globalFlareSolverrTestResult.success}
				<p class="text-xs text-red-600 mt-2">
					{m.settings_proxy_flaresolverr_fix_before_save()}
				</p>
			{/if}
		</fieldset>
	</SettingsSubsection>
</SettingsSection>
