package api

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestCallSuccessDecodesEnvelope(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost || r.URL.Path != "/botTOKEN/sendMessage" {
			t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("content-type = %q", ct)
		}
		var got map[string]any
		_ = json.NewDecoder(r.Body).Decode(&got)
		if got["chat_id"] != "abc" {
			t.Errorf("chat_id = %v", got["chat_id"])
		}
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":"m1","date":123}}`))
	}))
	defer srv.Close()

	c := NewClient("TOKEN", srv.URL, srv.Client())
	raw, err := c.Call(context.Background(), "sendMessage", map[string]any{"chat_id": "abc", "text": "hi"})
	if err != nil {
		t.Fatalf("Call: %v", err)
	}
	if string(raw) != `{"message_id":"m1","date":123}` {
		t.Fatalf("raw = %s", raw)
	}
}

func TestCallAPIErrorMapsCodeAndStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"ok":false,"error_code":401,"description":"Unauthorized"}`))
	}))
	defer srv.Close()

	c := NewClient("TOKEN", srv.URL, srv.Client())
	_, err := c.Call(context.Background(), "getMe", map[string]any{})
	var ae *Error
	if !errors.As(err, &ae) {
		t.Fatalf("err = %v (%T), want *Error", err, err)
	}
	if ae.Code != 401 || ae.HTTPStatus != 401 || ae.Description != "Unauthorized" {
		t.Fatalf("Error = %+v", ae)
	}
}

func TestCallAcceptsCamelCaseErrorCode(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"ok":false,"errorCode":426,"description":"quota"}`))
	}))
	defer srv.Close()
	c := NewClient("TOKEN", srv.URL, srv.Client())
	_, err := c.Call(context.Background(), "testWebhook", map[string]any{})
	var ae *Error
	if !errors.As(err, &ae) || ae.Code != 426 {
		t.Fatalf("err = %v", err)
	}
}

func TestCallHTMLBodyIsDecodeErrorWithStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`<html>unauthorized</html>`))
	}))
	defer srv.Close()
	c := NewClient("TOKEN", srv.URL, srv.Client())
	_, err := c.Call(context.Background(), "getMe", map[string]any{})
	var de *DecodeError
	if !errors.As(err, &de) {
		t.Fatalf("err = %v (%T), want *DecodeError", err, err)
	}
	if de.HTTPStatus != 401 {
		t.Fatalf("HTTPStatus = %d", de.HTTPStatus)
	}
}

func TestCallRequestConstructionErrorRedactsToken(t *testing.T) {
	c := NewClient("BAD\nTOKEN", "http://example.com", &http.Client{})
	_, err := c.Call(context.Background(), "getMe", map[string]any{})
	var te *TransportError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v (%T), want *TransportError", err, err)
	}
	if strings.Contains(err.Error(), "BAD") || strings.Contains(err.Error(), "BAD\nTOKEN") {
		t.Fatalf("token leaked: %v", err)
	}
}

func TestTransportErrorRedactsTokenAndKeepsCause(t *testing.T) {
	c := NewClient("SECRET", "http://127.0.0.1:1", &http.Client{})
	_, err := c.Call(context.Background(), "getMe", map[string]any{})
	var te *TransportError
	if !errors.As(err, &te) {
		t.Fatalf("err = %v (%T), want *TransportError", err, err)
	}
	if strings.Contains(err.Error(), "SECRET") {
		t.Fatalf("token leaked: %v", err)
	}
	if !strings.Contains(te.RedactedURL, "/bot<TOKEN>/getMe") {
		t.Fatalf("redacted url = %q", te.RedactedURL)
	}
	if te.Unwrap() == nil {
		t.Fatal("Unwrap() = nil, want underlying cause")
	}
}
