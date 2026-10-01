import {
	Activity,
	AlertTriangle,
	ArrowDown,
	ArrowDownUp,
	ArrowUp,
	Building2,
	CalendarDays,
	ChevronDown,
	ExternalLink,
	FileText,
	Landmark,
	LoaderCircle,
	Search,
	TrendingUp,
	WalletCards,
} from 'lucide-react';
import { type MouseEvent as ReactMouseEvent, useEffect, useMemo, useState } from 'react';
import type {
	MarketBillboardItem,
	MarketBillboardDetail,
	MarketBillboardSeat,
	MarketFundFlow,
	MarketIndexSeries,
	MarketIndexSnapshot,
	MarketIndustryMomentum,
	MarketMarginPoint,
	MarketFuturesPositionSeries,
	MarketFuturesMembers,
	MarketFuturesConsensus,
	MarketResearchItem,
	SourceMeta,
} from '../../lib/backend';
import { classifyBillboardSeat } from '../../lib/billboard';
import { formatFuturesOpeningHands } from '../../lib/futures-position';
import { sourceName } from '../../lib/source-integrations';
import { sourceFieldAvailable } from '../../lib/source-fields';
import { formatIndexHistoryDate, formatIndexTradeTime } from '../../lib/source-time';

type DataState = 'idle' | 'loading' | 'ready' | 'error';
type SortDirection = 'asc' | 'desc';
type SortState = { key: string; direction: SortDirection };

const FUTURES_VARIETY_NAMES: Record<string, string> = { IF: '沪深300', IH: '上证50', IC: '中证500', IM: '中证1000' };

export type BillboardDetailEntry = {
	state: DataState;
	detail?: MarketBillboardDetail;
	error?: string;
};

export function ModuleState({ state, error, children }: { state: DataState; error: string; children: React.ReactNode }) {
	if (state === 'loading') return <div className="market-module-loading" aria-label="行情数据加载中">{Array.from({ length: 10 }, (_, index) => <i key={index} />)}</div>;
	if (state === 'error') return <div className="market-module-empty"><AlertTriangle size={24} /><strong>{error.includes('暂不可用') ? '该功能暂不可用' : '数据加载失败'}</strong><span>{error || '请稍后刷新重试'}</span></div>;
	return <>{children}</>;
}

export function SourceNotice({ meta }: { meta: SourceMeta | null }) {
	if (!meta) return null;
	return <div className={`market-source-notice ${meta.stale ? 'stale' : ''}`}>
		<span>来源 {sourceName(meta.source)} · 抓取 {formatDateTime(meta.fetched_at)}</span>
		{meta.partial && <em>部分覆盖{meta.missing_ids?.length ? ` · 缺少 ${meta.missing_ids.join('、')}` : ''}</em>}
		{meta.requested_sort && meta.effective_sort && meta.requested_sort !== meta.effective_sort && <em>排序降级：{meta.requested_sort} → {meta.effective_sort}</em>}
		{(meta.stale || meta.fallback_reason) && <em>{meta.stale ? '缓存快照' : ''}{meta.stale && meta.fallback_reason ? ' · ' : ''}{meta.fallback_reason || ''}</em>}
	</div>;
}

export function CoreIndexView({ indexes, selectedID, onSelect, series, seriesLoading, meta }: {
	indexes: MarketIndexSnapshot[];
	selectedID: string;
	onSelect: (id: string) => void;
	series: MarketIndexSeries | null;
	seriesLoading: boolean;
	meta: SourceMeta | null;
}) {
	const selected = indexes.find((item) => item.id === selectedID) || indexes[0];
	const lines = series?.lines || [];
	const rowHas = (item: MarketIndexSnapshot, field: string) => sourceFieldAvailable(item.meta, field, meta);
	const first = lines[0]?.close;
	const latest = lines.at(-1)?.close;
	const returnPercent = first && latest && lines.every(line => sourceFieldAvailable(line.meta, 'close', series?.meta)) ? (latest / first - 1) * 100 : NaN;
	const high = lines.length && lines.every(line => sourceFieldAvailable(line.meta, 'high', series?.meta)) ? Math.max(...lines.map((line) => line.high)) : NaN;
	const low = lines.length && lines.every(line => sourceFieldAvailable(line.meta, 'low', series?.meta)) ? Math.min(...lines.map((line) => line.low)) : NaN;
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<div className="market-index-selector">{indexes.map((item) => <button type="button" className={item.id === selected?.id ? 'active' : ''} key={item.id} onClick={() => onSelect(item.id)}>
			<span>{item.name}</span><strong>{formatAvailable(item.price, rowHas(item, 'price'), formatPrice)}</strong><em className={availableTone(item.change_percent, rowHas(item, 'change_percent'))}>{formatAvailable(item.change_percent, rowHas(item, 'change_percent'), formatPercent)}</em>
		</button>)}</div>
		{selected ? <section className="market-index-detail">
			<header><div><span>{selected.region} · {selected.market}</span><h3>{selected.name}</h3><small>最近 {lines.length || '--'} 个交易周期 · {statusLabel(selected.status)}</small></div><div><strong>{formatAvailable(selected.price, rowHas(selected, 'price'), formatPrice)}</strong><em className={availableTone(selected.change_percent, rowHas(selected, 'change_percent'))}>{formatAvailable(selected.change_percent, rowHas(selected, 'change_percent'), formatPercent)}</em></div></header>
			<div className="market-index-chart-wrap">
				{seriesLoading ? <div className="market-chart-loading">走势图加载中…</div> : <IndexLineChart lines={lines.filter(line => sourceFieldAvailable(line.meta, 'close', series?.meta) && Number.isFinite(line.close) && line.close > 0)} />}
				<aside>
					<MiniStat label="区间收益" value={formatPercent(returnPercent)} tone={toneClass(returnPercent)} />
					<MiniStat label="区间高点" value={formatPrice(high)} />
					<MiniStat label="区间低点" value={formatPrice(low)} />
					<MiniStat label="行情时间" value={formatIndexTradeTime(selected.trade_time, selected.meta)} />
				</aside>
			</div>
			<div className="market-kline-table"><header><span>日期</span><span>开盘</span><span>最高</span><span>最低</span><span>收盘</span><span>涨跌</span></header>{lines.slice(-8).reverse().map((line) => <article key={line.time}><span>{formatIndexHistoryDate(line.time)}</span><span>{formatAvailable(line.open, sourceFieldAvailable(line.meta, 'open', series?.meta), formatPrice)}</span><span>{formatAvailable(line.high, sourceFieldAvailable(line.meta, 'high', series?.meta), formatPrice)}</span><span>{formatAvailable(line.low, sourceFieldAvailable(line.meta, 'low', series?.meta), formatPrice)}</span><strong>{formatAvailable(line.close, sourceFieldAvailable(line.meta, 'close', series?.meta), formatPrice)}</strong><em className={availableTone(line.change_percent ?? NaN, sourceFieldAvailable(line.meta, 'change_percent', series?.meta))}>{formatAvailable(line.change_percent ?? NaN, sourceFieldAvailable(line.meta, 'change_percent', series?.meta), formatPercent)}</em></article>)}</div>
		</section> : <EmptyData title="暂无核心指数" detail="等待指数目录恢复。" />}
	</div>;
}

