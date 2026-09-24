package zalobot

import (
	"context"
	"errors"
	"net/http"
	"sync/atomic"
	"testing"
	"time"
)

func TestStartRejectsBadClientTimeout(t *testing.T) {
	b, _ := New("TOKEN", WithPolling(PollingOptions{Timeout: 30 * time.Second}),
		WithHTTPClient(&http.Client{Timeout: 5 * time.Second}))
	if err := b.Start(context.Background()); err == nil {
		t.Fatal("want validation error for client timeout")
	}
}

func TestStartErrorsWhenWebhookActive(t *testing.T) {
	b := fakeBot(t, func(method string, _ map[string]any) string {
		return `{"ok":true,"result":{"url":"https://hook","updated_at":1}}`
	})
	err := b.Start(context.Background())
	var wae *WebhookActiveError
	if !errors.As(err, &wae) {
		t.Fatalf("err = %v, want *WebhookActiveError", err)
	}
}

func TestPollingTreats408AsEmptyPoll(t *testing.T) {
	var calls atomic.Int32
	b := fakeBot(t, func(method string, _ map[string]any) string {
		if method == "getWebhookInfo" {
			return `{"ok":true,"result":{"url":""}}`
		}
		calls.Add(1)
		return `{"ok":false,"error_code":408,"description":"timeout"}`
	})
	var onErrorCalls atomic.Int32
	b.OnError(func(error) { onErrorCalls.Add(1) })
	ctx, cancel := context.WithCancel(context.Background())
	if err := b.Start(ctx); err != nil {
		cancel()
		t.Fatal(err)
	}
	deadline := time.After(3 * time.Second)
	for calls.Load() < 2 {
		select {
		case <-deadline:
			cancel()
			<-b.Done()
			t.Fatal("poll loop did not continue after 408")
		case <-time.After(10 * time.Millisecond):
		}
	}
	cancel()
	<-b.Done()
	if onErrorCalls.Load() != 0 {
		t.Fatalf("OnError called %d times for 408", onErrorCalls.Load())
	}
}

func TestPolling401IsFatalAndStops(t *testing.T) {
	b := fakeBot(t, func(method string, _ map[string]any) string {
		if method == "getWebhookInfo" {
			return `{"ok":true,"result":{"url":""}}`
		}
		return `{"ok":false,"error_code":401,"description":"Unauthorized"}`
	})
	if err := b.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("Done() not closed after fatal 401")
	}
	if !IsUnauthorized(b.Err()) {
		t.Fatalf("Err() = %v, want 401", b.Err())
	}
}

func TestGetUpdatesTolerantShapes(t *testing.T) {
	for _, payload := range []string{
		`{"ok":true,"result":{"event_name":"message.text.received"}}`,
		`{"ok":true,"result":[{"event_name":"message.text.received"}]}`,
		`{"ok":true,"result":null}`,
		`{"ok":true}`,
	} {
		b := fakeBot(t, func(string, map[string]any) string { return payload })
		ups, err := b.GetUpdates(context.Background(), &GetUpdatesOptions{Timeout: time.Second})
		if err != nil {
			t.Fatalf("%s: %v", payload, err)
		}
		if len(ups) > 1 {
			t.Fatalf("%s: len = %d", payload, len(ups))
		}
	}
}

func TestGetUpdatesSendsTimeoutAsString(t *testing.T) {
	var seen map[string]any
	b := fakeBot(t, func(_ string, body map[string]any) string { seen = body; return `{"ok":true}` })
	_, _ = b.GetUpdates(context.Background(), &GetUpdatesOptions{Timeout: 30 * time.Second})
	if v, ok := seen["timeout"].(string); !ok || v != "30" {
		t.Fatalf("timeout = %#v, want string \"30\"", seen["timeout"])
	}
}
