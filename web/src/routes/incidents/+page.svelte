<script lang="ts">
	import { onMount } from 'svelte';
	import { goto } from '$app/navigation';
	import { api, type IncidentsResponse } from '$lib/api';
	import { monitor } from '$lib/monitor.svelte';
	import { poll, Resource } from '$lib/live.svelte';
	import { formatDuration, formatInteger, formatUtc } from '$lib/format';
	import { href } from '$lib/links';
	import { clock } from '$lib/live.svelte';
	import PageHeader from '$lib/components/PageHeader.svelte';
	import Segmented from '$lib/components/Segmented.svelte';
	import IncidentBadge from '$lib/components/IncidentBadge.svelte';
	import Amount from '$lib/components/Amount.svelte';
	import BlockLink from '$lib/components/BlockLink.svelte';
	import EmptyState from '$lib/components/EmptyState.svelte';
	import OfflineBanner from '$lib/components/OfflineBanner.svelte';
	import Ago from '$lib/components/Ago.svelte';
	import ChainChip from '$lib/components/ChainChip.svelte';

	type F = 'all' | 'open' | 'closed';
	let filter = $state<F>('all');
	const list = new Resource<IncidentsResponse>((s) => api.incidents('all', 500, s));
	onMount(() => poll(() => list.load(), () => monitor.intervalMs));

	const all = $derived(list.data?.incidents ?? []);
	const isOpen = (i: { closed_at?: string | null }) => !i.closed_at;
	const rows = $derived(
		all
			.filter((i) => filter === 'all' || (filter === 'open' ? isOpen(i) : !isOpen(i)))
			.sort((a, b) => Number(isOpen(b)) - Number(isOpen(a)) || b.id - a.id)
	);
	const openCount = $derived(all.filter(isOpen).length);

	function duration(i: (typeof all)[number]) {
		if (i.duration_seconds != null && !isOpen(i)) return formatDuration(i.duration_seconds);
		const start = new Date(i.opened_at).getTime();
		return formatDuration((clock.now - start) / 1000);
	}
	function rowClick(e: MouseEvent, id: number) {
		if ((e.target as HTMLElement).closest('a,button')) return;
		goto(href.incident(id));
	}
</script>

<svelte:head><title>Incidents · Argus</title></svelte:head>

