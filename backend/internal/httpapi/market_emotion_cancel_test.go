package httpapi

import (
	"context"
	"errors"
	"testing"
	"time"

	"easy-stock/backend/internal/marketemotion"
)

func TestEmotionCancelledRefreshCanRetryImmediately(t *testing.T) {
	for _, withSnapshot := range []bool{false, true} {
		t.Run(map[bool]string{false: "first-load", true: "expired-snapshot"}[withSnapshot], func(t *testing.T) {
			now := time.Now()
			cache := newMarketEmotionIntradayCache(10 * time.Minute)
			cache.now = func() time.Time { return now }
			if withSnapshot {
				_, err := cache.load(context.Background(), func(context.Context) (marketemotion.IntradaySnapshot, error) {
					return marketemotion.IntradaySnapshot{TradeDate: "2026-09-30", Status: "old"}, nil
				})
				if err != nil {
					t.Fatal(err)
				}
				now = now.Add(11 * time.Minute)
			}
			previousExpiry, previousSnapshot := cache.expiresAt, cache.snapshot
			ctx, cancel := context.WithCancel(context.Background())
			_, err := cache.load(ctx, func(ctx context.Context) (marketemotion.IntradaySnapshot, error) {
				cancel()
				return marketemotion.IntradaySnapshot{}, ctx.Err()
			})
			if !errors.Is(err, context.Canceled) || !cache.expiresAt.Equal(previousExpiry) || cache.snapshot.Status != previousSnapshot.Status || cache.snapshot.Stale != previousSnapshot.Stale {
				t.Fatalf("cancellation changed cache: err=%v expiry=%v snapshot=%+v", err, cache.expiresAt, cache.snapshot)
			}
			calls := 0
			fresh, err := cache.load(context.Background(), func(context.Context) (marketemotion.IntradaySnapshot, error) {
				calls++
				return marketemotion.IntradaySnapshot{Status: "fresh"}, nil
			})
			if err != nil || calls != 1 || fresh.Status != "fresh" || fresh.Stale {
				t.Fatalf("healthy retry suppressed: calls=%d fresh=%+v err=%v", calls, fresh, err)
			}
		})
	}
}

func TestEmotionCallerDeadlineDoesNotCacheFailure(t *testing.T) {
	cache := newMarketEmotionIntradayCache(10 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	_, err := cache.load(ctx, func(ctx context.Context) (marketemotion.IntradaySnapshot, error) {
		<-ctx.Done()
		return marketemotion.IntradaySnapshot{}, ctx.Err()
	})
	if !errors.Is(err, context.DeadlineExceeded) || !cache.expiresAt.IsZero() || cache.lastErr != nil {
		t.Fatalf("caller deadline became source negative cache: err=%v expiry=%v cached err=%v", err, cache.expiresAt, cache.lastErr)
	}
}
