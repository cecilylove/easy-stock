import type { KLine } from './backend';
import { shanghaiDayAndMinute } from './stock-intraday';

function sameBar(a: KLine, b: KLine) {
	return a.symbol === b.symbol && a.time === b.time && a.open === b.open && a.high === b.high && a.low === b.low && a.close === b.close
		&& a.volume === b.volume && a.amount === b.amount && a.previous_close === b.previous_close && a.change_percent === b.change_percent && a.turnover_rate === b.turnover_rate;
}

// Snapshot polling remains authoritative for historical/adjusted candles. Only
// a same-source, same-day minute series can retain points missing from a delta.
export function reconcileChartLines(previous: KLine[], incoming: KLine[], intraday: boolean): KLine[] {
	const valid = incoming.filter(bar => Number.isFinite(Date.parse(bar.time)) && Number.isFinite(bar.close) && (!intraday || bar.close > 0));
	if (!valid.length) return [];
	const ordered = [...valid].sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
	const latest = ordered.at(-1)!;
	const oldLatest = previous.at(-1);
	const day = shanghaiDayAndMinute(latest.time)?.day;
	const retain = intraday && oldLatest?.symbol === latest.symbol && oldLatest?.meta?.source === latest.meta?.source && shanghaiDayAndMinute(oldLatest.time)?.day === day;
	const byTime = new Map<number, KLine>();
	if (retain) for (const bar of previous) byTime.set(Date.parse(bar.time), bar);
	for (const bar of ordered) {
		const time = Date.parse(bar.time);
		const old = byTime.get(time) || previous.find(item => Date.parse(item.time) === time);
		// Ignore an out-of-order minute snapshot, not an explicit newer correction.
		if (retain && old && Date.parse(bar.meta?.fetched_at || '') < Date.parse(old.meta?.fetched_at || '')) continue;
		byTime.set(time, old && sameBar(old, bar) && JSON.stringify(old.meta) === JSON.stringify(bar.meta) ? old : bar);
	}
	return [...byTime.values()].sort((a, b) => Date.parse(a.time) - Date.parse(b.time));
}

// A source omitting amount/volume must not be represented as a verified VWAP.
export function intradayAveragePrices(lines: KLine[]): Array<number | null> {
	let amount = 0, volume = 0, complete = true;
	let unit: number | null = null;
	return lines.map(line => {
		if (!Number.isFinite(line.volume) || line.volume < 0 || !Number.isFinite(line.amount) || line.amount < 0 || (line.volume > 0 && line.amount <= 0)) complete = false;
		if (!complete) return null;
		amount += line.amount;
		volume += line.volume;
		// EastMoney may supply volume in lots, Sina in shares. Accept only an
		// unambiguous convention consistent with this bar's reported price range.
		if (volume <= 0) return null;
		const ratio = line.volume > 0 ? line.amount / line.volume : 0;
		const fits = (price: number) => price >= line.low * .98 && price <= line.high * 1.02;
		const shares = fits(ratio), lots = fits(ratio / 100);
		if (line.volume > 0 && shares === lots) { complete = false; return null; }
		if (line.volume > 0) {
			const divisor = shares ? 1 : 100;
			if (unit != null && unit !== divisor) { complete = false; return null; }
			unit = divisor;
		}
		return unit ? amount / volume / unit : null;
	});
}
