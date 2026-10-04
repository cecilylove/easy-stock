package stockanalysis

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"math"
	"net/url"
	"sort"
	"strings"
	"time"

	"easy-stock/backend/internal/foundation"
)

func NewResearchID() string {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		panic(err)
	}
	return hex.EncodeToString(value[:])
}

func NormalizeResearchRequest(request ResearchRequest) (ResearchRequest, error) {
	symbol, err := foundation.NormalizeSymbol(request.Symbol)
	if err != nil {
		return request, err
	}
	request.Symbol = symbol.Canonical
	level, validLevel := normalizeResearchLevel(request.AnalysisLevel)
	if !validLevel {
		return request, fmt.Errorf("analysis_level must be quantitative, quick, standard or deep")
	}
	request.AnalysisLevel = level
	if request.Purpose == "" {
		request.Purpose = "observe"
	}
	if request.Horizon == "" {
		request.Horizon = "swing"
	}
	switch request.Purpose {
	case "observe", "new_position", "holding":
	default:
		return request, fmt.Errorf("purpose must be observe, new_position or holding")
	}
	switch request.Horizon {
	case "short", "swing", "medium":
	default:
		return request, fmt.Errorf("horizon must be short, swing or medium")
	}
	if request.CostPrice != nil && (!finite(*request.CostPrice) || *request.CostPrice <= 0 || request.Purpose != "holding") {
		return request, fmt.Errorf("cost_price must be positive and is only valid for holding research")
	}
	return request, nil
}

func finite(value float64) bool { return !math.IsNaN(value) && !math.IsInf(value, 0) }

