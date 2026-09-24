package zalobot

import (
	"context"
	"crypto/subtle"
	"errors"
	"io"
	"net/http"
	"strings"
)

const maxWebhookBody = 1 << 20

// WebhookInfo describes the webhook configuration returned by the webhook
// methods.
type WebhookInfo struct {
	URL                  string             `json:"url"`
	UpdatedAt            int64              `json:"updated_at"`
	HasCustomCertificate bool               `json:"has_custom_certificate"`
	Verification         *WebhookTestResult `json:"verification,omitempty"`
}

// WebhookTestResult is the outcome of a webhook verification attempt.
type WebhookTestResult struct {
	OK      bool   `json:"ok"`
	URL     string `json:"url"`
	Outcome string `json:"outcome"`
	Hint    string `json:"hint"`
}

// VerifyWebhookSecret reports whether got matches want in constant time. It
// returns false when want is empty, so an unset secret never authenticates a
// request.
func VerifyWebhookSecret(got, want string) bool {
	if want == "" {
		return false
	}
	return subtle.ConstantTimeCompare([]byte(got), []byte(want)) == 1
}

// WebhookHandler validates the secret length in bytes (8..256); ASCII secrets
// behave as character counts, while non-ASCII secrets may count differently.
func (b *Bot) WebhookHandler(secret string) (http.Handler, error) {
	if n := len(secret); n < 8 || n > 256 {
		return nil, &ValidationError{Field: "secret", Reason: "must be 8..256 characters"}
	}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			w.Header().Set("Allow", http.MethodPost)
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		if !VerifyWebhookSecret(r.Header.Get("X-Bot-Api-Secret-Token"), secret) {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		r.Body = http.MaxBytesReader(w, r.Body, maxWebhookBody)
		raw, err := io.ReadAll(r.Body)
		if err != nil {
			http.Error(w, "payload too large", http.StatusRequestEntityTooLarge)
			return
		}
		if err := b.ProcessUpdate(raw); err != nil {
			if errors.Is(err, ErrQueueFull) || errors.Is(err, ErrStopped) {
				http.Error(w, "unavailable", http.StatusServiceUnavailable)
				return
			}
			http.Error(w, "bad request", http.StatusBadRequest)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}), nil
}

// SetWebhook validates the secret length in bytes (8..256); ASCII secrets
// behave as character counts, while non-ASCII secrets may count differently.
func (b *Bot) SetWebhook(ctx context.Context, rawURL, secret string) (*WebhookInfo, error) {
	if !strings.HasPrefix(rawURL, "https://") {
		return nil, &ValidationError{Field: "url", Reason: "must be an https URL"}
	}
	if n := len(secret); n < 8 || n > 256 {
		return nil, &ValidationError{Field: "secret", Reason: "must be 8..256 characters"}
	}
	var info WebhookInfo
	if err := b.call(ctx, "setWebhook", map[string]any{"url": rawURL, "secret_token": secret}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// DeleteWebhook removes the bot's webhook so that getUpdates can be used.
func (b *Bot) DeleteWebhook(ctx context.Context) (*WebhookInfo, error) {
	var info WebhookInfo
	if err := b.call(ctx, "deleteWebhook", map[string]any{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// GetWebhookInfo returns the bot's current webhook configuration. An empty URL
// means no webhook is registered.
func (b *Bot) GetWebhookInfo(ctx context.Context) (*WebhookInfo, error) {
	var info WebhookInfo
	if err := b.call(ctx, "getWebhookInfo", map[string]any{}, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

// TestWebhook asks Zalo to call the registered webhook and reports whether it
// responded successfully. It is rate limited per bot per day (error 426).
func (b *Bot) TestWebhook(ctx context.Context) (*WebhookTestResult, error) {
	var res WebhookTestResult
	if err := b.call(ctx, "testWebhook", map[string]any{}, &res); err != nil {
		return nil, err
	}
	return &res, nil
}
