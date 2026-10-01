import { BrainCircuit, CheckCircle2, ChevronRight, CircleAlert, Clock3, LoaderCircle, RefreshCw, ShieldAlert, Sparkles, Target, WalletCards, X } from 'lucide-react';
import { useCallback, useEffect, useMemo, useState } from 'react';
import { BackendConfig, PortfolioExpectationJob, PortfolioExpectationReport, StockDirectoryData, StockDirectoryEntry, requestJSON } from '../lib/backend';
import { PortfolioDraft, portfolioDraftChangedEvent, portfolioDraftStorageKey, portfolioDraftToHoldings, readPortfolioDraft, writePortfolioDraft } from '../lib/portfolio-draft';
import { matchingPortfolioExpectation, portfolioExpectationIdentity } from '../lib/portfolio-expectation';
import { PortfolioSetupForm } from './PortfolioSetupForm';

type Props = {
	config: BackendConfig | null;
	summaryDate: string;
};

export function PortfolioTomorrowExpectation({ config, summaryDate }: Props) {
	// A new review date/backend must never reuse an earlier session's report.
	return <PortfolioExpectationSession key={JSON.stringify([config?.backendUrl, config?.token, summaryDate])} config={config} summaryDate={summaryDate} />;
}

function PortfolioExpectationSession({ config, summaryDate }: Props) {
	const [job, setJob] = useState<PortfolioExpectationJob | null>(null);
	const [portfolio, setPortfolio] = useState(readPortfolioDraft);
	const [setupDraft, setSetupDraft] = useState<PortfolioDraft | null>(null);
	const [directory, setDirectory] = useState<StockDirectoryEntry[]>([]);
	const [reportOpen, setReportOpen] = useState(false);
	const [starting, setStarting] = useState(false);
	const [error, setError] = useState('');
	const currentJob = matchingPortfolioExpectation(job, summaryDate, portfolio) ? job : null;
	const running = currentJob?.status === 'running';
	const portfolioKey = portfolioExpectationIdentity(summaryDate, portfolio);
	useEffect(() => {
		const changed = () => setPortfolio(readPortfolioDraft());
		const stored = (event: StorageEvent) => { if (event.key === portfolioDraftStorageKey || event.key === null) changed(); };
		window.addEventListener(portfolioDraftChangedEvent, changed); window.addEventListener('storage', stored);
		return () => { window.removeEventListener(portfolioDraftChangedEvent, changed); window.removeEventListener('storage', stored); };
	}, []);

	useEffect(() => {
		if (!config || !summaryDate) return;
		setJob(current => matchingPortfolioExpectation(current, summaryDate, portfolio) ? current : null);
		setReportOpen(false); setError('');
		let active = true;
		requestJSON<{ data: PortfolioExpectationJob | null }>(config, `/api/v1/reviews/portfolio-expectations/latest?summary_date=${encodeURIComponent(summaryDate)}`)
			.then((payload) => {
				if (!active || !matchingPortfolioExpectation(payload.data, summaryDate, readPortfolioDraft())) return;
				setJob(current => current && matchingPortfolioExpectation(current, summaryDate, readPortfolioDraft()) ? current : payload.data);
			})
			.catch(() => undefined);
		return () => { active = false; };
	}, [config, summaryDate, portfolioKey]);

	useEffect(() => {
		if (!config || !currentJob || currentJob.status !== 'running') return;
		let active = true;
		const poll = async () => {
			try {
				const payload = await requestJSON<{ data: PortfolioExpectationJob }>(config, `/api/v1/reviews/portfolio-expectations/${encodeURIComponent(currentJob.id)}`);
				if (!active || !matchingPortfolioExpectation(payload.data, summaryDate, readPortfolioDraft())) return;
				setJob(payload.data);
				if (payload.data.report_available && payload.data.status !== 'running') setReportOpen(true);
				if (payload.data.status === 'failed') setError(payload.data.error || payload.data.message || '持仓明日预期生成失败');
			} catch {
				// Returning to the review page reloads the persisted job.
			}
		};
		void poll();
		const timer = window.setInterval(() => void poll(), 3000);
		return () => { active = false; window.clearInterval(timer); };
	}, [config, currentJob?.id, currentJob?.status, portfolioKey, summaryDate]);

	const startExpectation = useCallback(async (draft: PortfolioDraft, force = false) => {
		if (!config || !draft.holdings.length) return;
		setStarting(true);
		setError('');
		setReportOpen(false);
		try {
			const payload = await requestJSON<{ data: PortfolioExpectationJob }>(config, '/api/v1/reviews/portfolio-expectations', {
				method: 'POST',
				headers: { 'Content-Type': 'application/json' },
				body: JSON.stringify({ summary_date: summaryDate, trader_profile: draft.profile, holdings: portfolioDraftToHoldings(draft.holdings), force }),
			});
			if (!matchingPortfolioExpectation(payload.data, summaryDate, readPortfolioDraft())) return;
			setPortfolio(readPortfolioDraft()); setJob(payload.data);
			setSetupDraft(null);
			if (payload.data.report_available) setReportOpen(true);
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : '持仓明日预期启动失败');
		} finally {
			setStarting(false);
		}
	}, [config, summaryDate]);

	const openSetup = useCallback(async () => {
		setSetupDraft(readPortfolioDraft());
		if (directory.length || !config) return;
		try {
			const payload = await requestJSON<{ data: StockDirectoryData }>(config, '/api/v1/stocks/directory');
			setDirectory(payload.data.stocks || []);
		} catch (cause) {
			setError(cause instanceof Error ? cause.message : '股票目录加载失败');
		}
	}, [config, directory.length]);

	const handlePrimaryAction = () => {
		if (running || starting) return;
		if (currentJob?.report_available && currentJob.report && matchingPortfolioExpectation(currentJob, summaryDate, readPortfolioDraft())) { setReportOpen(true); return; }
		const draft = readPortfolioDraft();
		if (!draft.holdings.length) { void openSetup(); return; }
		void startExpectation(draft);
	};

	const label = running ? `分析中 ${currentJob?.completed_stocks || 0}/${currentJob?.total_stocks || '—'}` : starting ? '正在启动' : currentJob?.report_available ? '查看明日预期' : '持仓明日预期';
	return <>
		<button type="button" className="portfolio-expectation-trigger" onClick={handlePrimaryAction} disabled={running || starting} title="结合今日复盘和当前持仓生成明日情景预期">{running || starting ? <LoaderCircle className="spin" size={14} /> : currentJob?.report_available ? <CheckCircle2 size={14} /> : <WalletCards size={14} />}{label}</button>
		{error && <div className="portfolio-expectation-error" role="alert"><CircleAlert size={14} /><span>{error}</span><button type="button" onClick={() => setError('')} aria-label="关闭错误"><X size={13} /></button></div>}
		{setupDraft && <PortfolioExpectationSetupDialog draft={setupDraft} directory={directory} busy={starting} onChange={setSetupDraft} onClose={() => setSetupDraft(null)} onSubmit={() => { writePortfolioDraft(setupDraft); void startExpectation(setupDraft); }} />}
		{reportOpen && currentJob?.report && <PortfolioExpectationReportDialog report={currentJob.report} partial={currentJob.status === 'partial'} onClose={() => setReportOpen(false)} onRegenerate={() => { setReportOpen(false); void startExpectation(readPortfolioDraft(), true); }} />}
	</>;
}