func BuildResearchSnapshot(input Input, analysis Analysis, cutoff time.Time) ResearchSnapshot {
	if cutoff.IsZero() {
		cutoff = time.Now().UTC()
	}
	lines := normalizeKLines(input.KLines)
	baseline := analysis.Scorecard
	// Excluded disclosure must not survive indirectly in the rule baseline.
	if input.Fundamentals != nil && !input.Fundamentals.PublishedAt.IsZero() && input.Fundamentals.PublishedAt.After(cutoff) {
		withoutDisclosure := input
		withoutDisclosure.Fundamentals = nil
		if revised, err := Analyze(withoutDisclosure); err == nil {
			baseline = revised.Scorecard
		} else {
			baseline = Scorecard{}
		}
	}
	snapshot := ResearchSnapshot{
		ID: NewResearchID(), Version: 1, Symbol: analysis.Symbol, Name: analysis.Name,
		CapturedAt: time.Now().UTC(), CutoffAt: cutoff, Quote: analysis.Quote,
		Sources: []ResearchSource{}, Anchors: []PriceAnchor{}, Baseline: baseline,
		DailyBars:   compactDailyBars(lines, 300),
		Limitations: append([]string{}, input.CollectionGaps...),
	}
	addMetric := func(id, title string, value any, date string) {
		encoded, _ := json.Marshal(value)
		snapshot.Sources = append(snapshot.Sources, ResearchSource{ID: id, Kind: "calculation", Title: title,
			Content: string(encoded), Provider: "local-calculation", CapturedAt: snapshot.CapturedAt, ReportDate: date, TimeStatus: "dated"})
	}
	lastDate := ""
	if len(snapshot.DailyBars) > 0 {
		lastDate = snapshot.DailyBars[len(snapshot.DailyBars)-1].Date
	}
	lineMeta := latestKLineMeta(lines)
	priceBasis := researchPriceBasis(lineMeta)
	missing := []string{}
	for _, quality := range historicalFieldQuality(lines, 20) {
		if quality.Status == "limited" {
			missing = append(missing, quality.Message)
		}
	}
	snapshot.Limitations = append(snapshot.Limitations, missing...)
	addMetric("m-price", "日线量价统计（收益为百分比，价格为元，成交额为元）", map[string]any{
		"summary": summarizeDailyKLines(analysis.dailyBars), "recent_bars": compactDailyBars(lines, 20),
		"source": lineMeta, "price_basis": priceBasis, "missing_fields": missing,
		"volume_unit":    firstNonEmpty(lineMeta.VolumeUnit, "unknown"),
		"volume_caution": "仅按来源明确标注的单位使用；unknown/provider_index_volume不可冒充股数或与股票量跨源比较", "intraday_caution": "当日收盘前的日线可能尚未完成，不能作为已完成收盘确认",
	}, lastDate)
	addMetric("m-quote", "行情快照（不等于收盘价）", analysis.Quote, analysis.Quote.TradeTime.Format(time.RFC3339))
	if input.Fundamentals != nil && input.Fundamentals.ReportDate != "" {
		encoded, _ := json.Marshal(map[string]any{"data": input.Fundamentals, "definitions": map[string]string{"revenue": "营业总收入（并非营业收入）", "net_profit": "归属于母公司股东的净利润", "deducted_net_profit": "扣除非经常性损益后的归母净利润", "period": "报告期累计值，不能充作单季度值", "operating_cash_flow_per_share": "每股经营现金流，不能直接当作现金流总额", "field_validity": "meta.fields_known=true时只能使用available_fields列明的指标；其它数字为缺失占位，不是有效零。扣非金额与同比有效性独立，未知不推断为无风险"}})
		published := input.Fundamentals.PublishedAt
		status := "publication_unknown"
		if !published.IsZero() {
			status = "dated"
		}
		if published.IsZero() || !published.After(snapshot.CutoffAt) {
			snapshot.Sources = append(snapshot.Sources, ResearchSource{ID: "f-financial", Kind: "disclosure", Title: "财务披露快照（累计口径，金额为元）",
				Content: string(encoded), Provider: input.Fundamentals.Meta.Source, URL: input.Fundamentals.Meta.SourceURL, PublishedAt: published, CapturedAt: snapshot.CapturedAt, ReportDate: input.Fundamentals.ReportDate, TimeStatus: status})
		} else {
			snapshot.Limitations = append(snapshot.Limitations, "财务披露晚于研究截止时间，未纳入该快照证据")
		}
		snapshot.Limitations = append(snapshot.Limitations, "财务仅含单期披露快照，不能断言连续改善；报告期与披露日期分开，仍非历史修订/PIT财报库")
		if input.Fundamentals.Meta.FallbackReason != "" {
			snapshot.Limitations = append(snapshot.Limitations, "财务来源降级："+input.Fundamentals.Meta.FallbackReason)
		}
	} else {
		snapshot.Limitations = append(snapshot.Limitations, "财务数据不足，不能完成盈利质量或估值判断")
	}
	if input.Business != "" || input.BusinessDetail != "" {
		snapshot.Sources = append(snapshot.Sources, ResearchSource{ID: "f-business", Kind: "company_profile", Title: "公司业务资料",
			Content: truncateText(input.Business+"\n"+input.BusinessDetail, 1800), Provider: input.BusinessSource, URL: input.BusinessMeta.SourceURL, CapturedAt: snapshot.CapturedAt, TimeStatus: "publication_unknown"})
		if input.BusinessMeta.FallbackReason != "" {
			snapshot.Limitations = append(snapshot.Limitations, "公司资料来源降级："+input.BusinessMeta.FallbackReason)
		}
	}
	if len(input.BenchmarkKLines) > 0 && analysis.Relative.Available {
		benchmarkLines := normalizeKLines(input.BenchmarkKLines)
		benchmarkMeta := latestKLineMeta(benchmarkLines)
		addMetric("m-relative", "对照基准统计（不是行业龙头认定）", map[string]any{
			"symbol": input.BenchmarkSymbol, "name": input.BenchmarkName, "bars": compactDailyBars(benchmarkLines, 20),
			"source": benchmarkMeta, "price_basis": researchPriceBasis(benchmarkMeta),
			"volume_unit":    firstNonEmpty(benchmarkMeta.VolumeUnit, "unknown"),
			"volume_caution": "指数成交量单位仅沿用来源标注，不推断为股票股数",
		}, lastDate)
	}
	var stockEvents []foundation.LimitUpEvent
	for _, event := range input.LimitUps {
		if event.Symbol == input.Symbol {
			stockEvents = append(stockEvents, event)
		}
	}
	if len(stockEvents) > 0 {
		addMetric("m-limit", "涨停事件记录", stockEvents[:min(12, len(stockEvents))], lastDate)
	}
	// Membership is kept separate from evidence that a theme caused a price move.
	addMetric("m-concepts", "概念目录与行业归属（不能证明业务或上涨原因）", map[string]any{"industry": input.Industry, "concepts": input.Concepts}, "")
	if len(input.Themes) > 0 {
		themes := append([]foundation.ThemeOverview(nil), input.Themes...)
		sort.SliceStable(themes, func(i, j int) bool { return themes[i].TrendScore > themes[j].TrendScore })
		compact := make([]map[string]any, 0, 12)
		for _, theme := range themes[:min(12, len(themes))] {
			compact = append(compact, map[string]any{"name": theme.Name, "change_percent": theme.ChangePercent, "limit_up_count": theme.LimitUpCount, "date": theme.TradeDate})
		}
		addMetric("m-themes", "市场题材截面（非个股归因）", compact, "")
	}
	for _, item := range input.Announcements[:min(18, len(input.Announcements))] {
		AppendResearchSources(&snapshot, []ResearchSource{ResearchItemSource(item, "announcement", snapshot.CapturedAt)})
	}
	for _, item := range input.Reports[:min(6, len(input.Reports))] {
		AppendResearchSources(&snapshot, []ResearchSource{ResearchItemSource(item, "opinion", snapshot.CapturedAt)})
	}
	terms := []string{analysis.Name, strings.Split(input.Symbol, ".")[0]}
	for _, item := range filterNewsByTerms(input.News, terms)[:min(8, len(filterNewsByTerms(input.News, terms)))] {
		AppendResearchSources(&snapshot, []ResearchSource{NewResearchSource("news", item.Title, item.Content, item.Meta.Source, item.URL, item.PublishedAt, snapshot.CapturedAt)})
	}
	snapshot.Limitations = append(snapshot.Limitations, "新闻与公告为有限检索结果；未检索到不能推断不存在风险", "价格和事件同时出现不证明因果；模型记忆不是本次证据", "没有次日竞价、开盘或逐笔资金数据，不得描述为已经发生")
	if len(snapshot.DailyBars) < 20 {
		snapshot.Limitations = append(snapshot.Limitations, "历史样本不足20日，不足以判断成熟趋势；样本不足本身也不能证明是新上市公司")
	}
	if analysis.Quote.Price <= 0 || input.Quote.Price <= 0 {
		snapshot.Limitations = append(snapshot.Limitations, "实时行情缺失，报价来自最近日线收盘，不能当作盘中实时价格")
	}
	if lastDate != "" {
		day, err := time.Parse("2006-01-02", lastDate)
		if err == nil && cutoff.Sub(day) > 5*24*time.Hour {
			snapshot.Limitations = append(snapshot.Limitations, "日线距分析时点超过5天，行情可能过期或停牌，禁止生成新仓价格计划")
		}
	}
	addAnchor := func(id, label string, price float64) {
		if price > 0 && finite(price) {
			snapshot.Anchors = append(snapshot.Anchors, PriceAnchor{ID: id, Label: label, Price: round2(price), SourceID: "m-price", AsOf: lastDate})
		}
	}
	addAnchor("last_close", "最近日线收盘", analysis.Trend.LatestClose)
	if len(snapshot.DailyBars) >= 20 {
		addAnchor("ma20", "20日均价", analysis.Trend.MA20)
	}
	if len(snapshot.DailyBars) >= 60 {
		addAnchor("ma60", "60日均价", analysis.Trend.MA60)
	}
	if len(snapshot.DailyBars) >= 120 {
		addAnchor("ma120", "120日均价", analysis.Trend.MA120)
	}
	if len(snapshot.DailyBars) > 1 {
		bars := snapshot.DailyBars[max(0, len(snapshot.DailyBars)-20):]
		hi, lo := bars[0].High, bars[0].Low
		for _, bar := range bars {
			hi = math.Max(hi, bar.High)
			lo = math.Min(lo, bar.Low)
		}
		addAnchor("range_high", "样本最近至多20日最高", hi)
		addAnchor("range_low", "样本最近至多20日最低", lo)
	}
	snapshot.Version = 1
	snapshot.Limitations = uniqueStrings(snapshot.Limitations, 24)
	return snapshot
}

