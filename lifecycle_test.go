package zalobot

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"go.uber.org/goleak"
)

func TestMain(m *testing.M) {
	goleak.VerifyTestMain(m)
}

func TestShutdownFromHandlerReturnsPromptly(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(1))
	done := make(chan struct{})
	b.OnMessage(func(ctx context.Context, _ *Message) {
		_ = b.Shutdown(ctx) // must not deadlock
		close(done)
	})
	if err := b.ProcessUpdate([]byte(`{"event_name":"message.text.received","message":{"text":"x"}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Shutdown from handler deadlocked")
	}
	select {
	case <-b.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() never closed")
	}
	if b.Err() != nil {
		t.Fatalf("Err() = %v, want nil after graceful stop", b.Err())
	}
}

func TestDoneWaitsForHandlerThatIgnoresCancellation(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(1), WithDrainTimeout(20*time.Millisecond))
	var release atomic.Bool
	entered := make(chan struct{})
	b.OnMessage(func(context.Context, *Message) {
		close(entered)
		for !release.Load() {
			time.Sleep(5 * time.Millisecond)
		}
	})
	if err := b.ProcessUpdate([]byte(`{"event_name":"message.text.received","message":{"text":"x"}}`)); err != nil {
		t.Fatal(err)
	}
	<-entered
	err := b.Stop()
	if err == nil {
		t.Fatal("Stop should report the drain deadline")
	}
	select {
	case <-b.Done():
		t.Fatal("Done() closed while a handler was still running")
	case <-time.After(50 * time.Millisecond):
	}
	release.Store(true)
	select {
	case <-b.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("Done() never closed after handler exited")
	}
}
