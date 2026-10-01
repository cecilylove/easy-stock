import { Maximize2, X } from 'lucide-react';
import { useEffect, useId, useMemo, useRef, useState } from 'react';
import type { BackendConfig, KLine } from '../lib/backend';
import { shanghaiDayAndMinute } from '../lib/stock-intraday';
import { useModalDialog } from '../lib/use-modal-dialog';
import { ProfessionalKLineChart } from './ProfessionalKLineChart';
import { HistoricalIntradayDialog } from './HistoricalIntradayDialog';
import './theme-stock-chart.css';

type Props = { config: BackendConfig | null; symbol: string; name: string; lines: KLine[]; state: 'idle' | 'loading' | 'ready' | 'error' };

// Keep the compact chart mounted while inspecting overlays, preserving its
// time anchor and the surrounding theme selection and filters.
export function ThemeStockChart({ config, symbol, name, lines, state }: Props) {
	const [expandedSymbol, setExpanded] = useState<string | null>(null);
	const expanded = expandedSymbol === symbol;
	const [intraday, setIntraday] = useState<{ symbol: string; date: string } | null>(null);
	const intradayDate = config && intraday?.symbol === symbol ? intraday.date : null;
	const openHistory = (date: string) => setIntraday({ symbol, date });
	const [historyVisible, setHistoryVisible] = useState(false);
	const dialogRef = useRef<HTMLDivElement>(null);
	const titleId = useId();
	const dates = useMemo(() => [...new Set(lines.map(line => shanghaiDayAndMinute(line.time)?.day).filter((date): date is string => Boolean(date)))].sort(), [lines]);
	useModalDialog(expanded && !intradayDate, dialogRef, () => setExpanded(null));
	// Handoff focus/scroll ownership in a separate commit: first release the
	// expanded dialog, then mount the historical dialog's independent trap.
	useEffect(() => { setHistoryVisible(Boolean(intradayDate)); }, [intradayDate]);
	useEffect(() => { setExpanded(null); setIntraday(null); setHistoryVisible(false); }, [symbol]);
	const closeHistory = () => { setHistoryVisible(false); setIntraday(null); };
	return <div className="theme-stock-chart">
		<div className="theme-chart-actions"><span>悬浮查看量价 · 单击锁定日期</span><button type="button" disabled={!lines.length || !symbol} onClick={() => setExpanded(symbol)}><Maximize2 size={14} />放大图表</button></div>
		<ProfessionalKLineChart key={symbol} lines={lines} symbol={symbol} periodLabel="日K" state={state} compact onOpenIntraday={config ? openHistory : undefined} />
		{expanded && <div className="theme-chart-backdrop" hidden={Boolean(intradayDate)} onMouseDown={event => { if (event.target === event.currentTarget) setExpanded(null); }}>
			<div className="theme-chart-dialog" ref={dialogRef} role="dialog" aria-modal="true" aria-labelledby={titleId} tabIndex={-1}>
				<header><div><span>题材个股日 K</span><h2 id={titleId}>{name || symbol} · {symbol}</h2></div><button type="button" data-dialog-autofocus aria-label="关闭放大图表" onClick={() => setExpanded(null)}><X size={20} /></button></header>
				<ProfessionalKLineChart lines={lines} symbol={symbol} periodLabel="日K" state={state} onOpenIntraday={config ? openHistory : undefined} />
			</div>
		</div>}
		{historyVisible && intradayDate && config && <HistoricalIntradayDialog config={config} symbol={symbol} date={intradayDate} availableDates={dates} onClose={closeHistory} />}
	</div>;
}
