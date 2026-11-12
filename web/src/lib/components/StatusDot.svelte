<script lang="ts">
	import type { Status } from '#lib/api.ts';
	import { statusMeta } from '#lib/status.ts';

	let { status, pulse = false, size = 8 }: { status: Status | string; pulse?: boolean; size?: number } =
		$props();
	const meta = $derived(statusMeta(status));
</script>

<span class="dot {meta.tone}" class:pulse style:--size="{size}px" aria-hidden="true"></span>

<style>
	.dot {
		position: relative;
		display: inline-block;
		flex: none;
		width: var(--size);
		height: var(--size);
		border-radius: 50%;
		background: var(--c);
	}
	.ok {
		--c: var(--ok-mark);
	}
	.warn {
		--c: var(--warn-mark);
	}
	.bad {
		--c: var(--bad-mark);
	}
	.unk {
		--c: var(--unk-mark);
	}
	.pulse::after {
		content: '';
		position: absolute;
		inset: 0;
		border-radius: 50%;
		background: var(--c);
		animation: ping 2.4s var(--ease) infinite;
	}
	@keyframes ping {
		0% {
			transform: scale(1);
			opacity: 0.55;
		}
		70%,
		100% {
			transform: scale(2.6);
			opacity: 0;
		}
	}
	@media (prefers-reduced-motion: reduce) {
		.pulse::after {
			display: none;
		}
	}
</style>
