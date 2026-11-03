import type { Status } from './api';
import type { IconName } from './icons';

export interface StatusMeta {
	label: string;
	icon: IconName;
	/** CSS variable stems: var(--ok), var(--ok-bg), ... */
	tone: 'ok' | 'warn' | 'bad' | 'unk';
	description: string;
}

export const STATUS_META: Record<Status, StatusMeta> = {
	green: {
		label: 'Backed',
		icon: 'check',
		tone: 'ok',
		description: 'held ≥ owed at the latest snapshot'
	},
	amber: {
		label: 'Settling',
		icon: 'clock',
		tone: 'warn',
		description: 'held < owed, inside the settling window'
	},
	red: {
		label: 'Deficit',
		icon: 'alert',
		tone: 'bad',
		description: 'held < owed for K consecutive snapshots'
	},
	unknown: {
		label: 'Unknown',
		icon: 'help',
		tone: 'unk',
		description: 'no successful snapshot yet, or the last read failed'
	}
};

export function statusMeta(s: string | null | undefined): StatusMeta {
	return STATUS_META[(s as Status) in STATUS_META ? (s as Status) : 'unknown'];
}