func latestKLineMeta(lines []foundation.KLine) foundation.SourceMeta {
	if len(lines) == 0 {
		return foundation.SourceMeta{}
	}
	return lines[len(lines)-1].Meta
}

func researchPriceBasis(meta foundation.SourceMeta) string {
	label := "数据源未明确标注有效复权口径，不能认定为统一前复权序列"
	switch meta.EffectiveAdjustment {
	case "none":
		label = "数据源明确标注不复权日线"
	case "qfq":
		label = "数据源明确标注前复权日线；跨除权时点不可直接比较原阈值"
	case "hfq":
		label = "数据源明确标注后复权日线；不可直接与实时未复权报价比较"
	case "":
	default:
		label = "数据源标注有效口径=" + meta.EffectiveAdjustment + "；不能认定为统一前复权序列"
	}
	if meta.RequestedAdjustment != "" {
		label += "；请求口径=" + meta.RequestedAdjustment + "（请求不等于实际响应）"
	}
	if meta.AdjustmentConvention != "" {
		label += "；来源约定=" + meta.AdjustmentConvention
	}
	if meta.BasisID != "" {
		label += "；基准身份=" + meta.BasisID
	}
	return label + "；不同供应商同名复权不保证等价，不能仅凭请求URL认定有效口径"
}

