<script lang="ts">
	import { createSettingsStore } from '../settings-store.svelte';

	const noop = () => {};
	const settings = createSettingsStore({
		getProfileTestResults: () => ({}),
		getGlobalProxyTestResult: () => null,
		getGlobalFlareSolverrTestResult: () => null,
		getVerificationTokens: () => ({}),
		clearTestResults: noop,
		invalidateGlobalProxyTest: noop,
		invalidateGlobalFlareSolverrTest: noop,
		onConfigInitialized: noop,
	});
</script>

<button
	onclick={settings.handleSave}
	disabled={settings.loading || settings.saveConfigMutation.isPending}
>Save</button>
<button onclick={settings.reloadConfig}>Reload</button>
<output data-testid="error">{settings.error ?? ''}</output>
<output data-testid="query-error">{settings.configQuery.error?.message ?? ''}</output>
<output data-testid="loading">{String(settings.loading)}</output>
<output data-testid="loaded">{String(settings.configInitialized)}</output>
