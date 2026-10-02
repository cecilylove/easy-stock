package service

import (
	"context"
	"errors"
	"io"
	"testing"
	"time"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
)

type priceTestKLine func(context.Context, string, string, int) ([]foundation.KLine, error)

func (f priceTestKLine) KLine(ctx context.Context, symbol, period string, limit int) ([]foundation.KLine, error) {
	return f(ctx, symbol, period, limit)
}

type priceTestAdjusted struct {
	load    func(context.Context, string, string, int, string) ([]foundation.KLine, error)
	support bool
}

func (p priceTestAdjusted) KLineAdjusted(ctx context.Context, symbol, period string, limit int, adjustment string) ([]foundation.KLine, error) {
	return p.load(ctx, symbol, period, limit, adjustment)
}
func (p priceTestAdjusted) SupportsAdjustedKLine(string, string, string) bool { return p.support }

func priceTestBars(source, adjustment string) []foundation.KLine {
	return []foundation.KLine{{
		Symbol: "000002.SZ", Time: time.Date(2026, 9, 30, 0, 0, 0, 0, time.UTC),
		Open: 10, High: 12, Low: 9, Close: 11, Volume: 500,
		Meta: foundation.SourceMeta{
			Source: source + ":stock-kline", Provider: source,
			EffectiveAdjustment: adjustment, AdjustmentConvention: source + ":current",
			BasisID: source + ":" + adjustment, VolumeUnit: "shares", AmountCurrency: "CNY",
			FieldsKnown: true, AvailableFields: []string{"open", "high", "low", "close", "volume"},
		},
	}}
}

func TestPriceRegisteredStrictSourceDoesNotDependOnSupplierName(t *testing.T) {
	defaultCalls, strictCalls := 0, 0
	var observed []foundation.SourceObservation
	prices := NewPriceService(PriceServiceConfig{
		Routes: []PriceRoute{{SourceID: "default", Provider: priceTestKLine(func(context.Context, string, string, int) ([]foundation.KLine, error) {
			defaultCalls++
			return nil, errors.New("strict request must not use default")
		})}},
		Strict: map[string]contracts.AdjustedKLineProvider{"new-source": priceTestAdjusted{support: true, load: func(_ context.Context, symbol, period string, limit int, adjustment string) ([]foundation.KLine, error) {
			strictCalls++
			if symbol != "000002.SZ" || period != "month" || limit != 36 || adjustment != "qfq" {
				t.Fatalf("strict annual request lost its identity: %s/%s/%d/%s", symbol, period, limit, adjustment)
			}
			return priceTestBars("new-source", adjustment), nil
		}}},
		Observe: func(event foundation.SourceObservation) { observed = append(observed, event) },
	})
	if !prices.SupportsAdjusted("new-source", "000002.SZ", "year", "qfq") {
		t.Fatal("registered source was not recognized")
	}
	bars, err := prices.KLineAdjusted(context.Background(), "000002.SZ", "year", 2, "qfq", "new-source")
	if err != nil || len(bars) != 1 || bars[0].Meta.Period != "year" || bars[0].Meta.Provider != "new-source" || defaultCalls != 0 || strictCalls != 1 {
		t.Fatalf("registered strict source failed: %+v %v calls=%d/%d", bars, err, defaultCalls, strictCalls)
	}
	if len(observed) != 1 || observed[0].SourceID != "new-source" || observed[0].Failed || observed[0].Capability != "stock-kline:month:qfq" {
		t.Fatalf("annual aggregation duplicated or misattributed upstream attempt: %+v", observed)
	}
}

func TestPriceStrictIdentityFailureCannotFallBackOrTripNetworkBreaker(t *testing.T) {
	for _, invalid := range []string{"identity", "adjustment"} {
		t.Run(invalid, func(t *testing.T) {
			clock := time.Now()
			state := NewPriceRouteState(func() time.Time { return clock }, nil)
			defaultCalls := 0
			var observed []foundation.SourceObservation
			prices := NewPriceService(PriceServiceConfig{
				Routes: []PriceRoute{{SourceID: "healthy", Provider: priceTestKLine(func(context.Context, string, string, int) ([]foundation.KLine, error) {
					defaultCalls++
					return priceTestBars("healthy", "none"), nil
				})}},
				Strict: map[string]contracts.AdjustedKLineProvider{"selected": priceTestAdjusted{support: true, load: func(context.Context, string, string, int, string) ([]foundation.KLine, error) {
					if invalid == "identity" {
						return priceTestBars("other", "qfq"), nil
					}
					return priceTestBars("selected", "none"), nil
				}}},
				State: state, Observe: func(event foundation.SourceObservation) { observed = append(observed, event) },
			})
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := prices.KLineAdjusted(context.Background(), "000002.SZ", "day", 1, "qfq", "selected"); err == nil {
					t.Fatal("mismatched strict series accepted")
				}
			}
			if defaultCalls != 0 || !state.Ready("selected:SZ:day:qfq") || len(observed) != 2 {
				t.Fatalf("identity mismatch changed policy: default=%d events=%+v", defaultCalls, observed)
			}
			for _, event := range observed {
				if event.SourceID != "selected" || event.Capability != "stock-kline:day:qfq:symbol:000002.SZ" || !event.Failed {
					t.Fatalf("invalid series misattributed: %+v", event)
				}
			}
		})
	}
}

