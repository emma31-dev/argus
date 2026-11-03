<script module lang="ts">
	export interface SegOption<V> {
		value: V;
		label: string;
		count?: number;
		dot?: string;
	}
</script>

<script lang="ts" generics="T extends string">
	import StatusDot from './StatusDot.svelte';

	let {
		options,
		value = $bindable(),
		label,
		size = 'md'
	}: { options: SegOption<T>[]; value: T; label: string; size?: 'sm' | 'md' } = $props();

	let buttons: HTMLButtonElement[] = $state([]);

	function onKey(e: KeyboardEvent, i: number) {
		let next = -1;
		if (e.key === 'ArrowRight' || e.key === 'ArrowDown') next = (i + 1) % options.length;
		else if (e.key === 'ArrowLeft' || e.key === 'ArrowUp') next = (i - 1 + options.length) % options.length;
		else if (e.key === 'Home') next = 0;
		else if (e.key === 'End') next = options.length - 1;
		if (next >= 0) {
			e.preventDefault();
			value = options[next].value;
			buttons[next]?.focus();
		}
	}
</script>

<div class="seg {size}" role="radiogroup" aria-label={label}>
	{#each options as opt, i (opt.value)}
		<button
			bind:this={buttons[i]}
			type="button"
			role="radio"
			aria-checked={value === opt.value}
			tabindex={value === opt.value ? 0 : -1}
			class:on={value === opt.value}
			onclick={() => (value = opt.value)}
			onkeydown={(e) => onKey(e, i)}
		>
			{#if opt.dot}<StatusDot status={opt.dot} size={6} />{/if}
			<span>{opt.label}</span>
			{#if opt.count !== undefined}<span class="count num">{opt.count}</span>{/if}
		</button>
	{/each}
</div>

<style>
	.seg {
		display: inline-flex;
		gap: 2px;
		padding: 2px;
		border-radius: var(--r-sm);
		background: var(--surface-3);
		border: 1px solid var(--border-soft);
		max-width: 100%;
		overflow-x: auto;
		scrollbar-width: none;
	}
	.seg::-webkit-scrollbar {
		display: none;
	}
	button {
		display: inline-flex;
		align-items: center;
		gap: 6px;
		height: 26px;
		padding: 0 10px;
		border-radius: 4px;
		color: var(--text-2);
		font-size: var(--text-sm);
		font-weight: 500;
		white-space: nowrap;
		transition:
			background var(--dur) var(--ease),
			color var(--dur) var(--ease);
	}
	.sm button {
		height: 24px;
		padding: 0 9px;
		font-size: var(--text-xs);
	}
	button:hover {
		color: var(--text);
	}
	button.on {
		background: var(--surface);
		color: var(--text);
		box-shadow:
			0 1px 2px rgba(16, 20, 28, 0.08),
			0 0 0 1px var(--border);
	}
	.count {
		color: var(--text-3);
		font-size: var(--text-xs);
	}
	.on .count {
		color: var(--text-2);
	}
</style>
