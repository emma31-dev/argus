import { base } from '$app/paths';

export const REPO_URL = 'https://github.com/emma31-dev/argus';
export const API_DOC_URL = `${REPO_URL}/blob/main/docs/api.md`;
export const INTENTS_CONTRACT = 'intents.near';

const EXPLORER = 'https://nearblocks.io';
export const explorer = {
	block: (height: number | string) => `${EXPLORER}/blocks/${height}`,
	account: (id: string) => `${EXPLORER}/address/${encodeURIComponent(id)}`,
	tx: (hash: string) => `${EXPLORER}/txns/${encodeURIComponent(hash)}`
};

/** Internal routes. Ids travel in the query string so the site stays fully static. */
export const href = {
	home: () => `${base}/`,
	token: (id: string) => `${base}/token/?id=${encodeURIComponent(id)}`,
	incidents: () => `${base}/incidents/`,
	incident: (id: number | string) => `${base}/incident/?id=${encodeURIComponent(String(id))}`,
	how: (hash = '') => `${base}/how-it-works/${hash}`,
	replay: () => `${base}/replay/`,
	api: (hash = '') => `${base}/api/${hash}`
};
