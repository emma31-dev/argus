<script lang="ts">
	import type { StatusResponse, Status } from '#lib/api.ts';
	import { formatAgo, formatInteger, formatUtc } from '#lib/format.ts';
	import { monitor } from '#lib/monitor.svelte.ts';
	import { href } from '#lib/links.ts';
	import BlockLink from './BlockLink.svelte';
	import Icon from './Icon.svelte';
	import StatusDot from './StatusDot.svelte';
	import { statusMeta } from '#lib/status.ts';

	let { status }: { status: StatusResponse } = $props();

	const counts = $derived(status.tokens_by_status ?? { green: 0, amber: 0, red: 0, unknown: 0 });
	const total = $derived(status.tokens_total || Object.values(counts).reduce((a, b) => a + b, 0));
	const halted = $derived(monitor.neverChecked || monitor.stale);
	const meta = $derived.by(() => {
		const m = statusMeta(status.status);
		return halted ? { ...statusMeta('unknown'), icon: 'clock' as const, label: monitor.neverChecked ? 'Waiting' : 'Stale' } : m;
	});

	const headline = $derived.by(() => {
		const n = (k: Status) => counts[k] ?? 0;
		const t = (c: number) => `${c} ${c === 1 ? 'token' : 'tokens'}`;
		if (monitor.neverChecked) return 'Waiting for the first snapshot round';
		if (monitor.stale) return 'The monitor is not checking right now';
		if (status.status === 'red') return `Deficit on ${t(n('red'))}`;
		if (status.status === 'amber') return `${t(n('amber'))} below zero, settling`;
		if (status.status === 'unknown' && n('green') === 0) return 'Waiting for the first snapshot';
		if (n('unknown') > 0) return `${formatInteger(n('green'))} of ${formatInteger(total)} tokens fully backed`;
		return `All ${formatInteger(total)} tokens fully backed`;
	});

	const sub = $derived.by(() => {
		if (monitor.neverChecked)
			return `Argus is running but has not completed a same-block read yet, so it cannot vouch for any token. Figures appear here after the first successful round.`;
		if (monitor.stale)
			return `No snapshot round has completed ${monitor.ageMs !== null ? `for ${formatAgo(monitor.ageMs).replace(' ago', '')}` : 'recently'}. Until one does, Argus cannot vouch for any token; figures below are from the last successful round.`;
		if (status.status === 'red')
			return `The ledger owes more than the contract holds and the gap persisted for ${status.debounce_snapshots} consecutive snapshots.`;
		if (status.status === 'amber')
			return `A negative gap was seen. It turns red if it persists for ${status.debounce_snapshots} consecutive snapshots; in-flight deposits usually settle within a few blocks.`;
		return `For every tracked token, the balance held by intents.near covers what its ledger owes users, read at the same block.`;
	});

	const segments = $derived(
		(['green', 'amber', 'red', 'unknown'] as Status[])
			.map((s) => ({ s, n: counts[s] ?? 0 }))
			.filter((x) => x.n > 0)
	);
	const ago = $derived(monitor.ageMs);
</script>

