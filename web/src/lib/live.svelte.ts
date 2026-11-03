import { ApiError } from './api';

/** A wall clock that ticks once per second, shared by every component. */
class Clock {
	now = $state(Date.now());
	constructor() {
		if (typeof window !== 'undefined') {
			setInterval(() => (this.now = Date.now()), 1000);
		}
	}
}
export const clock = new Clock();

/**
 * One API resource: keeps the last good value while refetching (so the UI
 * never flashes back to a skeleton), and exposes the last error separately.
 */
export class Resource<T> {
	data = $state.raw<T | null>(null);
	error = $state.raw<ApiError | null>(null);
	loading = $state(false);
	/** True once the first request has settled (successfully or not). */
	settled = $state(false);
	fetchedAt = $state<number | null>(null);

	#fetcher: (signal: AbortSignal) => Promise<T>;
	#ctrl: AbortController | null = null;

	constructor(fetcher: (signal: AbortSignal) => Promise<T>) {
		this.#fetcher = fetcher;
	}

	async load(): Promise<void> {
		this.#ctrl?.abort();
		const ctrl = new AbortController();
		this.#ctrl = ctrl;
		this.loading = true;
		try {
			const value = await this.#fetcher(ctrl.signal);
			if (ctrl.signal.aborted) return;
			this.data = value;
			this.error = null;
			this.fetchedAt = Date.now();
		} catch (e) {
			if (ctrl.signal.aborted) return;
			this.error = e instanceof ApiError ? e : new ApiError(0, e instanceof Error ? e.message : String(e));
		} finally {
			if (this.#ctrl === ctrl) {
				this.loading = false;
				this.settled = true;
			}
		}
	}

	/** Forget the current value (e.g. when the selected id changes). */
	reset() {
		this.#ctrl?.abort();
		this.data = null;
		this.error = null;
		this.settled = false;
		this.loading = false;
	}

	abort() {
		this.#ctrl?.abort();
	}
}

/**
 * Run `tick` now and then every `intervalMs()` milliseconds, re-reading the
 * interval each time (it comes from /v1/status). Pauses while the tab is
 * hidden and refreshes immediately when it becomes visible again.
 * Returns a cleanup function, so it can be returned from $effect.
 */
export function poll(tick: () => unknown, intervalMs: () => number): () => void {
	let timer: ReturnType<typeof setTimeout> | undefined;
	let stopped = false;
	const schedule = () => {
		clearTimeout(timer);
		if (stopped) return;
		timer = setTimeout(run, Math.max(5_000, intervalMs()));
	};
	const run = async () => {
		if (stopped) return;
		if (typeof document !== 'undefined' && document.visibilityState === 'hidden') return;
		await tick();
		schedule();
	};
	const onVisible = () => {
		if (document.visibilityState === 'visible') run();
	};
	run();
	document.addEventListener('visibilitychange', onVisible);
	return () => {
		stopped = true;
		clearTimeout(timer);
		document.removeEventListener('visibilitychange', onVisible);
	};
}
