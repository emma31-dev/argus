/**
 * Human summaries for the raw RPC errors the backend reports in
 * `last_error` (token) and `providers[].last_error` (status). The raw text
 * is always shown too, in a monospace disclosure; these are the one-liners.
 */

/** Host of a URL, or the input unchanged. */
export function hostOf(url: string): string {
	try {
		return new URL(url).host || url;
	} catch {
		return url;
	}
}

/**
 * The tail of a provider error: `rpc: provider unavailable (provider x,
 * method block): Post "https://x": Forbidden` -> `Forbidden`.
 */
export function shortError(err: string | null | undefined, max = 80): string {
	if (!err) return '';
	const firstLine = err.split('\n')[0].trim();
	const parts = firstLine.split(/:\s+/).filter(Boolean);
	let tail = (parts[parts.length - 1] ?? firstLine).replace(/^"+|"+$/g, '').trim();
	if (!tail) tail = firstLine;
	return tail.length > max ? `${tail.slice(0, max - 1)}…` : tail;
}

/** One-line summary of a token's failed read. */
export function summarizeReadError(err: string | null | undefined): string {
	if (!err) return 'The last read of this token failed.';
	const all = err.match(/failed on (\d+) provider/);
	if (all) {
		const n = Number(all[1]);
		const reason = shortError(err.split('\n')[0]);
		const what = n === 1 ? 'The only RPC provider failed' : `All ${n} RPC providers failed`;
		return `${what} for this round${reason ? ` (${reason})` : ''}.`;
	}
	const reason = shortError(err);
	return reason ? `The last read failed: ${reason}.` : 'The last read of this token failed.';
}
