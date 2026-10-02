package service

import (
	"context"
	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
	"errors"
	"fmt"
	"time"
)

// LatestFuturesMembers is the exchange's bounded latest-day acquisition. It is
// separate from the explicit contract/date member capability.
type LatestFuturesMembers = contracts.FuturesSnapshotProvider
type Futures struct {
	history                contracts.FuturesTrendProvider
	latest                 LatestFuturesMembers
	members                contracts.FuturesMembersProvider
	consensus              contracts.FuturesConsensusProvider
	historyID, exchangeID  string
	membersID, consensusID string
}

func NewFutures(history contracts.FuturesTrendProvider, latest LatestFuturesMembers, members contracts.FuturesMembersProvider, consensus contracts.FuturesConsensusProvider) *Futures {
	return NewFuturesWithSources("eastmoney", "cffex", history, latest, members, consensus)
}
func NewFuturesWithSources(historyID, exchangeID string, history contracts.FuturesTrendProvider, latest LatestFuturesMembers, members contracts.FuturesMembersProvider, consensus contracts.FuturesConsensusProvider) *Futures {
	return NewFuturesCapabilities(FuturesConfig{History: history, Snapshot: latest, Members: members, Consensus: consensus, HistoryID: historyID, SnapshotID: exchangeID, MembersID: exchangeID, ConsensusID: exchangeID})
}

type FuturesConfig struct {
	History                                       contracts.FuturesTrendProvider
	Snapshot                                      contracts.FuturesSnapshotProvider
	Members                                       contracts.FuturesMembersProvider
	Consensus                                     contracts.FuturesConsensusProvider
	HistoryID, SnapshotID, MembersID, ConsensusID string
}

func NewFuturesCapabilities(c FuturesConfig) *Futures {
	return &Futures{history: c.History, latest: c.Snapshot, members: c.Members, consensus: c.Consensus, historyID: c.HistoryID, exchangeID: c.SnapshotID, membersID: c.MembersID, consensusID: c.ConsensusID}
}
func (s *Futures) Trend(ctx context.Context, variety string, limit int) (foundation.MarketFuturesPositionSeries, error) {
	if err := ctx.Err(); err != nil {
		return foundation.MarketFuturesPositionSeries{}, err
	}
	var series foundation.MarketFuturesPositionSeries
	var err error
	var observations []foundation.SourceObservation
	if s.history != nil {
		child, cancel := context.WithTimeout(ctx, 7*time.Second)
		series, err = s.history.Trend(child, variety, limit)
		cancel()
		if series.Meta.Source == "" {
			series.Meta.Source = s.historyID
		}
		series.Meta.Capability = "futures"
		if err == nil && len(series.Rows) == 0 {
			err = &contracts.Error{Kind: contracts.NoData, SourceID: s.historyID, Capability: "futures"}
		}
		if errors.Is(err, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
			return series, context.Canceled
		}
		observations = append(observations, sourceAttempt(series.Meta, s.historyID, "futures", err))
		series.Meta.Observations = observations
		if err == nil {
			return series, nil
		}
	}
	if ctx.Err() != nil {
		return series, ctx.Err()
	}
	if s.latest == nil {
		if err == nil {
			err = &contracts.Error{Kind: contracts.Unsupported, Capability: "futures"}
		}
		return series, err
	}
	members, fallbackErr := s.latest.LatestMembers(ctx, variety, series.ContractCode)
	if fallbackErr == nil && (len(members.Members) == 0 || members.TradeDate == "" || members.ContractCode == "") {
		fallbackErr = &contracts.Error{Kind: contracts.NoData, SourceID: s.exchangeID, Capability: "futures"}
	}
	if errors.Is(fallbackErr, context.Canceled) || errors.Is(ctx.Err(), context.Canceled) {
		return series, context.Canceled
	}
	observations = append(observations, sourceAttempt(members.Meta, s.exchangeID, "futures", fallbackErr))
	if fallbackErr != nil {
		series.Meta.Observations = observations
		return series, fmt.Errorf("期指历史不可用：%v；%s备用数据不可用：%w", err, sourceLabel(s.exchangeID), fallbackErr)
	}
	series.ContractCode = members.ContractCode
	series.Variety = variety
	series.Rows = []foundation.MarketFuturesPositionRow{FuturesMemberPoint(members)}
	series.Meta = members.Meta
	if series.Meta.Source == "" {
		series.Meta.Source = s.exchangeID
	}
	series.Meta.Observations = observations
	series.Meta.Capability = "futures"
	series.Meta.TradeDate = members.TradeDate
	series.Meta.FallbackReason = fmt.Sprintf("%s期指数据不可用，降级为%s最近交易日快照；仅提供单日持仓，不提供历史走势、指数或基差", sourceLabel(s.historyID), sourceLabel(s.exchangeID))
	if s.history == nil {
		series.Meta.FallbackReason = sourceLabel(s.exchangeID) + "最近交易日快照；仅提供单日持仓，不提供历史走势、指数或基差"
	}
	return series, nil
}

// FuturesMemberPoint derives positions only. Missing prices, basis and changes
// remain missing and cannot become a fabricated history or a zero price.
func FuturesMemberPoint(members foundation.MarketFuturesMembers) foundation.MarketFuturesPositionRow {
	point := foundation.MarketFuturesPositionRow{TradeDate: members.TradeDate}
	var longChange, shortChange int64
	longComplete, shortComplete := len(members.Members) > 0, len(members.Members) > 0
	for _, m := range members.Members {
		point.LongPosition += m.LongPosition
		point.ShortPosition += m.ShortPosition
		if m.LongChange == nil {
			longComplete = false
		} else {
			longChange += *m.LongChange
		}
		if m.ShortChange == nil {
			shortComplete = false
		} else {
			shortChange += *m.ShortChange
		}
	}
	if longComplete {
		point.LongChange = &longChange
	}
	if shortComplete {
		point.ShortChange = &shortChange
	}
	point.NetPosition = point.LongPosition - point.ShortPosition
	return point
}
func (s *Futures) Members(ctx context.Context, contract, date string) (foundation.MarketFuturesMembers, error) {
	if s.members == nil {
		return foundation.MarketFuturesMembers{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: s.membersID, Capability: "futures-members"}
	}
	if err := ctx.Err(); err != nil {
		return foundation.MarketFuturesMembers{}, err
	}
	value, err := s.members.Members(ctx, contract, date)
	if value.Meta.Source == "" {
		value.Meta.Source = s.membersID
	}
	value.Meta.Capability = "futures-members"
	if !errors.Is(err, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
		value.Meta.Observations = []foundation.SourceObservation{sourceAttempt(value.Meta, s.membersID, "futures-members", err)}
	}
	return value, err
}
func (s *Futures) Consensus(ctx context.Context, date string) (foundation.MarketFuturesConsensus, error) {
	if s.consensus == nil {
		return foundation.MarketFuturesConsensus{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: s.consensusID, Capability: "futures-consensus"}
	}
	if err := ctx.Err(); err != nil {
		return foundation.MarketFuturesConsensus{}, err
	}
	value, err := s.consensus.Consensus(ctx, date)
	if value.Meta.Source == "" {
		value.Meta.Source = s.consensusID
	}
	value.Meta.Capability = "futures-consensus"
	if !errors.Is(err, context.Canceled) && !errors.Is(ctx.Err(), context.Canceled) {
		value.Meta.Observations = []foundation.SourceObservation{sourceAttempt(value.Meta, s.consensusID, "futures-consensus", err)}
	}
	return value, err
}
