export type Theme = 'light' | 'dark' | 'system';

const STORAGE_KEY = 'javinizer-theme';

let current: Theme = $state<Theme>('system');
let resolved: 'light' | 'dark' = $state<'light' | 'dark'>(
	typeof document !== 'undefined' && document.documentElement.classList.contains('dark')
		? 'dark'
		: 'light',
);
let mediaQueryHandler: (() => void) | null = null;
let mediaQueryList: MediaQueryList | null = null;

function getSystemPreference(): 'light' | 'dark' {
	if (typeof window === 'undefined') return 'light';
	return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

function applyTheme(theme: Theme): void {
	resolved = theme === 'system' ? getSystemPreference() : theme;

	if (typeof document !== 'undefined') {
		const html = document.documentElement;
		if (resolved === 'dark') {
			html.classList.add('dark');
		} else {
			html.classList.remove('dark');
		}
	}
}

function persist(theme: Theme): void {
	if (typeof localStorage !== 'undefined') {
		if (theme === 'system') {
			localStorage.removeItem(STORAGE_KEY);
		} else {
			localStorage.setItem(STORAGE_KEY, theme);
		}
	}
}

function load(): Theme {
	if (typeof localStorage === 'undefined') return 'system';
	const stored = localStorage.getItem(STORAGE_KEY);
	if (stored === 'light' || stored === 'dark') return stored;
	return 'system';
}

function setTheme(theme: Theme): void {
	current = theme;
	persist(theme);
	applyTheme(theme);
}

function initTheme(): void {
	current = load();
	applyTheme(current);

	if (typeof window !== 'undefined') {
		mediaQueryList ??= window.matchMedia('(prefers-color-scheme: dark)');
		if (mediaQueryHandler) {
			mediaQueryList.removeEventListener('change', mediaQueryHandler);
		}
		const handler = () => {
			if (current === 'system') {
				applyTheme('system');
			}
		};
		mediaQueryHandler = handler;
		mediaQueryList.addEventListener('change', handler);
	}
}

function destroyTheme(): void {
	if (typeof window !== 'undefined' && mediaQueryHandler && mediaQueryList) {
		mediaQueryList.removeEventListener('change', mediaQueryHandler);
		mediaQueryHandler = null;
	}
}

function cycleTheme(): void {
	const next: Record<Theme, Theme> = { light: 'dark', dark: 'system', system: 'light' };
	setTheme(next[current]);
}

export function getThemeStore() {
	return {
		get current() {
			return current;
		},
		get resolved() {
			return resolved;
		},
		setTheme,
		initTheme,
		destroyTheme,
		cycleTheme,
	};
}