export function IndustryMomentumView({ items, meta }: { items: MarketIndustryMomentum[]; meta: SourceMeta | null }) {
	const [query, setQuery] = useState('');
	const [sortState, setSortState] = useState<SortState>({ key: 'score', direction: 'desc' });
	const columnAvailable = (key: string) => items.some(item => sortableValuePresent(industrySortValue(item, key, meta)));
	const flowAvailable = columnAvailable('main_net_inflow');
	const breadthAvailable = columnAvailable('breadth');
	const scoreAvailable = columnAvailable('score');
	const rowHas = (item: MarketIndustryMomentum, field: string) => sourceFieldAvailable(item.meta, field, meta);
	const visible = useMemo(() => items.filter((item) => !query || `${item.name}${item.leader_name || ''}`.toLowerCase().includes(query.toLowerCase())).sort((a, b) => {
		const left = industrySortValue(a, sortState.key, meta);
		const right = industrySortValue(b, sortState.key, meta);
		return compareSortValues(left, right, sortState.direction);
	}), [items, query, sortState, meta]);
	const toggleSort = (key: string, available = true, defaultDirection: SortDirection = 'desc') => {
		if (!available) return;
		setSortState((current) => nextSortState(current, key, defaultDirection));
	};
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<MarketFilter query={query} onQuery={setQuery}><span className="market-sort-hint">点击列名排序</span></MarketFilter>
		{visible.length ? <div className="market-data-table momentum"><header>
			<SortButton label="行业 / 领涨" active={sortState.key === 'name'} direction={sortState.direction} onClick={() => toggleSort('name', true, 'asc')} />
			<SortButton label="动能" active={sortState.key === 'score'} direction={sortState.direction} disabled={!scoreAvailable} onClick={() => toggleSort('score', scoreAvailable)} />
			<SortButton label="当日" active={sortState.key === 'change_percent'} direction={sortState.direction} disabled={!columnAvailable('change_percent')} onClick={() => toggleSort('change_percent', columnAvailable('change_percent'))} />
			<SortButton label="5 日" active={sortState.key === 'five_day_change_percent'} direction={sortState.direction} disabled={!columnAvailable('five_day_change_percent')} onClick={() => toggleSort('five_day_change_percent', columnAvailable('five_day_change_percent'))} />
			<SortButton label="20 日" active={sortState.key === 'twenty_day_change_percent'} direction={sortState.direction} disabled={!columnAvailable('twenty_day_change_percent')} onClick={() => toggleSort('twenty_day_change_percent', columnAvailable('twenty_day_change_percent'))} />
			<SortButton label="涨跌家数" active={sortState.key === 'breadth'} direction={sortState.direction} disabled={!breadthAvailable} onClick={() => toggleSort('breadth', breadthAvailable)} />
			<SortButton label="主力净流入" active={sortState.key === 'main_net_inflow'} direction={sortState.direction} disabled={!flowAvailable} onClick={() => toggleSort('main_net_inflow', flowAvailable)} />
		</header>{visible.map((item, index) => <article key={item.code}>
			<span><i>{String(index + 1).padStart(2, '0')}</i><span><strong>{item.name}</strong><small>{rowHas(item, 'leader_name') ? <>{item.leader_name || '暂无领涨标的'} {item.leader_name && formatAvailable(item.leader_change_percent, rowHas(item, 'leader_change_percent'), formatPercent)}</> : '数据源未提供领涨标的'}</small></span></span>
			<span>{rowHas(item, 'score') && Number.isFinite(item.score) && <b style={{ width: `${Math.max(3, item.score)}%` }} />}<strong>{formatAvailable(item.score, rowHas(item, 'score'), (value) => Number.isFinite(value) ? value.toFixed(1) : '--')}</strong></span>
			<em className={availableTone(item.change_percent, rowHas(item, 'change_percent'))}>{formatAvailable(item.change_percent, rowHas(item, 'change_percent'), formatPercent)}</em>
			<em className={availableTone(item.five_day_change_percent, rowHas(item, 'five_day_change_percent'))}>{formatAvailable(item.five_day_change_percent, rowHas(item, 'five_day_change_percent'), formatPercent)}</em>
			<em className={availableTone(item.twenty_day_change_percent, rowHas(item, 'twenty_day_change_percent'))}>{formatAvailable(item.twenty_day_change_percent, rowHas(item, 'twenty_day_change_percent'), formatPercent)}</em>
			<span>{rowHas(item, 'rising_count') && rowHas(item, 'falling_count') ? `${item.rising_count} / ${item.falling_count}` : '--'}</span>
			<strong className={availableTone(item.main_net_inflow, rowHas(item, 'main_net_inflow'))}>{formatAvailable(item.main_net_inflow, rowHas(item, 'main_net_inflow'), formatMoney)}</strong>
		</article>)}</div> : <EmptyData title="没有匹配行业" detail="调整搜索条件或刷新行情。" />}
	</div>;
}

export function FundFlowView({ items, dimension, meta }: {
	items: MarketFundFlow[];
	dimension: 'industry' | 'theme' | 'stock';
	meta: SourceMeta | null;
}) {
	const [query, setQuery] = useState('');
	const visible = items.filter((item) => !query || `${item.name}${item.code}${item.symbol || ''}${item.leader_name || ''}`.toLowerCase().includes(query.toLowerCase()));
	const netField = fundFlowNetField(meta, items);
	const netLabel = netField === 'main_net_inflow' ? '主力净流入' : '总净流入';
	const availableItems = items.filter((item) => primaryNetInflow(item, meta, netField) !== null);
	const flowValues = availableItems.map((item) => primaryNetInflow(item, meta, netField)!);
	const topFlowItem = availableItems.reduce<MarketFundFlow | null>((best, item) => !best || primaryNetInflow(item, meta, netField)! > primaryNetInflow(best, meta, netField)! ? item : best, null);
	const topFlow = topFlowItem ? primaryNetInflow(topFlowItem, meta, netField)! : 0;
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<MarketFilter query={query} onQuery={setQuery}><span className="market-sort-hint">点击列名排序</span></MarketFilter>
		<section className="market-flow-summary">
			<SummaryMetric icon={<TrendingUp size={17} />} label={`${netLabel}项目`} value={flowValues.length ? String(flowValues.filter((value) => value > 0).length) : '--'} detail={`共 ${items.length} 项 · 有效 ${flowValues.length} 项`} tone="up" />
			<SummaryMetric icon={<Building2 size={17} />} label={`榜首${netLabel}`} value={topFlowItem ? formatMoney(topFlow) : '--'} detail={topFlowItem?.name || '当前口径未提供'} tone={toneClass(topFlow)} />
			<SummaryMetric icon={<Activity size={17} />} label="口径" value={dimension === 'industry' ? '行业' : dimension === 'theme' ? '题材' : '个股'} detail={fundFlowSourceLabel(meta)} />
		</section>
		{visible.length ? dimension === 'stock'
			? <StockFundFlowTable items={visible} meta={meta} />
			: <SectorFundFlowTable items={visible} meta={meta} netField={netField} />
			: <EmptyData title="没有匹配资金记录" detail="调整搜索条件或刷新资金榜。" />}
	</div>;
}

