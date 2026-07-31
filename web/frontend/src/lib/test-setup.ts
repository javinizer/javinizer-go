// jsdom does not implement window.matchMedia, which Svelte's MediaQuery
// (svelte/reactivity) and components that check prefers-reduced-motion depend
// on. Provide a no-op stub that reports no matches and never fires listeners.
if (typeof window !== 'undefined' && !window.matchMedia) {
	window.matchMedia = (query: string): MediaQueryList => ({
		matches: false,
		media: query,
		onchange: null,
		addEventListener: () => {},
		removeEventListener: () => {},
		addListener: () => {},
		removeListener: () => {},
		dispatchEvent: () => false,
	});
}