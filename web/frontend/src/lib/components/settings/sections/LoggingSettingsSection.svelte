<script lang="ts">
	import { untrack } from 'svelte';
	import SettingsSection from '$lib/components/settings/SettingsSection.svelte';
	import SettingsSubsection from '$lib/components/settings/SettingsSubsection.svelte';
	import type { SettingsConfig } from '$lib/api/types';

	interface Props {
		config: SettingsConfig;
		inputClass: string;
	}

	let { config, inputClass }: Props = $props();
	let logging = $derived(config.logging);

	function coerceToInt(value: string | number): number {
		if (typeof value === 'number') return value < 0 ? 0 : value;
		const num = parseInt(value, 10);
		if (isNaN(num) || num < 0) return 0;
		return num;
	}

	function ensureLoggingDefaults(cfg: SettingsConfig): void {
		if (!cfg.logging) cfg.logging = {};
		cfg.logging.level ??= 'info';
		cfg.logging.format ??= 'text';
		cfg.logging.output ??= 'stdout';
		cfg.logging.max_size_mb ??= 0;
		cfg.logging.max_backups ??= 0;
		cfg.logging.max_age_days ??= 0;
		cfg.logging.compress ??= false;
	}

	$effect(() => {
		if (config) {
			untrack(() => ensureLoggingDefaults(config));
		}
	});
</script>

<SettingsSection title="Logging Settings" description="Configure logging level, format, and output destination" defaultExpanded={false}>
	<div class="space-y-4">
		<div>
			<label class="block text-sm font-medium mb-2" for="log-level">Log Level</label>
			<select id="log-level" bind:value={logging.level} class={inputClass}>
				<option value="debug">Debug</option>
				<option value="info">Info</option>
				<option value="warn">Warning</option>
				<option value="error">Error</option>
			</select>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="log-format">Log Format</label>
			<select id="log-format" bind:value={logging.format} class={inputClass}>
				<option value="text">Text</option>
				<option value="json">JSON</option>
			</select>
		</div>

		<div>
			<label class="block text-sm font-medium mb-2" for="log-output">Log Output</label>
			<input
				id="log-output"
				type="text"
				bind:value={logging.output}
				class={inputClass}
				placeholder="stdout or file path"
			/>
			<p class="text-xs text-muted-foreground mt-1">
				Use "stdout" for console, file path, or comma-separated (e.g., "stdout,data/logs/javinizer.log")
			</p>
		</div>

		<SettingsSubsection title="Log Rotation" description="Automatically rotate log files when they grow too large">
			<div class="space-y-4">
				<div>
					<label class="block text-sm font-medium mb-2" for="log-max-size">Max Size (MB)</label>
					<input
						id="log-max-size"
						type="number"
						value={logging.max_size_mb}
						oninput={(e) => { logging.max_size_mb = coerceToInt((e.target as HTMLInputElement).value); }}
						class={inputClass}
						min="0"
						placeholder="10"
					/>
					<p class="text-xs text-muted-foreground mt-1">
						Maximum file size before rotation (0 = disabled)
					</p>
				</div>

				<div>
					<label class="block text-sm font-medium mb-2" for="log-max-backups">Max Backups</label>
					<input
						id="log-max-backups"
						type="number"
						value={logging.max_backups}
						oninput={(e) => { logging.max_backups = coerceToInt((e.target as HTMLInputElement).value); }}
						class={inputClass}
						min="0"
						placeholder="5"
					/>
					<p class="text-xs text-muted-foreground mt-1">
						Number of old log files to keep (0 = unlimited)
					</p>
				</div>

				<div>
					<label class="block text-sm font-medium mb-2" for="log-max-age">Max Age (days)</label>
					<input
						id="log-max-age"
						type="number"
						value={logging.max_age_days}
						oninput={(e) => { logging.max_age_days = coerceToInt((e.target as HTMLInputElement).value); }}
						class={inputClass}
						min="0"
						placeholder="0"
					/>
					<p class="text-xs text-muted-foreground mt-1">
						Maximum age in days to keep log files (0 = no limit)
					</p>
				</div>

				<div class="flex items-center gap-2">
					<input
						id="log-compress"
						type="checkbox"
						bind:checked={logging.compress}
						class="w-4 h-4"
					/>
					<label class="text-sm font-medium" for="log-compress">Compress rotated files</label>
				</div>
			</div>
		</SettingsSubsection>
	</div>
</SettingsSection>
