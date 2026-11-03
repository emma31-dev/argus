<script lang="ts">
	import { onMount } from 'svelte';
	import { api, type IncidentsResponse, type TokensResponse } from '$lib/api';
	import { monitor } from '$lib/monitor.svelte';
	import { poll, Resource } from '$lib/live.svelte';
	import { formatAmount, formatInteger } from '$lib/format';
	import { href } from '$lib/links';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import StatusHero from '$lib/components/StatusHero.svelte';
	import TokenTable from '$lib/components/TokenTable.svelte';
	import OfflineBanner from '$lib/components/OfflineBanner.svelte';
	import StaleBanner from '$lib/components/StaleBanner.svelte';
	import BlockLink from '$lib/components/BlockLink.svelte';
	import Ago from '$lib/components/Ago.svelte';
	import Icon from '$lib/components/Icon.svelte';
	import ChainChip from '$lib/components/ChainChip.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';

	const tokens = new Resource<TokensResponse>((s) => api.tokens(s));
	const openIncidents = new Resource<IncidentsResponse>((s) => api.incidents('open', 10, s));

	const status = $derived(monitor.status.data);
	const error = $derived(tokens.error ?? monitor.status.error);
	const hasOpen = $derived((status?.open_incidents ?? 0) > 0);

	onMount(() =>
		poll(
			() => Promise.all([tokens.load(), hasOpen ? openIncidents.load() : null]),
			() => monitor.intervalMs
		)
	);

	$effect(() => {
		if (hasOpen && !openIncidents.settled) openIncidents.load();
	});

	async function retry() {
		await Promise.all([monitor.status.load(), tokens.load()]);
	}
</script>

<svelte:head>
	<title>Argus · NEAR Intents solvency monitor</title>
</svelte:head>

<div class="container">
	<PageHeader eyebrow="NEAR Intents · intents.near" title="Is every token backed?">
		Argus reads, at the same block, what the NEAR Intents ledger owes its users and what the contract
		actually holds, for every token. If the ledger ever owes more than it holds, it shows here.
	</PageHeader>

	<div class="stack">
		{#if error && (monitor.status.settled || tokens.settled)}
			<OfflineBanner {error} {retry} hasData={!!status} />
		{/if}
		<StaleBanner />

		{#if status}
			<StatusHero {status} />
		{:else if !error}
			<div class="card hero-sk" aria-busy="true" aria-label="Loading status">
				<div>
					<span class="skeleton" style="width:160px;height:12px"></span>
					<span class="skeleton" style="width:min(420px,80%);height:34px;margin-top:22px"></span>
					<span class="skeleton" style="width:min(360px,70%);height:12px;margin-top:16px"></span>
					<span class="skeleton" style="width:100%;height:8px;margin-top:48px;border-radius:99px"></span>
				</div>
				<div class="hero-sk-grid">
					{#each Array(4) as _, i (i)}
						<div>
							<span class="skeleton" style="width:90px;height:10px"></span>
							<span class="skeleton" style="width:120px;height:24px;margin-top:12px"></span>
						</div>
					{/each}
				</div>
			</div>
		{/if}

		{#if hasOpen && openIncidents.data?.incidents.length}
			<section class="incidents" aria-label="Open incidents">
				{#each openIncidents.data.incidents as inc (inc.id)}
					<a class="inc" href={href.incident(inc.id)}>
						<span class="inc-icon"><Icon name="alert" size={16} stroke={2} /></span>
						<span class="inc-body">
							<span class="inc-title"
								>Open incident #{inc.id} · <strong>{inc.symbol}</strong> <ChainChip id={inc.token_id} /> ledger owes
								<span class="num">{formatAmount(inc.max_deficit, inc.decimals)}</span>
								{inc.symbol} more than intents.near holds</span
							>
							<span class="inc-meta">
								Negative since block <BlockLink height={inc.start_block} />, red at
								<BlockLink height={inc.red_block} /> · opened <Ago iso={inc.opened_at} />
							</span>
						</span>
						<span class="inc-go">View <Icon name="arrowRight" size={14} /></span>
					</a>
				{/each}
			</section>
		{/if}
	</div>

	<section class="tokens" aria-labelledby="tokens-h">
		<div class="sec-head">
			<h2 id="tokens-h">Tokens</h2>
			{#if tokens.data}
				<p class="muted">
					Latest snapshot at block <BlockLink height={tokens.data.block_height} /> ·
					{formatInteger(tokens.data.tokens.length)} tracked
				</p>
			{/if}
		</div>
		{#if !tokens.data && tokens.error && tokens.settled}
			<div class="card">
				<EmptyState icon="offline" title="No data to show">
					Token figures appear here as soon as the monitor answers. Argus never shows placeholder numbers.
					{#snippet actions()}
						<button class="btn" type="button" onclick={retry}><Icon name="refresh" size={14} />Retry</button>
					{/snippet}
				</EmptyState>
			</div>
		{:else}
			<TokenTable tokens={tokens.data?.tokens ?? null} loading={tokens.loading} />
		{/if}
	</section>
</div>

<style>
	.stack {
		display: grid;
		gap: var(--s-4);
	}
	.hero-sk {
		display: grid;
		grid-template-columns: 1.1fr 1fr;
		min-height: 300px;
	}
	.hero-sk > div:first-child {
		padding: var(--s-8);
		display: flex;
		flex-direction: column;
	}
	.hero-sk-grid {
		display: grid;
		grid-template-columns: 1fr 1fr;
		border-left: 1px solid var(--border);
		background: var(--surface-2);
	}
	.hero-sk-grid > div {
		padding: var(--s-6);
	}
	@media (max-width: 960px) {
		.hero-sk {
			grid-template-columns: 1fr;
		}
		.hero-sk-grid {
			border-left: 0;
			border-top: 1px solid var(--border);
		}
	}

	.incidents {
		display: grid;
		gap: var(--s-2);
	}
	.inc {
		display: flex;
		align-items: center;
		gap: var(--s-3);
		padding: var(--s-3) var(--s-4);
		border-radius: var(--r);
		border: 1px solid var(--bad-border);
		background: var(--bad-bg);
		transition: border-color var(--dur) var(--ease);
	}
	.inc:hover {
		border-color: var(--bad-mark);
	}
	.inc-icon {
		color: var(--bad);
		flex: none;
	}
	.inc-body {
		flex: 1;
		min-width: 0;
		display: grid;
		gap: 2px;
	}
	.inc-title {
		font-size: var(--text-sm);
		font-weight: 500;
	}
	.inc-title strong {
		font-weight: 650;
	}
	.inc-meta {
		font-size: var(--text-xs);
		color: var(--text-2);
	}
	.inc-go {
		display: inline-flex;
		align-items: center;
		gap: 4px;
		flex: none;
		font-size: var(--text-sm);
		font-weight: 500;
		color: var(--bad);
	}
	@media (max-width: 560px) {
		.inc-go {
			display: none;
		}
	}

	.tokens {
		margin-top: var(--s-12);
	}
	.sec-head {
		display: flex;
		align-items: baseline;
		justify-content: space-between;
		gap: var(--s-4);
		flex-wrap: wrap;
		margin-bottom: var(--s-4);
	}
	.sec-head h2 {
		font-size: var(--text-xl);
	}
	.sec-head p {
		font-size: var(--text-sm);
	}
</style>
