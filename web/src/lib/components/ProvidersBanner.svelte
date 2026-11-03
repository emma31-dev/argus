<script lang="ts">
	import { monitor } from '$lib/monitor.svelte';
	import { formatAgo } from '$lib/format';
	import { hostOf, shortError } from '$lib/errors';
	import Banner from './Banner.svelte';

	const providers = $derived(monitor.status.data?.providers ?? []);
</script>

{#if monitor.providersDown && !monitor.offline}
	<Banner tone="warn" icon="offline" title="Argus can't reach any NEAR RPC provider">
		Every configured provider failed on its last request, so no new snapshots are being taken.
		{monitor.neverChecked
			? 'No round has completed yet, so there are no figures to show.'
			: `The last completed round was ${formatAgo(monitor.ageMs)}; figures below are from that round.`}
		<details class="prov">
			<summary>{providers.length} provider{providers.length === 1 ? '' : 's'} unreachable</summary>
			<ul>
				{#each providers as p (p.url + p.name)}
					<li>
						<span class="host mono">{hostOf(p.url)}</span>
						<span class="why">{shortError(p.last_error) || 'unhealthy'}</span>
						{#if p.last_error}<pre class="raw">{p.last_error}</pre>{/if}
					</li>
				{/each}
			</ul>
		</details>
	</Banner>
{/if}

<style>
	.prov {
		margin-top: var(--s-2);
	}
	summary {
		cursor: pointer;
		width: max-content;
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--text-2);
	}
	summary:hover {
		color: var(--text);
	}
	ul {
		margin-top: var(--s-2);
		display: grid;
		gap: var(--s-2);
	}
	li {
		display: grid;
		grid-template-columns: minmax(0, auto) minmax(0, 1fr);
		gap: 2px var(--s-3);
		align-items: baseline;
		padding-top: var(--s-2);
		border-top: 1px solid var(--warn-border);
	}
	.host {
		font-size: 12px;
		color: var(--text);
	}
	.why {
		font-size: var(--text-xs);
		color: var(--text-2);
	}
	.raw {
		grid-column: 1 / -1;
		margin: 2px 0 0;
		font-size: 11px;
		line-height: 1.5;
		color: var(--text-3);
		white-space: pre-wrap;
		overflow-wrap: anywhere;
	}
</style>
