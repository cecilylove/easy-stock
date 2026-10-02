package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
)

// Themes binds retained snapshots to the registered supplier. Stored historical
// snapshots keep their original identity when the active route changes.
type Themes struct {
	id       string
	provider contracts.ThemeFetcher
}

func NewThemes(id string, provider contracts.ThemeFetcher) *Themes {
	return &Themes{id: id, provider: provider}
}

func (p *Themes) identity(meta *foundation.SourceMeta, suffix string) error {
	if (meta.Provider != "" && meta.Provider != p.id) || (meta.Source != "" && strings.SplitN(meta.Source, ":", 2)[0] != p.id) {
		return &contracts.Error{Kind: contracts.InvalidResponse, SourceID: p.id, Capability: "theme-identity"}
	}
	meta.Provider = p.id
	if meta.Source == "" {
		meta.Source = p.id + ":" + suffix
	}
	return nil
}

func (p *Themes) Fetch(ctx context.Context, limit int) (foundation.ThemeSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return foundation.ThemeSnapshot{}, err
	}
	if p.provider == nil {
		return foundation.ThemeSnapshot{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: p.id, Capability: "theme"}
	}
	value, err := p.provider.Fetch(ctx, limit)
	if err != nil {
		return value, err
	}
	if len(value.Themes) == 0 {
		return foundation.ThemeSnapshot{}, &contracts.Error{Kind: contracts.NoData, SourceID: p.id, Capability: "theme"}
	}
	if _, err := time.Parse("2006-01-02", value.TradeDate); err != nil {
		return foundation.ThemeSnapshot{}, &contracts.Error{Kind: contracts.InvalidResponse, SourceID: p.id, Capability: "theme-date", Cause: err}
	}
	if err := p.identity(&value.Meta, "theme"); err != nil {
		return foundation.ThemeSnapshot{}, err
	}
	value.Meta.FetchedAt, value.Meta.TradeDate = value.FetchedAt, value.TradeDate
	if value.ID == "" && !value.FetchedAt.IsZero() {
		value.ID = fmt.Sprintf("%s-theme-%s-%d", p.id, value.TradeDate, value.FetchedAt.UnixMilli())
	}
	return value, nil
}

func (p *Themes) FetchLimitUpPool(ctx context.Context) (foundation.LimitUpPoolSnapshot, error) {
	if err := ctx.Err(); err != nil {
		return foundation.LimitUpPoolSnapshot{}, err
	}
	if p.provider == nil {
		return foundation.LimitUpPoolSnapshot{}, &contracts.Error{Kind: contracts.Unsupported, SourceID: p.id, Capability: "theme-pool"}
	}
	value, err := p.provider.FetchLimitUpPool(ctx)
	if err != nil {
		return value, err
	}
	if _, err := time.Parse("2006-01-02", value.TradeDate); err != nil {
		return foundation.LimitUpPoolSnapshot{}, &contracts.Error{Kind: contracts.InvalidResponse, SourceID: p.id, Capability: "theme-pool-date", Cause: err}
	}
	if err := p.identity(&value.Meta, "limit-up"); err != nil {
		return foundation.LimitUpPoolSnapshot{}, err
	}
	value.Meta.FetchedAt, value.Meta.TradeDate = value.FetchedAt, value.TradeDate
	if value.ID == "" && !value.FetchedAt.IsZero() {
		value.ID = fmt.Sprintf("%s-pool-%s-%d", p.id, value.TradeDate, value.FetchedAt.UnixMilli())
	}
	value.Events = append([]foundation.LimitUpEvent(nil), value.Events...)
	for i := range value.Events {
		value.Events[i].PoolThemeKind = foundation.ThemeAttributionPool
		meta := &value.Events[i].Meta
		if err := p.identity(meta, "limit-up"); err != nil {
			return foundation.LimitUpPoolSnapshot{}, err
		}
		if meta.FetchedAt.IsZero() {
			meta.FetchedAt = value.FetchedAt
		}
		if meta.TradeDate == "" {
			meta.TradeDate = value.TradeDate
		}
	}
	return value, nil
}
