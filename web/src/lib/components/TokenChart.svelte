<script lang="ts">
	import { scaleLinear } from 'd3-scale';
	import { area, line, curveMonotoneX } from 'd3-shape';
	import type { Snapshot } from '#lib/api.ts';
	import { chartId, formatTimeTick, nearestIndex, timeTicks } from '#lib/chart.ts';
	import {
		formatAxis,
		formatInteger,
		formatPercent,
		formatTokens,
		formatUtc,
		parseAmount,
		ratioPercent,
		toTokenNumber
	} from '#lib/format.ts';

	let {
		points,
		decimals,
		symbol,
		from,
		to,
		busy = false
	}: {
		points: Snapshot[];
		decimals: number;
		symbol: string;
		from: number;
		to: number;
		busy?: boolean;
	} = $props();

	const id = chartId('tc');
	let width = $state(0);
	const H_TOP = 220;
	const H_GAP = 112;
	const GAP_BETWEEN = 36;
	const M = { top: 24, right: 12, bottom: 26, left: 58 };
	const height = M.top + H_TOP + GAP_BETWEEN + H_GAP + M.bottom;
	const innerW = $derived(Math.max(10, width - M.left - M.right));

	interface P {
		t: number;
		owed: number;
		held: number;
		gap: number;
		raw: Snapshot;
		gapBig: bigint | null;
		pct: number | null;
	}

	const data = $derived.by<P[]>(() =>
		points
			.map((p) => {
				const owed = parseAmount(p.owed);
				const held = parseAmount(p.held);
				const gap = parseAmount(p.gap);
				return {
					t: new Date(p.block_time).getTime(),
					owed: owed === null ? NaN : toTokenNumber(owed, decimals),
					held: held === null ? NaN : toTokenNumber(held, decimals),
					gap: gap === null ? NaN : toTokenNumber(gap, decimals),
					raw: p,
					gapBig: gap,
					pct: ratioPercent(gap, owed)
				};
			})
			.filter((p) => Number.isFinite(p.t) && Number.isFinite(p.owed) && Number.isFinite(p.held))
			.sort((a, b) => a.t - b.t)
	);

	const x = $derived(scaleLinear().domain([from, to]).range([0, innerW]));

	const yTop = $derived.by(() => {
		let lo = Infinity;
		let hi = -Infinity;
		for (const p of data) {
			lo = Math.min(lo, p.owed, p.held);
			hi = Math.max(hi, p.owed, p.held);
		}
		if (!Number.isFinite(lo)) [lo, hi] = [0, 1];
		const pad = (hi - lo) * 0.08 || Math.abs(hi) * 0.001 || 1;
		return scaleLinear()
			.domain([lo - pad, hi + pad])
			.range([H_TOP, 0])
			.nice(4);
	});

	const yGap = $derived.by(() => {
		let lo = 0;
		let hi = 0;
		for (const p of data) {
			lo = Math.min(lo, p.gap);
			hi = Math.max(hi, p.gap);
		}
		if (lo === 0 && hi === 0) hi = 1;
		const pad = (hi - lo) * 0.1;
		return scaleLinear()
			.domain([lo < 0 ? lo - pad : 0, hi > 0 ? hi + pad : 0])
			.range([H_GAP, 0])
			.nice(3);
	});

	const owedLine = $derived(
		line<P>()
			.x((p) => x(p.t))
			.y((p) => yTop(p.owed))
			.curve(curveMonotoneX)(data) ?? ''
	);
	const heldLine = $derived(
		line<P>()
			.x((p) => x(p.t))
			.y((p) => yTop(p.held))
			.curve(curveMonotoneX)(data) ?? ''
	);
	const between = $derived(
		area<P>()
			.x((p) => x(p.t))
			.y0((p) => yTop(p.owed))
			.y1((p) => yTop(p.held))
			.curve(curveMonotoneX)(data) ?? ''
	);
	// Clip regions above / below the owed line, so the band between the lines
	// is green where held > owed and red where held < owed.
	const aboveOwed = $derived(
		area<P>()
			.x((p) => x(p.t))
			.y0(-10)
			.y1((p) => yTop(p.owed))
			.curve(curveMonotoneX)(data) ?? ''
	);
	const belowOwed = $derived(
		area<P>()
			.x((p) => x(p.t))
			.y0((p) => yTop(p.owed))
			.y1(H_TOP + 10)
			.curve(curveMonotoneX)(data) ?? ''
	);
	const gapArea = $derived(
		area<P>()
			.x((p) => x(p.t))
			.y0(() => yGap(0))
			.y1((p) => yGap(p.gap))
			.curve(curveMonotoneX)(data) ?? ''
	);
	const gapLine = $derived(
		line<P>()
			.x((p) => x(p.t))
			.y((p) => yGap(p.gap))
			.curve(curveMonotoneX)(data) ?? ''
	);
	const zeroY = $derived(yGap(0));

	const xTicks = $derived(timeTicks(from, to, Math.max(2, Math.floor(innerW / 110))));
	const topTicks = $derived(yTop.ticks(4));
	const gapTicks = $derived(yGap.ticks(3));
	const span = $derived(to - from);
	const xs = $derived(data.map((p) => p.t));
	const negCount = $derived(data.filter((p) => p.gapBig !== null && p.gapBig < 0n).length);

	let active = $state<number | null>(null);
	const hp = $derived(active !== null ? data[active] : null);

	function onMove(e: PointerEvent) {
		const svg = (e.currentTarget as SVGElement).getBoundingClientRect();
		const px = e.clientX - svg.left - M.left;
		active = nearestIndex(xs, x.invert(px));
	}
	function onKey(e: KeyboardEvent) {
		if (!data.length) return;
		const last = data.length - 1;
		const step = e.shiftKey ? Math.max(1, Math.round(data.length / 20)) : 1;
		let next: number | null = active;
		if (e.key === 'ArrowRight') next = active === null ? last : Math.min(last, active + step);
		else if (e.key === 'ArrowLeft') next = active === null ? last : Math.max(0, active - step);
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = last;
		else if (e.key === 'Escape') next = null;
		else if (e.key === 'n' || e.key === 'N') {
			// Jump to the next negative-gap snapshot.
			const start = active === null ? 0 : active + 1;
			const i = data.findIndex((p, j) => j >= start && p.gapBig !== null && p.gapBig < 0n);
			if (i >= 0) next = i;
		} else return;
		e.preventDefault();
		active = next;
	}

	const tipLeft = $derived(hp ? M.left + x(hp.t) : 0);
	const tipFlip = $derived(tipLeft > width - 250);
	const summary = $derived.by(() => {
		if (!data.length) return `No snapshots in this range for ${symbol}.`;
		const last = data[data.length - 1];
		return `Owed and held ${symbol} over ${data.length} snapshots. Latest gap ${formatTokens(last.gapBig, decimals, { sign: true })} ${symbol}. ${negCount} snapshot${negCount === 1 ? '' : 's'} with a negative gap. Use arrow keys to inspect points, N to jump to the next negative snapshot.`;
	});