export function MarginBalanceView({ items, limit, onLimit, meta }: {
	items: MarketMarginPoint[];
	limit: number;
	onLimit: (limit: number) => void;
	meta: SourceMeta | null;
}) {
	const latest = items.at(-1);
	const financingShare = latest?.margin_balance ? latest.financing_balance / latest.margin_balance * 100 : 0;
	return <div className="market-data-view market-margin-view">
		<SourceNotice meta={meta} />
		<div className="market-margin-toolbar">
			<div><strong>全市场两融余额</strong><span>沪市、深市、北交所合并口径，交易日收盘后更新</span></div>
			<nav aria-label="融资融券图表周期">{[30, 60, 120, 250].map((value) => <button type="button" className={limit === value ? 'active' : ''} onClick={() => onLimit(value)} key={value}>{value === 250 ? '近1年' : `${value}日`}</button>)}</nav>
		</div>
		<section className="market-flow-summary market-margin-summary">
			<SummaryMetric icon={<WalletCards size={17} />} label="两融余额" value={latest ? formatHundredMillion(latest.margin_balance) : '--'} detail={latest?.trade_date || '等待交易日'} />
			<SummaryMetric icon={<TrendingUp size={17} />} label="融资余额" value={latest ? formatHundredMillion(latest.financing_balance) : '--'} detail={`占两融 ${financingShare.toFixed(2)}%`} />
			<SummaryMetric icon={<Landmark size={17} />} label="融券余额" value={latest ? formatHundredMillion(latest.securities_lending_balance) : '--'} detail="按市值口径汇总" />
			<SummaryMetric icon={<Activity size={17} />} label="当日余额变化" value={latest ? formatSignedHundredMillion(latest.margin_balance_change) : '--'} detail={latest ? `融资净买入 ${formatSignedHundredMillion(latest.financing_net_buy_amount)}` : '等待数据'} tone={toneClass(latest?.margin_balance_change || 0)} />
		</section>
		<section className="market-margin-panel">
			<header><div><span>MARGIN BALANCE TREND</span><h3>融资融券余额趋势</h3></div><div className="market-margin-legend"><span className="total">两融余额</span><span className="financing">融资余额</span><span className="lending">融券余额（右轴）</span></div></header>
			<MarginBalanceChart items={items} />
			<footer>单位：亿元 · 两融余额 = 融资余额 + 融券余额；数据口径与历史来源以返回元数据为准。</footer>
		</section>
	</div>;
}

export function FuturesPositionView({ series, members, consensus, variety, onVariety, meta }: {
	series: MarketFuturesPositionSeries | null;
	members: MarketFuturesMembers | null;
	consensus: MarketFuturesConsensus | null;
	variety: string;
	onVariety: (variety: string) => void;
	meta: SourceMeta | null;
}) {
	const rows = series?.variety === variety ? series.rows : [];
	const latest = rows.at(-1);
	const consensusVariety = consensus?.varieties.find((item) => item.variety === variety);
	const memberMeta = members && members.contract_code === series?.contract_code && members.trade_date === latest?.trade_date ? (members.meta ?? null) : null;
	const maxPosition = Math.max(1, ...rows.flatMap((row) => [row.long_position, row.short_position].map((value) => Math.abs(value))));
	return <div className="market-data-view market-futures-view">
		<SourceNotice meta={meta} />
		<div className="market-margin-toolbar"><div><strong>股指期货前20会员多空持仓</strong><span>不是个股机构资金；多空单为会员持仓汇总，盘后更新</span></div><nav aria-label="股指期货品种">{['IF', 'IH', 'IC', 'IM'].map((item) => <button type="button" className={variety === item ? 'active' : ''} onClick={() => onVariety(item)} key={item}>{item}</button>)}</nav></div>
		{latest ? <>
			<section className="market-flow-summary market-futures-summary">
					<SummaryMetric icon={<ArrowDown size={17} />} label="前20多空单净值" value={formatFuturesOpeningHands(consensusVariety?.net_long_position ?? null, true)} detail="+ 多单 · − 空单 · 全部合约" tone={toneClass(consensusVariety?.net_long_position || 0)} />
					<SummaryMetric icon={<ArrowDownUp size={17} />} label="当日多空单变化" value={formatFuturesOpeningHands(consensusVariety?.net_long_change ?? null, true)} detail="持买单增减 − 持卖单增减" tone={toneClass(consensusVariety?.net_long_change || 0)} />
					<SummaryMetric icon={<Activity size={17} />} label="中信期货多空单变化" value={formatFuturesOpeningHands(consensusVariety?.citic_net_long_change ?? null, true)} detail="+ 多单 · − 空单 · 全部合约" tone={toneClass(consensusVariety?.citic_net_long_change || 0)} />
					<SummaryMetric icon={<Building2 size={17} />} label="统计范围" value={consensusVariety ? `${consensusVariety.contract_count} 个合约` : '--'} detail={`${consensus?.trade_date || latest.trade_date} · ${series?.index_code || '--'}`} tone="flat" />
			</section>
			{!consensus && <div className="market-module-empty market-futures-consensus-empty"><AlertTriangle size={20} /><strong>共识统计未获取</strong><span>全部合约前20会员多空单数据暂不可用，不以主力合约或持仓变化代替。</span></div>}
			{consensus?.varieties?.length ? <section className="market-futures-consensus" aria-label="股指期货共识口径统计">
				<header><div><span>CFFEX CONSENSUS</span><h3>{consensus.trade_date} 四大期指多空单共识</h3></div><small>全部合约 · 前20会员</small></header>
				<div className="market-futures-consensus-scroll" role="region" aria-label="四大期指多空数据，可横向滚动" tabIndex={0}>
					<table className="market-futures-consensus-table" aria-label="四大期指多空单共识">
						<thead><tr><th scope="col">品种</th><th scope="col">前20多空单净值</th><th scope="col">当日多空单变化</th><th scope="col">中信多空单变化</th><th scope="col">合约数</th></tr></thead>
						<tbody>{consensus.varieties.map((item) => <tr key={item.variety}>
							<th scope="row"><strong>{item.variety}</strong><small>{FUTURES_VARIETY_NAMES[item.variety]}</small></th>
							<td className={toneClass(item.net_long_position)}>{formatFuturesOpeningHands(item.net_long_position, true)}</td>
							<td className={toneClass(item.net_long_change)}>{formatFuturesOpeningHands(item.net_long_change, true)}</td>
							<td className={toneClass(item.citic_net_long_change)}>{formatFuturesOpeningHands(item.citic_net_long_change, true)}</td>
							<td>{item.contract_count}</td>
						</tr>)}</tbody>
					</table>
				</div>
				<footer><span>+ 多单 · − 空单</span><span>中信统计：中信期货(代客)</span></footer>
			</section> : null}
			<section className="market-futures-panel"><header><div><span>MAIN CONTRACT REFERENCE</span><h3>{series?.variety_name || variety} · 主力合约持仓趋势参考</h3></div><div className="market-margin-legend"><span className="total">多单</span><span className="lending">空单</span><span className="financing">净持仓</span></div></header>
				<div className="market-futures-bars">{rows.slice(-30).map((row) => <article key={row.trade_date}><time>{formatMonthDay(row.trade_date)}</time><div><i className="long" style={{ width: `${Math.max(1, Math.abs(row.long_position) / maxPosition * 100)}%` }} /><span>{formatFuturesHands(row.long_position)}</span></div><div><i className="short" style={{ width: `${Math.max(1, Math.abs(row.short_position) / maxPosition * 100)}%` }} /><span>{formatFuturesHands(row.short_position)}</span></div><strong className={toneClass(row.net_position)}>{formatFuturesHands(row.net_position)}</strong></article>)}</div>
				<footer>共识统计采用 {consensus?.trade_date || latest.trade_date} 全部合约前20会员多空单口径（+ 多单，− 空单）；此处图表为主力合约持仓趋势参考。结算价 {latest.settle_price == null ? '--' : latest.settle_price.toFixed(2)} · 现货指数 {latest.index_close == null ? '--' : latest.index_close.toFixed(2)} · 基差 {latest.basis == null ? '--' : latest.basis.toFixed(2)}。</footer>
			</section>
			{members?.members?.length ? <section className="market-futures-members"><SourceNotice meta={memberMeta} /><header><div><span>TOP 20 MEMBERS</span><h3>{members.trade_date} 会员多空明细</h3></div><small>中金所成交持仓排名 · 主力合约参考</small></header><div className="market-data-table"><header><span>排名</span><span>持买单会员</span><span>多单 / 增减</span><span>持卖单会员</span><span>空单 / 增减</span></header>{members.members.map((member) => <article key={`${member.contract}-${member.rank}`}><span>{member.rank}</span><strong>{member.long_name || '--'}</strong><span>{formatFuturesHands(member.long_position)} / {formatFuturesChangeShort(member.long_change)}</span><strong>{member.short_name || '--'}</strong><span>{formatFuturesHands(member.short_position)} / {formatFuturesChangeShort(member.short_change)}</span></article>)}</div></section> : null}
		</> : <EmptyData title="暂无机构多空单" detail="期指持仓通常在交易日盘后更新，请稍后刷新。" />}
	</div>;
}

