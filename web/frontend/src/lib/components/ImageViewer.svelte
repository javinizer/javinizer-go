<script lang="ts">
	import { ChevronLeft, ChevronRight, X, ZoomIn, ZoomOut } from 'lucide-svelte';

	interface Props {
		show: boolean;
		images: string[]; // Array of image URLs
		initialIndex?: number;
		title?: string;
		onClose: () => void;
	}

	let { show = $bindable(false), images, initialIndex = 0, title, onClose }: Props = $props();

	let currentIndex = $state(0);
	let zoom = $state(1); // Changed to decimal scale (1 = 100%)
	let panX = $state(0);
	let panY = $state(0);
	let isDragging = $state(false);
	let dragStartX = $state(0);
	let dragStartY = $state(0);
	let hasDragged = $state(false); // Track if user has dragged
	let pointerDownTracking = $state(false); // Track if pointer down was initiated
	let imageContainer: HTMLDivElement | undefined = $state();

	// Reset state when modal opens or image changes
	$effect(() => {
		if (show) {
			currentIndex = initialIndex;
			zoom = 1;
			panX = 0;
			panY = 0;
		}
	});

	function close() {
		show = false;
		onClose();
	}

	function nextImage() {
		if (currentIndex < images.length - 1) {
			currentIndex++;
			// Keep zoom level, but reset pan position for new image
			panX = 0;
			panY = 0;
		}
	}

	function prevImage() {
		if (currentIndex > 0) {
			currentIndex--;
			// Keep zoom level, but reset pan position for new image
			panX = 0;
			panY = 0;
		}
	}

	function zoomIn() {
		zoom = Math.min(zoom + 0.25, 3); // Max 300%
	}

	function zoomOut() {
		zoom = Math.max(zoom - 0.25, 0.5); // Min 50%
		// Reset pan if zooming out to 100% or less
		if (zoom <= 1) {
			panX = 0;
			panY = 0;
		}
	}

	function resetZoom() {
		zoom = 1;
		panX = 0;
		panY = 0;
	}

	// Wheel zoom support
	function handleWheel(e: WheelEvent) {
		if (e.ctrlKey || e.metaKey) {
			e.preventDefault();
			const delta = e.deltaY > 0 ? -0.1 : 0.1;
			zoom = Math.min(Math.max(zoom + delta, 0.5), 3);

			// Reset pan if zooming to 100% or less
			if (zoom <= 1) {
				panX = 0;
				panY = 0;
			}
		}
	}

	// Pan support with mouse/touch
	function handlePointerDown(e: PointerEvent) {
		if (zoom > 1) {
			pointerDownTracking = true;
			hasDragged = false; // Reset drag state
			dragStartX = e.clientX;
			dragStartY = e.clientY;
			// Don't capture pointer yet - wait for actual movement
		} else {
			pointerDownTracking = false;
		}
	}

	function handlePointerMove(e: PointerEvent) {
		// Only track movement if pointer down was initiated on the container (not on image)
		if (zoom > 1 && pointerDownTracking) {
			const deltaX = Math.abs(e.clientX - dragStartX);
			const deltaY = Math.abs(e.clientY - dragStartY);

			// Only start dragging if moved more than 5 pixels
			if (deltaX > 5 || deltaY > 5) {
				if (!isDragging) {
					// First time we detect actual movement, start dragging and capture pointer
					isDragging = true;
					dragStartX = e.clientX - panX;
					dragStartY = e.clientY - panY;
					(e.currentTarget as HTMLElement).setPointerCapture(e.pointerId);
				}
				hasDragged = true;
				panX = e.clientX - dragStartX;
				panY = e.clientY - dragStartY;
			}
		}
	}

	function handlePointerUp(e: PointerEvent) {
		pointerDownTracking = false;
		if (isDragging) {
			isDragging = false;
			try {
				(e.currentTarget as HTMLElement).releasePointerCapture(e.pointerId);
			} catch (err) {
				// Ignore errors if pointer wasn't captured
			}
		}
	}

	// Handle click on container (empty area)
	function handleContainerClick(e: MouseEvent) {
		// Only close if:
		// 1. Clicked on the container itself (not a child element like the image)
		// 2. Not dragging/panning
		// 3. Target is not an IMG element
		const target = e.target as HTMLElement;
		if (e.target === e.currentTarget && !hasDragged && target.tagName !== 'IMG') {
			close();
		}
	}

	// Keyboard handler for accessibility
	function handleContainerKeyDown(e: KeyboardEvent) {
		// Close on Enter or Space when focused on container
		if ((e.key === 'Enter' || e.key === ' ') && e.target === e.currentTarget) {
			e.preventDefault();
			close();
		}
	}

	// Handle image click for zoom
	function handleImageClick(e: MouseEvent) {
		// Prevent both default behavior and propagation
		e.preventDefault();
		e.stopPropagation();
		e.stopImmediatePropagation();

		// Don't zoom if user was dragging (panning)
		if (hasDragged) {
			hasDragged = false; // Reset for next interaction
			return;
		}

		// Toggle between normal and zoomed state
		if (zoom === 1) {
			// Zoom in by 50% (to 150%)
			zoom = 1.5;
		} else {
			// If already zoomed, reset to 100%
			resetZoom();
		}

		// Reset drag state after zoom
		hasDragged = false;
	}

	// Keyboard handler for image zoom
	function handleImageKeyDown(e: KeyboardEvent) {
		// Prevent event from bubbling
		e.stopPropagation();

		// Zoom on Enter or Space
		if (e.key === 'Enter' || e.key === ' ') {
			e.preventDefault();
			if (zoom === 1) {
				zoom = 1.5;
			} else {
				resetZoom();
			}
		}
	}

	const zoomPercent = $derived(Math.round(zoom * 100));
	const containerCursor = $derived(isDragging ? 'grabbing' : zoom > 1 ? 'grab' : 'default');
	const imageCursor = $derived(isDragging ? 'grabbing' : zoom > 1 ? 'grab' : 'zoom-in');

	// Keyboard navigation
	$effect(() => {
		if (!show) return;

		function handleKeyDown(e: KeyboardEvent) {
			switch (e.key) {
				case 'Escape':
					close();
					break;
				case 'ArrowLeft':
					prevImage();
					break;
				case 'ArrowRight':
					nextImage();
					break;
				case '+':
				case '=':
					zoomIn();
					break;
				case '-':
					zoomOut();
					break;
				case '0':
					resetZoom();
					break;
			}
		}

		window.addEventListener('keydown', handleKeyDown);
		return () => window.removeEventListener('keydown', handleKeyDown);
	});

	const currentImage = $derived(images[currentIndex]);
	const hasMultipleImages = $derived(images.length > 1);
