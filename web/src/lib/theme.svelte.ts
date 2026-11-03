export type Theme = 'light' | 'dark';
const KEY = 'argus-theme';

function read(): Theme | null {
	try {
		const v = localStorage.getItem(KEY);
		return v === 'light' || v === 'dark' ? v : null;
	} catch {
		return null;
	}
}

function systemTheme(): Theme {
	if (typeof window === 'undefined' || !window.matchMedia) return 'light';
	return window.matchMedia('(prefers-color-scheme: dark)').matches ? 'dark' : 'light';
}

class ThemeState {
	/** Explicit choice, or null to follow the OS. */
	choice = $state<Theme | null>(null);
	system = $state<Theme>('light');
	resolved = $derived<Theme>(this.choice ?? this.system);

	init() {
		this.choice = read();
		this.system = systemTheme();
		const mq = window.matchMedia?.('(prefers-color-scheme: dark)');
		mq?.addEventListener('change', (e) => (this.system = e.matches ? 'dark' : 'light'));
	}

	toggle() {
		const next: Theme = this.resolved === 'dark' ? 'light' : 'dark';
		this.choice = next;
		document.documentElement.dataset.theme = next;
		try {
			localStorage.setItem(KEY, next);
		} catch {
			/* storage unavailable: the choice lasts for this page view */
		}
	}
}

export const theme = new ThemeState();
