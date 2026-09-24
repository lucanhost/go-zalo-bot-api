package zalobot

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
)

func fakeBot(t *testing.T, handler func(method string, body map[string]any) string) *Bot {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body map[string]any
		_ = json.NewDecoder(r.Body).Decode(&body)
		method := r.URL.Path[len("/botTOKEN/"):]
		_, _ = w.Write([]byte(handler(method, body)))
	}))
	t.Cleanup(srv.Close)
	b, err := New("TOKEN", WithBaseURL(srv.URL), WithHTTPClient(srv.Client()))
	if err != nil {
		t.Fatal(err)
	}
	return b
}

func TestSendMessageEncodesOptions(t *testing.T) {
	var seen map[string]any
	b := fakeBot(t, func(method string, body map[string]any) string {
		if method != "sendMessage" {
			t.Errorf("method = %s", method)
		}
		seen = body
		return `{"ok":true,"result":{"message_id":"m1","date":5}}`
	})
	got, err := b.SendMessage(context.Background(), "chat", "hi", &SendMessageOptions{
		TextStyles: []TextStyle{{Start: 0, Len: 2, Styles: []StyleCode{StyleBold}}},
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.MessageID != "m1" || got.Date != 5 {
		t.Fatalf("got = %+v", got)
	}
	if seen["chat_id"] != "chat" || seen["text"] != "hi" {
		t.Fatalf("body = %v", seen)
	}
	styles, ok := seen["text_styles"].([]any)
	if !ok || len(styles) != 1 {
		t.Fatalf("text_styles = %v", seen["text_styles"])
	}
}

func TestSendMessageRejectsParseModeWithTextStyles(t *testing.T) {
	b := fakeBot(t, func(string, map[string]any) string { return `{"ok":true,"result":{}}` })
	_, err := b.SendMessage(context.Background(), "c", "hi", &SendMessageOptions{
		ParseMode:  ParseModeMarkdown,
		TextStyles: []TextStyle{{Start: 0, Len: 1}},
	})
	var ve *ValidationError
	if !errors.As(err, &ve) {
		t.Fatalf("err = %v, want *ValidationError", err)
	}
}

func TestSendMessageLengthCheckSkippedWithParseMode(t *testing.T) {
	b := fakeBot(t, func(string, map[string]any) string { return `{"ok":true,"result":{}}` })
	long := make([]byte, 0, 3000)
	for i := 0; i < 3000; i++ {
		long = append(long, 'x')
	}
	if _, err := b.SendMessage(context.Background(), "c", string(long), &SendMessageOptions{ParseMode: ParseModeHTML}); err != nil {
		t.Fatalf("parse_mode should skip length check: %v", err)
	}
	if _, err := b.SendMessage(context.Background(), "c", string(long), nil); err == nil {
		t.Fatal("want length validation error")
	}
}

func TestSendVoiceRequiresAAC(t *testing.T) {
	b := fakeBot(t, func(string, map[string]any) string { return `{"ok":true,"result":{}}` })
	if _, err := b.SendVoice(context.Background(), "c", "https://x/y.mp3"); err == nil {
		t.Fatal("want .aac validation error")
	}
	if _, err := b.SendVoice(context.Background(), "c", "https://x/y.aac"); err != nil {
		t.Fatalf("aac should pass: %v", err)
	}
}

func TestSendChatActionIgnoresMissingResult(t *testing.T) {
	b := fakeBot(t, func(string, map[string]any) string { return `{"ok":true}` })
	if err := b.SendChatAction(context.Background(), "c", ChatActionTyping); err != nil {
		t.Fatal(err)
	}
}
