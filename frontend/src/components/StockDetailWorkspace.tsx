import { ArrowRight, Clock3, RefreshCw, Search, ShieldAlert } from 'lucide-react';
import { type FormEvent, type KeyboardEvent, useEffect, useMemo, useRef, useState } from 'react';
import { KLineChart } from './KLineChart';
import { StockIntradayChart } from './StockIntradayChart';
import { requestJSON, type AuctionResponse, type AuctionTrace, type BackendConfig, type KLine, type Quote, type StockDirectoryData, type StockDirectoryEntry } from '../lib/backend';
import { latestTradingDayKLines } from '../lib/kline';
import { shanghaiDayAndMinute } from '../lib/stock-intraday';
import { refreshInterval, type RefreshKind } from '../lib/stock-refresh';
import { readAuctionTrace, saveAuctionTrace } from '../lib/stock-auction-cache';
import { searchStockDirectory } from '../lib/stock-analysis';
import { loadCachedStockDirectory, saveCachedStockDirectory } from '../lib/stock-directory-cache';
import { resolveStockDetailSymbol, stockDetailPeriods, type StockDetailPeriod } from '../lib/stock-detail';

const chartCacheTTL = 60_000;
type SavedChart = { key: string; lines: KLine[]; loadedAt: string };
type LoadState = 'idle' | 'loading' | 'ready' | 'error';

type Props = {
	config: BackendConfig | null;
	symbol: string;
	onSelectSymbol: (symbol: string) => void;
	onOpenAnalysis: (symbol: string) => void;
	refreshKey: number;
};

function dateTime(value?: string) {
	if (!value) return '--';
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? '--' : date.toLocaleString('zh-CN');
}

function sourceNotice(meta: { source: string; fetched_at: string; stale: boolean; fallback_reason?: string } | undefined) {
	if (!meta) return '来源待确认';
	return `来源 ${meta.source || '待确认'} · 抓取 ${dateTime(meta.fetched_at)}${meta.stale ? ' · 缓存/陈旧' : ''}${meta.fallback_reason ? ` · ${meta.fallback_reason}` : ''}`;
}

