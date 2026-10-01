import type { KLine } from './backend';
import { sourceFieldAvailable } from './source-fields';

const morningStart = 9 * 60 + 30;
const morningEnd = 11 * 60 + 30;
const afternoonStart = 13 * 60;
const afternoonEnd = 15 * 60;
const tradingMinutes = (morningEnd - morningStart) + (afternoonEnd - afternoonStart);

export function shanghaiDayAndMinute(iso: string): { day: string; minute: number } | null {
	const date = new Date(iso);
	if (Number.isNaN(date.getTime())) return null;
	const parts = new Intl.DateTimeFormat('en-US', { timeZone: 'Asia/Shanghai', year: 'numeric', month: '2-digit', day: '2-digit', hour: '2-digit', minute: '2-digit', hourCycle: 'h23' }).formatToParts(date);
	const value = (key: string) => parts.find(part => part.type === key)?.value || '';
	const minute = Number(value('hour')) * 60 + Number(value('minute'));
	return { day: `${value('year')}-${value('month')}-${value('day')}`, minute };
}

export function tradingFraction(minute: number): number | null {
	if (minute < morningStart || minute > afternoonEnd || (minute > morningEnd && minute < afternoonStart)) return null;
	return minute <= morningEnd ? (minute - morningStart) / tradingMinutes : (morningEnd - morningStart + minute - afternoonStart) / tradingMinutes;
}

export function auctionFraction(minute: number): number | null {
	return minute >= 9 * 60 + 15 && minute <= 9 * 60 + 25 ? (minute - (9 * 60 + 15)) / 10 : null;
}

export function tradingSessionForDay(lines: KLine[], day: string) {
	return lines.map(line => ({ line, instant: shanghaiDayAndMinute(line.time) }))
		.filter((item): item is { line: KLine; instant: { day: string; minute: number } } => Boolean(item.instant) && item.instant?.day === day && tradingFraction(item.instant.minute) != null && sourceFieldAvailable(item.line.meta, 'close') && Number.isFinite(item.line.close) && item.line.close > 0 && Number.isFinite(item.line.volume) && item.line.volume >= 0)
		.sort((a, b) => a.instant.minute - b.instant.minute);
}

export function minuteText(minute: number) {
	return `${String(Math.floor(minute / 60)).padStart(2, '0')}:${String(minute % 60).padStart(2, '0')}`;
}

// Coverage is relative to the last observed minute, never to a fabricated close.
// Completed bars may begin at 09:31 / 13:01. Sina's closing auction is represented
// by a single 15:00 bar after 14:57; the two omitted labels do not imply a gap.
export function intradaySampleCoverage(minutes: number[]) {
	const observed = [...new Set(minutes)].sort((a, b) => a - b);
	const first = observed[0];
	const startsAtOpen = first === morningStart || first === morningStart + 1;
	const hasGaps = observed.some((minute, index) => {
		if (!index || minute - observed[index - 1] <= 1) return false;
		const previous = observed[index - 1];
		return !(previous === morningEnd && (minute === afternoonStart || minute === afternoonStart + 1))
			&& !(previous === afternoonEnd - 3 && minute === afternoonEnd);
	});
	return { startsAtOpen, hasGaps, first, last: observed.at(-1), count: observed.length };
}

export function latestTradingSession(lines: KLine[]) {
	const indexed = lines.map(line => ({ line, instant: shanghaiDayAndMinute(line.time) })).filter((item): item is { line: KLine; instant: { day: string; minute: number } } => Boolean(item.instant));
	const latestDay = indexed.reduce((day, item) => item.instant.day > day ? item.instant.day : day, '');
	return indexed.filter(item => item.instant.day === latestDay && tradingFraction(item.instant.minute) != null).sort((a, b) => a.instant.minute - b.instant.minute);
}
