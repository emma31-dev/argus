import { api, type StatusResponse } from './api';
import { clock, Resource } from './live.svelte';

/**
 * App-wide monitor status. The layout polls /v1/status every
 * `interval_seconds`; pages read from here instead of fetching it again.
 */
class Monitor {
	status = new Resource<StatusResponse>((s) => api.status(s));

	intervalMs = $derived((this.status.data?.interval_seconds ?? 30) * 1000);

	/** No snapshot round has ever completed (last_checked_at is null / last_block is 0). */
	neverChecked = $derived(!!this.status.data && (!this.status.data.last_checked_at || !this.status.data.last_block));

	/** Milliseconds since the last completed snapshot round. */
	ageMs = $derived.by(() => {
		const at = this.status.data?.last_checked_at;
		if (!at) return null;
		const t = new Date(at).getTime();
		return Number.isNaN(t) ? null : Math.max(0, clock.now - t);
	});

	/**
	 * The monitor has stopped producing fresh rounds. The API's own
	 * `monitor_stale` is authoritative; our 3x-interval check is a fallback
	 * for older API versions and for the time between polls.
	 */
	stale = $derived(
		!!this.status.data &&
			(this.status.data.monitor_stale === true ||
				this.neverChecked ||
				(this.ageMs !== null && this.ageMs > 3 * this.intervalMs))
	);

	/** Every configured RPC provider reported unhealthy. */
	providersDown = $derived.by(() => {
		const p = this.status.data?.providers ?? [];
		return p.length > 0 && p.every((x) => !x.healthy);
	});

	/** The latest status request failed because the API is unreachable. */
	offline = $derived(this.status.error !== null && this.status.error.offline);

	/** What the header pill says. */
	pill = $derived.by<'live' | 'stale' | 'waiting' | 'offline' | 'loading'>(() => {
		if (this.offline) return 'offline';
		if (!this.status.data) return 'loading';
		if (this.neverChecked) return 'waiting';
		if (this.stale) return 'stale';
		return 'live';
	});
}

export const monitor = new Monitor();