function MarginBalanceChart({ items }: { items: MarketMarginPoint[] }) {
	const [hoveredIndex, setHoveredIndex] = useState<number | null>(null);
	if (items.length < 2) return <div className="market-chart-loading">暂无足够的融资融券历史数据</div>;
	const points = [...items].sort((left, right) => left.trade_date.localeCompare(right.trade_date));
	const width = 960;
	const height = 380;
	const left = 74;
	const right = 74;
	const top = 25;
	const bottom = 320;
	const plotWidth = width - left - right;
	const plotHeight = bottom - top;
	const mainExtent = paddedExtent(points.flatMap((point) => [point.margin_balance, point.financing_balance]));
	const lendingExtent = paddedExtent(points.map((point) => point.securities_lending_balance));
	const x = (index: number) => left + index / (points.length - 1) * plotWidth;
	const mainY = (value: number) => top + (mainExtent.max - value) / (mainExtent.max - mainExtent.min) * plotHeight;
	const lendingY = (value: number) => top + (lendingExtent.max - value) / (lendingExtent.max - lendingExtent.min) * plotHeight;
	const linePath = (value: (point: MarketMarginPoint) => number, y: (value: number) => number) => points.map((point, index) => `${index ? 'L' : 'M'} ${x(index).toFixed(2)} ${y(value(point)).toFixed(2)}`).join(' ');
	const totalPath = linePath((point) => point.margin_balance, mainY);
	const financingPath = linePath((point) => point.financing_balance, mainY);
	const lendingPath = linePath((point) => point.securities_lending_balance, lendingY);
	const hovered = hoveredIndex == null ? null : points[hoveredIndex];
	const hoveredX = hoveredIndex == null ? null : x(hoveredIndex);
	const labelIndexes = Array.from(new Set([0, Math.floor((points.length - 1) / 4), Math.floor((points.length - 1) / 2), Math.floor((points.length - 1) * 3 / 4), points.length - 1]));
	const handleMove = (event: ReactMouseEvent<SVGRectElement>) => {
		const bounds = event.currentTarget.getBoundingClientRect();
		if (!bounds.width) return;
		const ratio = Math.max(0, Math.min(1, (event.clientX - bounds.left) / bounds.width));
		setHoveredIndex(Math.round(ratio * (points.length - 1)));
	};
	return <div className="market-margin-chart-wrap">
		<svg className="market-margin-chart" viewBox={`0 0 ${width} ${height}`} role="img" aria-label="融资融券余额折线图">
			<defs><linearGradient id="marginBalanceArea" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="var(--blue)" stopOpacity=".18" /><stop offset="1" stopColor="var(--blue)" stopOpacity=".01" /></linearGradient></defs>
			{Array.from({ length: 5 }, (_, index) => {
				const ratio = index / 4;
				const y = top + ratio * plotHeight;
				const mainValue = mainExtent.max - ratio * (mainExtent.max - mainExtent.min);
				const lendingValue = lendingExtent.max - ratio * (lendingExtent.max - lendingExtent.min);
				return <g key={index}><line className="market-margin-grid" x1={left} x2={width - right} y1={y} y2={y} /><text className="market-margin-axis" x={left - 10} y={y + 4} textAnchor="end">{formatAxisHundredMillion(mainValue)}</text><text className="market-margin-axis right" x={width - right + 10} y={y + 4}>{formatAxisHundredMillion(lendingValue)}</text></g>;
			})}
			<path className="market-margin-area" d={`${totalPath} L ${x(points.length - 1).toFixed(2)} ${bottom} L ${left} ${bottom} Z`} />
			<path className="market-margin-line total" d={totalPath} />
			<path className="market-margin-line financing" d={financingPath} />
			<path className="market-margin-line lending" d={lendingPath} />
			{labelIndexes.map((index) => <text className="market-margin-date" x={x(index)} y={height - 25} textAnchor={index === 0 ? 'start' : index === points.length - 1 ? 'end' : 'middle'} key={points[index].trade_date}>{formatMonthDay(points[index].trade_date)}</text>)}
			{hovered && hoveredIndex != null && hoveredX != null && <g className="market-margin-crosshair"><line x1={hoveredX} x2={hoveredX} y1={top} y2={bottom} /><circle className="total" cx={hoveredX} cy={mainY(hovered.margin_balance)} r="4" /><circle className="financing" cx={hoveredX} cy={mainY(hovered.financing_balance)} r="4" /><circle className="lending" cx={hoveredX} cy={lendingY(hovered.securities_lending_balance)} r="4" /></g>}
			<rect className="market-margin-hover-layer" x={left} y={top} width={plotWidth} height={plotHeight} onMouseMove={handleMove} onMouseLeave={() => setHoveredIndex(null)} />
		</svg>
		{hovered && hoveredIndex != null && <div className={`market-margin-tooltip ${hoveredIndex > points.length / 2 ? 'left' : 'right'}`}>
			<strong>{hovered.trade_date}</strong>
			<div><span>两融余额</span><b>{formatHundredMillion(hovered.margin_balance)}</b></div>
			<div><span>融资余额</span><b>{formatHundredMillion(hovered.financing_balance)}</b></div>
			<div><span>融券余额</span><b>{formatHundredMillion(hovered.securities_lending_balance)}</b></div>
			<div><span>余额变化</span><b className={toneClass(hovered.margin_balance_change)}>{formatSignedHundredMillion(hovered.margin_balance_change)}</b></div>
			<div><span>融资净买入</span><b className={toneClass(hovered.financing_net_buy_amount)}>{formatSignedHundredMillion(hovered.financing_net_buy_amount)}</b></div>
		</div>}
	</div>;
}