<div class="container">
	<PageHeader eyebrow="History" title="Incidents">
		An incident opens when a token's gap stays below zero for {monitor.status.data?.debounce_snapshots ?? 4} consecutive
		snapshots, and closes when held covers owed again. Each one keeps its block range, the largest deficit and the
		intents.near events in that window.
	</PageHeader>

	{#if list.error}
		<div class="mb"><OfflineBanner error={list.error} retry={() => list.load()} hasData={!!list.data} /></div>
	{/if}

	<div class="toolbar">
		<Segmented
			label="Filter incidents"
			bind:value={filter}
			options={[
				{ value: 'all', label: 'All', count: all.length },
				{ value: 'open', label: 'Open', count: openCount, dot: openCount ? 'red' : undefined },
				{ value: 'closed', label: 'Resolved', count: all.length - openCount }
			]}
		/>
	</div>

	<div class="card">
		{#if !list.data && !list.error}
			{#each Array(4) as _, i (i)}
				<div class="sk-row">
					<span class="skeleton" style="width:64px;height:20px;border-radius:99px"></span>
					<span class="skeleton" style="width:30%;height:12px"></span>
					<span class="skeleton" style="width:18%;height:12px;margin-left:auto"></span>
				</div>
			{/each}
		{:else if all.length === 0 && list.data}
			<EmptyState icon="shield" title="No incidents recorded">
				Argus has not seen any token's ledger owe more than intents.near holds for longer than the settling window.
			</EmptyState>
		{:else if rows.length === 0}
			<EmptyState icon="check" title={filter === 'open' ? 'No open incidents' : 'Nothing here'} compact>
				{filter === 'open' ? 'Every token is currently backed or settling.' : 'No incidents match this filter.'}
			</EmptyState>
		{:else}
			<table class="tbl">
				<caption class="sr-only">Incidents, open first, newest first</caption>
				<thead>
					<tr>
						<th scope="col">Status</th>
						<th scope="col">Incident</th>
						<th scope="col">Opened</th>
						<th scope="col" class="r">Duration</th>
						<th scope="col" class="r">Max deficit</th>
						<th scope="col" class="r">Block range</th>
					</tr>
				</thead>
				<tbody>
					{#each rows as i (i.id)}
						<tr class:open={isOpen(i)} onclick={(e) => rowClick(e, i.id)}>
							<td class="w1"><IncidentBadge open={isOpen(i)} /></td>
							<td>
								<a class="title" href={href.incident(i.id)}><span class="num muted">#{i.id}</span> {i.symbol}</a>
								<ChainChip id={i.token_id} chain={i.chain} />
								<span class="sub mono">{i.token_id}</span>
							</td>
							<td>
								<span class="num">{formatUtc(i.opened_at, { seconds: false })}</span>
								<span class="sub"><Ago iso={i.opened_at} /></span>
							</td>
							<td class="r num">{duration(i)}{#if isOpen(i)}<span class="sub">ongoing</span>{/if}</td>
							<td class="r">
								<span class="defi"><Amount value={i.max_deficit} decimals={i.decimals} symbol={i.symbol} /></span>
								<span class="sub">{i.symbol}</span>
							</td>
							<td class="r range">
								<BlockLink height={i.start_block} />
								<span class="arrow">→</span>
								{#if i.end_block}<BlockLink height={i.end_block} />{:else}<span class="muted">now</span>{/if}
								<span class="sub">{i.end_block ? formatInteger(i.end_block - i.start_block) + ' blocks' : 'still negative'}</span>
							</td>
						</tr>
					{/each}
				</tbody>
			</table>
			<ul class="cards">
				{#each rows as i (i.id)}
					<li>
						<a class="icard" class:open={isOpen(i)} href={href.incident(i.id)}>
							<div class="ih">
								<span class="title"><span class="num muted">#{i.id}</span> {i.symbol} <ChainChip id={i.token_id} chain={i.chain} /></span>
								<IncidentBadge open={isOpen(i)} />
							</div>
							<dl>
								<div><dt>Max deficit</dt><dd class="num"><Amount value={i.max_deficit} decimals={i.decimals} symbol={i.symbol} unit /></dd></div>
								<div><dt>Duration</dt><dd class="num">{duration(i)}</dd></div>
								<div><dt>Opened</dt><dd class="num">{formatUtc(i.opened_at, { seconds: false })}</dd></div>
								<div><dt>Blocks</dt><dd class="num mono sm">{formatInteger(i.start_block)} → {i.end_block ? formatInteger(i.end_block) : 'now'}</dd></div>
							</dl>
						</a>
					</li>
				{/each}
			</ul>
		{/if}
	</div>
</div>

<style>
	.mb {
		margin-bottom: var(--s-4);
	}
	.toolbar {
		margin-bottom: var(--s-3);
	}
	.tbl {
		width: 100%;
		border-collapse: separate;
		border-spacing: 0;
		font-size: var(--text-sm);
	}
	th {
		position: sticky;
		top: var(--nav-h);
		background: var(--surface-2);
		color: var(--text-3);
		font-size: var(--text-xs);
		font-weight: 500;
		text-align: left;
		padding: 9px 14px;
		border-bottom: 1px solid var(--border);
		white-space: nowrap;
		z-index: 1;
	}
	th:first-child {
		border-top-left-radius: var(--r-lg);
	}
	th:last-child {
		border-top-right-radius: var(--r-lg);
	}
	td {
		padding: 12px 14px;
		border-bottom: 1px solid var(--border-soft);
		vertical-align: top;
	}
	tbody tr:last-child td {
		border-bottom: 0;
	}
	tbody tr {
		cursor: pointer;
	}
	tbody tr:hover td {
		background: var(--surface-2);
	}
	tr.open td {
		background: color-mix(in srgb, var(--bad-bg) 70%, transparent);
	}
	tr.open td:first-child {
		box-shadow: inset 2px 0 0 var(--bad-mark);
	}
	.w1 {
		width: 1%;
	}
	.r {
		text-align: right;
	}
	.title {
		font-weight: 600;
		color: var(--text);
	}
	a.title:hover {
		text-decoration: underline;
		text-underline-offset: 3px;
	}
	.sub {
		display: block;
		margin-top: 2px;
		font-size: var(--text-xs);
		color: var(--text-3);
		max-width: 340px;
		overflow: hidden;
		text-overflow: ellipsis;
		white-space: nowrap;
	}
	.r .sub {
		margin-left: auto;
	}
	.sub.mono {
		font-size: 11.5px;
	}
	.defi {
		font-weight: 600;
		color: var(--bad);
	}
	.range {
		white-space: nowrap;
		font-size: var(--text-xs);
	}
	.arrow {
		color: var(--text-4);
		margin: 0 4px;
	}
	.sk-row {
		display: flex;
		align-items: center;
		gap: var(--s-4);
		padding: 16px;
		border-bottom: 1px solid var(--border-soft);
	}
	.cards {
		display: none;
	}
	@media (max-width: 880px) {
		.tbl {
			display: none;
		}
		.card {
			background: transparent;
			border: 0;
			box-shadow: none;
		}
		.cards {
			display: grid;
			gap: var(--s-2);
		}
		.icard {
			display: block;
			padding: var(--s-4);
			border: 1px solid var(--border);
			border-radius: var(--r-lg);
			background: var(--surface);
		}
		.icard.open {
			border-color: var(--bad-border);
			background: var(--bad-bg);
		}
		.ih {
			display: flex;
			justify-content: space-between;
			align-items: center;
		}
		.icard dl {
			display: grid;
			grid-template-columns: 1fr 1fr;
			gap: var(--s-3) var(--s-4);
			margin-top: var(--s-3);
			padding-top: var(--s-3);
			border-top: 1px solid var(--border-soft);
		}
		.icard dt {
			font-size: var(--text-2xs);
			font-weight: 500;
			color: var(--text-3);
			text-transform: uppercase;
			letter-spacing: var(--tracking-caps);
		}
		.icard dd {
			margin-top: 2px;
			font-size: var(--text-sm);
		}
		.icard dd.sm {
			font-size: 11.5px;
		}
	}
</style>