</script>

{#if show && currentImage}
	<div class="fixed inset-0 z-50 flex items-center justify-center">
		<!-- Backdrop button -->
		<button
			onclick={close}
			class="absolute inset-0 bg-black/90 cursor-default"
			aria-label="Close viewer"
		></button>

		<!-- Modal content -->
		<div
			class="relative w-full h-full flex items-center justify-center p-4 z-10"
			role="dialog"
			aria-modal="true"
			tabindex="-1"
		>
			<!-- Close Button -->
			<button
				onclick={close}
				class="absolute top-4 right-4 z-10 p-2 bg-black/50 hover:bg-black/70 rounded-full text-white transition-colors"
				title="Close (Esc)"
			>
				<X class="h-6 w-6" />
			</button>

			<!-- Title or Counter -->
			<div class="absolute top-4 left-4 z-10 px-3 py-2 bg-black/50 rounded text-white text-sm">
				{#if title}
					{title}
				{:else if hasMultipleImages}
					{currentIndex + 1} / {images.length}
				{/if}
			</div>

			<!-- Zoom Controls -->
			<div
				class="absolute top-4 left-1/2 -translate-x-1/2 z-10 flex items-center gap-2 bg-black/50 rounded px-3 py-2"
			>
				<button
					onclick={zoomOut}
					disabled={zoom <= 0.5}
					class="p-1 text-white hover:bg-white/10 rounded disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
					title="Zoom Out (-)"
				>
					<ZoomOut class="h-5 w-5" />
				</button>
				<button
					onclick={resetZoom}
					class="px-2 py-1 text-white hover:bg-white/10 rounded text-sm transition-colors"
					title="Reset Zoom (0)"
				>
					{zoomPercent}%
				</button>
				<button
					onclick={zoomIn}
					disabled={zoom >= 3}
					class="p-1 text-white hover:bg-white/10 rounded disabled:opacity-30 disabled:cursor-not-allowed transition-colors"
					title="Zoom In (+)"
				>
					<ZoomIn class="h-5 w-5" />
				</button>
			</div>

			<!-- Previous Button -->
			{#if hasMultipleImages && currentIndex > 0}
				<button
					onclick={prevImage}
					class="absolute left-4 top-1/2 -translate-y-1/2 z-10 p-3 bg-black/50 hover:bg-black/70 rounded-full text-white transition-colors cursor-pointer"
					title="Previous (←)"
				>
					<ChevronLeft class="h-8 w-8" />
				</button>
			{/if}

			<!-- Next Button -->
			{#if hasMultipleImages && currentIndex < images.length - 1}
				<button
					onclick={nextImage}
					class="absolute right-4 top-1/2 -translate-y-1/2 z-10 p-3 bg-black/50 hover:bg-black/70 rounded-full text-white transition-colors cursor-pointer"
					title="Next (→)"
				>
					<ChevronRight class="h-8 w-8" />
				</button>
			{/if}

			<!-- Image Container with Pan Support -->
			<div
				bind:this={imageContainer}
				class="absolute inset-0 flex items-center justify-center overflow-hidden"
				role="button"
				tabindex="0"
				onwheel={handleWheel}
				onpointerdown={handlePointerDown}
				onpointermove={handlePointerMove}
				onpointerup={handlePointerUp}
				onpointercancel={handlePointerUp}
				onclick={handleContainerClick}
				onkeydown={handleContainerKeyDown}
				style="cursor: {containerCursor};"
			>
				<!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
				<!-- svelte-ignore a11y_click_events_have_key_events -->
				<!-- svelte-ignore a11y_no_noninteractive_tabindex -->
				<img
					src={currentImage}
					alt={title || `Image ${currentIndex + 1}`}
					tabindex="0"
					onclick={handleImageClick}
					onkeydown={handleImageKeyDown}
					style="transform: scale({zoom}) translate({panX}px, {panY}px); transition: {isDragging ? 'none' : 'transform 0.1s ease-out'}; user-select: none; cursor: {imageCursor};"
					class="max-w-full max-h-full object-contain"
					draggable="false"
					aria-label={zoom === 1 ? 'Click to zoom in' : 'Click to zoom out or drag to pan'}
				/>
			</div>
		</div>
	</div>
{/if}
