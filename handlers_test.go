package zalobot

import (
	"context"
	"regexp"
	"sync"
	"testing"
	"time"
)

func TestRoutingOrderAndAllMatching(t *testing.T) {
	b, _ := New("TOKEN")
	var mu sync.Mutex
	var calls []string
	b.OnEvent(EventTextReceived, func(context.Context, Update) { mu.Lock(); calls = append(calls, "event"); mu.Unlock() })
	b.OnMessage(func(context.Context, *Message) { mu.Lock(); calls = append(calls, "message"); mu.Unlock() })
	b.OnText(regexp.MustCompile(`^/hi (\w+)$`), func(_ context.Context, _ *Message, m []string) {
		mu.Lock()
		calls = append(calls, "text:"+m[1])
		mu.Unlock()
	})
	b.OnCommand("hi", func(_ context.Context, _ *Message, args []string) {
		mu.Lock()
		calls = append(calls, "command:"+args[0])
		mu.Unlock()
	})
	u := msgUpdate("c", "/hi bob")
	b.handlers.run(b.handlerCtx, u)
	want := []string{"event", "message", "text:bob", "command:bob"}
	if len(calls) != len(want) {
		t.Fatalf("calls = %v", calls)
	}
	for i := range want {
		if calls[i] != want[i] {
			t.Fatalf("calls = %v, want %v", calls, want)
		}
	}
}

func TestUnsupportedEventSkipsMessageAndText(t *testing.T) {
	b, _ := New("TOKEN")
	var messages, texts int
	b.OnMessage(func(context.Context, *Message) { messages++ })
	b.OnText(regexp.MustCompile(`.`), func(context.Context, *Message, []string) { texts++ })
	var events int
	b.OnEvent(EventUnsupportedReceived, func(context.Context, Update) { events++ })
	b.handlers.run(b.handlerCtx, Update{EventName: EventUnsupportedReceived})
	if messages != 0 || texts != 0 || events != 1 {
		t.Fatalf("messages=%d texts=%d events=%d", messages, texts, events)
	}
}

func TestHandlerPanicIsReportedAndDoesNotStopOthers(t *testing.T) {
	b, _ := New("TOKEN")
	var reported bool
	b.OnError(func(error) { reported = true })
	b.OnMessage(func(context.Context, *Message) { panic("boom") })
	ran := false
	b.OnMessage(func(context.Context, *Message) { ran = true })
	b.handlers.run(b.handlerCtx, msgUpdate("c", "hi"))
	if !reported || !ran {
		t.Fatalf("reported=%v ran=%v", reported, ran)
	}
}

func TestProcessUpdateAdmitsAndUpdatesChannelSeesIt(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(1))
	ch := b.Updates()
	b.OnMessage(func(context.Context, *Message) {})
	if err := b.ProcessUpdate([]byte(`{"ok":true,"result":{"event_name":"message.text.received","message":{"text":"x"}}}`)); err != nil {
		t.Fatal(err)
	}
	select {
	case <-ch:
	case <-time.After(2 * time.Second):
		t.Fatal("timeout")
	}
	b.disp.stop()
	b.disp.wait()
}