</script>

<div class="chart" class:busy class:empty={data.length === 0} bind:clientWidth={width}>
	{#if data.length === 0}
		<div class="nodata">{busy ? 'Loading…' : 'No snapshots in this range.'}</div>
	{:else if width > 0}
		<!-- Focusable, keyboard-navigable chart: arrows move the crosshair, the live tooltip announces values. -->
		<!-- svelte-ignore a11y_no_noninteractive_tabindex, a11y_no_noninteractive_element_interactions -->
		<svg
			{width}
			{height}
			role="application"
			tabindex="0"
			aria-label={summary}
			onpointermove={onMove}
			onpointerleave={() => (active = null)}
			onkeydown={onKey}
			onblur={() => (active = null)}
		>
			<defs>
				<clipPath id="{id}-above"><path d={aboveOwed} /></clipPath>
				<clipPath id="{id}-below"><path d={belowOwed} /></clipPath>
				<clipPath id="{id}-pos"><rect x="-2" y="-10" width={innerW + 4} height={zeroY + 11} /></clipPath>
				<clipPath id="{id}-neg"><rect x="-2" y={zeroY + 1} width={innerW + 4} height={H_GAP - zeroY + 10} /></clipPath>
				<clipPath id="{id}-plot"><rect x="-2" y="-4" width={innerW + 4} height={H_TOP + 8} /></clipPath>
			</defs>

			<!-- Panel 1: owed vs held -->
			<g transform="translate({M.left},{M.top})">
				{#each topTicks as t (t)}
					<line class="grid" x1="0" x2={innerW} y1={yTop(t)} y2={yTop(t)} />
					<text class="tick" x="-10" y={yTop(t)} dy="0.32em" text-anchor="end">{formatAxis(t)}</text>
				{/each}
				<g clip-path="url(#{id}-plot)">
					<path class="band neg" d={between} clip-path="url(#{id}-below)" />
					<path class="band pos" d={between} clip-path="url(#{id}-above)" />
					<path class="ln held" d={heldLine} />
					<path class="ln owed" d={owedLine} />
				</g>
				<text class="panel-label" x="0" y="-12">{symbol} · owed and held</text>
			</g>

			<!-- Panel 2: gap = held - owed -->
			<g transform="translate({M.left},{M.top + H_TOP + GAP_BETWEEN})">
				<text class="panel-label" x="0" y="-14">Gap · held − owed</text>
				{#each gapTicks as t (t)}
					<line class="grid" class:zero={t === 0} x1="0" x2={innerW} y1={yGap(t)} y2={yGap(t)} />
					<text class="tick" x="-10" y={yGap(t)} dy="0.32em" text-anchor="end">{formatAxis(t)}</text>
				{/each}
				<line class="zero-line" x1="0" x2={innerW} y1={zeroY} y2={zeroY} />
				<g clip-path="url(#{id}-pos)">
					<path class="gap-area pos" d={gapArea} />
					<path class="gap-line pos" d={gapLine} />
				</g>
				<g clip-path="url(#{id}-neg)">
					<path class="gap-area neg" d={gapArea} />
					<path class="gap-line neg" d={gapLine} />
				</g>
				<!-- Every negative snapshot gets a mark, so a single-snapshot blip is never lost in the curve. -->
				{#each data as p, i (i)}
					{#if p.gapBig !== null && p.gapBig < 0n}
						<circle class="negpt" cx={x(p.t)} cy={yGap(p.gap)} r="2.5" />
					{/if}
				{/each}
				{#each xTicks as d (d.getTime())}
					<text class="tick" x={x(d.getTime())} y={H_GAP + 18} text-anchor="middle">{formatTimeTick(d, span)}</text>
				{/each}
			</g>

			<!-- Crosshair -->
			{#if hp}
				<g transform="translate({M.left},{M.top})" pointer-events="none">
					<line class="cross" x1={x(hp.t)} x2={x(hp.t)} y1="0" y2={H_TOP + GAP_BETWEEN + H_GAP} />
					<circle class="dot owed" cx={x(hp.t)} cy={yTop(hp.owed)} r="4" />
					<circle class="dot held" cx={x(hp.t)} cy={yTop(hp.held)} r="4" />
					<circle
						class="dot"
						class:gneg={hp.gap < 0}
						class:gpos={hp.gap >= 0}
						cx={x(hp.t)}
						cy={H_TOP + GAP_BETWEEN + yGap(hp.gap)}
						r="4"
					/>
				</g>
			{/if}
			<rect x={M.left} y={M.top} width={innerW} height={H_TOP + GAP_BETWEEN + H_GAP} fill="transparent" />
		</svg>

		{#if hp}
			<div class="tip" class:flip={tipFlip} style:left="{tipLeft}px" role="status" aria-live="polite">
				<p class="tip-time">{formatUtc(hp.raw.block_time)}</p>
				<p class="tip-block mono">Block {formatInteger(hp.raw.block_height)}</p>
				<dl>
					<div><dt><i class="key owed"></i>Owed</dt><dd class="num">{formatTokens(parseAmount(hp.raw.owed), decimals)}</dd></div>
					<div><dt><i class="key held"></i>Held</dt><dd class="num">{formatTokens(parseAmount(hp.raw.held), decimals)}</dd></div>
					<div class="tip-gap" class:neg={hp.gap < 0}>
						<dt><i class="key gap" class:neg={hp.gap < 0}></i>Gap</dt>
						<dd class="num">{formatTokens(hp.gapBig, decimals, { sign: true })}</dd>
					</div>
					<div><dt>Gap / owed</dt><dd class="num">{formatPercent(hp.pct)}</dd></div>
				</dl>
			</div>
		{/if}
	{/if}
</div>

<style>
	.chart {
		position: relative;
		width: 100%;
		min-height: 418px;
		transition: opacity var(--dur) var(--ease);
	}
	.busy {
		opacity: 0.55;
	}
	.chart.empty {
		min-height: 0;
	}
	.chart.empty .nodata {
		position: static;
		padding: var(--s-8) 0;
		border: 1px dashed var(--border);
		border-radius: var(--r);
	}
	svg {
		display: block;
		overflow: visible;
		touch-action: pan-y;
		user-select: none;
	}
	svg:focus-visible {
		outline: 2px solid var(--accent);
		outline-offset: 4px;
	}
	.grid {
		stroke: var(--grid);
		stroke-width: 1;
		shape-rendering: crispEdges;
	}
	.zero-line {
		stroke: var(--axis);
		stroke-width: 1;
		shape-rendering: crispEdges;
	}
	.tick {
		fill: var(--text-3);
		font-size: 11px;
		font-family: var(--font-sans);
		font-variant-numeric: tabular-nums;
	}
	.panel-label {
		fill: var(--text-2);
		font-size: 11px;
		font-weight: 550;
	}
	.ln {
		fill: none;
		stroke-width: 2;
		stroke-linejoin: round;
		stroke-linecap: round;
	}
	.ln.owed {
		stroke: var(--series-owed);
		stroke-width: 1.5;
		stroke-dasharray: 5 4;
		stroke-linecap: butt;
	}
	.ln.held {
		stroke: var(--series-held);
	}
	.band.pos {
		fill: var(--gap-pos);
		opacity: 0.14;
	}
	.band.neg {
		fill: var(--gap-neg);
		opacity: 0.2;
	}
	.gap-area.pos {
		fill: var(--gap-pos);
		opacity: 0.12;
	}
	.gap-area.neg {
		fill: var(--gap-neg);
		opacity: 0.18;
	}
	.gap-line {
		fill: none;
		stroke-width: 1.75;
		stroke-linejoin: round;
	}
	.gap-line.pos {
		stroke: var(--gap-pos);
	}
	.gap-line.neg {
		stroke: var(--gap-neg);
	}
	.negpt {
		fill: var(--gap-neg);
		stroke: var(--surface);
		stroke-width: 1;
	}
	.cross {
		stroke: var(--text-3);
		stroke-width: 1;
		stroke-dasharray: none;
		opacity: 0.6;
	}
	.dot {
		stroke: var(--surface);
		stroke-width: 2;
	}
	.dot.owed {
		fill: var(--series-owed);
	}
	.dot.held {
		fill: var(--series-held);
	}
	.dot.gpos {
		fill: var(--gap-pos);
	}
	.dot.gneg {
		fill: var(--gap-neg);
	}

	.tip {
		position: absolute;
		top: 4px;
		transform: translateX(16px);
		width: 224px;
		padding: 10px 12px;
		border-radius: var(--r);
		background: var(--surface);
		border: 1px solid var(--border);
		box-shadow: var(--shadow-pop);
		pointer-events: none;
		font-size: var(--text-xs);
		z-index: 5;
	}
	.tip.flip {
		transform: translateX(calc(-100% - 16px));
	}
	.tip-time {
		font-weight: 600;
		color: var(--text);
	}
	.tip-block {
		color: var(--text-3);
		font-size: 11px;
		margin-top: 1px;
	}
	.tip dl {
		margin-top: 8px;
		padding-top: 8px;
		border-top: 1px solid var(--border-soft);
		display: grid;
		gap: 4px;
	}
	.tip dl div {
		display: flex;
		justify-content: space-between;
		gap: 12px;
	}
	.tip dt {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		color: var(--text-3);
	}
	.tip dd {
		font-weight: 600;
		color: var(--text);
	}
	.tip-gap.neg dd {
		color: var(--bad);
	}
	.key {
		display: inline-block;
		width: 10px;
		height: 2px;
		border-radius: 1px;
	}
	.key.owed {
		background: var(--series-owed);
	}
	.key.held {
		background: var(--series-held);
	}
	.key.gap {
		background: var(--gap-pos);
	}
	.key.gap.neg {
		background: var(--gap-neg);
	}
	.nodata {
		position: absolute;
		inset: 0;
		display: grid;
		place-items: center;
		color: var(--text-3);
		font-size: var(--text-sm);
	}
</style>
