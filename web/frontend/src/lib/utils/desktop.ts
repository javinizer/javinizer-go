import { browser } from '$app/environment';

// isDesktopApp detects the Wails desktop webview: macOS loads the SPA from
// the custom wails:// scheme, Windows/Linux from http(s)://wails.localhost.
// Webviews lack browser plumbing the SPA relies on (blob downloads, native
// confirm()), so desktop-only fallbacks branch on this.
export function isDesktopApp(): boolean {
	if (!browser) return false;
	if (location.protocol === 'wails:') return true;
	return location.hostname === 'wails.localhost';
}
