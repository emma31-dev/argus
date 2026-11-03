import { api, type StatusResponse } from './api';
import { clock, Resource } from './live.svelte';

/**
 * App-wide monitor status. The layout polls /v1/status every
 * `interval_seconds`; pages read from here instead of fetching it again.
 */
class Monitor {
	status = new Resource<StatusResponse>((s) => api.status(s));

	intervalMs = $derived((this.status.data?.interval_seconds ?? 30) * 1000);

	/** Milliseconds since the last completed snapshot round. */
	ageMs = $derived.by(() => {
		const at = this.status.data?.last_checked_at;
		if (!at) return null;
		const t = new Date(at).getTime();
		return Number.isNaN(t) ? null : Math.max(0, clock.now - t);
	});

	/** Data older than 3x the check interval is flagged as stale. */
	stale = $derived(this.ageMs !== null && this.ageMs > 3 * this.intervalMs);

	/** No successful response, or the latest request failed. */
	offline = $derived(this.status.error !== null && this.status.error.offline);
}

export const monitor = new Monitor();