func TestPriceDefaultFallbackUsesOneSnapshotAndPreservesSourceDefault(t *testing.T) {
	primaryCalls, fallbackCalls := 0, 0
	var observed []foundation.SourceObservation
	prices := NewPriceService(PriceServiceConfig{
		Routes: []PriceRoute{
			{SourceID: "primary", Provider: priceTestKLine(func(context.Context, string, string, int) ([]foundation.KLine, error) {
				primaryCalls++
				return nil, io.EOF
			})},
			{SourceID: "backup", Provider: priceTestKLine(func(context.Context, string, string, int) ([]foundation.KLine, error) {
				fallbackCalls++
				return priceTestBars("backup", "none"), nil
			})},
		},
		Observe: func(event foundation.SourceObservation) { observed = append(observed, event) },
	})
	for attempt := 0; attempt < 3; attempt++ {
		bars, err := prices.KLine(context.Background(), "000002.SZ", "day", 1)
		if err != nil || len(bars) != 1 || bars[0].Meta.Source != "backup:stock-kline" || bars[0].Meta.RequestedAdjustment != "source" || bars[0].Meta.EffectiveAdjustment != "none" || bars[0].Meta.FallbackReason == "" {
			t.Fatalf("fallback rewrote price semantics: %+v %v", bars, err)
		}
	}
	if primaryCalls != 2 || fallbackCalls != 3 || len(observed) != 5 {
		t.Fatalf("cooling or unattempted source observations changed: %d/%d %+v", primaryCalls, fallbackCalls, observed)
	}
}

func TestPriceRegisteredDefaultCannotRelabelAnotherSupplierSnapshot(t *testing.T) {
	var observed []foundation.SourceObservation
	prices := NewPriceService(PriceServiceConfig{
		Routes: []PriceRoute{
			{SourceID: "requested", Provider: priceTestKLine(func(context.Context, string, string, int) ([]foundation.KLine, error) {
				return priceTestBars("other", "none"), nil
			})},
			{SourceID: "backup", Provider: priceTestKLine(func(context.Context, string, string, int) ([]foundation.KLine, error) {
				return priceTestBars("backup", "none"), nil
			})},
		},
		Observe: func(event foundation.SourceObservation) { observed = append(observed, event) },
	})
	bars, err := prices.KLine(context.Background(), "000002.SZ", "day", 1)
	if err != nil || len(bars) != 1 || bars[0].Meta.Source != "backup:stock-kline" {
		t.Fatalf("foreign snapshot relabelled as requested source: %+v %v", bars, err)
	}
	if len(observed) != 2 || observed[0].SourceID != "requested" || !observed[0].Failed || observed[1].SourceID != "backup" || observed[1].Failed {
		t.Fatalf("foreign supplier identity recorded as successful: %+v", observed)
	}
}

func TestPriceTotalBudgetAndCancellationDoNotAttemptRemainingSources(t *testing.T) {
	var calls []string
	prices := NewPriceService(PriceServiceConfig{Routes: []PriceRoute{
		{SourceID: "slow", Provider: priceTestKLine(func(ctx context.Context, _ string, _ string, _ int) ([]foundation.KLine, error) {
			calls = append(calls, "slow")
			deadline, ok := ctx.Deadline()
			if !ok || time.Until(deadline) > 55*time.Millisecond {
				t.Fatalf("first source took entire caller budget: %v", deadline)
			}
			return nil, context.DeadlineExceeded
		})},
		{SourceID: "backup", Provider: priceTestKLine(func(context.Context, string, string, int) ([]foundation.KLine, error) {
			calls = append(calls, "backup")
			return priceTestBars("backup", "none"), nil
		})},
	}})
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := prices.KLine(ctx, "000002.SZ", "day", 1); err != nil || len(calls) != 2 {
		t.Fatalf("fallback budget lost: %v %v", err, calls)
	}
	cancel()
	if _, err := prices.KLine(ctx, "000002.SZ", "day", 1); !errors.Is(err, context.Canceled) || len(calls) != 2 {
		t.Fatalf("cancelled caller attempted sources: %v %v", err, calls)
	}
}

