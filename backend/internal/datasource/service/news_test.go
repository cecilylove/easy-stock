package service

import (
	"context"
	"errors"
	"testing"

	"easy-stock/backend/internal/datasource/contracts"
	"easy-stock/backend/internal/foundation"
)

type cancelingNews struct{ err error }

func (p cancelingNews) LatestNews(context.Context, int) ([]foundation.NewsItem, error) {
	return nil, p.err
}

func TestNewsAdapterCancellationDoesNotRecordFailure(t *testing.T) {
	for _, canceled := range []error{context.Canceled, &contracts.Error{Kind: contracts.Canceled}} {
		observations := 0
		news := NewNews("fixture", cancelingNews{err: canceled}, func(foundation.SourceObservation) { observations++ })
		_, err := news.LatestNews(context.Background(), 10)
		if !errors.Is(err, context.Canceled) || observations != 0 {
			t.Fatalf("supplier cancellation recorded as failure: %v observations=%d", err, observations)
		}
	}
}
