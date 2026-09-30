import type { SourceHealth, SourceMeta } from '../lib/backend';
import { sourceCategoryLabel, sourceHealthCounts, sourceHealthDetail, sourceHealthLabel } from '../lib/source-health';
import { sourceFallbackPolicies, sourceIntegrations } from '../lib/source-integrations';

type Props = {
	id: string;
	sources: SourceHealth[];
	context: 'themes' | 'market';
	error?: string;
	onRefresh: () => void;
	onOpenSettings?: () => void;
	meta?: SourceMeta | null;
	steps?: Record<string, string>;
	stepErrors?: Record<string, string>;
};
const stepLabels: Record<string, string> = { industry: '行业强度', kaipanla: '开盘啦题材 / 涨停池', strength: '成分股强度', overview: '题材快照' };
const stepStatusLabels: Record<string, string> = { ready: '已完成', loading: '更新中', error: '失败' };

function timeLabel(value?: string) {
	if (!value) return '';
	const date = new Date(value);
	return Number.isNaN(date.getTime()) ? '' : date.toLocaleString('zh-CN');
}

export function SourceHealthPanel({ id, sources, context, error, onRefresh, onOpenSettings, meta, steps = {}, stepErrors = {} }: Props) {
	const counts = sourceHealthCounts(sources);
	return <section id={id} className="source-health-panel" aria-label="数据源详情与观测规则">
		<header><div><strong>数据源最近观测</strong><small>全局来源目录 · {counts.available} 可用 / {sources.length || '--'} 项 · {counts.degraded} 失败或降级 · {counts.unknown} 未检测或过期 · {counts.unconfigured} 未接入</small></div><div className="source-health-actions">{onOpenSettings && <button type="button" onClick={onOpenSettings}>数据源接入与配置</button>}<button type="button" onClick={onRefresh}>更新观测记录</button></div></header>
		<p className="source-health-rule">这里记录整个后端的实际请求，其他工作台也会影响状态。每 30 秒读取记录，不主动探测上游；观测有效期 10 分钟，超期显示“观测已过期”，服务重启后清空。缓存读取不会续期；一次失败或部分失败会标为降级，备用来源成功单独记录。</p>
		<div className="source-health-list source-health-detail-list">{sources.map(source => <div key={source.id} className={source.status}>
			<i aria-hidden="true" /><span><strong>{source.name}</strong><small>{sourceCategoryLabel(source.category)}</small><small>{sourceHealthDetail(source)}</small>{(source.last_success || source.last_failure) && <small>{source.last_success && `最近成功 ${timeLabel(source.last_success)}`}{source.last_success && source.last_failure && ' · '}{source.last_failure && `最近失败 ${timeLabel(source.last_failure)}`}</small>}<small>{sourceIntegrations.find(item => item.id === source.id)?.configuration}</small></span><em>{sourceHealthLabel(source)}</em>
		</div>)}</div>
		{!sources.length && !error && <p role="status">等待观测记录…</p>}
		{error && <p role="alert">{error}；上面的记录可能尚未更新。</p>}
		<div className="source-health-flow"><strong>{context === 'themes' ? '趋势题材如何取数' : '行情总览如何取数'}</strong>
			{context === 'themes' ? <ol>
				<li>行业强度：腾讯优先，失败回退东方财富；与开盘啦题材强度等权融合。</li>
				<li>开盘啦：题材榜、龙头与涨停池共用至少 5 分钟刷新间隔，期间读取缓存；旧题材按交易日衰减，超过两个交易日不再参与融合。</li>
				<li>成分股强度：由行情、K 线计算，当日 / 五日强度最多每 10 分钟更新。某一步失败仍可展示其余来源或旧快照，不代表所有来源都可用。</li>
			</ol> : <p>盘面脉搏请求财联社快讯和题材快照；指数、行业、资金流等模块在进入时各自取数。这里的 7 项是全局来源目录，包含尚未接入的 TradingView、Tushare，不是当前模块用了 7 个来源。某家供应商一次请求成功也不代表其所有接口都正常。</p>}
			{context === 'themes' && Object.keys(steps).length > 0 && <ul className="source-health-steps">{Object.entries(steps).map(([key, status]) => <li key={key}><strong>{stepLabels[key] || key}：{stepStatusLabels[status] || status}</strong>{stepErrors[key] && <span>{stepErrors[key]}</span>}</li>)}</ul>}
			{meta && <p>当前题材快照：{meta.source} · 抓取 {timeLabel(meta.fetched_at) || '未知'}{meta.trade_date && ` · 交易日 ${meta.trade_date}`}{meta.stale ? ' · 已标记陈旧 / 降级' : ''}{meta.fallback_reason && ` · ${meta.fallback_reason}`}{meta.next_refresh_at && ` · 开盘啦下次允许请求 ${timeLabel(meta.next_refresh_at)}`}</p>}
		</div>
		<details className="source-fallback-details"><summary>失败后如何降级，什么时候会不可用？</summary><p>降级按具体功能执行，不会把所有供应商简单平均或自动启用未接入来源。陈旧快照可用于参考，但不能当作最新行情；已有数据时请同时查看抓取时间、来源、缺失字段和降级说明。</p><div className="source-fallback-table"><table><thead><tr><th>功能</th><th>来源链路</th><th>失效边界</th></tr></thead><tbody>{sourceFallbackPolicies.map(policy => <tr key={policy.feature}><th scope="row">{policy.feature}</th><td>{policy.chain}</td><td>{policy.boundary}</td></tr>)}</tbody></table></div></details>
	</section>;
}