func ResearchItemSource(item foundation.MarketResearchItem, kind string, captured time.Time) ResearchSource {
	content := item.Content
	if content == "" {
		content = item.Title
	}
	if item.ContentStatus != "" {
		content = fmt.Sprintf("【内容范围：%s；状态：%s；%s】\n%s", item.ContentScope, item.ContentStatus, item.ContentIssue, content)
	}
	if kind == "opinion" {
		content += fmt.Sprintf("\n机构：%s；评级：%s；前次评级：%s", item.Organization, item.Rating, item.PreviousRating)
		if item.Meta.FieldsKnown {
			content += "\n仅available_fields所列结构化指标已取得；正文中的预测/评级为第三方观点，不等于结构化字段或系统结论。"
		}
	}
	if item.Meta.FallbackReason != "" {
		content += "\n来源/覆盖限制：" + item.Meta.FallbackReason
	}
	return NewResearchSource(kind, item.Title, content, item.Meta.Source, item.URL, item.PublishedAt, captured)
}

func NewResearchSource(kind, title, content, provider, rawURL string, published, captured time.Time) ResearchSource {
	content = truncateExactText(content, 1800)
	hash := sha256.Sum256([]byte(kind + "|" + rawURL + "|" + title + "|" + published.Format(time.RFC3339) + "|" + content))
	status := "dated"
	if published.IsZero() {
		status = "publication_unknown"
	}
	parsed, err := url.Parse(rawURL)
	if err != nil || (parsed.Scheme != "https" && parsed.Scheme != "http") {
		rawURL = ""
	}
	return ResearchSource{ID: "s-" + hex.EncodeToString(hash[:8]), Kind: kind, Title: truncateExactText(title, 160), Content: content, Provider: provider, URL: rawURL, PublishedAt: published, CapturedAt: captured, TimeStatus: status}
}

func AppendResearchSources(snapshot *ResearchSnapshot, sources []ResearchSource) int {
	seen := map[string]bool{}
	for _, source := range snapshot.Sources {
		seen[source.ID] = true
	}
	added := 0
	for _, source := range sources {
		if seen[source.ID] || source.ID == "" || strings.TrimSpace(source.Content) == "" || len(snapshot.Sources) >= 60 {
			continue
		}
		if !source.PublishedAt.IsZero() && source.PublishedAt.After(snapshot.CutoffAt) {
			snapshot.Limitations = uniqueStrings(append(snapshot.Limitations, "补充材料含分析时点之后的披露，已排除，重新分析后才可使用"), 24)
			continue
		}
		seen[source.ID] = true
		snapshot.Sources = append(snapshot.Sources, source)
		added++
	}
	if added > 0 {
		snapshot.Version++
	}
	return added
}