function paddedExtent(values: number[]) {
	const minimum = Math.min(...values);
	const maximum = Math.max(...values);
	const range = maximum - minimum;
	const padding = range > 0 ? range * 0.08 : Math.max(Math.abs(maximum) * 0.01, 1);
	return { min: minimum - padding, max: maximum + padding };
}

function SectorFundFlowTable({ items, meta, netField }: { items: MarketFundFlow[]; meta: SourceMeta | null; netField: 'net_inflow' | 'main_net_inflow' }) {
	const columnAvailable = (key: string) => items.some(item => sortableValuePresent(sectorSortValue(item, key, meta, netField)));
	const netAvailable = columnAvailable('net_value');
	const rowHas = (item: MarketFundFlow, field: string) => sourceFieldAvailable(item.meta, field, meta);
	const [sortState, setSortState] = useState<SortState>({ key: 'net_value', direction: 'desc' });
	const sortedItems = useMemo(() => [...items].sort((a, b) => compareSortValues(sectorSortValue(a, sortState.key, meta, netField), sectorSortValue(b, sortState.key, meta, netField), sortState.direction)), [items, meta, sortState, netField]);
	const toggleSort = (key: string, available = true, defaultDirection: SortDirection = 'desc') => { if (available) setSortState((current) => nextSortState(current, key, defaultDirection)); };
	return <div className="market-data-table flow sector"><header>
		<SortButton label="名称 / 代码" active={sortState.key === 'name'} direction={sortState.direction} onClick={() => toggleSort('name', true, 'asc')} />
		<SortButton label="均价" active={sortState.key === 'price'} direction={sortState.direction} disabled={!columnAvailable('price')} onClick={() => toggleSort('price', columnAvailable('price'))} />
		<SortButton label="涨跌幅" active={sortState.key === 'change_percent'} direction={sortState.direction} disabled={!columnAvailable('change_percent')} onClick={() => toggleSort('change_percent', columnAvailable('change_percent'))} />
		<SortButton label="资金流入" active={sortState.key === 'inflow'} direction={sortState.direction} disabled={!columnAvailable('inflow')} onClick={() => toggleSort('inflow', columnAvailable('inflow'))} />
		<SortButton label="资金流出" active={sortState.key === 'outflow'} direction={sortState.direction} disabled={!columnAvailable('outflow')} onClick={() => toggleSort('outflow', columnAvailable('outflow'))} />
		<SortButton label={netField === 'main_net_inflow' ? '主力净流入' : '总净流入'} active={sortState.key === 'net_value'} direction={sortState.direction} disabled={!netAvailable} onClick={() => toggleSort('net_value', netAvailable)} />
		<SortButton label={netField === 'main_net_inflow' ? '主力净流入率' : '总净流入率'} active={sortState.key === 'net_ratio'} direction={sortState.direction} disabled={!columnAvailable('net_ratio')} onClick={() => toggleSort('net_ratio', columnAvailable('net_ratio'))} />
		<SortButton label="领涨标的" active={sortState.key === 'leader_name'} direction={sortState.direction} disabled={!columnAvailable('leader_name')} onClick={() => toggleSort('leader_name', columnAvailable('leader_name'), 'asc')} />
	</header>{sortedItems.map((item, index) => {
		const netValue = item[netField];
		const netValueAvailable = rowHas(item, netField);
		const ratioField = `${netField}_ratio` as 'net_inflow_ratio' | 'main_net_inflow_ratio';
		return <article key={`${item.dimension}-${item.code}`}>
			<FlowIdentity item={item} index={index} />
			<strong>{formatAvailable(item.price, rowHas(item, 'price'), formatPrice)}</strong>
			<em className={availableTone(item.change_percent, rowHas(item, 'change_percent'))}>{formatAvailable(item.change_percent, rowHas(item, 'change_percent'), formatPercent)}</em>
			<span className={availableTone(item.inflow, rowHas(item, 'inflow'))}>{formatAvailable(item.inflow, rowHas(item, 'inflow'), formatMoney)}</span>
			<span className={availableTone(-item.outflow, rowHas(item, 'outflow'))}>{formatAvailable(item.outflow, rowHas(item, 'outflow'), formatMoney)}</span>
			<strong className={availableTone(netValue, netValueAvailable)}>{formatAvailable(netValue, netValueAvailable, formatMoney)}</strong>
			<em className={availableTone(item[ratioField], rowHas(item, ratioField))}>{formatAvailable(item[ratioField], rowHas(item, ratioField), formatPercent)}</em>
			<span className="market-flow-leader">{rowHas(item, 'leader_name') ? <><strong>{item.leader_name || '--'}</strong><small>{rowHas(item, 'leader_symbol') ? item.leader_symbol || '' : ''}{item.leader_name ? ` · ${formatAvailable(item.leader_change_percent, rowHas(item, 'leader_change_percent'), formatPercent)}` : ''}</small></> : <small>--</small>}</span>
		</article>;
	})}</div>;
}

