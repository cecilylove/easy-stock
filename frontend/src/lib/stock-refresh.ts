import { shanghaiDayAndMinute } from './stock-intraday';

export type RefreshKind = 'quote' | 'intraday' | 'kline' | 'auction';

export function refreshInterval(kind: RefreshKind, now: Date): number | null {
	const china = shanghaiDayAndMinute(now.toISOString());
	if (!china) return null;
	const weekday = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', weekday: 'short' }).format(now);
	if (weekday === 'Sat' || weekday === 'Sun') return null;
	const minute = china.minute;
	const auction = minute >= 9 * 60 + 15 && minute < 9 * 60 + 30;
	const trade = (minute >= 9 * 60 + 30 && minute < 11 * 60 + 31) || (minute >= 13 * 60 && minute <= 15 * 60);
	if (kind === 'auction') return auction ? 5_000 : null;
	if (kind === 'intraday') return trade ? 5_000 : null;
	if (kind === 'kline') return trade ? 60_000 : null;
	return auction || trade ? 5_000 : null;
}
