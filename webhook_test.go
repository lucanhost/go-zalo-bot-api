package zalobot

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestVerifyWebhookSecret(t *testing.T) {
	if !VerifyWebhookSecret("abc", "abc") {
		t.Fatal("equal secrets should match")
	}
	if VerifyWebhookSecret("abc", "abd") {
		t.Fatal("different secrets should not match")
	}
	if VerifyWebhookSecret("", "") {
		t.Fatal("empty want must be false")
	}
}

func TestWebhookHandlerValidatesSecretLength(t *testing.T) {
	b, _ := New("TOKEN")
	if _, err := b.WebhookHandler("short"); err == nil {
		t.Fatal("want validation error")
	}
}

func TestWebhookHandlerOrderAndAsyncDispatch(t *testing.T) {
	b, _ := New("TOKEN")
	h, err := b.WebhookHandler("secret-123")
	if err != nil {
		t.Fatal(err)
	}
	handled := make(chan struct{}, 1)
	b.OnMessage(func(context.Context, *Message) { handled <- struct{}{} })

	// 405
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusMethodNotAllowed || rec.Header().Get("Allow") != http.MethodPost {
		t.Fatalf("405: code=%d allow=%q", rec.Code, rec.Header().Get("Allow"))
	}

	// 403
	rec = httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{}`))
	req.Header.Set("X-Bot-Api-Secret-Token", "wrong")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusForbidden {
		t.Fatalf("403: code=%d", rec.Code)
	}

	// 200 + async dispatch
	rec = httptest.NewRecorder()
	req = httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{"ok":true,"result":{"event_name":"message.text.received","message":{"text":"x"}}}`))
	req.Header.Set("X-Bot-Api-Secret-Token", "secret-123")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("200: code=%d body=%s", rec.Code, rec.Body.String())
	}
	select {
	case <-handled:
	case <-time.After(2 * time.Second):
		t.Fatal("handler did not run")
	}
	b.disp.stop()
	b.disp.wait()
}

func TestWebhookHandlerBadJSONIs400(t *testing.T) {
	b, _ := New("TOKEN")
	h, _ := b.WebhookHandler("secret-123")
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(`{`))
	req.Header.Set("X-Bot-Api-Secret-Token", "secret-123")
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("code = %d", rec.Code)
	}
}

func TestWebhookHandlerFullQueueIs503(t *testing.T) {
	b, _ := New("TOKEN", WithWorkers(1), WithPerChatBuffer(1), WithMaxBuffered(2))
	h, _ := b.WebhookHandler("secret-123")
	release := make(chan struct{})
	b.OnMessage(func(context.Context, *Message) { <-release })
	payload := `{"ok":true,"result":{"event_name":"message.text.received","message":{"chat":{"id":"c"},"text":"x"}}}`
	code := 0
	for i := 0; i < 20; i++ {
		rec := httptest.NewRecorder()
		req := httptest.NewRequest(http.MethodPost, "/", strings.NewReader(payload))
		req.Header.Set("X-Bot-Api-Secret-Token", "secret-123")
		h.ServeHTTP(rec, req)
		if rec.Code == http.StatusServiceUnavailable {
			code = rec.Code
			break
		}
	}
	if code != http.StatusServiceUnavailable {
		t.Fatalf("never returned 503, last code = %d", code)
	}
	close(release)
	b.disp.stop()
	b.disp.wait()
}

func TestSetWebhookValidatesInputs(t *testing.T) {
	b, _ := New("TOKEN")
	if _, err := b.SetWebhook(context.Background(), "http://insecure", "secret-123"); err == nil {
		t.Fatal("want https validation error")
	}
	if _, err := b.SetWebhook(context.Background(), "https://x", "short"); err == nil {
		t.Fatal("want secret length validation error")
	}
}

func TestSetWebhookReturnsVerificationWithoutError(t *testing.T) {
	b := fakeBot(t, func(method string, body map[string]any) string {
		if method != "setWebhook" {
			t.Errorf("method = %s", method)
		}
		return `{"ok":true,"result":{"url":"https://x","updated_at":1,"verification":{"ok":false,"url":"https://x","outcome":"webhook.http.403","hint":"blocked"}}}`
	})
	info, err := b.SetWebhook(context.Background(), "https://x", "secret-123")
	if err != nil {
		t.Fatalf("failed verification must not be an error: %v", err)
	}
	if info.Verification == nil || info.Verification.Outcome != "webhook.http.403" {
		t.Fatalf("info = %+v", info)
	}
}
