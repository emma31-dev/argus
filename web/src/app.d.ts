// See https://svelte.dev/docs/kit/types#app.d.ts
declare global {
	/** Injected by vite.config.ts from PUBLIC_API_BASE ('' = same origin). */
	const __ARGUS_API_BASE__: string;

	namespace App {
		// interface Error {}
		// interface Locals {}
		// interface PageData {}
		// interface PageState {}
		// interface Platform {}
	}
}

export {};
