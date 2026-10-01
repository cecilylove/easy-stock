import { ChevronLeft, ChevronRight, RefreshCw, X } from 'lucide-react';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import { requestJSON, type BackendConfig, type HistoricalIntradayData, type HistoricalIntradayResponse } from '../lib/backend';
import { sourceName } from '../lib/source-integrations';
import { minuteText, shanghaiDayAndMinute, tradingSessionForDay } from '../lib/stock-intraday';
import { useModalDialog } from '../lib/use-modal-dialog';
import { StockIntradayChart } from './StockIntradayChart';
import './historical-intraday.css';

type Props = {
	config: BackendConfig;
	symbol: string;
	date: string;
	availableDates?: string[];
	onClose: () => void;
};

function validDate(value: string) {
	if (!/^\d{4}-\d{2}-\d{2}$/.test(value)) return false;
	const parsed = new Date(`${value}T00:00:00Z`);
	return Number.isFinite(parsed.getTime()) && parsed.toISOString().slice(0, 10) === value;
}

export function HistoricalIntradayDialog({ config: suppliedConfig, symbol, date, availableDates = [], onClose }: Props) {
	const config = useMemo(() => ({ backendUrl: suppliedConfig.backendUrl, token: suppliedConfig.token }), [suppliedConfig.backendUrl, suppliedConfig.token]);
	const scope = JSON.stringify([config.backendUrl, config.token, symbol]);
	const selectionKey = JSON.stringify([symbol, date]);
	const [selection, setSelection] = useState({ key: selectionKey, date });
	const selectedDate = selection.key === selectionKey ? selection.date : date;
	const selectDate = (value: string) => setSelection({ key: selectionKey, date: value });
	const [result, setResult] = useState<(HistoricalIntradayData & { scope: string }) | null>(null);
	const [busy, setBusy] = useState(true);
	const [error, setError] = useState('');
	const [retry, setRetry] = useState(0);
	const dialogRef = useRef<HTMLDivElement>(null);
	const titleId = useId();
	useModalDialog(true, dialogRef, onClose);
	useEffect(() => { setResult(null); }, [scope, date]);

	useEffect(() => {
		if (!validDate(selectedDate)) { setBusy(false); setError('交易日格式无效'); return; }
		const abort = new AbortController();
		setBusy(true); setError('');
		void requestJSON<HistoricalIntradayResponse>(config, `/api/v1/quotes/intraday?symbol=${encodeURIComponent(symbol)}&date=${encodeURIComponent(selectedDate)}`, { signal: abort.signal })
			.then(({ data }) => {
				if (abort.signal.aborted) return;
				if (!data || data.symbol !== symbol || data.trade_date !== selectedDate) throw new Error('来源返回的股票或交易日与当前选择不一致');
				if (!['available', 'partial', 'unavailable'].includes(data.availability)) throw new Error('历史分时数据状态无效');
				const lines = data.availability === 'unavailable' ? [] : (data.lines || []).filter(line => line.symbol === symbol && shanghaiDayAndMinute(line.time)?.day === selectedDate && Number.isFinite(line.close) && line.close > 0);
				if (data.availability !== 'unavailable' && !lines.length) throw new Error('该交易日未返回有效分时数据');
				setResult({ ...data, scope, lines, available_dates: (data.available_dates || []).filter(validDate) });
			})
			.catch(reason => { if (!abort.signal.aborted) setError(reason instanceof Error ? reason.message : '历史分时暂不可用'); })
			.finally(() => { if (!abort.signal.aborted) setBusy(false); });
		return () => abort.abort();
	}, [config, scope, symbol, selectedDate, retry]);

	const current = result?.scope === scope && result.trade_date === selectedDate ? result : null;
	const serviceDates = result?.scope === scope ? result.available_dates : [];
	const knownAvailable = [...new Set((serviceDates.length ? serviceDates : availableDates).filter(validDate))].sort();
	const dates = [...new Set([...knownAvailable, selectedDate].filter(validDate))].sort();
	const index = dates.indexOf(selectedDate);
	const previous = index > 0 ? dates[index - 1] : null;
	const next = index >= 0 && index < dates.length - 1 ? dates[index + 1] : null;
	const meta = current?.meta;
	const fetched = meta?.fetched_at ? new Date(meta.fetched_at) : null;
	const session = tradingSessionForDay(current?.lines || [], selectedDate);
	const coverageText = session.length ? `${minuteText(session[0].instant.minute)}–${minuteText(session.at(-1)!.instant.minute)} · ${session.length} 个有效时间点` : '';
	return <div className="historical-intraday-backdrop" onMouseDown={event => { if (event.target === event.currentTarget) onClose(); }}>
		<div className="historical-intraday-dialog" ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1}>
			<header className="historical-intraday-header"><div><span>历史分时</span><h2 id={titleId}>{symbol} · {selectedDate}</h2></div><button type="button" className="historical-intraday-close" aria-label="关闭历史分时" data-dialog-autofocus onClick={onClose}><X size={20} /></button></header>
			<div className="historical-intraday-navigation" role="group" aria-label="历史分时日期导航">
				<button type="button" disabled={!previous} onClick={() => previous && selectDate(previous)}><ChevronLeft size={16} />上一交易日</button>
				<label>交易日 <input type="date" aria-label="历史分时交易日" value={selectedDate} onChange={event => { if (validDate(event.target.value)) selectDate(event.target.value); }} /></label>
				<button type="button" disabled={!next} onClick={() => next && selectDate(next)}>下一交易日<ChevronRight size={16} /></button>
				<button type="button" disabled={busy} onClick={() => setRetry(value => value + 1)}><RefreshCw size={15} />{error ? '重试' : '重新获取'}</button>
			</div>
			<p className="historical-intraday-range">{serviceDates.length ? `本次来源覆盖 ${knownAvailable[0]} 至 ${knownAvailable.at(-1)}；${knownAvailable.length} 个日期来自来源分时覆盖记录。` : '分时覆盖范围尚未确认；导航候选来自已加载日 K，分时是否可用仍以查询结果为准。'} 前后导航使用已知交易日，未提供的日期不会猜测。</p>
			<div className={`historical-intraday-status${!busy && current?.availability === 'partial' ? ' historical-intraday-partial' : ''}`} role="status">{busy ? `正在获取 ${selectedDate} 的历史分时…` : current?.availability === 'partial' ? `来源仅返回该交易日部分分时点：${coverageText}。缺少的时段没有数据，不能作为全日走势。` : current?.availability === 'unavailable' ? `${selectedDate} 的历史分时暂不可用。${current.message || '当前来源未返回该日数据。'}` : current ? `${selectedDate} 的历史分时已加载 · ${coverageText}。` : null}</div>
			{error && <p className="historical-intraday-error" role="alert">{selectedDate}：{error}{current?.lines.length ? '；保留本交易日上次查询快照。' : ''}</p>}
			<div className="historical-intraday-content" aria-busy={busy}>
				{current?.lines.length ? <StockIntradayChart key={`${symbol}:${selectedDate}`} lines={current.lines} showAuction={false} symbol={symbol} tradeDay={selectedDate} previousClose={current.previous_close} /> : <div className="historical-intraday-empty">{busy ? '历史分时加载中' : error || current?.availability === 'unavailable' ? '所选交易日没有可展示的分时数据' : '该交易日暂未返回分时数据'}</div>}
			</div>
			<footer className="historical-intraday-footer"><p>来源 {meta?.source ? sourceName(meta.source) : '待确认'}{fetched && Number.isFinite(fetched.getTime()) ? ` · 抓取 ${fetched.toLocaleString('zh-CN')}` : ''}{meta?.stale ? ' · 缓存 / 陈旧快照，非新查询数据' : ''}{meta?.fallback_reason ? ` · ${meta.fallback_reason}` : ''}{current?.message ? ` · ${current.message}` : ''}</p><p>历史分时使用当日原始成交价格，与前复权 / 后复权日 K 的价格口径不同。历史查询不自动刷新，不包含集合竞价参考轨迹；数据仅供研究参考。</p></footer>
		</div>
	</div>;
}
