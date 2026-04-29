<script lang="ts">
	import { ChevronLeft, ChevronRight } from 'lucide-svelte';
	import Card from '$lib/components/ui/Card.svelte';
	import Button from '$lib/components/ui/Button.svelte';

	let {
		currentPage,
		totalPages,
		canGoPrev,
		canGoNext,
		onPrevPage,
		onNextPage
	}: {
		currentPage: number;
		totalPages: number;
		canGoPrev: boolean;
		canGoNext: boolean;
		onPrevPage: () => void;
		onNextPage: () => void;
	} = $props();
</script>

<Card class="p-3">
	<div class="flex items-center justify-between text-sm">
		<div class="text-muted-foreground">
			Page {currentPage} of {totalPages}
		</div>
		<div class="flex items-center gap-2">
			<Button variant="outline" size="sm" onclick={onPrevPage} disabled={!canGoPrev}>
				<ChevronLeft class="h-4 w-4" />
				Prev
			</Button>
			<Button variant="outline" size="sm" onclick={onNextPage} disabled={!canGoNext}>
				Next
				<ChevronRight class="h-4 w-4" />
			</Button>
		</div>
	</div>
</Card>