function PortfolioExpectationSetupDialog({ draft, directory, busy, onChange, onClose, onSubmit }: { draft: PortfolioDraft; directory: StockDirectoryEntry[]; busy: boolean; onChange: (draft: PortfolioDraft) => void; onClose: () => void; onSubmit: () => void }) {
	return <div className="portfolio-expectation-overlay" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
		<section className="portfolio-expectation-setup" role="dialog" aria-modal="true" aria-labelledby="portfolio-expectation-setup-title">
			<header><div><span><WalletCards size={16} />PORTFOLIO SETUP</span><h2 id="portfolio-expectation-setup-title">配置持仓并生成明日预期</h2><p>保存后会同步到“持仓 AI 巡检”，两处始终使用同一份配置。</p></div><button type="button" onClick={onClose} aria-label="关闭"><X size={18} /></button></header>
			<PortfolioSetupForm draft={draft} directory={directory} disabled={busy} busy={busy} actionLabel="保存并开始分析" busyLabel="正在启动" actionIcon={<Sparkles size={17} />} onChange={onChange} onSubmit={onSubmit} />
		</section>
	</div>;
}

function PortfolioExpectationReportDialog({ report, partial, onClose, onRegenerate }: { report: PortfolioExpectationReport; partial: boolean; onClose: () => void; onRegenerate: () => void }) {
	const conclusion = report.conclusion;
	const names = useMemo(() => new Map(report.holdings.map((item) => [item.holding.symbol, item.holding.name || item.holding.symbol])), [report.holdings]);
	return <div className="portfolio-expectation-overlay report" role="presentation" onMouseDown={(event) => { if (event.target === event.currentTarget) onClose(); }}>
		<section className="portfolio-expectation-report" role="dialog" aria-modal="true" aria-labelledby="portfolio-expectation-report-title">
			<header className="portfolio-expectation-report-heading"><div><span><BrainCircuit size={16} />PORTFOLIO TOMORROW</span><h2 id="portfolio-expectation-report-title">{report.trade_date} 持仓明日预期</h2><p>{report.profile.label}型 · 持仓 {report.metrics.total_position_percent}% · 现金 {report.metrics.cash_percent}% · 置信度 {Math.round(conclusion.confidence * 100)}%</p></div><div><button type="button" onClick={onRegenerate}><RefreshCw size={14} />重新生成</button><button type="button" onClick={onClose} aria-label="关闭"><X size={18} /></button></div></header>
			<div className={`portfolio-expectation-bias ${biasClass(conclusion.portfolio_bias)}`}><div><span>组合明日基线</span><strong>{conclusion.portfolio_bias}</strong></div><p>{conclusion.headline}</p></div>
			{partial && <div className="portfolio-expectation-warning"><ShieldAlert size={15} />部分个股或 AI 综合分析使用了降级结果，请结合数据限制阅读。</div>}
			<section className="portfolio-expectation-core"><header><Target size={15} /><strong>核心矛盾</strong></header><p>{conclusion.core_conflict}</p></section>
			<section className="portfolio-expectation-section"><header><strong>复盘与持仓映射</strong><small>作者观点只作为情景证据，不覆盖个股结构</small></header><div className="portfolio-exposure-grid">{conclusion.review_exposure.map((item) => <article key={item.symbol}><div><strong>{names.get(item.symbol) || item.symbol}</strong><em className={alignmentClass(item.alignment)}>{item.alignment}</em></div><EvidenceGroup label="复盘证据" items={item.review_evidence} /><EvidenceGroup label="结构证据" items={item.structure_evidence} /></article>)}</div></section>
			<section className="portfolio-expectation-section"><header><strong>次日三情景</strong><small>按可观察条件切换预案，不给伪精确概率</small></header><div className="portfolio-expectation-scenarios">{conclusion.scenarios.map((scenario) => <article className={scenario.key} key={scenario.key}><header><span>{scenario.name}</span><em>{scenario.key.toUpperCase()}</em></header><p>{scenario.portfolio_impact}</p><ul>{scenario.market_triggers.map((item) => <li key={item}>{item}</li>)}</ul><strong>{scenario.total_position_response}</strong><details><summary>逐股响应 <ChevronRight size={13} /></summary>{scenario.holding_responses.map((item) => <div key={item.symbol}><b>{names.get(item.symbol) || item.symbol}</b><span>{item.expected_behavior}</span><small>动作：{item.action}</small><small>确认：{item.confirmation}</small><small>失效：{item.invalidation}</small></div>)}</details></article>)}</div></section>
			<section className="portfolio-expectation-section"><header><strong>逐股明日预期</strong><small>竞价/开盘、盘中确认、收盘验证</small></header><div className="portfolio-expectation-holdings">{conclusion.holdings.map((item) => <article key={item.symbol}><header><div><strong>{names.get(item.symbol) || item.symbol}</strong><small>{item.symbol}</small></div><em>{item.priority}</em></header><div className="portfolio-expectation-stage-grid"><span><b>开盘前30分钟</b>{item.opening_expectation}</span><span><b>盘中</b>{item.intraday_expectation}</span><span><b>收盘</b>{item.close_expectation}</span></div>{item.cost_context && <p>持仓处境：{item.cost_context}</p>}<dl><div><dt>触发前</dt><dd>{item.before_trigger}</dd></div><div><dt>转强确认</dt><dd>{item.positive_trigger}</dd></div><div><dt>风险触发</dt><dd>{item.negative_trigger}</dd></div><div><dt>预案失效</dt><dd>{item.invalidation}</dd></div></dl></article>)}</div></section>
			<section className="portfolio-expectation-section"><header><Clock3 size={15} /><strong>明日核验时间轴</strong></header><div className="portfolio-expectation-timeline"><TimelineStage label="盘前 / 竞价" items={conclusion.timeline.pre_open} /><TimelineStage label="开盘30分钟" items={conclusion.timeline.opening_30m} /><TimelineStage label="盘中确认" items={conclusion.timeline.intraday} /><TimelineStage label="收盘验证" items={conclusion.timeline.close} /></div></section>
			<div className="portfolio-expectation-bottom-grid"><section><strong>风险提醒</strong><BulletList items={conclusion.risk_alerts} empty="暂无额外风险提醒" /></section><section><strong>数据限制</strong><BulletList items={conclusion.data_limitations} empty="未发现明显数据缺口" /></section></div>
			<footer>仅用于信息整理、研究与复盘，不构成投资建议或交易指令。</footer>
		</section>
	</div>;
}

function EvidenceGroup({ label, items }: { label: string; items: string[] }) { return <div><b>{label}</b>{items?.length ? <ul>{items.map((item) => <li key={item}>{item}</li>)}</ul> : <span>暂无直接证据</span>}</div>; }
function TimelineStage({ label, items }: { label: string; items: string[] }) { return <article><strong>{label}</strong><BulletList items={items} empty="等待盘面确认" /></article>; }
function BulletList({ items, empty }: { items?: string[]; empty: string }) { return items?.length ? <ul>{items.map((item) => <li key={item}>{item}</li>)}</ul> : <p>{empty}</p>; }
function biasClass(value: string) { return value === '有利' ? 'positive' : value === '承压' ? 'negative' : 'neutral'; }
function alignmentClass(value: string) { return value === '共振' ? 'positive' : value === '背离' ? 'negative' : value === '部分共振' ? 'partial' : 'neutral'; }