export function StockDetailWorkspace({ config, symbol, onSelectSymbol, onOpenAnalysis, refreshKey }: Props) {
	const [directory, setDirectory] = useState<StockDirectoryEntry[]>(loadCachedStockDirectory);
	const [directoryState, setDirectoryState] = useState<LoadState>('idle');
	const [directoryStale, setDirectoryStale] = useState(false);
	const [directoryFromCache, setDirectoryFromCache] = useState(() => loadCachedStockDirectory().length > 0);
	const [query, setQuery] = useState(symbol);
	const [searchOpen, setSearchOpen] = useState(false);
	const [activeSuggestion, setActiveSuggestion] = useState(-1);
	const [searchError, setSearchError] = useState('');
	const [periodKey, setPeriodKey] = useState<StockDetailPeriod>('day');
	const [showAuction, setShowAuction] = useState(false);
	const [auction, setAuction] = useState<AuctionTrace | null>(null);
	const [auctionState, setAuctionState] = useState<LoadState | 'stale'>('idle');
	const [auctionError, setAuctionError] = useState('');
	const [auctionPollKey, setAuctionPollKey] = useState(0);
	const auctionRetryAfter = useRef(0);
	const auctionMemory = useRef(new Map<string, AuctionTrace>());
	const pendingRequest = useRef<Record<RefreshKind, AbortController | null>>({ quote: null, intraday: null, auction: null });
	const [quote, setQuote] = useState<Quote | null>(null);
	const [quoteState, setQuoteState] = useState<LoadState>('idle');
	const [quoteError, setQuoteError] = useState('');
	const [chart, setChart] = useState<SavedChart | null>(null);
	const [chartState, setChartState] = useState<LoadState>('idle');
	const [chartError, setChartError] = useState('');
	const [reload, setReload] = useState(0);
	const [quotePollKey, setQuotePollKey] = useState(0);
	const [currentDay, setCurrentDay] = useState(() => shanghaiDayAndMinute(new Date().toISOString())?.day || '');
	const [chartPollKey, setChartPollKey] = useState(0);
	const chartCache = useRef(new Map<string, SavedChart>());
	const previousRefresh = useRef({ reload, refreshKey, chartPollKey });
	const activeSymbol = useRef(symbol);
	const activePeriod = useRef(periodKey);
	activeSymbol.current = symbol;
	activePeriod.current = periodKey;
	const period = stockDetailPeriods.find(item => item.key === periodKey) || stockDetailPeriods[2];
	const chartForSelection = chart?.key === `${symbol}:${periodKey}` ? chart : null;
	const today = currentDay;
	const chartDay = chartForSelection?.lines.length ? shanghaiDayAndMinute(chartForSelection.lines.at(-1)!.time)?.day : '';
	const todayIntraday = chartDay === today ? chartForSelection?.lines || [] : [];
	const auctionForSelection = auction?.symbol === symbol && auction.trade_date === today ? auction : null;
	const displayedDay = periodKey === 'intraday' && showAuction ? today : chartDay || today;
	const suggestions = useMemo(() => searchStockDirectory(directory, query), [directory, query]);
	const shownQuote = quote?.symbol === symbol ? quote : null;
	const quoteDay = shownQuote?.trade_time ? shanghaiDayAndMinute(shownQuote.trade_time)?.day : '';
	const previousClose = quoteDay === displayedDay && shownQuote?.previous_close && shownQuote.previous_close > 0 ? shownQuote.previous_close : undefined;
	const historicalQuote = Boolean(shownQuote?.meta.fallback_reason?.includes('历史快照'));
	const historicalChart = Boolean(chartForSelection?.lines.at(-1)?.meta.fallback_reason?.includes('历史快照'));
	const stockName = directory.find(item => item.symbol === symbol)?.name || shownQuote?.name || symbol;
	const autoRefresh = '工作日盘中且页面可见时自动刷新；午休、收盘后与周末暂停，节假日仍以来源数据时间为准';

	useEffect(() => { setQuery(symbol); setSearchError(''); setSearchOpen(false); }, [symbol]);
	useEffect(() => { setPeriodKey('day'); }, [symbol]);
	useEffect(() => {
		const cached = symbol ? auctionMemory.current.get(`${symbol}:${currentDay}`) || readAuctionTrace(symbol, currentDay) : null;
		setAuction(cached || null); setAuctionState('idle');
	}, [symbol, currentDay]);

	useEffect(() => {
		if (!config) return;
		const abort = new AbortController();
		setDirectoryState('loading');
		void requestJSON<{ data: StockDirectoryData }>(config, '/api/v1/stocks/directory', { signal: abort.signal })
			.then(({ data }) => {
				if (abort.signal.aborted) return;
				setDirectory(data.stocks || []);
				setDirectoryStale(data.stale);
				setDirectoryFromCache(data.stale);
				setDirectoryState('ready');
				if (!data.stale) saveCachedStockDirectory(data.stocks || []);
			})
			.catch(() => { if (!abort.signal.aborted) { setDirectoryState('error'); setDirectoryFromCache(loadCachedStockDirectory().length > 0); } });
		return () => abort.abort();
	}, [config]);

	useEffect(() => {
		if (!symbol || !config) return;
		const lastPoll: Record<RefreshKind, number> = { quote: Date.now(), intraday: Date.now(), auction: Date.now() };
		const tick = () => {
			const now = new Date();
			const day = shanghaiDayAndMinute(now.toISOString())?.day || '';
			if (day) setCurrentDay(oldDay => day === oldDay ? oldDay : day);
			if (document.visibilityState !== 'visible') return;
			for (const kind of ['quote', 'intraday', 'auction'] as const) {
				if (kind === 'intraday' && periodKey !== 'intraday') continue;
				if (kind === 'auction' && (periodKey !== 'intraday' || !showAuction)) continue;
				const interval = refreshInterval(kind, now);
				if (!interval || pendingRequest.current[kind] || now.getTime() - lastPoll[kind] < interval || (kind === 'auction' && now.getTime() < auctionRetryAfter.current)) continue;
				lastPoll[kind] = now.getTime();
				if (kind === 'quote') setQuotePollKey(value => value + 1);
				if (kind === 'intraday') setChartPollKey(value => value + 1);
				if (kind === 'auction') setAuctionPollKey(value => value + 1);
			}
		};
		const timer = window.setInterval(tick, 5_000);
		document.addEventListener('visibilitychange', tick);
		return () => { window.clearInterval(timer); document.removeEventListener('visibilitychange', tick); };
	}, [symbol, config, periodKey, showAuction]);

	useEffect(() => {
		if (!symbol || !config || periodKey !== 'intraday' || !showAuction) { setAuctionState('idle'); return; }
		const abort = new AbortController();
		pendingRequest.current.auction = abort;
		setAuctionError('');
		setAuctionState('loading');
		void requestJSON<AuctionResponse>(config, `/api/v1/quotes/auction?symbol=${encodeURIComponent(symbol)}&detail=1`, { signal: abort.signal })
			.then(({ data, status }) => {
				if (abort.signal.aborted) return;
				if (status !== 'ready' || !data.points.length) {
					const day = shanghaiDayAndMinute(new Date().toISOString())?.day || '';
					const cached = auctionMemory.current.get(`${symbol}:${day}`) || readAuctionTrace(symbol, day);
					auctionRetryAfter.current = Date.now() + 60_000;
					setAuction(cached); setAuctionState(cached ? 'stale' : 'error'); setAuctionError('来源没有当日竞价点'); return;
				}
				if (data.symbol !== symbol || data.trade_date !== shanghaiDayAndMinute(new Date().toISOString())?.day) throw new Error('竞价来源股票或交易日与当前选择不一致');
				if (data.meta.stale) {
					setAuction(data); setAuctionState('stale'); setAuctionError('上游刷新失败，服务端正在退避'); return;
				}
				saveAuctionTrace(data);
				auctionMemory.current.set(`${symbol}:${data.trade_date}`, data);
				auctionRetryAfter.current = 0;
				setAuction(data); setAuctionState('ready');
			})
			.catch(error => { if (!abort.signal.aborted) {
				const day = shanghaiDayAndMinute(new Date().toISOString())?.day || '';
				const cached = auctionMemory.current.get(`${symbol}:${day}`) || readAuctionTrace(symbol, day);
				auctionRetryAfter.current = Date.now() + 60_000;
				setAuction(cached); setAuctionState(cached ? 'stale' : 'error'); setAuctionError(error instanceof Error ? error.message : '竞价数据暂不可用');
			} }).finally(() => { if (pendingRequest.current.auction === abort) pendingRequest.current.auction = null; });
		return () => { abort.abort(); if (pendingRequest.current.auction === abort) pendingRequest.current.auction = null; };
	}, [symbol, config, periodKey, showAuction, auctionPollKey, reload, refreshKey, currentDay]);

	useEffect(() => {
		if (!symbol || !config) { setQuote(null); setQuoteState('idle'); return; }
		const abort = new AbortController();
		pendingRequest.current.quote = abort;
		setQuoteError('');
		setQuoteState('loading');
		void requestJSON<{ data: Quote[] }>(config, `/api/v1/quotes/realtime?symbols=${encodeURIComponent(symbol)}&detail=1`, { signal: abort.signal })
			.then(({ data }) => {
				if (abort.signal.aborted) return;
				const result = data.find(item => item.symbol === symbol);
				if (!result) throw new Error('未获取到该股票的实时行情');
				setQuote(result); setQuoteState('ready');
			})
			.catch(error => { if (!abort.signal.aborted) { setQuoteState('error'); setQuoteError(error instanceof Error ? error.message : '行情暂不可用'); } })
			.finally(() => { if (pendingRequest.current.quote === abort) pendingRequest.current.quote = null; });
		return () => { abort.abort(); if (pendingRequest.current.quote === abort) pendingRequest.current.quote = null; };
	}, [symbol, config, reload, refreshKey, quotePollKey]);

	useEffect(() => {
		if (!symbol || !config) { setChart(null); setChartState('idle'); return; }
		const key = `${symbol}:${period.key}`;
		const cached = chartCache.current.get(key) || null;
		const force = previousRefresh.current.reload !== reload || previousRefresh.current.refreshKey !== refreshKey || (period.key === 'intraday' && previousRefresh.current.chartPollKey !== chartPollKey);
		previousRefresh.current = { reload, refreshKey, chartPollKey };
		setChart(cached);
		setChartError('');
		if (cached && !force && Date.now() - new Date(cached.loadedAt).getTime() < chartCacheTTL) { setChartState('ready'); return; }
		const abort = new AbortController();
		if (period.key === 'intraday') pendingRequest.current.intraday = abort;
		setChartState('loading');
		void requestJSON<{ data: KLine[] }>(config, `/api/v1/quotes/kline?symbol=${encodeURIComponent(symbol)}&period=${period.apiPeriod}&limit=${period.limit}${period.key === 'intraday' ? '&detail=1' : ''}`, { signal: abort.signal })
			.then(({ data }) => {
				if (abort.signal.aborted || activeSymbol.current !== symbol || activePeriod.current !== period.key) return;
				const lines = period.key === 'intraday' ? latestTradingDayKLines(data || []) : data || [];
				if (!lines.length) throw new Error(`${period.label}暂未返回数据`);
				const stale = Boolean(lines.at(-1)?.meta.stale);
				const historical = Boolean(lines.at(-1)?.meta.fallback_reason?.includes('历史快照'));
				const previous = chartCache.current.get(key);
				if (stale && !historical && previous && !previous.lines.at(-1)?.meta.stale) {
					setChart(previous); setChartState('error'); setChartError('上游刷新失败，保留本页上次成功的分时快照'); return;
				}
				const next = { key, lines, loadedAt: stale ? lines.at(-1)!.meta.fetched_at : new Date().toISOString() };
				if (!stale) {
					chartCache.current.delete(key);
					chartCache.current.set(key, next);
					while (chartCache.current.size > 24) chartCache.current.delete(chartCache.current.keys().next().value!);
				}
				setChart(next); setChartState('ready');
			})
			.catch(error => { if (!abort.signal.aborted) { setChartState('error'); setChartError(error instanceof Error ? error.message : `${period.label}暂不可用`); } })
			.finally(() => { if (pendingRequest.current.intraday === abort) pendingRequest.current.intraday = null; });
		return () => { abort.abort(); if (pendingRequest.current.intraday === abort) pendingRequest.current.intraday = null; };
	}, [symbol, period.key, period.apiPeriod, period.limit, config, reload, refreshKey, chartPollKey]);

	const submit = (event: FormEvent) => {
		event.preventDefault();
		const resolved = resolveStockDetailSymbol(query, directory);
		if (!resolved) { setSearchError('未找到唯一股票，请从候选中选择或输入有效的六位 A 股代码'); return; }
		setSearchError(''); setSearchOpen(false);
		if (resolved === symbol) { setReload(current => current + 1); return; }
		onSelectSymbol(resolved);
	};
	const onKeyDown = (event: KeyboardEvent<HTMLInputElement>) => {
		if (event.key === 'Escape') { setSearchOpen(false); return; }
		if (!searchOpen || !suggestions.length) return;
		if (event.key === 'ArrowDown') { event.preventDefault(); setActiveSuggestion(index => (index + 1) % suggestions.length); }
		if (event.key === 'ArrowUp') { event.preventDefault(); setActiveSuggestion(index => index < 0 ? suggestions.length - 1 : (index - 1 + suggestions.length) % suggestions.length); }
		if (event.key === 'Enter' && activeSuggestion >= 0 && suggestions[activeSuggestion] && !resolveStockDetailSymbol(query, directory)) {
			event.preventDefault();
			onSelectSymbol(suggestions[activeSuggestion].symbol);
		}
	};

	return <section className="stock-detail-workspace">
		<header className="stock-detail-hero">
			<div><span className="stock-detail-eyebrow">STOCK QUOTES</span><h2>个股详情</h2><p>搜索股票，直接查看行情和多周期 K 线；无需调用 AI。</p></div>
			<form onSubmit={submit} className="stock-detail-search" onBlur={event => { if (!event.currentTarget.contains(event.relatedTarget as Node | null)) setSearchOpen(false); }}>
				<label><Search size={18} /><input aria-label="搜索股票名称或代码" value={query} onChange={event => { setQuery(event.target.value); setSearchError(''); setSearchOpen(true); setActiveSuggestion(-1); }} onFocus={() => setSearchOpen(true)} onKeyDown={onKeyDown} placeholder="输入股票名称或代码，如 贵州茅台 / 600519" role="combobox" aria-autocomplete="list" aria-controls="stock-detail-suggestions" aria-activedescendant={searchOpen && suggestions.length && activeSuggestion >= 0 ? `stock-detail-option-${activeSuggestion}` : undefined} aria-expanded={searchOpen && suggestions.length > 0} /></label>
				<button type="submit">查看</button>
				{searchOpen && query.trim() && suggestions.length > 0 && <div className="stock-detail-suggestions" id="stock-detail-suggestions" role="listbox">{suggestions.map((item, index) => <button id={`stock-detail-option-${index}`} key={item.symbol} type="button" role="option" aria-selected={index === activeSuggestion} className={index === activeSuggestion ? 'active' : ''} onMouseDown={event => event.preventDefault()} onMouseEnter={() => setActiveSuggestion(index)} onClick={() => onSelectSymbol(item.symbol)}><strong>{item.name}</strong><small>{item.symbol}</small></button>)}</div>}
			</form>
		</header>
		{searchError && <p className="stock-detail-error" role="alert">{searchError}</p>}
		{directoryState === 'error' && <p className="stock-detail-hint">股票目录暂不可用，{directoryFromCache ? '名称搜索沿用本机旧目录；' : ''}可以直接输入六位代码。</p>}
		{directoryStale && <p className="stock-detail-hint">股票名称来自旧目录快照，代码仍需以行情返回为准。</p>}
		{directoryState === 'loading' && directoryFromCache && <p className="stock-detail-hint">名称搜索暂用本机目录缓存，正在检查最新目录。</p>}
		{!symbol ? <div className="stock-detail-empty"><Search size={29} /><h3>选择一只 A 股</h3><p>输入股票名称或代码，即可查看行情和 K 线。</p></div> : <>
			<section className="stock-detail-quote">
				<div><span>最近行情快照</span><h3>{stockName}<small>{symbol}</small></h3></div>
				<div className="stock-detail-price"><strong>{shownQuote && Number.isFinite(shownQuote.price) && shownQuote.price > 0 ? shownQuote.price.toFixed(2) : '--'}</strong><span className={shownQuote && shownQuote.change_percent > 0 ? 'up' : shownQuote && shownQuote.change_percent < 0 ? 'down' : ''}>{shownQuote && Number.isFinite(shownQuote.change_percent) ? `${shownQuote.change_percent > 0 ? '+' : ''}${shownQuote.change_percent.toFixed(2)}%` : '--'}</span>{shownQuote && (quoteState !== 'ready' || shownQuote.meta.stale) && <small className="stock-detail-old-badge">{historicalQuote ? '历史行情 · 非当日' : quoteState === 'error' || shownQuote.meta.stale ? '旧快照 · 来源退避' : '旧快照 · 更新中'}</small>}</div>
				<div className="stock-detail-actions"><button type="button" onClick={() => setReload(current => current + 1)}><RefreshCw size={15} />刷新行情</button><button type="button" onClick={() => onOpenAnalysis(symbol)}>去个股分析 <ArrowRight size={15} /></button></div>
				<div className="stock-detail-quote-meta"><Clock3 size={14} />{quoteState === 'loading' ? (shownQuote ? `更新中，暂显示上次查询快照 · ${sourceNotice(shownQuote.meta)} · 行情时间 ${dateTime(shownQuote.trade_time)}` : '实时行情加载中…') : quoteState === 'error' ? <span className="stock-detail-warning"><ShieldAlert size={14} />{shownQuote ? '实时行情更新失败，以上为上次查询快照：' : '实时行情暂不可用：'}{quoteError} <button type="button" onClick={() => setReload(current => current + 1)}>重试</button></span> : shownQuote ? `${sourceNotice(shownQuote.meta)} · 行情时间 ${dateTime(shownQuote.trade_time)}` : '实时行情未返回'}</div>
			</section>
			<section className="stock-detail-chart-panel">
				<div className="stock-detail-chart-header"><div><span>价格走势</span><h3>{stockName} · {period.label}</h3></div><div className="stock-detail-periods" role="group" aria-label="K线周期">{stockDetailPeriods.map(item => <button type="button" aria-pressed={periodKey === item.key} className={periodKey === item.key ? 'active' : ''} key={item.key} onClick={() => setPeriodKey(item.key)}>{item.label}</button>)}</div></div>
				{periodKey === 'intraday' && <label className="stock-detail-auction-toggle"><input type="checkbox" checked={showAuction} onChange={event => setShowAuction(event.target.checked)} />显示集合竞价参考轨迹</label>}
				{periodKey === 'intraday' && showAuction && <p className={auctionState === 'error' || auctionState === 'stale' ? 'stock-detail-error' : 'stock-detail-hint'} role={auctionState === 'error' || auctionState === 'stale' ? 'alert' : undefined}>{auctionState === 'loading' ? (auctionForSelection ? `正在更新竞价；暂显示上次参考点（${sourceNotice(auctionForSelection.meta)}），非实时` : '正在获取竞价参考点…') : auctionState === 'error' || auctionState === 'stale' ? <>{auctionForSelection ? `竞价来源暂不可用（${auctionError}）；左侧仅显示当日旧快照（${sourceNotice(auctionForSelection.meta)}），非实时。` : `竞价来源暂不可用（${auctionError}）；当前没有可验证的当日竞价参考点。`}<button type="button" onClick={() => { auctionRetryAfter.current = 0; setAuctionPollKey(key => key + 1); }}>重试竞价</button></> : auctionForSelection ? `${sourceNotice(auctionForSelection.meta)} · 竞价参考点 ${auctionForSelection.points.length} 个` : '当日竞价参考点尚未返回，暂不显示'}</p>}
				{periodKey === 'intraday' && chartForSelection?.lines.at(-1)?.meta.stale && <p className={historicalChart ? 'stock-detail-hint' : 'stock-detail-error'} role="status">{historicalChart ? '当前来源只有上一交易日的分时数据，尚无当日分钟线。' : '分时来源暂时无法刷新，图中显示旧快照，非实时。'}来源时间 {dateTime(chartForSelection.lines.at(-1)?.meta.fetched_at)}。</p>}
				{chartState === 'loading' && chartForSelection && <p className="stock-detail-hint">正在更新；图表仍显示本页先前加载的旧快照（{dateTime(chartForSelection.loadedAt)}）。</p>}
				{chartState === 'error' && <p className="stock-detail-error" role="alert">{chartForSelection ? '更新失败，保留本页旧图：' : ''}{chartError} <button type="button" onClick={() => setReload(current => current + 1)}>重试</button></p>}
				{periodKey === 'intraday' && (chartForSelection?.lines.length || auctionForSelection?.points.length) ? <StockIntradayChart lines={showAuction ? todayIntraday : chartForSelection?.lines || []} auction={auctionForSelection} showAuction={showAuction} symbol={symbol} tradeDay={displayedDay} previousClose={previousClose} /> : <KLineChart key={`${symbol}:${period.key}`} symbol={symbol} lines={chartForSelection?.lines || []} state={chartForSelection ? 'ready' : chartState} mode={period.mode} periodLabel={period.label} />}
				<p className="stock-detail-hint">{autoRefresh} · 行情、分时、竞价各约每 5 秒触发；服务端同股请求合并，失败会退避，返回数据可能延迟，以各自时间为准。</p>
				<div className="stock-detail-chart-meta">{showAuction && periodKey === 'intraday' && chartDay && chartDay !== today ? `连续交易数据仅到 ${chartDay}，没有拼接到今日竞价；` : ''}{chartForSelection?.lines.length ? `${sourceNotice(chartForSelection.lines.at(-1)?.meta)} · 本页加载 ${dateTime(chartForSelection.loadedAt)}${chartState === 'error' ? ' · 更新失败' : ''}` : '此周期尚无可用数据'} · {period.key === 'five-day' ? '5分钟采样' : period.label} · 数据仅供研究参考</div>
			</section>
		</>}
	</section>;
}
