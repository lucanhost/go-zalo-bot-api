package zalobot

import (
	"context"
	"errors"
	"fmt"
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
	const chats, updates = 4, 24
	var mu sync.Mutex
	got := make(map[string][]string, chats)
	active := make(map[string]int, chats)
	var completed int
	done := make(chan struct{})
	d, _ := newTestDispatcher(dispatchConfig{workers: 4, quantum: 2, perChat: updates, maxBuffered: chats * updates}, func(u Update) {
		key := u.Message.Chat.ID
		mu.Lock()
		active[key]++
		if active[key] != 1 {
			t.Errorf("concurrent processing for chat %s", key)
		}
		got[key] = append(got[key], u.Message.Text)
		active[key]--
		completed++
		if completed == chats*updates {
			close(done)
		}
		mu.Unlock()
	})
	// Admit all work before waiting so every chat has multiple ready/requeue
	// turns while workers contend for the scheduler.
	for i := 0; i < updates; i++ {
		for c := 0; c < chats; c++ {
			if err := d.admitNonBlocking(msgUpdate(fmt.Sprintf("c%d", c), fmt.Sprint(i))); err != nil {
				t.Fatal(err)
			}
		}
	}
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	mu.Lock()
	for c := 0; c < chats; c++ {
		key := fmt.Sprintf("c%d", c)
		if len(got[key]) != updates {
			t.Errorf("chat %s got %d updates, want %d", key, len(got[key]), updates)
			continue
		}
		for i, text := range got[key] {
			if want := fmt.Sprint(i); text != want {
				t.Errorf("chat %s update %d = %q, want %q", key, i, text, want)
				break
			}
		}
	}
	mu.Unlock()
	d.stop()
	d.wait()
}

func TestDispatcherRejectedAdmissionDoesNotCreateQueue(t *testing.T) {
	d, _ := newTestDispatcher(dispatchConfig{workers: 0, quantum: 1, perChat: 1, maxBuffered: 1}, func(Update) {})
	if err := d.admitNonBlocking(msgUpdate("accepted", "a")); err != nil {
		t.Fatal(err)
	}
	if err := d.admitNonBlocking(msgUpdate("rejected", "b")); !errors.Is(err, ErrQueueFull) {
		t.Fatalf("err = %v, want ErrQueueFull", err)
	}
	d.mu.Lock()
	_, exists := d.queues["rejected"]
	d.mu.Unlock()
	if exists {
		t.Fatal("rejected admission created an empty queue")
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
		if errors.Is(err, ErrQueueFull) {
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
		if !errors.Is(err, ErrStopped) {
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
		if errors.Is(err, ErrUpdatesDropped) {
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
