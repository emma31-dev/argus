<script lang="ts">
	import { ICONS, type IconName } from '$lib/icons';

	let {
		name,
		size = 16,
		stroke = 1.75,
		label,
		class: cls = ''
	}: { name: IconName; size?: number; stroke?: number; label?: string; class?: string } = $props();
</script>

<svg
	class={cls}
	width={size}
	height={size}
	viewBox="0 0 24 24"
	fill="none"
	stroke="currentColor"
	stroke-width={stroke}
	stroke-linecap="round"
	stroke-linejoin="round"
	role={label ? 'img' : undefined}
	aria-label={label}
	aria-hidden={label ? undefined : 'true'}
>
	{#each ICONS[name] as d (d)}
		<path {d} />
	{/each}
</svg>
