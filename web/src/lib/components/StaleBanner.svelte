<script lang="ts">
	import { formatAgo, formatDuration } from '$lib/format';
	import { monitor } from '$lib/monitor.svelte';
	import Banner from './Banner.svelte';
</script>

{#if monitor.stale && !monitor.offline}
	<Banner tone="warn" title="Data may be stale">
		The last completed check was {formatAgo(monitor.ageMs)}, more than three times the
		{formatDuration(monitor.intervalMs / 1000)} interval. The monitor may be behind or its RPC providers
		may be failing; treat the figures below as out of date.
	</Banner>
{/if}
