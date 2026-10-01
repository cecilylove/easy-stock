import type { SourceMeta } from './backend';

function usableTimestamp(value: string | undefined) {
	return Boolean(value && !/^0001-01-01(?:T|$)/.test(value) && Number.isFinite(Date.parse(value)));
}

// Index provider wall-clock strings with an unknown offset are evidence, not
// instants. Never interpret them in the browser's or the China market's zone.
export function formatIndexTradeTime(value: string | undefined, meta: SourceMeta | null | undefined) {
	if (meta?.time_zone === 'unknown') {
		return meta.native_timestamp ? `未知（来源时间 ${meta.native_timestamp}；时区偏移未确认）` : '未知（时区偏移未确认）';
	}
	if (!usableTimestamp(value)) return '未知';
	if (!meta?.time_zone) return value!; // Legacy timestamp retains its explicit text/offset.
	try {
		return new Date(value!).toLocaleString('zh-CN', { timeZone: meta.time_zone, hour12: false });
	} catch {
		return '未知（时区未确认）';
	}
}

// Only for index day/week/month history: its ISO midnight encodes a provider
// trading-date label, not a clock instant to shift into the host time zone.
export function formatIndexHistoryDate(value: string) {
	if (!usableTimestamp(value)) return '--';
	const date = /^(\d{4})-(\d{2})-(\d{2})(?:T|$)/.exec(value);
	return date ? `${date[2]}-${date[3]}` : '--';
}
