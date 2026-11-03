<script lang="ts">
	import { formatAgo, formatDuration } from '$lib/format';
	import { monitor } from '$lib/monitor.svelte';
	import Banner from './Banner.svelte';

	/** Set where ProvidersBanner is shown, so the same cause isn't reported twice. */
	let { deferToProviders = false }: { deferToProviders?: boolean } = $props();
	const show = $derived(monitor.stale && !monitor.offline && !(deferToProviders && monitor.providersDown));
</script>

{#if show}
	{#if monitor.neverChecked}
		<Banner tone="info" icon="clock" title="Waiting for the first snapshot round">
			The monitor is up but has not completed a same-block read yet, so there are no figures to show. A round normally
			takes about one {formatDuration(monitor.intervalMs / 1000)} interval.
		</Banner>
	{:else}
		<Banner tone="warn" title="Data may be stale">
			The last completed check was {formatAgo(monitor.ageMs)}, well past the {formatDuration(monitor.intervalMs / 1000)}
			interval. The monitor may be behind or its RPC providers may be failing; treat the figures below as out of date.
		</Banner>
	{/if}
{/if}
