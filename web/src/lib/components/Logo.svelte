<script lang="ts">
	/**
	 * The Argus mark: an open eye drawn as two lids that never quite close,
	 * around a pupil. One of Argus Panoptes' hundred eyes, always open.
	 */
	let { size = 22, wordmark = true }: { size?: number; wordmark?: boolean } = $props();
</script>

<span class="logo">
	<svg width={size} height={size} viewBox="0 0 32 32" aria-hidden="true">
		<rect width="32" height="32" rx="8" class="tile" />
		<path d="M5.5 16.6C8.4 11.4 12 9 16 9s7.6 2.4 10.5 7.6" class="lid" />
		<path d="M8.6 19.6C10.8 22 13.3 23.2 16 23.2s5.2-1.2 7.4-3.6" class="lid" />
		<circle cx="16" cy="16.4" r="3.6" class="pupil" />
	</svg>
	{#if wordmark}<span class="word">Argus</span>{/if}
</span>

<style>
	.logo {
		display: inline-flex;
		align-items: center;
		gap: 9px;
	}
	.tile {
		fill: var(--text);
	}
	.lid {
		fill: none;
		stroke: var(--bg);
		stroke-width: 2.4;
		stroke-linecap: round;
	}
	.pupil {
		fill: var(--bg);
	}
	.word {
		font-weight: 650;
		font-size: 15px;
		letter-spacing: -0.02em;
	}
</style>
