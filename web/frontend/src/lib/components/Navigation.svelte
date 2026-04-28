<script lang="ts">
	import { page } from '$app/stores';
	import { cubicOut } from 'svelte/easing';
	import { fly } from 'svelte/transition';
	import { FolderOpen, Settings, Film, Users, LogOut, Activity, FileText, ChevronDown, Sun, Moon, Monitor } from 'lucide-svelte';
	import { getThemeStore } from '$lib/stores/theme.svelte';
	import type { Theme } from '$lib/stores/theme.svelte';

	interface Props {
		authenticated?: boolean;
		username?: string;
		onLogout?: () => Promise<void> | void;
	}

	let { authenticated = false, username = '', onLogout }: Props = $props();

	const themeStore = getThemeStore();

	const navItems = [
		{ href: '/browse', label: 'Scrape', icon: FolderOpen },
		{ href: '/jobs', label: 'Jobs', icon: Activity },
		{ href: '/actresses', label: 'Actresses', icon: Users }
	];

	const subMenuItems = [
		{ href: '/logs', label: 'Logs', icon: FileText },
		{ href: '/settings', label: 'Settings', icon: Settings }
	];

	let subMenuOpen = $state(false);

	const currentPath = $derived($page.url.pathname);

	const isSubMenuActive = $derived(
		subMenuItems.some((item) => currentPath === item.href || currentPath.startsWith(item.href + '/'))
	);

	const themeIcon = $derived(
		themeStore.current === 'dark' ? Moon : themeStore.current === 'light' ? Sun : Monitor
	);

	const themeLabel = $derived(
		themeStore.current === 'dark' ? 'Dark' : themeStore.current === 'light' ? 'Light' : 'System'
	);

	function toggleSubMenu() {
		subMenuOpen = !subMenuOpen;
	}

	function closeSubMenu() {
		subMenuOpen = false;
	}

	function handleSubMenuClick() {
		closeSubMenu();
	}

	function handleClickOutside(event: MouseEvent) {
		const target = event.target as HTMLElement;
		if (!target.closest('[data-submenu]')) {
			closeSubMenu();
		}
	}
</script>

<svelte:window onclick={handleClickOutside} />

<nav
	class="sticky top-0 z-50 border-b bg-card/95 backdrop-blur supports-backdrop-filter:bg-card/80"
	in:fly|local={{ y: -10, duration: 220, easing: cubicOut }}
>
	<div class="container mx-auto px-4">
		<div class="flex items-center justify-between h-16">
			<!-- Logo -->
			<a href="/" class="flex items-center gap-2 font-bold text-xl transition-opacity duration-200 hover:opacity-80">
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

				<!-- Settings & Logs dropdown -->
				<div class="relative" data-submenu>
					<button
						type="button"
						onclick={toggleSubMenu}
						class="flex items-center gap-1.5 px-3 py-2 rounded-md transition-all duration-200 {isSubMenuActive
							? 'bg-primary text-primary-foreground shadow-sm -translate-y-0.5'
							: 'hover:bg-accent hover:-translate-y-px'}"
					>
						<Settings class="h-4 w-4" />
						<ChevronDown
							class="h-3 w-3 transition-transform duration-200 {subMenuOpen ? 'rotate-180' : ''}"
						/>
					</button>

					{#if subMenuOpen}
						<div
							class="absolute right-0 top-full mt-1 w-48 rounded-lg border bg-card p-1 shadow-lg"
							in:fly={{ y: -4, duration: 120 }}
						>
							<button
								type="button"
								onclick={() => themeStore.cycleTheme()}
								class="flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-all duration-150 hover:bg-accent hover:translate-x-0.5 w-full"
							>
								<svelte:component this={themeIcon} class="h-4 w-4" />
								<span>{themeLabel}</span>
							</button>

							<div class="my-1 border-t"></div>

							{#each subMenuItems as item}
								{@const Icon = item.icon}
								<a
									href={item.href}
									onclick={handleSubMenuClick}
									class="flex items-center gap-2.5 px-3 py-2 rounded-md text-sm transition-all duration-150 {currentPath ===
									item.href
										? 'bg-accent text-accent-foreground font-medium'
										: 'hover:bg-accent hover:translate-x-0.5'}"
								>
									<Icon class="h-4 w-4" />
									{item.label}
								</a>
							{/each}
						</div>
					{/if}
				</div>

				{#if authenticated}
					<button
						type="button"
						class="flex items-center gap-2 px-4 py-2 rounded-md transition-all duration-200 hover:bg-accent hover:-translate-y-px hover:text-destructive"
						onclick={() => onLogout?.()}
						title="Logout"
					>
						<LogOut class="h-4 w-4" />
						<span class="hidden md:inline">{username ? `${username} · Logout` : 'Logout'}</span>
					</button>
				{/if}
			</div>
		</div>
	</div>
</nav>