function StockFundFlowTable({ items, meta }: { items: MarketFundFlow[]; meta: SourceMeta | null }) {
	const columnAvailable = (key: string) => items.some(item => sortableValuePresent(stockSortValue(item, key, meta)));
	const rowHas = (item: MarketFundFlow, field: string) => sourceFieldAvailable(item.meta, field, meta);
	const [sortState, setSortState] = useState<SortState>({ key: 'net_inflow', direction: 'desc' });
	const sortedItems = useMemo(() => [...items].sort((a, b) => compareSortValues(stockSortValue(a, sortState.key, meta), stockSortValue(b, sortState.key, meta), sortState.direction)), [items, meta, sortState]);
	const toggleSort = (key: string, available = true, defaultDirection: SortDirection = 'desc') => { if (available) setSortState((current) => nextSortState(current, key, defaultDirection)); };
	return <div className="market-data-table flow stock"><header>
		<SortButton label="名称 / 代码" active={sortState.key === 'name'} direction={sortState.direction} onClick={() => toggleSort('name', true, 'asc')} />
		<SortButton label="价格" active={sortState.key === 'price'} direction={sortState.direction} disabled={!columnAvailable('price')} onClick={() => toggleSort('price', columnAvailable('price'))} />
		<SortButton label="涨跌幅" active={sortState.key === 'change_percent'} direction={sortState.direction} disabled={!columnAvailable('change_percent')} onClick={() => toggleSort('change_percent', columnAvailable('change_percent'))} />
		<SortButton label="总净流入" active={sortState.key === 'net_inflow'} direction={sortState.direction} disabled={!columnAvailable('net_inflow')} onClick={() => toggleSort('net_inflow', columnAvailable('net_inflow'))} />
		<SortButton label="总净流入率" active={sortState.key === 'net_inflow_ratio'} direction={sortState.direction} disabled={!columnAvailable('net_inflow_ratio')} onClick={() => toggleSort('net_inflow_ratio', columnAvailable('net_inflow_ratio'))} />
		<SortButton label="主力净流入" active={sortState.key === 'main_net_inflow'} direction={sortState.direction} disabled={!columnAvailable('main_net_inflow')} onClick={() => toggleSort('main_net_inflow', columnAvailable('main_net_inflow'))} />
		<SortButton label="主力净流入率" active={sortState.key === 'main_net_inflow_ratio'} direction={sortState.direction} disabled={!columnAvailable('main_net_inflow_ratio')} onClick={() => toggleSort('main_net_inflow_ratio', columnAvailable('main_net_inflow_ratio'))} />
		<SortButton label="散户净流入" active={sortState.key === 'retail_net_inflow'} direction={sortState.direction} disabled={!columnAvailable('retail_net_inflow')} onClick={() => toggleSort('retail_net_inflow', columnAvailable('retail_net_inflow'))} />
		<SortButton label="散户净流入率" active={sortState.key === 'retail_net_inflow_ratio'} direction={sortState.direction} disabled={!columnAvailable('retail_net_inflow_ratio')} onClick={() => toggleSort('retail_net_inflow_ratio', columnAvailable('retail_net_inflow_ratio'))} />
	</header>{sortedItems.map((item, index) => <article key={`${item.dimension}-${item.code}`}>
		<FlowIdentity item={item} index={index} />
		<strong>{formatAvailable(item.price, rowHas(item, 'price'), formatPrice)}</strong>
		<em className={availableTone(item.change_percent, rowHas(item, 'change_percent'))}>{formatAvailable(item.change_percent, rowHas(item, 'change_percent'), formatPercent)}</em>
		<strong className={availableTone(item.net_inflow, rowHas(item, 'net_inflow'))}>{formatAvailable(item.net_inflow, rowHas(item, 'net_inflow'), formatMoney)}</strong>
		<em className={availableTone(item.net_inflow_ratio, rowHas(item, 'net_inflow_ratio'))}>{formatAvailable(item.net_inflow_ratio, rowHas(item, 'net_inflow_ratio'), formatPercent)}</em>
		<strong className={availableTone(item.main_net_inflow, rowHas(item, 'main_net_inflow'))}>{formatAvailable(item.main_net_inflow, rowHas(item, 'main_net_inflow'), formatMoney)}</strong>
		<em className={availableTone(item.main_net_inflow_ratio, rowHas(item, 'main_net_inflow_ratio'))}>{formatAvailable(item.main_net_inflow_ratio, rowHas(item, 'main_net_inflow_ratio'), formatPercent)}</em>
		<strong className={availableTone(item.retail_net_inflow, rowHas(item, 'retail_net_inflow'))}>{formatAvailable(item.retail_net_inflow, rowHas(item, 'retail_net_inflow'), formatMoney)}</strong>
		<em className={availableTone(item.retail_net_inflow_ratio, rowHas(item, 'retail_net_inflow_ratio'))}>{formatAvailable(item.retail_net_inflow_ratio, rowHas(item, 'retail_net_inflow_ratio'), formatPercent)}</em>
	</article>)}</div>;
}

function FlowIdentity({ item, index }: { item: MarketFundFlow; index: number }) {
	return <span><i>{String(index + 1).padStart(2, '0')}</i><span><strong>{item.name}</strong><small>{item.symbol || item.code}</small></span></span>;
}

export function BillboardView({ items, tradeDate, onTradeDate, meta, details, onLoadDetail }: {
	items: MarketBillboardItem[];
	tradeDate: string;
	onTradeDate: (value: string) => void;
	meta: SourceMeta | null;
	details: Record<string, BillboardDetailEntry>;
	onLoadDetail: (item: MarketBillboardItem) => void;
}) {
	const [query, setQuery] = useState('');
	const [expandedKeys, setExpandedKeys] = useState<Set<string>>(() => new Set());
	const visible = items.filter((item) => !query || `${item.name}${item.symbol}${item.reason}`.toLowerCase().includes(query.toLowerCase()));
	const netTotal = items.reduce((sum, item) => sum + item.net_amount, 0);
	useEffect(() => setExpandedKeys(new Set()), [items]);
	const toggleDetail = (item: MarketBillboardItem) => {
		const key = billboardDetailKey(item);
		const expanding = !expandedKeys.has(key);
		setExpandedKeys((current) => {
			const next = new Set(current);
			if (next.has(key)) next.delete(key);
			else next.add(key);
			return next;
		});
		if (expanding && details[key]?.state !== 'loading' && details[key]?.state !== 'ready') onLoadDetail(item);
	};
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<div className="market-filter-bar"><label><Search size={14} /><input aria-label="搜索龙虎榜" value={query} onChange={(event) => setQuery(event.target.value)} placeholder="搜索股票、代码或上榜原因" /></label><label className="market-date-control"><CalendarDays size={14} /><input aria-label="龙虎榜交易日" type="date" value={tradeDate} onChange={(event) => onTradeDate(event.target.value)} /></label></div>
		<section className="market-flow-summary">
			<SummaryMetric icon={<Landmark size={17} />} label="上榜记录" value={String(items.length)} detail={meta?.trade_date || tradeDate || '最近交易日'} />
			<SummaryMetric icon={<TrendingUp size={17} />} label="合计净额" value={formatMoney(netTotal)} detail="当前列表简单合计" tone={toneClass(netTotal)} />
			<SummaryMetric icon={<Building2 size={17} />} label="机构现身" value={String(items.filter((item) => item.institution_buyers > 0).length)} detail="含机构买方记录" />
		</section>
		{visible.length ? <div className="market-billboard-list">{visible.map((item, index) => {
			const key = billboardDetailKey(item);
			const expanded = expandedKeys.has(key);
			const detailEntry = details[key];
			const detailID = `billboard-seats-${index}`;
			return <article className={expanded ? 'expanded' : ''} key={key}>
				<header className="market-billboard-card-header"><span><strong>{item.name}</strong><small>{item.symbol} · {item.trade_date}</small></span><em className={toneClass(item.change_percent)}>{formatPercent(item.change_percent)}</em></header>
				<p>{item.reason || '未提供上榜原因'}</p>{item.summary && <blockquote>{item.summary}</blockquote>}
				<footer><span>买入 <strong>{formatMoney(item.buy_amount)}</strong></span><span>卖出 <strong>{formatMoney(item.sell_amount)}</strong></span><span>净额 <strong className={toneClass(item.net_amount)}>{formatMoney(item.net_amount)}</strong></span><span>机构买方 <strong>{item.institution_buyers}</strong></span><span>换手 <strong>{formatPercent(item.turnover_rate)}</strong></span></footer>
				<button type="button" className="market-billboard-toggle" aria-expanded={expanded} aria-controls={detailID} onClick={() => toggleDetail(item)}><ChevronDown size={14} />{expanded ? '收起买卖五席' : '查看买一至买五 / 卖一至卖五'}</button>
				{expanded && <div className="market-billboard-detail" id={detailID}>
					{detailEntry?.state === 'loading' && <div className="market-billboard-detail-state"><LoaderCircle className="spin" size={17} /><span>正在读取完整买卖五席</span></div>}
					{detailEntry?.state === 'error' && <div className="market-billboard-detail-state error"><AlertTriangle size={17} /><span>{detailEntry.error || '席位明细加载失败'}</span><button type="button" onClick={() => onLoadDetail(item)}>重试</button></div>}
					{detailEntry?.state === 'ready' && detailEntry.detail && <div className="market-billboard-seat-grid">
						<BillboardSeatPanel title="买方五席" prefix="买" seats={detailEntry.detail.buy_seats} />
						<BillboardSeatPanel title="卖方五席" prefix="卖" seats={detailEntry.detail.sell_seats} />
					</div>}
				</div>}
			</article>;
		})}</div> : <EmptyData title="该交易日暂无龙虎榜" detail="留空日期可自动回溯到最近有数据的交易日。" />}
	</div>;
}

