<script lang="ts">
	import { X } from 'lucide-svelte';

	interface Props {
		show: boolean;
		videoUrl: string;
		title?: string;
		onClose: () => void;
	}

	let { show = $bindable(false), videoUrl, title, onClose }: Props = $props();

	function close() {
		show = false;
		onClose();
	}

	// Keyboard navigation
	$effect(() => {
		if (!show) return;

		function handleKeyDown(e: KeyboardEvent) {
			if (e.key === 'Escape') {
				close();
			}
		}

		window.addEventListener('keydown', handleKeyDown);
		return () => window.removeEventListener('keydown', handleKeyDown);
	});
</script>

{#if show && videoUrl}
	<div class="fixed inset-0 z-50 flex items-center justify-center p-4">
		<!-- Backdrop button -->
		<button
			onclick={close}
			class="absolute inset-0 bg-black/90 cursor-default"
			aria-label="Close video modal"
		></button>

		<!-- Modal content -->
		<div class="relative w-full max-w-4xl z-10" role="dialog" aria-modal="true" tabindex="-1">
			<!-- Close Button -->
			<button
				onclick={close}
				class="absolute -top-12 right-0 p-2 bg-black/50 hover:bg-black/70 rounded-full text-white transition-colors"
				title="Close (Esc)"
			>
				<X class="h-6 w-6" />
			</button>

			<!-- Title (optional) -->
			{#if title}
				<div class="absolute -top-12 left-0 px-3 py-2 bg-black/50 rounded text-white text-sm">
					{title}
				</div>
			{/if}

			<!-- Video -->
			<!-- svelte-ignore a11y_media_has_caption -->
			<video controls class="w-full rounded" src={videoUrl}>
				<track kind="captions" />
				Your browser does not support the video tag.
			</video>
		</div>
	</div>
{/if}
