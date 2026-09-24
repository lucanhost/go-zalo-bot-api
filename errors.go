package zalobot

import (
	"errors"
	"fmt"

	"github.com/lucanhost/go-zalo-bot-api/internal/api"
)

// APIError reports an error returned by the Zalo Bot API. Code is the API
// error code and HTTPStatus is the HTTP status of the response.
type APIError = api.Error

// TransportError reports a failure to reach the Zalo Bot API. The URL is
// redacted so the bot token never appears in error messages.
type TransportError = api.TransportError

// DecodeError reports a response that could not be decoded.
type DecodeError = api.DecodeError

// ValidationError reports an argument rejected by the SDK before any request
// was made.
type ValidationError struct {
	Field  string
	Reason string
}

func (e *ValidationError) Error() string {
	return fmt.Sprintf("zalobot: invalid %s: %s", e.Field, e.Reason)
}

// WebhookActiveError is returned by Start when a webhook is already registered
// for the bot. Zalo disables getUpdates while a webhook is set, so polling
// cannot start until the webhook is deleted.
type WebhookActiveError struct{ URL string }

func (e *WebhookActiveError) Error() string {
	return fmt.Sprintf("zalobot: a webhook is active (%s); call DeleteWebhook or set WithAutoDeleteWebhook(true)", e.URL)
}

var (
	// ErrNoToken is returned by New when the bot token is empty.
	ErrNoToken = errors.New("zalobot: bot token is empty")
	// ErrStopped is returned when an operation is attempted on a stopped bot.
	ErrStopped = errors.New("zalobot: bot is stopped")
	// ErrAlreadyPolling is returned by Start when polling is already running.
	ErrAlreadyPolling = errors.New("zalobot: bot is already polling")
	// ErrQueueFull is returned when the update queue cannot accept an update.
	ErrQueueFull = errors.New("zalobot: update queue is full")
	// ErrSubstringNotFound is returned by StyleRange/StyleRangeN when the
	// substring does not occur.
	ErrSubstringNotFound = errors.New("zalobot: substring not found")
	// ErrUpdatesDropped is reported through OnError when the Updates channel
	// buffer overflows. It never affects handler delivery.
	ErrUpdatesDropped = errors.New("zalobot: updates channel buffer full; update dropped for Updates() consumers")
)

func apiCode(err error) (int, bool) {
	var ae *APIError
	if errors.As(err, &ae) {
		return ae.Code, true
	}
	return 0, false
}

func httpStatus(err error) (int, bool) {
	var ae *APIError
	if errors.As(err, &ae) && ae.HTTPStatus != 0 {
		return ae.HTTPStatus, true
	}
	var de *DecodeError
	if errors.As(err, &de) && de.HTTPStatus != 0 {
		return de.HTTPStatus, true
	}
	return 0, false
}

func matchesStatus(err error, status int) bool {
	if c, ok := apiCode(err); ok && c == status {
		return true
	}
	if s, ok := httpStatus(err); ok && s == status {
		return true
	}
	return false
}

// IsUnauthorized reports whether err is a 401, by API code or HTTP status.
func IsUnauthorized(err error) bool { return matchesStatus(err, 401) }

// IsRateLimited reports whether err is a 429, by API code or HTTP status.
func IsRateLimited(err error) bool { return matchesStatus(err, 429) }

// IsPollingTimeout reports whether err is a 408, which for long polling means
// an idle poll rather than a failure.
func IsPollingTimeout(err error) bool { return matchesStatus(err, 408) }

// IsWebhookQuotaExceeded reports whether err is a 426, the testWebhook daily
// quota being exceeded.
func IsWebhookQuotaExceeded(err error) bool { return matchesStatus(err, 426) }
