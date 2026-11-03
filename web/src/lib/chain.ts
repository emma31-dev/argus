/**
 * Human chain label for an Intents token id. The API sends `chain` on token
 * objects; this is the fallback for objects that only carry a token id
 * (incidents, replay) and for older API versions. Display only.
 */

const OMFT_PREFIX: Record<string, string> = {
	eth: 'Ethereum',
	base: 'Base',
	arb: 'Arbitrum',
	sol: 'Solana',
	tron: 'Tron',
	btc: 'Bitcoin',
	zec: 'Zcash',
	doge: 'Dogecoin',
	xrp: 'XRP Ledger',
	gnosis: 'Gnosis',
	bera: 'Berachain',
	sui: 'Sui',
	ton: 'TON',
	aptos: 'Aptos',
	cardano: 'Cardano',
	ltc: 'Litecoin',
	bch: 'Bitcoin Cash',
	stellar: 'Stellar',
	pol: 'Polygon',
	bsc: 'BNB Chain',
	avax: 'Avalanche',
	op: 'Optimism'
};

/** EVM chain ids used by HOT omni (nep245:v2_1.omni.hot.tg:<chainId>_<addr>). */
const EVM_CHAIN: Record<string, string> = {
	'1': 'Ethereum',
	'10': 'Optimism',
	'56': 'BNB Chain',
	'137': 'Polygon',
	'8453': 'Base',
	'42161': 'Arbitrum',
	'43114': 'Avalanche',
	'1117': 'TON',
	'1100': 'TON'
};

export function chainOf(tokenId: string): string {
	const [standard, ...rest] = tokenId.split(':');
	const contract = rest[0] ?? '';
	if (standard === 'nep245' && contract.endsWith('omni.hot.tg')) {
		const chainId = (rest[1] ?? '').split('_')[0];
		return EVM_CHAIN[chainId] ?? `HOT Omni ${chainId}`.trim();
	}
	if (contract.endsWith('.omft.near')) {
		const head = contract.slice(0, -'.omft.near'.length);
		const prefix = head.split(/[-.]/)[0];
		return OMFT_PREFIX[prefix] ?? 'Bridged';
	}
	// Same labels as the backend's tokens.ChainOf.
	if (contract === 'eth.bridge.near' || contract.endsWith('.factory.bridge.near')) return 'Ethereum (Rainbow)';
	if (contract === 'nbtc.bridge.near') return 'Bitcoin (Omni)';
	return 'NEAR';
}

/** Prefer the API's label, fall back to deriving it from the id. */
export function chainLabel(tokenId: string, chain?: string): string {
	return chain || chainOf(tokenId);
}