<section class="hero card {meta.tone}" aria-labelledby="hero-title">
	<div class="lead">
		<div class="state">
			<span class="mark"><Icon name={meta.icon} size={18} stroke={2.25} /></span>
			<span class="eyebrow">Overall status · {meta.label}</span>
		</div>
		<h2 id="hero-title" class="headline">{headline}</h2>
		<p class="sub">{sub}</p>

		<div class="dist">
			<div class="bar" role="img" aria-label={segments.map((x) => `${x.n} ${statusMeta(x.s).label.toLowerCase()}`).join(', ')}>
				{#each segments as seg (seg.s)}
					<span class="seg {statusMeta(seg.s).tone}" style:flex-grow={seg.n}></span>
				{/each}
			</div>
			<ul class="legend">
				{#each ['green', 'amber', 'red', 'unknown'] as const as s (s)}
					<li class:zero={(counts[s] ?? 0) === 0}>
						<StatusDot status={s} size={7} />
						<span class="num n">{formatInteger(counts[s] ?? 0)}</span>
						<span class="l">{statusMeta(s).label}</span>
					</li>
				{/each}
			</ul>
		</div>
	</div>

	<dl class="metrics">
		<div class="metric">
			<dt>Last checked block</dt>
			<dd class="big"><BlockLink height={status.last_block} /></dd>
			<dd class="note">{status.last_block_time ? formatUtc(status.last_block_time) : 'No round completed yet'}</dd>
		</div>
		<div class="metric">
			<dt>Last check</dt>
			<dd class="big num" class:stale={monitor.stale && !monitor.neverChecked}>{ago === null ? 'Never' : formatAgo(ago)}</dd>
			<dd class="note">
				{monitor.neverChecked ? 'No round completed yet' : monitor.stale ? 'Stale: no round completed recently' : 'Snapshot round completed'}
			</dd>
		</div>
		<div class="metric">
			<dt>Check interval</dt>
			<dd class="big num">{status.interval_seconds}s</dd>
			<dd class="note">Red after {status.debounce_snapshots} negative snapshots</dd>
		</div>
		<div class="metric">
			<dt>Open incidents</dt>
			<dd class="big num" class:alarm={status.open_incidents > 0}>{status.open_incidents}</dd>
			<dd class="note"><a class="link" href={href.incidents()}>Incident history</a></dd>
		</div>
	</dl>
</section>

<style>
	.hero {
		display: grid;
		grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr);
		overflow: hidden;
		position: relative;
	}
	.hero::before {
		content: '';
		position: absolute;
		inset: 0 auto 0 0;
		width: 3px;
		background: var(--accent-bar);
	}
	.ok {
		--accent-bar: var(--ok-mark);
		--tone: var(--ok);
		--tone-bg: var(--ok-bg);
		--tone-border: var(--ok-border);
	}
	.warn {
		--accent-bar: var(--warn-mark);
		--tone: var(--warn);
		--tone-bg: var(--warn-bg);
		--tone-border: var(--warn-border);
	}
	.bad {
		--accent-bar: var(--bad-mark);
		--tone: var(--bad);
		--tone-bg: var(--bad-bg);
		--tone-border: var(--bad-border);
	}
	.unk {
		--accent-bar: var(--unk-mark);
		--tone: var(--unk);
		--tone-bg: var(--unk-bg);
		--tone-border: var(--unk-border);
	}
	.lead {
		padding: var(--s-8);
		display: flex;
		flex-direction: column;
	}
	.state {
		display: flex;
		align-items: center;
		gap: var(--s-3);
	}
	.state .eyebrow {
		color: var(--tone);
	}
	.mark {
		display: grid;
		place-items: center;
		width: 30px;
		height: 30px;
		border-radius: var(--r-sm);
		background: var(--tone-bg);
		border: 1px solid var(--tone-border);
		color: var(--tone);
	}
	.headline {
		margin-top: var(--s-5);
		font-size: var(--text-3xl);
		font-weight: 620;
		letter-spacing: -0.03em;
	}
	.sub {
		margin-top: var(--s-3);
		color: var(--text-2);
		max-width: 52ch;
		line-height: var(--leading-loose);
	}
	.dist {
		margin-top: auto;
		padding-top: var(--s-8);
	}
	.bar {
		display: flex;
		gap: 2px;
		height: 8px;
		border-radius: var(--r-pill);
		overflow: hidden;
		background: var(--surface-3);
	}
	.seg {
		flex-basis: 0;
		min-width: 6px;
		border-radius: var(--r-pill);
	}
	.seg.ok {
		background: var(--ok-mark);
	}
	.seg.warn {
		background: var(--warn-mark);
	}
	.seg.bad {
		background: var(--bad-mark);
	}
	.seg.unk {
		background: var(--unk-mark);
	}
	.legend {
		display: flex;
		flex-wrap: wrap;
		gap: var(--s-2) var(--s-5);
		margin-top: var(--s-3);
		font-size: var(--text-sm);
	}
	.legend li {
		display: inline-flex;
		align-items: center;
		gap: 6px;
	}
	.legend .n {
		font-weight: 600;
	}
	.legend .l {
		color: var(--text-3);
	}
	.legend .zero {
		opacity: 0.55;
	}

	.metrics {
		display: grid;
		grid-template-columns: 1fr 1fr;
		border-left: 1px solid var(--border);
		background: var(--surface-2);
	}
	.metric {
		padding: var(--s-6) var(--s-6);
		min-width: 0;
	}
	.metric:nth-child(odd) {
		border-right: 1px solid var(--border);
	}
	.metric:nth-child(-n + 2) {
		border-bottom: 1px solid var(--border);
	}
	dt {
		font-size: var(--text-xs);
		font-weight: 500;
		color: var(--text-3);
	}
	.big {
		margin-top: var(--s-2);
		font-size: var(--text-2xl);
		font-weight: 560;
		letter-spacing: -0.02em;
		line-height: 1.15;
		color: var(--text);
	}
	.big :global(.block) {
		font-family: var(--font-sans);
		font-variant-numeric: tabular-nums;
		letter-spacing: -0.01em;
		font-weight: 500;
		text-decoration-color: var(--border-strong);
	}
	.big.stale {
		color: var(--warn);
	}
	.big.alarm {
		color: var(--bad);
	}
	.note {
		margin-top: var(--s-1);
		font-size: var(--text-xs);
		color: var(--text-3);
		white-space: nowrap;
		overflow: hidden;
		text-overflow: ellipsis;
	}

	@media (max-width: 960px) {
		.hero {
			grid-template-columns: 1fr;
		}
		.metrics {
			border-left: 0;
			border-top: 1px solid var(--border);
		}
	}
	@media (max-width: 560px) {
		.lead {
			padding: var(--s-6) var(--s-5);
		}
		.headline {
			font-size: var(--text-2xl);
		}
		.metric {
			padding: var(--s-4) var(--s-5);
		}
		.big {
			font-size: var(--text-xl);
		}
	}
</style>