function BillboardSeatPanel({ title, prefix, seats }: { title: string; prefix: '买' | '卖'; seats: MarketBillboardSeat[] }) {
	const seatByRank = new Map(seats.map((seat) => [seat.rank, seat]));
	return <section className={`market-billboard-seat-panel ${prefix === '买' ? 'buy' : 'sell'}`}>
		<header><strong>{title}</strong><span>{prefix === '买' ? '按买入额排名' : '按卖出额排名'}</span></header>
		<div className="market-billboard-seat-head"><span>排名 / 席位</span><span>买入</span><span>卖出</span><span>净额</span></div>
		{Array.from({ length: 5 }, (_, index) => {
			const rank = index + 1;
			const seat = seatByRank.get(rank);
			const label = seat ? classifyBillboardSeat(seat) : null;
			return <article key={rank}>
				<span className="market-seat-name"><i>{prefix}{rank}</i><span><strong title={seat?.name || '数据源未提供'}>{seat?.name || '数据源未提供'}</strong>{label && <small className={`market-seat-label ${label.kind}`} title={label.note}>{label.label}</small>}</span></span>
				<SeatAmount amount={seat?.buy_amount} ratio={seat?.buy_ratio} />
				<SeatAmount amount={seat?.sell_amount} ratio={seat?.sell_ratio} />
				<strong className={seat ? toneClass(seat.net_amount) : 'flat'}>{seat ? formatMoney(seat.net_amount) : '--'}</strong>
			</article>;
		})}
	</section>;
}

function SeatAmount({ amount, ratio }: { amount?: number; ratio?: number }) {
	if (amount === undefined) return <span className="market-seat-amount"><strong>--</strong></span>;
	return <span className="market-seat-amount"><strong>{formatMoney(amount)}</strong>{ratio !== undefined && ratio !== 0 && <small>{formatPercent(ratio)}</small>}</span>;
}

function billboardDetailKey(item: MarketBillboardItem) {
	return `${item.trade_date}|${item.symbol}|${item.reason}`;
}

export function ResearchView({ items, kind, queryDraft, onQueryDraft, onSearch, category, onCategory, meta }: {
	items: MarketResearchItem[];
	kind: 'announcement' | 'stock' | 'industry';
	queryDraft: string;
	onQueryDraft: (value: string) => void;
	onSearch: () => void;
	category: string;
	onCategory: (value: string) => void;
	meta: SourceMeta | null;
}) {
	return <div className="market-data-view">
		<SourceNotice meta={meta} />
		<form className="market-filter-bar" onSubmit={(event) => { event.preventDefault(); onSearch(); }}><label><Search size={14} /><input aria-label="搜索研究信号" value={queryDraft} onChange={(event) => onQueryDraft(event.target.value)} placeholder={kind === 'announcement' ? '搜索公告标题或输入股票关键词' : '搜索公司、行业、机构或观点'} /></label>{kind === 'announcement' && <select aria-label="公告分类" value={category} onChange={(event) => onCategory(event.target.value)}><option value="all">全部公告</option><option value="重大">重大事项</option><option value="业绩">业绩公告</option><option value="融资">融资公告</option><option value="风险">风险提示</option></select>}<button type="submit"><Search size={14} />检索</button></form>
		{items.length ? <div className="market-research-list">{items.map((item) => <article key={`${item.kind}-${item.id}`}>
			<div className="market-research-icon">{kind === 'announcement' ? <FileText size={18} /> : kind === 'stock' ? <Building2 size={18} /> : <TrendingUp size={18} />}</div>
			<div><header><span>{item.category || (kind === 'stock' ? '个股研报' : kind === 'industry' ? '行业研报' : '公告')}</span><time>{formatDateTime(item.published_at)}</time></header><h3>{item.title}</h3><p>{[item.stock_name || item.symbol, item.industry_name, item.organization, item.researchers].filter(Boolean).join(' · ') || '市场研究信号'}</p><footer>{item.rating && <span>评级 <strong>{item.rating}</strong>{item.previous_rating && ` / 前值 ${item.previous_rating}`}</span>}{(item.target_low || item.target_high) && <span>目标价 <strong>{formatTarget(item.target_low, item.target_high)}</strong></span>}{item.eps ? <span>预测 EPS <strong>{item.eps.toFixed(2)}</strong></span> : null}{item.pe ? <span>预测 PE <strong>{item.pe.toFixed(1)}</strong></span> : null}</footer></div>
			{item.url && <a href={item.url} target="_blank" rel="noreferrer" title="查看原文"><ExternalLink size={16} /></a>}
		</article>)}</div> : <EmptyData title="暂无匹配研究信号" detail="调整关键词或公告分类后重新检索。" />}
	</div>;
}

function MarketFilter({ query, onQuery, children }: { query: string; onQuery: (value: string) => void; children?: React.ReactNode }) {
	return <div className="market-filter-bar"><label><Search size={14} /><input value={query} onChange={(event) => onQuery(event.target.value)} placeholder="搜索名称、代码或领涨标的" /></label>{children}</div>;
}

function SummaryMetric({ icon, label, value, detail, tone = '' }: { icon: React.ReactNode; label: string; value: string; detail: string; tone?: string }) {
	return <article><i>{icon}</i><span><small>{label}</small><strong className={tone}>{value}</strong><em>{detail}</em></span></article>;
}

function MiniStat({ label, value, tone = '' }: { label: string; value: string; tone?: string }) {
	return <article><small>{label}</small><strong className={tone}>{value}</strong></article>;
}

function SortButton({ label, active, direction, disabled = false, onClick }: { label: string; active: boolean; direction: SortDirection; disabled?: boolean; onClick: () => void }) {
	const Icon = active ? direction === 'asc' ? ArrowUp : ArrowDown : ArrowDownUp;
	return <button type="button" className={`market-sort-button ${active ? 'active' : ''}`} disabled={disabled} aria-label={`${label}${disabled ? '不可排序' : active ? direction === 'asc' ? '升序' : '降序' : '排序'}`} aria-pressed={active} onClick={onClick}>
		<span>{label}</span><Icon size={11} aria-hidden="true" />
	</button>;
}

export function nextSortState(current: SortState, key: string, defaultDirection: SortDirection): SortState {
	return current.key === key ? { key, direction: current.direction === 'asc' ? 'desc' : 'asc' } : { key, direction: defaultDirection };
}

export function compareSortValues(left: string | number | null | undefined, right: string | number | null | undefined, direction: SortDirection) {
	const leftMissing = left === null || left === undefined || (typeof left === 'number' && !Number.isFinite(left)) || (typeof left === 'string' && !left.trim());
	const rightMissing = right === null || right === undefined || (typeof right === 'number' && !Number.isFinite(right)) || (typeof right === 'string' && !right.trim());
	if (leftMissing || rightMissing) {
		if (leftMissing && rightMissing) return 0;
		return leftMissing ? 1 : -1;
	}
	const multiplier = direction === 'asc' ? 1 : -1;
	if (typeof left === 'number' && typeof right === 'number') return (left - right) * multiplier;
	return String(left).localeCompare(String(right), 'zh-CN', { numeric: true, sensitivity: 'base' }) * multiplier;
}

