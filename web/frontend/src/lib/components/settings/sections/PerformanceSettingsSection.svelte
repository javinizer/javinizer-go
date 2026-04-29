<script lang="ts">
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	}

	let { config, inputClass }: Props = $props();
</script>

<SettingsSection title="Performance Settings" description="Configure worker pool and performance tuning options" defaultExpanded={false}>
	<div class="space-y-4">
		<div>
			<label class="block text-sm font-medium mb-2" for="max-workers">
				Max Workers (concurrent tasks)
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
				Higher values = faster but more resource intensive
			</p>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="worker-timeout">Worker Timeout (seconds)</label>
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
			<label class="block text-sm font-medium mb-2" for="buffer-size">Buffer Size</label>
			<input
				id="buffer-size"
				type="number"
				bind:value={config.performance.buffer_size}
				class={inputClass}
				min="10"
				max="1000"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				Channel buffer size for task communication
			</p>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="update-interval">UI Update Interval (ms)</label>
			<input
				id="update-interval"
				type="number"
				bind:value={config.performance.update_interval}
				class={inputClass}
				min="50"
				max="1000"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				How often to update the UI (lower = more responsive but more CPU)
			</p>
		</div>
	</div>
</SettingsSection>