func TestPriceAdapterCancellationStopsDefaultAndStrictRoutes(t *testing.T) {
	for _, canceled := range []error{context.Canceled, &contracts.Error{Kind: contracts.Canceled}} {
		defaultCalls, backupCalls, strictCalls, observations := 0, 0, 0, 0
		state := NewPriceRouteState(nil, nil)
		prices := NewPriceService(PriceServiceConfig{
			Routes: []PriceRoute{
				{SourceID: "primary", Provider: priceTestKLine(func(context.Context, string, string, int) ([]foundation.KLine, error) {
					defaultCalls++
					return nil, canceled
				})},
				{SourceID: "backup", Provider: priceTestKLine(func(context.Context, string, string, int) ([]foundation.KLine, error) {
					backupCalls++
					return priceTestBars("backup", "none"), nil
				})},
			},
			Strict: map[string]contracts.AdjustedKLineProvider{"primary": priceTestAdjusted{support: true, load: func(context.Context, string, string, int, string) ([]foundation.KLine, error) {
				strictCalls++
				return nil, canceled
			}}},
			State: state, Observe: func(foundation.SourceObservation) { observations++ },
		})
		for attempt := 0; attempt < 3; attempt++ {
			if _, err := prices.KLine(context.Background(), "000002.SZ", "day", 1); !errors.Is(err, context.Canceled) {
				t.Fatalf("default route lost cancellation: %v", err)
			}
			if _, err := prices.KLineAdjusted(context.Background(), "000002.SZ", "day", 1, "qfq", "primary"); !errors.Is(err, context.Canceled) {
				t.Fatalf("strict route lost cancellation: %v", err)
			}
		}
		if defaultCalls != 3 || strictCalls != 3 || backupCalls != 0 || observations != 0 || len(state.failures) != 0 {
			t.Fatalf("cancellation invoked a backup or mutated health/backoff: calls=%d/%d/%d observations=%d failures=%+v", defaultCalls, strictCalls, backupCalls, observations, state.failures)
		}
	}
}

func TestPriceContractRejectsMixedUnitsAndDoesNotMutateProviderData(t *testing.T) {
	first := priceTestBars("source", "qfq")[0]
	second := first
	second.Time = second.Time.AddDate(0, 0, 1)
	second.Meta.VolumeUnit = "lots"
	if _, err := NormalizeKLineContract([]foundation.KLine{first, second}, "000002.SZ", "day", "source", ""); !errors.Is(err, foundation.ErrInvalidPriceData) {
		t.Fatalf("mixed quantity units accepted: %v", err)
	}
	first.Meta.AvailableFields = append([]string(nil), first.Meta.AvailableFields...)
	bars, err := NormalizeKLineContract([]foundation.KLine{first}, "000002.SZ", "day", "source", "")
	if err != nil {
		t.Fatal(err)
	}
	bars[0].Meta.AvailableFields[0] = "changed"
	if first.Meta.AvailableFields[0] != "open" {
		t.Fatal("result shares mutable field metadata with provider")
	}
}

func TestPriceTypedMissingInstrumentDoesNotBlockHealthyInstrument(t *testing.T) {
	for _, kind := range []contracts.ErrorKind{contracts.NoData, contracts.InvalidResponse} {
		t.Run(string(kind), func(t *testing.T) {
			calls := 0
			var observed []foundation.SourceObservation
			prices := NewPriceService(PriceServiceConfig{
				Routes: []PriceRoute{{SourceID: "source", Provider: priceTestKLine(func(_ context.Context, symbol, _ string, _ int) ([]foundation.KLine, error) {
					calls++
					if symbol == "009999.SZ" {
						return nil, &contracts.Error{Kind: kind, SourceID: "source", Capability: "kline"}
					}
					return priceTestBars("source", "none"), nil
				})}},
				Observe: func(event foundation.SourceObservation) { observed = append(observed, event) },
			})
			for attempt := 0; attempt < 2; attempt++ {
				if _, err := prices.KLine(context.Background(), "009999.SZ", "day", 1); err == nil {
					t.Fatal("missing instrument accepted")
				}
			}
			if _, err := prices.KLine(context.Background(), "000002.SZ", "day", 1); err != nil || calls != 3 || len(observed) != 3 {
				t.Fatalf("typed instrument issue disabled healthy prices: %v calls=%d events=%+v", err, calls, observed)
			}
			if observed[0].Capability != "stock-kline:day:source:symbol:009999.SZ" || observed[2].Failed {
				t.Fatalf("instrument diagnostic leaked into whole capability: %+v", observed)
			}
		})
	}
}