function sortableValuePresent(value: string | number | null | undefined) {
	return typeof value === 'number' ? Number.isFinite(value) : typeof value === 'string' && Boolean(value.trim());
}

function industrySortValue(item: MarketIndustryMomentum, key: string, meta: SourceMeta | null): string | number | null {
	if (key === 'name') return item.name;
	if (key === 'breadth') return sourceFieldAvailable(item.meta, 'rising_count', meta) && sourceFieldAvailable(item.meta, 'falling_count', meta) && Number.isFinite(item.rising_count) && Number.isFinite(item.falling_count) ? item.rising_count - item.falling_count : null;
	if (!sourceFieldAvailable(item.meta, key, meta)) return null;
	return item[key as keyof MarketIndustryMomentum] as string | number | null;
}

function sectorSortValue(item: MarketFundFlow, key: string, meta: SourceMeta | null, netField: 'net_inflow' | 'main_net_inflow'): string | number | null {
	if (key === 'name') return item.name || item.code;
	const field = key === 'net_value' ? netField : key === 'net_ratio' ? `${netField}_ratio` : key;
	if (!sourceFieldAvailable(item.meta, field, meta)) return null;
	return item[field as keyof MarketFundFlow] as string | number | null;
}

function stockSortValue(item: MarketFundFlow, key: string, meta: SourceMeta | null): string | number | null {
	if (key === 'name') return item.name || item.symbol || item.code;
	if (!sourceFieldAvailable(item.meta, key, meta)) return null;
	return item[key as keyof MarketFundFlow] as string | number | null;
}

function IndexLineChart({ lines }: { lines: MarketIndexSeries['lines'] }) {
	if (lines.length < 2) return <div className="market-chart-loading">暂无足够走势数据</div>;
	const values = lines.map((line) => line.close);
	const minValue = Math.min(...values);
	const maxValue = Math.max(...values);
	const range = maxValue - minValue || 1;
	const points = values.map((value, index) => `${(index / (values.length - 1)) * 100},${92 - ((value - minValue) / range) * 80}`).join(' ');
	const area = `0,100 ${points} 100,100`;
	const up = values.at(-1)! >= values[0];
	return <div className={`market-index-chart ${up ? 'up' : 'down'}`}><svg viewBox="0 0 100 100" preserveAspectRatio="none" role="img" aria-label="指数收盘走势"><defs><linearGradient id="marketArea" x1="0" y1="0" x2="0" y2="1"><stop offset="0" stopColor="currentColor" stopOpacity=".22" /><stop offset="1" stopColor="currentColor" stopOpacity="0" /></linearGradient></defs><polygon points={area} fill="url(#marketArea)" /><polyline points={points} fill="none" stroke="currentColor" strokeWidth="1.8" vectorEffect="non-scaling-stroke" /></svg><span>{formatPrice(maxValue)}</span><span>{formatPrice(minValue)}</span></div>;
}

function EmptyData({ title, detail }: { title: string; detail: string }) {
	return <div className="market-module-empty"><AlertTriangle size={22} /><strong>{title}</strong><span>{detail}</span></div>;
}

function formatTarget(low?: number, high?: number) {
	if (low && high) return `${low.toFixed(2)} - ${high.toFixed(2)}`;
	return formatPrice(high || low || 0);
}

function formatPrice(value: number) {
	if (!Number.isFinite(value) || value === 0) return '--';
	return value >= 10_000 ? value.toLocaleString('zh-CN', { maximumFractionDigits: 1 }) : value.toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 });
}

function formatPercent(value: number) {
	if (!Number.isFinite(value)) return '--';
	const fractionDigits = value !== 0 && Math.abs(value) < 0.01 ? 4 : 2;
	return `${value > 0 ? '+' : ''}${value.toFixed(fractionDigits)}%`;
}

function formatMoney(value: number) {
	if (!Number.isFinite(value)) return '--';
	const absolute = Math.abs(value);
	if (absolute >= 100_000_000) return `${(value / 100_000_000).toFixed(2)}亿`;
	if (absolute >= 10_000) return `${(value / 10_000).toFixed(1)}万`;
	return value.toFixed(0);
}

function formatFuturesHands(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${value.toLocaleString('zh-CN')} 手`;
}

function formatFuturesChangeShort(value?: number | null) {
	return value == null ? '--' : `${value > 0 ? '+' : ''}${value.toLocaleString('zh-CN')}`;
}

function formatHundredMillion(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${(value / 100_000_000).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}亿`;
}

function formatSignedHundredMillion(value: number) {
	if (!Number.isFinite(value)) return '--';
	return `${value > 0 ? '+' : ''}${(value / 100_000_000).toLocaleString('zh-CN', { minimumFractionDigits: 2, maximumFractionDigits: 2 })}亿`;
}

function formatAxisHundredMillion(value: number) {
	return (value / 100_000_000).toLocaleString('zh-CN', { maximumFractionDigits: 0 });
}

function formatMonthDay(value: string) {
	const parts = value.slice(0, 10).split('-');
	return parts.length === 3 ? `${parts[1]}/${parts[2]}` : value;
}

function hasField(meta: SourceMeta | null, field: string) {
	return sourceFieldAvailable(meta, field);
}

function formatAvailable(value: number, available: boolean, formatter: (value: number) => string) {
	return available ? formatter(value) : '--';
}

function availableTone(value: number, available: boolean) {
	return available ? toneClass(value) : 'flat';
}

// Select one named list-wide metric; never substitute a row's main flow for
// missing total flow (or rank the two different definitions against each other).
function fundFlowNetField(meta: SourceMeta | null, items: MarketFundFlow[]): 'net_inflow' | 'main_net_inflow' {
	if (items.some(item => sourceFieldAvailable(item.meta, 'net_inflow', meta) && Number.isFinite(item.net_inflow))) return 'net_inflow';
	if (items.some(item => sourceFieldAvailable(item.meta, 'main_net_inflow', meta) && Number.isFinite(item.main_net_inflow))) return 'main_net_inflow';
	return hasField(meta, 'net_inflow') ? 'net_inflow' : 'main_net_inflow';
}

function primaryNetInflow(item: MarketFundFlow, meta: SourceMeta | null, field: 'net_inflow' | 'main_net_inflow'): number | null {
	return sourceFieldAvailable(item.meta, field, meta) && Number.isFinite(item[field]) ? item[field] : null;
}

function fundFlowSourceLabel(meta: SourceMeta | null) {
	if (!meta) return '等待数据来源';
	if (meta.source === 'sina:stock-money-flow') return '新浪总资金 / 主力 / 散户';
	if (meta.source.includes('sina:')) return '新浪板块资金与领涨标的';
	if (meta.source === 'eastmoney:bkzj') return '东方财富主力净流入快照';
	return sourceName(meta.source);
}

function formatDateTime(value?: string) {
	if (!value) return '--';
	const date = new Date(value);
	if (Number.isNaN(date.getTime())) return value;
	return date.toLocaleString('zh-CN', { hour12: false });
}

function toneClass(value: number) {
	return value > 0 ? 'up' : value < 0 ? 'down' : 'flat';
}

function statusLabel(status: string) {
	return status === 'open' ? '交易中' : status === 'closed' ? '已收盘' : '状态未知';
}
