import type { KLine } from './backend';

export function movingAverage(values: number[], period: number): Array<number | null> {
	let sum = 0;
	return values.map((value, index) => {
		sum += value;
		if (index >= period) sum -= values[index - period];
		return index + 1 >= period ? sum / period : null;
	});
}

export function calculateIndicators(lines: KLine[]) {
	const closes = lines.map(line => line.close);
	const ma = [5, 10, 20, 60].map(period => ({ period, values: movingAverage(closes, period) }));
	let ema12 = closes[0] || 0, ema26 = ema12, dea = 0, k = 50, d = 50;
	const macd: { dif: number; dea: number; histogram: number }[] = [];
	const kdj: Array<{ k: number; d: number; j: number } | null> = [];
	lines.forEach((line, index) => {
		ema12 = index ? ema12 * 11 / 13 + line.close * 2 / 13 : line.close;
		ema26 = index ? ema26 * 25 / 27 + line.close * 2 / 27 : line.close;
		const dif = ema12 - ema26;
		dea = dea * .8 + dif * .2;
		macd.push({ dif, dea, histogram: 2 * (dif - dea) });
		const sample = lines.slice(Math.max(0, index - 8), index + 1);
		const high = Math.max(...sample.map(item => item.high)), low = Math.min(...sample.map(item => item.low));
		const rsv = high === low ? 50 : (line.close - low) / (high - low) * 100;
		k = k * 2 / 3 + rsv / 3; d = d * 2 / 3 + k / 3;
		kdj.push(index < 8 ? null : { k, d, j: 3 * k - 2 * d });
	});
	return { ma, volumeMA: [5, 10].map(period => ({ period, values: movingAverage(lines.map(line => line.volume), period) })), macd, kdj };
}

export function resolveChartWindow(lines: KLine[], count: number, anchor: string | null) {
	let end = lines.length - 1;
	if (anchor) {
		end = 0;
		for (let index = 0; index < lines.length; index++) if (Date.parse(lines[index].time) <= Date.parse(anchor)) end = index;
	}
	return { start: Math.max(0, end - count + 1), end, count };
}
