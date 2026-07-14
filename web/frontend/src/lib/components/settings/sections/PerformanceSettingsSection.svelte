<script lang="ts">
	import * as m from '$lib/paraglide/messages';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	selectClass: string;
	}

	let { config, inputClass, selectClass }: Props = $props();
</script>

<SettingsSection title={m.settings_performance_title()} description={m.settings_performance_desc()} defaultExpanded={false}>
	<div class="space-y-4">
		<div>
			<label class="block text-sm font-medium mb-2" for="max-workers">
				{m.settings_performance_max_workers_label()}
			</label>
			<input
				id="max-workers"
				type="number"
				bind:value={config.performance.max_workers}
				class={inputClass}
				min="1"
				max="20"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_performance_max_workers_desc()}
			</p>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="worker-timeout">{m.settings_performance_worker_timeout_label()}</label>
			<input
				id="worker-timeout"
				type="number"
				bind:value={config.performance.worker_timeout}
				class={inputClass}
				min="5"
				max="600"
			/>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="buffer-size">{m.settings_performance_buffer_size_label()}</label>
			<input
				id="buffer-size"
				type="number"
				bind:value={config.performance.buffer_size}
				class={inputClass}
				min="10"
				max="1000"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_performance_buffer_size_desc()}
			</p>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="update-interval">{m.settings_performance_update_interval_label()}</label>
			<input
				id="update-interval"
				type="number"
				bind:value={config.performance.update_interval}
				class={inputClass}
				min="50"
				max="1000"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				{m.settings_performance_update_interval_desc()}
			</p>
		</div>
	</div>
</SettingsSection>
