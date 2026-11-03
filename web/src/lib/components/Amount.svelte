<script lang="ts">
	import { formatExact, formatTokens, parseAmount } from '$lib/format';

	/**
	 * A token amount from the API (smallest-unit integer string), shown in
	 * whole tokens. The exact value and the raw integer are in the tooltip,
	 * so every displayed number can be audited.
	 */
	let {
		value,
		decimals,
		symbol = '',
		compact = false,
		sign = false,
		tone = false,
		unit = false
	}: {
		value: string | null | undefined;
		decimals: number;
		symbol?: string;
		compact?: boolean;
		sign?: boolean;
		/** Colour negatives red (used for gaps). */
		tone?: boolean;
		/** Append the symbol after the number. */
		unit?: boolean;
	} = $props();

	const v = $derived(parseAmount(value));
	const text = $derived(formatTokens(v, decimals, { compact, sign }));
	const title = $derived(
		v === null
			? 'No value'
			: `${formatExact(v, decimals)} ${symbol}\nraw: ${v.toString()} (smallest unit = 10^-${decimals}${symbol ? ' ' + symbol : ''})`
	);
</script>

<span class="amount num" class:neg={tone && v !== null && v < 0n} class:pos={tone && v !== null && v > 0n} {title}
	>{text}{#if unit && symbol && v !== null}<span class="unit">{symbol}</span>{/if}</span
>

<style>
	.amount {
		white-space: nowrap;
	}
	.neg {
		color: var(--bad);
		font-weight: 550;
	}
	.unit {
		margin-left: 0.35em;
		color: var(--text-3);
		font-weight: 450;
	}
</style>
