<script lang="ts">
	import { page } from '$app/stores';
	import { cubicOut } from 'svelte/easing';
	import { fly } from 'svelte/transition';
	import { House, FolderOpen, Settings, History, Film, Users } from 'lucide-svelte';

	const navItems = [
		{ href: '/', label: 'Home', icon: House },
		{ href: '/browse', label: 'Browse & Scrape', icon: FolderOpen },
		{ href: '/actresses', label: 'Actresses', icon: Users },
		{ href: '/settings', label: 'Settings', icon: Settings },
		{ href: '/history', label: 'History', icon: History }
	];

	const currentPath = $derived($page.url.pathname);
</script>

<nav
	class="sticky top-0 z-50 border-b bg-card/95 backdrop-blur supports-backdrop-filter:bg-card/80"
	in:fly|local={{ y: -10, duration: 220, easing: cubicOut }}
>
	<div class="container mx-auto px-4">
		<div class="flex items-center justify-between h-16">
			<!-- Logo -->
			<a href="/" class="flex items-center gap-2 font-bold text-xl">
				<Film class="h-6 w-6 text-primary" />
				<span>Javinizer</span>
			</a>

			<!-- Nav Links -->
			<div class="flex items-center gap-1">
				{#each navItems as item}
					{@const Icon = item.icon}
					<a
						href={item.href}
						class="flex items-center gap-2 px-4 py-2 rounded-md transition-all duration-200 {currentPath ===
						item.href
							? 'bg-primary text-primary-foreground shadow-sm -translate-y-0.5'
							: 'hover:bg-accent hover:-translate-y-px'}"
					>
						<Icon class="h-4 w-4" />
						<span class="hidden md:inline">{item.label}</span>
					</a>
				{/each}
			</div>
		</div>
	</div>
</nav>
