<script lang="ts">
	import { formatAgo, formatUtc } from '#lib/format.ts';
	import { clock } from '#lib/live.svelte.ts';

	let { iso }: { iso: string | null | undefined } = $props();
	const t = $derived(iso ? new Date(iso).getTime() : NaN);
</script>

<time datetime={iso ?? undefined} title={formatUtc(iso)} class="num">{Number.isNaN(t) ? '—' : formatAgo(clock.now - t)}</time>
