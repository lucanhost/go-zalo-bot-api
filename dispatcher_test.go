package zalobot

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func newTestDispatcher(cfg dispatchConfig, handle func(Update)) (*dispatcher, chan Update) {
	ch := make(chan Update, 64)
	var on atomic.Bool
	on.Store(true)
	return newDispatcher(cfg, ch, &on, func(error) {}, handle), ch
}

func msgUpdate(chat, text string) Update {
	return Update{EventName: EventTextReceived, Message: &Message{Chat: &Chat{ID: chat}, Text: text}}
}

func TestDispatcherProcessesInOrderPerChat(t *testing.T) {
	var mu sync.Mutex
	var got []string
	done := make(chan struct{})
	d, _ := newTestDispatcher(dispatchConfig{workers: 2, quantum: 1, perChat: 8, maxBuffered: 64}, func(u Update) {
		mu.Lock()
		got = append(got, u.Message.Text)
		if len(got) == 3 {
			close(done)
		}
		mu.Unlock()
	})
	for _, s := range []string{"1", "2", "3"} {
		if err := d.admitNonBlocking(msgUpdate("c1", s)); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	mu.Lock()
	ok := got[0] == "1" && got[1] == "2" && got[2] == "3"
	mu.Unlock()
	if !ok {
		t.Fatalf("order = %v", got)
	}
	d.stop()
	d.wait()
}

func TestDispatcherNonBlockingReturnsQueueFull(t *testing.T) {
	release := make(chan struct{})
	d, _ := newTestDispatcher(dispatchConfig{workers: 1, quantum: 1, perChat: 1, maxBuffered: 10}, func(Update) { <-release })
	_ = d.admitNonBlocking(msgUpdate("c1", "a")) // taken by worker (quantum 1)
	// Fill the per-chat queue to its cap, then expect ErrQueueFull.
	_ = d.admitNonBlocking(msgUpdate("c1", "b"))
	deadline := time.After(time.Second)
	for {
		err := d.admitNonBlocking(msgUpdate("c1", "c"))
		if err == ErrQueueFull {
			break
		}
		if err != nil {
			t.Fatalf("err = %v", err)
		}
		select {
		case <-deadline:
			t.Fatal("queue never filled")
		case <-time.After(time.Millisecond):
		}
	}
	close(release)
	d.stop()
	d.wait()
}

func TestDispatcherBlockingAdmissionAbortsOnContext(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var startOnce sync.Once
	d, _ := newTestDispatcher(dispatchConfig{workers: 1, quantum: 1, perChat: 1, maxBuffered: 10}, func(Update) { startOnce.Do(func() { close(started) }); <-release })
	_ = d.admitNonBlocking(msgUpdate("c1", "a"))
	<-started
	_ = d.admitNonBlocking(msgUpdate("c1", "b"))
	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()
	if err := d.admitBlocking(ctx, msgUpdate("c1", "c")); err == nil {
		t.Fatal("want context error")
	}
	close(release)
	d.stop()
	d.wait()
}

func TestDispatcherStopUnblocksAdmission(t *testing.T) {
	release := make(chan struct{})
	started := make(chan struct{})
	var startOnce sync.Once
	d, _ := newTestDispatcher(dispatchConfig{workers: 1, quantum: 1, perChat: 1, maxBuffered: 10}, func(Update) { startOnce.Do(func() { close(started) }); <-release })
	_ = d.admitNonBlocking(msgUpdate("c1", "a"))
	<-started
	_ = d.admitNonBlocking(msgUpdate("c1", "b"))
	errCh := make(chan error, 1)
	go func() { errCh <- d.admitBlocking(context.Background(), msgUpdate("c1", "c")) }()
	time.Sleep(20 * time.Millisecond)
	d.stop()
	select {
	case err := <-errCh:
		if err != ErrStopped {
			t.Fatalf("err = %v, want ErrStopped", err)
		}
	case <-time.After(time.Second):
		t.Fatal("admission did not unblock")
	}
	close(release)
	d.wait()
}

func TestDispatcherQuantumPreventsMonopolization(t *testing.T) {
	var mu sync.Mutex
	var order []string
	release := make(chan struct{})
	first := make(chan struct{})
	var once sync.Once
	d, _ := newTestDispatcher(dispatchConfig{workers: 1, quantum: 1, perChat: 8, maxBuffered: 64}, func(u Update) {
		mu.Lock()
		order = append(order, u.Message.Chat.ID)
		mu.Unlock()
		once.Do(func() { close(first) })
		<-release
	})
	// Flood chat A, then admit chat B while A is still being drained.
	for i := 0; i < 3; i++ {
		_ = d.admitNonBlocking(msgUpdate("A", "x"))
	}
	<-first
	_ = d.admitNonBlocking(msgUpdate("B", "y"))
	close(release)
	deadline := time.After(2 * time.Second)
	for {
		mu.Lock()
		n := len(order)
		var sawB bool
		for _, id := range order {
			if id == "B" {
				sawB = true
			}
		}
		mu.Unlock()
		if sawB && n >= 4 {
			break
		}
		select {
		case <-deadline:
			t.Fatal("chat B starved behind chat A")
		case <-time.After(5 * time.Millisecond):
		}
	}
	d.stop()
	d.wait()
}

func TestDispatcherFanoutDropsWithSingleError(t *testing.T) {
	var drops int32
	ch := make(chan Update, 1)
	var on atomic.Bool
	on.Store(true)
	d := newDispatcher(dispatchConfig{workers: 1, quantum: 10, perChat: 16, maxBuffered: 64}, ch, &on, func(err error) {
		if err == ErrUpdatesDropped {
			atomic.AddInt32(&drops, 1)
		}
	}, func(Update) {})
	for i := 0; i < 5; i++ {
		_ = d.admitNonBlocking(msgUpdate("c", "x"))
	}
	deadline := time.After(2 * time.Second)
	for atomic.LoadInt32(&drops) == 0 {
		select {
		case <-deadline:
			t.Fatal("no drop error")
		case <-time.After(5 * time.Millisecond):
		}
	}
	time.Sleep(50 * time.Millisecond)
	if n := atomic.LoadInt32(&drops); n != 1 {
		t.Fatalf("drop errors = %d, want 1 per episode", n)
	}
	d.stop()
	d.wait()
}
