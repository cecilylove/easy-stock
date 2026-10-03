package ths

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestSharedOwnerCancellationDoesNotCancelActiveViewer(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	var calls atomic.Int32
	client := localClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		close(entered)
		select {
		case <-release:
			w.Write([]byte(fixturePage))
		case <-r.Context().Done():
		}
	})
	ownerCtx, cancel := context.WithCancel(context.Background())
	owner := make(chan error, 1)
	go func() { _, _, err := client.Fetch(ownerCtx, "000001", "2026-09-30"); owner <- err }()
	<-entered
	viewer := make(chan error, 1)
	go func() { _, _, err := client.Fetch(context.Background(), "000002", "2026-09-30"); viewer <- err }()
	deadline := time.Now().Add(time.Second)
	for {
		client.mu.Lock()
		flight := client.flights["2026-09-30"]
		joined := flight != nil && flight.waiters == 2
		client.mu.Unlock()
		if joined {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("viewer did not join")
		}
		time.Sleep(time.Millisecond)
	}
	cancel()
	if err := <-owner; !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	if err := <-viewer; err != nil {
		t.Fatalf("owner cancelled another active viewer: %v", err)
	}
	if calls.Load() != 1 {
		t.Fatalf("duplicate requests %d", calls.Load())
	}
}
